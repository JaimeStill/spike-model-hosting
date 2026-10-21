package gate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/concurrent"
)

// capacityInputs are the embedding streams' texts: distinct sentences, so each input's vector is
// told from every other's.
var capacityInputs = []string{
	"The access code is seven four two nine.",
	"A red circle, a blue square, and a green triangle.",
	"The router serves four models side by side.",
	"Unified memory holds the weights and the KV cache together.",
	"Each request carries a nonce unique to the run.",
	"Rain is expected in the mountains by Thursday evening.",
	"She tuned the violin before the concert began.",
	"Compile the program, then run its tests.",
}

// capacity puts N requests in flight on the model for each role it takes, N its slots from its
// profile entry (4 for set A's), while the footprint reads the pool, and judges each of the N
// against the role's floor at N (Floors). The agent's, vision's and audio role's N go through the
// concurrent package's run, each answer carrying its nonce: the agent's in the agentic shape (an
// 8192-token prompt, 512 out, agenticBody) and judged by its decode, the audio role's a timed
// transcription judged by its RTFx, the vision role's an image to describe. A chat role's run
// carries an embedding stream to the set's embedding model beside it; an embedding model's run is
// one embedding stream a slot. The vision and embedding roles then send N of fitness's timed
// requests at once (atN), its image question and its 32-token query, each judged by fitness's own
// floor. Each role passes when every answer carries its own nonce and no other, every batched
// vector holds the embedding tolerance, no request ran out of context (contextCheck), each request
// meets the floor at N, and the pool's least free memory over the whole of it stays at the floor.
//
// A set run puts each of the set's models through it alone, one after another (perModel).
func (r *Runner) capacity(ctx context.Context, rep *Report) []Check {
	return r.perModel(ctx, rep, (*Runner).capModel)
}

// capModel is capacity for the model, each role it takes, reusing a role's passed record from
// the index when its model's key matches (reusable).
func (r *Runner) capModel(ctx context.Context, rep *Report) []Check {
	c := &client{base: r.Router, hc: r.HTTP}
	var checks []Check
	for _, role := range rep.Roles {
		if reused, ok := r.reusable(rep.Mode, r.modelEntry(StageCapacity, role, "", r.Model)); ok {
			checks = append(checks, reused...)
			continue
		}
		checks = append(checks, r.capRole(ctx, c, role)...)
	}
	return checks
}

// inFlight is the name of the check that records a capacity run that couldn't start.
const inFlight = "N in flight"

func (r *Runner) capRole(ctx context.Context, c *client, role string) []Check {
	return r.capAt(ctx, c, role).checks
}

// capRun is one role's capacity run at the model's slots: its checks, and what the sweep reads
// beside them, the chat stream's answers (rec, nil for the embedding role) and the requests sent
// N at once after the run (loaded).
type capRun struct {
	checks []Check
	rec    *recorder
	loaded []timed
}

