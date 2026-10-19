package gate

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// requestTimeout bounds one request of the fitness and capacity stages: the models are loaded,
// so it covers generation only.
const requestTimeout = 3 * time.Minute

// The fitness stage's request sizes.
const (
	// decodeTokens is the decode measurement's output.
	decodeTokens = 256
	// longPromptTokens is the agent's long prompt, the 8k its TTFT floor names.
	longPromptTokens = 8192
	// imageAnswerTokens is the timed image question's output, the ~100 its floor names.
	imageAnswerTokens = 100
	// embedBatch inputs of embedInputTokens each make the throughput batch, within the embedding
	// model's one 8192-token physical batch.
	embedBatch, embedInputTokens = 16, 500
	// queryTokens is the timed query's length.
	queryTokens = 32
)

// fitness smokes each role the model takes with the probe's request for it, measures its
// single-stream floors, and reads the pool idle. Speeds come from llama.cpp's own timings in
// each answer; a time is the client's, from sending to the answer.
func (r *Runner) fitness(ctx context.Context, rep *Report) []Check {
	c := &client{base: r.Router, hc: r.HTTP}
	var checks []Check
	for _, role := range rep.Roles {
		switch role {
		case RoleAgent:
			checks = append(checks, r.fitAgent(ctx, c)...)
		case RoleVision:
			checks = append(checks, r.fitVision(ctx, c)...)
		case RoleAudio:
			checks = append(checks, r.fitAudio(ctx, c)...)
		case RoleEmbedding:
			checks = append(checks, r.fitEmbedding(ctx, c)...)
		}
	}
	return append(checks, r.poolCheck(ctx, "idle pool free"))
}

// poolCheck reads the pool once and judges its free memory against the floor.
func (r *Runner) poolCheck(ctx context.Context, name string) Check {
	p, err := r.Footprint.Read(ctx)
	if err != nil {
		return broken(name, "", err)
	}
	ch := measure(name, "", float64(p.FreeMiB()), "MiB", AtLeast, r.Floors.Pool.FreeMiB)
	ch.Detail = fmt.Sprintf("%d of %d MiB used", p.UsedMiB, p.TotalMiB)
	return ch
}

func (r *Runner) fitAgent(ctx context.Context, c *client) []Check {
	f := r.Floors.Agent
	var checks []Check

	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	name, err := c.toolCall(rctx, r.Model)
	cancel()
	switch {
	case err != nil:
		checks = append(checks, broken("tool call", RoleAgent, err))
	default:
		checks = append(checks, fact("tool call", RoleAgent, fmt.Sprintf("called %q, want get_weather", name), name == "get_weather"))
	}

	rctx, cancel = context.WithTimeout(ctx, requestTimeout)
	comp, err := c.chat(rctx, r.Model, "Write a 300-word story about a lighthouse keeper who finds a message in a bottle.", decodeTokens)
	cancel()
	if err != nil {
		checks = append(checks, broken("decode", RoleAgent, err))
	} else {
		ch := measure("decode", RoleAgent, comp.Timings.PredictedPerSecond, "tok/s", AtLeast, f.DecodeTokS)
		ch.Values = map[string]float64{"decode_tok_s": comp.Timings.PredictedPerSecond, "predicted_n": float64(comp.Timings.PredictedN)}
		checks = append(checks, ch)
	}

	rctx, cancel = context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	text, err := c.fitText(rctx, r.Model, longPromptTokens)
	if err == nil {
		comp, err = c.stream(rctx, r.Model, text+"\n\nIn one sentence, what is the passage above about?", 16)
	}
	if err != nil {
		return append(checks, broken("prefill 8k", RoleAgent, err), broken("ttft 8k", RoleAgent, err))
	}
	values := map[string]float64{"prompt_n": float64(comp.Timings.PromptN), "cache_n": float64(comp.Timings.CacheN)}
	pre := measure("prefill 8k", RoleAgent, comp.Timings.PromptPerSecond, "tok/s", AtLeast, f.PrefillTokS)
	pre.Values = values
	ttft := measure("ttft 8k", RoleAgent, comp.TTFT.Seconds(), "s", AtMost, f.TTFT8kS)
	ttft.Values = values
	return append(checks, pre, ttft)
}

func (r *Runner) fitVision(ctx context.Context, c *client) []Check {
	var checks []Check
	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	comp, err := c.chat(rctx, r.Model, []map[string]any{textPart("Describe this image in one sentence."), imagePart(r.Media.PNG)}, 512)
	cancel()
	if err != nil {
		checks = append(checks, broken("image", RoleVision, err))
	} else {
		// The probe's image is a red circle, a blue square, and a green triangle; each shape and
		// colour must be named, in any phrasing.
		var missing []string
		for _, w := range []string{"circle", "square", "triangle", "red", "blue", "green"} {
			if !strings.Contains(strings.ToLower(comp.Text), w) {
				missing = append(missing, w)
			}
		}
		detail := fmt.Sprintf("%q", comp.Text)
		if len(missing) > 0 {
			detail += fmt.Sprintf(", doesn't name %s", strings.Join(missing, ", "))
		}
		checks = append(checks, fact("image", RoleVision, detail, len(missing) == 0))
	}

	// The timed question is the image, then the question, as a client asks it, with the prompt
	// cache off so the smoke's image tokens aren't reused.
	rctx, cancel = context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	comp, err = c.chatUncached(rctx, r.Model, []map[string]any{
		imagePart(r.Media.PNG), textPart("What shapes are in this image, and what colour is each?"),
	}, imageAnswerTokens)
	if err != nil {
		return append(checks, broken("image answer", RoleVision, err))
	}
	ch := measure("image answer", RoleVision, comp.Wall.Seconds(), "s", AtMost, r.Floors.Vision.ImageAnswerS)
	ch.Values = map[string]float64{
		"prompt_n": float64(comp.Timings.PromptN), "predicted_n": float64(comp.Timings.PredictedN),
		"decode_tok_s": comp.Timings.PredictedPerSecond, "prompt_tok_s": comp.Timings.PromptPerSecond,
	}
	return append(checks, ch)
}

