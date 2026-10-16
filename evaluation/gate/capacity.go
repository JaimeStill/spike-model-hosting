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

// capacity puts the profile's slots (4) of requests in flight on the model for each role it
// takes, through the concurrent package's run, while the footprint reads the pool. A chat
// role's run carries an embedding stream to the set's embedding model beside it; an embedding
// model's run is one embedding stream a slot. Each run passes when every answer carries its own
// nonce and no other, every batched vector holds the embedding tolerance, the pool's least free
// memory stays at the floor, and each stream meets the role's 4-stream floor.
func (r *Runner) capacity(ctx context.Context, rep *Report) []Check {
	c := &client{base: r.Router, hc: r.HTTP}
	var checks []Check
	for _, role := range rep.Roles {
		checks = append(checks, r.capRole(ctx, c, role)...)
	}
	return checks
}

func (r *Runner) capRole(ctx context.Context, c *client, role string) []Check {
	slots := r.Profile.Slots
	cfg := concurrent.Config{Rounds: 1, RequestTimeout: requestTimeout}
	var rec *recorder
	if role == RoleEmbedding {
		// An embedding stream sends batch after batch; three rounds each keep the four in flight
		// long enough for the footprint to read the pool while they are.
		cfg.Rounds = 3
		for i := range slots {
			k := i % len(capacityInputs)
			inputs := slices.Concat(capacityInputs[k:], capacityInputs[:k])
			cfg.Embed = append(cfg.Embed, concurrent.EmbedStream{
				Name: fmt.Sprintf("embedding-%d", i+1), Embedder: embedder{c, r.Model}, Inputs: inputs,
			})
		}
	} else {
		rec = &recorder{c: c, model: r.Model, role: role, media: r.Media}
		cfg.Chat = []concurrent.ChatStream{{Name: role, Chatter: rec, Workers: slots}}
		if e := r.Set.Roles.Embedding; e != "" {
			cfg.Embed = []concurrent.EmbedStream{{Name: "embedding", Embedder: embedder{c, e}, Inputs: capacityInputs}}
		}
	}

	var res concurrent.Report
	var runErr error
	low, readings, readErr := r.during(ctx, func() { res, runErr = concurrent.Run(ctx, cfg) })
	if runErr != nil {
		return []Check{broken("4 in flight", role, runErr)}
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

	switch role {
	case RoleAgent:
		checks = append(checks, rec.floor("4-stream decode", "tok/s", r.Floors.Agent.Stream4DecodeTokS,
			func(c completion) float64 { return c.Timings.PredictedPerSecond }))
	case RoleAudio:
		checks = append(checks, rec.floor("4-stream rtfx", "x", r.Floors.Audio.Stream4RTFx,
			func(c completion) float64 { return r.Media.AudioSeconds / c.Wall.Seconds() }))
	}

	if readings == 0 {
		return append(checks, broken("peak pool free", role, readErr))
	}
	peak := measure("peak pool free", role, float64(low.FreeMiB()), "MiB", AtLeast, r.Floors.Pool.FreeMiB)
	peak.Detail = fmt.Sprintf("%d of %d MiB used at the peak of %d readings", low.UsedMiB, low.TotalMiB, readings)
	return append(checks, peak)
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
	c          *client
	model      string
	role       string
	media      Media
	mu         sync.Mutex
	done       []completion
	errorCount int
}

// Chat sends the agent the prompt as given. An image or audio request carries the medium and
// asks for the role's work first, then the prompt's nonce on a line of its own, so the nonce
// check reads an answer that did the work.
func (rc *recorder) Chat(ctx context.Context, prompt string) (string, error) {
	var content any = prompt
	maxTokens := 4096 // room for a reasoning model's reasoning before its answer
	nonce := nonceRE.FindString(prompt)
	repeat := " Then, on a line of its own, repeat this request ID exactly: " + nonce
	switch rc.role {
	case RoleVision:
		content, maxTokens = []map[string]any{imagePart(rc.media.PNG), textPart("Describe this image in one sentence." + repeat)}, 512
	case RoleAudio:
		content, maxTokens = []map[string]any{audioPart(rc.media.WAV), textPart("Transcribe this audio clip." + repeat)}, 512
	}
	body := chatBody(rc.model, content, maxTokens, false)
	if rc.role == RoleAudio {
		// A transcription client turns reasoning off, as the profile leaves a request's shape to
		// the client: asked to repeat the nonce too, gemma-4-E4B reasons ~200 tokens before its
		// 25-token answer on b11669 (a plain "Transcribe this audio clip." doesn't), which would
		// time its reasoning, not its transcription. Templates without the switch ignore it.
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	comp, err := rc.c.complete(ctx, body)
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if err != nil {
		rc.errorCount++
		return "", err
	}
	rc.done = append(rc.done, comp)
	if comp.Text == "" {
		return "", errors.New("the answer's text is empty")
	}
	return comp.Text, nil
}

// floor judges the slowest of the run's answers by value against floor; every request must
// have answered.
func (rc *recorder) floor(name, unit string, floor float64, value func(completion) float64) Check {
	if rc.errorCount > 0 || len(rc.done) == 0 {
		return broken(name, rc.role, fmt.Errorf("%d of the requests failed", rc.errorCount))
	}
	values := map[string]float64{}
	least := math.Inf(1)
	for i, c := range rc.done {
		v := value(c)
		values[fmt.Sprintf("stream%d", i+1)] = v
		values[fmt.Sprintf("stream%d_predicted_n", i+1)] = float64(c.Timings.PredictedN)
		least = min(least, v)
	}
	ch := measure(name, rc.role, least, unit, AtLeast, floor)
	ch.Values = values
	ch.Detail = "the slowest of the streams"
	return ch
}

// embedder is an embedding stream's consumer.
type embedder struct {
	c     *client
	model string
}

// Embed embeds inputs in one request.
func (e embedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	got, err := e.c.embed(ctx, e.model, inputs)
	return got.Vectors, err
}