// capAt runs capacity for role at the model's slots, from its profile entry (capRole).
func (r *Runner) capAt(ctx context.Context, c *client, role string) capRun {
	slots := r.slotsOf(r.Model)
	if slots < 1 {
		return capRun{checks: []Check{broken(inFlight, role, fmt.Errorf("the profile gives %s no slots", r.Model))}}
	}
	cfg := concurrent.Config{Rounds: 1, RequestTimeout: requestTimeout}
	var rec *recorder
	if role == RoleEmbedding {
		// An embedding stream sends batch after batch; three rounds each keep the N in flight
		// long enough for the footprint to read the pool while they are.
		cfg.Rounds = 3
		_, document := r.embedPrompts(r.Model)
		for i := range slots {
			k := i % len(capacityInputs)
			inputs := slices.Concat(capacityInputs[k:], capacityInputs[:k])
			cfg.Embed = append(cfg.Embed, concurrent.EmbedStream{
				Name: fmt.Sprintf("embedding-%d", i+1), Embedder: embedder{c, r.Model, document}, Inputs: inputs,
			})
		}
	} else {
		rec = &recorder{c: c, model: r.Model, role: role, media: r.Media, asr: r.asrPrompt(r.Model), agentic: role == RoleAgent}
		cfg.Chat = []concurrent.ChatStream{{Name: role, Chatter: rec, Workers: slots}}
		if e := r.Set.Roles.Embedding; e != "" {
			_, document := r.embedPrompts(e)
			cfg.Embed = []concurrent.EmbedStream{{Name: "embedding", Embedder: embedder{c, e, document}, Inputs: capacityInputs}}
		}
	}

	var res concurrent.Report
	var runErr error
	var loaded []timed
	low, readings, readErr := r.during(ctx, func() {
		if res, runErr = concurrent.Run(ctx, cfg); runErr != nil {
			return
		}
		switch role {
		case RoleVision:
			loaded = atN(ctx, slots, func(ctx context.Context) (time.Duration, error) {
				comp, err := imageQuestion(ctx, c, r.Model, r.Media.PNG)
				return comp.Wall, err
			})
		case RoleEmbedding:
			loaded = r.queriesAtN(ctx, c, slots)
		}
	})
	if runErr != nil {
		return capRun{checks: []Check{broken(inFlight, role, runErr)}}
	}

	var checks []Check
	if len(res.Chats) > 0 {
		var failures []string
		for _, f := range res.Failures() {
			if !strings.HasPrefix(f, "embedding") {
				failures = append(failures, f)
			}
		}
		detail := fmt.Sprintf("%d answers in flight at once, each carrying its own nonce", len(res.Chats))
		if len(failures) > 0 {
			detail = strings.Join(failures, "; ")
		}
		checks = append(checks, fact("no crossed nonce", role, detail, len(failures) == 0))
	}
	if len(res.Embeds) > 0 {
		failed, least := 0, math.Inf(1)
		for _, e := range res.Embeds {
			if !e.Passed() {
				failed++
			}
			if e.Err == nil {
				least = min(least, e.Cosine)
			}
		}
		checks = append(checks, fact("embedding tolerance", role,
			fmt.Sprintf("%d of %d batched vectors within 1-%g of their lone embedding, least cosine %.6f",
				len(res.Embeds)-failed, len(res.Embeds), concurrent.EmbedTolerance, least), failed == 0))
	}
	checks = append(checks, contextCheck(role, slots, res, loaded))

	switch role {
	case RoleAgent:
		checks = append(checks, rec.floor("decode at N", "tok/s", slots, r.Floors.Agent.StreamDecodeTokS,
			func(c completion) float64 { return c.Timings.PredictedPerSecond }))
	case RoleAudio:
		checks = append(checks, rec.floor("rtfx at N", "x", slots, r.Floors.Audio.StreamRTFx,
			func(c completion) float64 { return r.Media.AudioSeconds / c.Wall.Seconds() }))
	case RoleVision:
		checks = append(checks, slowest("image answer at N", role, "s", 1, r.Floors.Vision.ImageAnswerS, loaded,
			"fitness's timed image question, N at once, the slowest of them"))
	case RoleEmbedding:
		query, _ := r.embedPrompts(r.Model)
		checks = append(checks, slowest("query 32 at N", role, "ms", 1000, r.Floors.Embedding.Query32MS, loaded,
			queryDetail(query, "fitness's timed query, N at once, the slowest of them, each")))
	}

	out := capRun{rec: rec, loaded: loaded}
	if readings == 0 {
		out.checks = append(checks, broken("peak pool free", role, readErr))
		return out
	}
	peak := measure("peak pool free", role, float64(low.FreeMiB()), "MiB", AtLeast, r.Floors.Pool.FreeMiB)
	peak.Values = poolValues(low)
	peak.Values["slots"] = float64(slots)
	peak.Detail = fmt.Sprintf("%d of %d MiB used at the peak of %d readings, %d in flight%s", low.UsedMiB, low.TotalMiB, readings, slots, ramDetail(low))
	out.checks = append(checks, peak)
	return out
}

// timed is one of atN's requests: its wall time, or why it failed.
type timed struct {
	wall time.Duration
	err  error
}

// atN sends n requests at once, each send bounded by requestTimeout, and returns each one's wall
// time or error, in the order they were started.
func atN(ctx context.Context, n int, send func(context.Context) (time.Duration, error)) []timed {
	out := make([]timed, n)
	var wg sync.WaitGroup
	for i := range out {
		wg.Go(func() {
			rctx, cancel := context.WithTimeout(ctx, requestTimeout)
			defer cancel()
			out[i].wall, out[i].err = send(rctx)
		})
	}
	wg.Wait()
	return out
}

// queriesAtN cuts n distinct 32-token queries for the model, each after its query prompt
// (embedPrompts), then embeds them at once, one a request, timing each embedding alone, as
// fitness times its one query.
func (r *Runner) queriesAtN(ctx context.Context, c *client, n int) []timed {
	query, _ := r.embedPrompts(r.Model)
	queries := make([]string, n)
	for i := range queries {
		q, err := c.fitText(ctx, r.Model, queryTokens)
		if err != nil {
			return []timed{{err: fmt.Errorf("cutting query %d: %w", i+1, err)}}
		}
		queries[i] = query + q
	}
	var next atomic.Int64
	return atN(ctx, n, func(ctx context.Context) (time.Duration, error) {
		e, err := c.embed(ctx, r.Model, []string{queries[next.Add(1)-1]})
		return e.Wall, err
	})
}