func (r *Runner) fitAudio(ctx context.Context, c *client) []Check {
	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	comp, err := c.complete(rctx, transcriptionBody(r.Model, r.Media.WAV, ""))
	if err != nil {
		return []Check{broken("transcription", RoleAudio, err), broken("rtfx", RoleAudio, err)}
	}
	// The clip says "The access code is seven four two nine."; the code may come back as digits
	// or words, spaced or not.
	got := alphanumeric(comp.Text)
	smoke := fact("transcription", RoleAudio, fmt.Sprintf("%q", comp.Text),
		strings.Contains(got, "7429") || strings.Contains(got, "sevenfourtwonine"))
	rtfx := measure("rtfx", RoleAudio, r.Media.AudioSeconds/comp.Wall.Seconds(), "x", AtLeast, r.Floors.Audio.RTFx)
	rtfx.Values = map[string]float64{
		"audio_s": r.Media.AudioSeconds, "wall_s": comp.Wall.Seconds(),
		"prompt_n": float64(comp.Timings.PromptN), "predicted_n": float64(comp.Timings.PredictedN),
		"decode_tok_s": comp.Timings.PredictedPerSecond,
	}
	return []Check{smoke, rtfx}
}

// ASRPrompt is the card's ASR prompt (https://huggingface.co/google/gemma-4-E4B-it, "6. Audio"),
// the language filled in, without its formatting bullets ("Only output the transcription, with
// no newlines"; "When transcribing numbers, write the digits"). On a clip of a word or two,
// gemma-4-E4B reads the bullets back as the transcript: on AMI's 40 shortest tier clips the full
// prompt scored 165% WER and the sentence alone 47%, and on 40 LibriSpeech test.other utterances
// both scored 6.3%. The normalizer writes numbers as digits whichever way they come, so the
// bullets don't bear on the score. This is its one source: fitness and capacity send it, and
// UV.ASR passes it to the quality stage's ASR task, asr_quality.py, as its --prompt, so every
// stage that times or scores a transcription asks for one the same way.
const ASRPrompt = "Transcribe the following speech segment in English into English text."

// transcriptionBody is a timed transcription of wav by model, as fitness and capacity send it
// and as the quality stage's ASR task does: the card's prompt first and the clip after it (the
// card's modality order for audio), at temperature 0, with reasoning off (the template's
// enable_thinking; templates without the switch ignore it) and the prompt cache off (llama.cpp's
// cache_prompt), so an earlier send of the clip isn't reused. Reasoning off times the
// transcription, not a reasoning model's thinking first: at the card's temperature 1,
// gemma-4-E4B sometimes reasoned 145-169 tokens before a clip-first "Transcribe this audio
// clip.", which put fitness's RTFx at 1.2-1.5 against its floor of 5. A non-empty after follows
// the clip as a text part of its own: capacity's request to repeat its nonce.
func transcriptionBody(model string, wav []byte, after string) map[string]any {
	content := []map[string]any{textPart(ASRPrompt), audioPart(wav)}
	if after != "" {
		content = append(content, textPart(after))
	}
	body := chatBody(model, content, 512, false)
	body["temperature"] = 0
	body["cache_prompt"] = false
	body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	return body
}

func (r *Runner) fitEmbedding(ctx context.Context, c *client) []Check {
	var checks []Check
	rctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	e, err := c.embed(rctx, r.Model, []string{"The access code is seven four two nine."})
	if err != nil {
		checks = append(checks, broken("embedding", RoleEmbedding, err))
	} else {
		dims := 0
		if len(e.Vectors) == 1 {
			dims = len(e.Vectors[0])
		}
		want := r.model().Dims
		checks = append(checks, fact("embedding", RoleEmbedding, fmt.Sprintf("%d dimensions, want %d", dims, want), dims == want))
	}

	inputs := make([]string, embedBatch)
	for i := range inputs {
		if inputs[i], err = c.fitText(rctx, r.Model, embedInputTokens); err != nil {
			break
		}
	}
	if err == nil {
		e, err = c.embed(rctx, r.Model, inputs)
	}
	if err != nil {
		checks = append(checks, broken("throughput", RoleEmbedding, err))
	} else {
		tokens := e.PromptTokens
		if tokens == 0 {
			tokens = embedBatch * embedInputTokens
		}
		ch := measure("throughput", RoleEmbedding, float64(tokens)/e.Wall.Seconds(), "tok/s", AtLeast, r.Floors.Embedding.ThroughputTokS)
		ch.Values = map[string]float64{"tokens": float64(tokens), "inputs": embedBatch, "wall_s": e.Wall.Seconds()}
		checks = append(checks, ch)
	}

	q, err := c.fitText(rctx, r.Model, queryTokens)
	if err == nil {
		e, err = c.embed(rctx, r.Model, []string{q})
	}
	if err != nil {
		return append(checks, broken("query 32", RoleEmbedding, err))
	}
	return append(checks, measure("query 32", RoleEmbedding, float64(e.Wall.Microseconds())/1000, "ms", AtMost, r.Floors.Embedding.Query32MS))
}

// alphanumeric lowercases s and keeps only its letters and digits.
func alphanumeric(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