// slowest judges the slowest of requests, each wall time in unit (scale a second), against floor
// at most; every request must have answered.
func slowest(name, role, unit string, scale, floor float64, requests []timed, detail string) Check {
	var errs []string
	values := map[string]float64{"slots": float64(len(requests))}
	most := 0.0
	for i, q := range requests {
		if q.err != nil {
			errs = append(errs, fmt.Sprintf("request %d: %v", i+1, q.err))
			continue
		}
		v := q.wall.Seconds() * scale
		values[fmt.Sprintf("request%d", i+1)] = v
		most = max(most, v)
	}
	switch {
	case len(requests) == 0:
		return broken(name, role, errors.New("no request was sent"))
	case len(errs) > 0:
		ch := broken(name, role, fmt.Errorf("%d of the %d requests failed: %s", len(errs), len(requests), listed(errs)))
		ch.Values = values
		return ch
	}
	ch := measure(name, role, most, unit, AtMost, floor)
	ch.Values = values
	ch.Detail = detail
	return ch
}

// contextExhausted matches a request's error when it ran out of context: llama.cpp's server
// refusing a prompt longer than its slot's context (exceed_context_size_error, "the request
// exceeds the available context size"), or ending a generation that filled it ("Context size has
// been exceeded."); the router passes either through as the model's server wrote it.
var contextExhausted = regexp.MustCompile(`(?i)context size has been exceeded|exceeds? the available context size|exceed_context_size`)

// contextCheck fails when any of a capacity run's requests, its concurrent run's or its loaded
// round's, ran out of context: each of the N sessions must fit in the model's context at once.
func contextCheck(role string, slots int, res concurrent.Report, loaded []timed) Check {
	var out []string
	for _, c := range res.Chats {
		if c.Err != nil && contextExhausted.MatchString(c.Err.Error()) {
			out = append(out, fmt.Sprintf("%s: %v", c.ID, c.Err))
		}
	}
	for _, e := range res.Embeds {
		if e.Err != nil && contextExhausted.MatchString(e.Err.Error()) {
			out = append(out, fmt.Sprintf("%s: %v", e.ID, e.Err))
		}
	}
	for i, q := range loaded {
		if q.err != nil && contextExhausted.MatchString(q.err.Error()) {
			out = append(out, fmt.Sprintf("request %d at N: %v", i+1, q.err))
		}
	}
	if len(out) > 0 {
		ch := fact("context held", role, fmt.Sprintf("%d ran out of context with %d in flight: %s", len(out), slots, listed(out)), false)
		ch.Values = map[string]float64{"exhausted": float64(len(out)), "slots": float64(slots)}
		return ch
	}
	ch := fact("context held", role, fmt.Sprintf("none ran out of context with %d in flight: %d answers, %d embedding vectors, %d timed requests",
		slots, len(res.Chats), len(res.Embeds), len(loaded)), true)
	ch.Values = map[string]float64{"exhausted": 0, "slots": float64(slots)}
	return ch
}

// during runs fn while the footprint reads the pool, once as fn starts and then every
// SampleEvery until it returns, and returns the reading with the least free memory and the
// number of readings taken; readErr is the last reading's error, when one failed.
func (r *Runner) during(ctx context.Context, fn func()) (low Pool, readings int, readErr error) {
	every := r.SampleEvery
	if every == 0 {
		every = 2 * time.Second
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			p, err := r.Footprint.Read(ctx)
			if err != nil {
				readErr = err
			} else if readings == 0 || p.FreeMiB() < low.FreeMiB() {
				low = p
			}
			if err == nil {
				readings++
			}
			select {
			case <-done:
				return
			case <-time.After(every):
			}
		}
	})
	fn()
	close(done)
	wg.Wait()
	return low, readings, readErr
}

// nonceRE matches the concurrent package's nonces, which its Prompt carries.
var nonceRE = regexp.MustCompile(`nonce-[0-9a-f]{16}`)

// recorder is a role's chat stream in capacity's run: it sends each prompt to the model, with
// the role's medium, and keeps each answer's measurements.
type recorder struct {
	c     *client
	model string
	role  string
	media Media
	// asr is the model's ASR prompt (Runner.asrPrompt), which an audio request sends first.
	asr string
	// agentic, set for capacity's agent streams, makes each request the agentic shape
	// (agenticBody): an agent session's, the shape the floor at N counts sessions by.
	agentic bool
	// cutOff, set for the set stage's crossing probe, makes an answer that hit its max_tokens
	// carrying no nonce at all an error (errCutOff), so the probe records it unanswered: it was
	// cut off before it could carry one, and holds nothing of another request's.
	cutOff     bool
	mu         sync.Mutex
	done       []completion
	errorCount int
}

// Chat sends the agent the prompt as given, or, agentic, in the agentic shape (agenticBody). An
// image request carries the image and asks for a description first, then the prompt's nonce on a
// line of its own, so the nonce check reads an answer that did the work; an audio request is
// fitness's timed transcription (transcriptionBody), the same request to repeat the nonce after
// the clip.
func (rc *recorder) Chat(ctx context.Context, prompt string) (string, error) {
	var body map[string]any
	var err error
	if rc.agentic {
		body, err = agenticBody(ctx, rc.c, rc.model, nonceRE.FindString(prompt))
	} else {
		body = rc.body(prompt)
	}
	var comp completion
	if err == nil {
		comp, err = rc.c.complete(ctx, body)
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if err != nil {
		rc.errorCount++
		return "", err
	}
	rc.done = append(rc.done, comp)
	if rc.cutOff && comp.Finish == "length" && !nonceRE.MatchString(comp.Text) {
		return "", fmt.Errorf("%w after %d tokens, with no nonce: %q", errCutOff, comp.Timings.PredictedN, comp.Text)
	}
	if comp.Text == "" {
		return "", errors.New("the answer's text is empty")
	}
	return comp.Text, nil
}

// errCutOff marks an answer that hit its max_tokens before it carried any nonce.
var errCutOff = errors.New("the answer hit its max_tokens")

// visionMaxTokens bounds the vision role's nonce request. Gemma 4 26B-A4B spends 200-460 tokens
// on the one-sentence description and the nonce, reasoning off; 512 cut its longest answers off
// before the nonce under the set stage's load (three in the 2026-10-17 baseline), so the bound
// is twice that.
const visionMaxTokens = 1024

// agenticTokens is the agentic shape's output: an 8192-token prompt (longPromptTokens), 512 out,
// the shape an agent session is counted by, as the deep stage's agentic grid cell sends it.
const agenticTokens = 512

// agenticBody is the agentic shape carrying nonce: an 8192-token passage cut for the model
// (fitText, its own run nonce first, so no cache holds it), then the request to repeat nonce on a
// line of its own first and summarize the passage after, up to agenticTokens out. The nonce comes
// first so an answer cut off at its max_tokens still carries it; the summary keeps the stream
// decoding toward the 512 an agent's turn produces.
func agenticBody(ctx context.Context, c *client, model, nonce string) (map[string]any, error) {
	text, err := c.fitText(ctx, model, longPromptTokens)
	if err != nil {
		return nil, fmt.Errorf("cutting the agentic prompt: %w", err)
	}
	ask := "\n\nFirst, on a line of its own, repeat this request ID exactly: " + nonce +
		"\nThen summarize the passage above in about 300 words."
	return chatBody(model, text+ask, agenticTokens, false), nil
}

// body is the role's request for prompt, which carries a nonce (Chat).
func (rc *recorder) body(prompt string) map[string]any {
	nonce := nonceRE.FindString(prompt)
	repeat := "Then, on a line of its own, repeat this request ID exactly: " + nonce
	switch rc.role {
	case RoleVision:
		return chatBody(rc.model, []map[string]any{imagePart(rc.media.PNG), textPart("Describe this image in one sentence. " + repeat)}, visionMaxTokens, false)
	case RoleAudio:
		return transcriptionBody(rc.model, rc.asr, rc.media.WAV, repeat)
	default:
		// Room for a reasoning model's reasoning before its answer.
		return chatBody(rc.model, prompt, 4096, false)
	}
}

// floor judges the slowest of the run's answers, its slots in flight, by value against floor;
// every request must have answered.
func (rc *recorder) floor(name, unit string, slots int, floor float64, value func(completion) float64) Check {
	if rc.errorCount > 0 || len(rc.done) == 0 {
		return broken(name, rc.role, fmt.Errorf("%d of the requests failed", rc.errorCount))
	}
	values := map[string]float64{"slots": float64(slots)}
	least := math.Inf(1)
	for i, c := range rc.done {
		v := value(c)
		values[fmt.Sprintf("stream%d", i+1)] = v
		values[fmt.Sprintf("stream%d_predicted_n", i+1)] = float64(c.Timings.PredictedN)
		values[fmt.Sprintf("stream%d_prompt_n", i+1)] = float64(c.Timings.PromptN)
		least = min(least, v)
	}
	ch := measure(name, rc.role, least, unit, AtLeast, floor)
	ch.Values = values
	ch.Detail = fmt.Sprintf("the slowest of the %d streams in flight", len(rc.done))
	return ch
}

// embedder is an embedding stream's consumer. Its inputs are documents, each sent after the
// model's document prompt (embedPrompts), batched and alone alike, so the tolerance compares one
// served form of each.
type embedder struct {
	c        *client
	model    string
	document string
}

// Embed embeds inputs in one request, each after the document prompt.
func (e embedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	prefixed := make([]string, len(inputs))
	for i, in := range inputs {
		prefixed[i] = e.document + in
	}
	got, err := e.c.embed(ctx, e.model, prefixed)
	return got.Vectors, err
}
