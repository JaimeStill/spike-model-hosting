package concurrent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultRequestTimeout bounds one request when Config.RequestTimeout is zero. The models are
// loaded already, so it covers a harness's start-up and generation only, which on gpt-oss-120b's
// reasoning, with every slot busy, takes tens of seconds.
const DefaultRequestTimeout = 5 * time.Minute

// Chatter answers one chat prompt. It must be safe for concurrent use: a stream's workers share
// it.
type Chatter interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

// Embedder embeds a batch of inputs, returning one vector per input, in input order. It must be
// safe for concurrent use.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// ChatStream is one consumer's chat traffic: Workers requests in flight at once, each worker
// sending Config.Rounds requests one after another.
type ChatStream struct {
	// Name names the stream in request IDs and the report, such as "pi".
	Name    string
	Chatter Chatter
	Workers int
}

// EmbedStream is one consumer's embedding traffic: Inputs embedded together, once per round.
type EmbedStream struct {
	// Name names the stream in request IDs and the report, such as "model-embed".
	Name     string
	Embedder Embedder
	// Inputs are the texts embedded, at least two, so a batch can mix them up.
	Inputs []string
}

// Config is one run.
type Config struct {
	// Rounds is how many requests each chat worker sends, and how many batches each embedding
	// stream sends.
	Rounds int
	Chat   []ChatStream
	Embed  []EmbedStream
	// RequestTimeout bounds one request; zero means DefaultRequestTimeout.
	RequestTimeout time.Duration
	// InjectSwap swaps the answers of two chat requests, from two streams when there are two,
	// after they return and before they are checked. A run with it set must fail, naming both:
	// it shows the check catches a swap on live traffic.
	InjectSwap bool
}

// ChatResult is one chat request and its outcome.
type ChatResult struct {
	// ID names the request as <stream>/w<worker>/r<round>, such as pi/w1/r0.
	ID     string
	Stream string
	Nonce  string
	Answer string
	// Start is when the request was sent, from the start of the run.
	Start    time.Duration
	Duration time.Duration
	Err      error
	// Foreign are the IDs of the other requests whose nonces the answer carries.
	Foreign []string
	// Unknown are the nonce-shaped tokens in the answer that no request in the run carried.
	Unknown []string
}

// Passed reports whether the request returned, its answer carries its own nonce, and no other.
func (r ChatResult) Passed() bool {
	return r.Err == nil && r.hasOwn() && len(r.Foreign) == 0 && len(r.Unknown) == 0
}

func (r ChatResult) hasOwn() bool { return containsNonce(r.Answer, r.Nonce) }

// EmbedResult is one input's vector from one batched embedding request, against the same
// input's embedding requested alone.
type EmbedResult struct {
	// ID names the vector as <stream>/r<round>/<index>, its input's index in Inputs.
	ID     string
	Stream string
	Input  string
	// Start is when the batch was sent, from the start of the run.
	Start    time.Duration
	Duration time.Duration
	Err      error
	// Cosine is the vector's cosine similarity to its input's lone embedding.
	Cosine float64
	// Closest is the index in Inputs of the lone embedding the vector is most similar to, which
	// is its own index unless the vector belongs to another input.
	Closest int
	index   int
}

// Passed reports whether the batch returned and the vector matches its own input's lone
// embedding within EmbedTolerance.
func (r EmbedResult) Passed() bool {
	return r.Err == nil && r.Cosine >= 1-EmbedTolerance && r.Closest == r.index
}

// Report is a run's outcome.
type Report struct {
	Chats  []ChatResult
	Embeds []EmbedResult
	// Elapsed is the concurrent phase's wall time, from the first request to the last answer.
	Elapsed time.Duration
	// Swapped names the two requests whose answers InjectSwap swapped, when it did.
	Swapped []string
}

// Passed reports whether every request passed.
func (r Report) Passed() bool { return len(r.Failures()) == 0 }

// Failures describes each failed request, one line each, naming the request and, for an answer
// or vector that belongs to another request, that request too.
func (r Report) Failures() []string {
	var out []string
	for _, c := range r.Chats {
		switch {
		case c.Err != nil:
			out = append(out, fmt.Sprintf("%s: %v", c.ID, c.Err))
		case c.Passed():
		default:
			for _, other := range c.Foreign {
				out = append(out, fmt.Sprintf("%s: the answer carries %s's nonce", c.ID, other))
			}
			for _, n := range c.Unknown {
				out = append(out, fmt.Sprintf("%s: the answer carries %s, which no request sent", c.ID, n))
			}
			if !c.hasOwn() {
				out = append(out, fmt.Sprintf("%s: the answer doesn't carry its nonce %s: %q", c.ID, c.Nonce, c.Answer))
			}
		}
	}
	for _, e := range r.Embeds {
		switch {
		case e.Err != nil:
			out = append(out, fmt.Sprintf("%s: %v", e.ID, e.Err))
		case e.Passed():
		case e.Closest != e.index:
			out = append(out, fmt.Sprintf("%s: the vector is input %d's, not its own input %d's (cosine to its own %.6f)",
				e.ID, e.Closest, e.index, e.Cosine))
		default:
			out = append(out, fmt.Sprintf("%s: cosine to its lone embedding is %.6f, below 1-%g",
				e.ID, e.Cosine, EmbedTolerance))
		}
	}
	return out
}

// Run runs cfg: it embeds each embedding stream's inputs alone, then starts every stream at
// once and checks each answer. Its error is for a run that couldn't start, such as a Config
// without streams or a lone embedding that failed; a request that fails once the streams run
// is a failure in the Report.
func Run(ctx context.Context, cfg Config) (Report, error) {
	if err := cfg.validate(); err != nil {
		return Report{}, err
	}
	timeout := cfg.RequestTimeout
	if timeout == 0 {
		timeout = DefaultRequestTimeout
	}
	refs := make([][][]float32, len(cfg.Embed))
	for i, s := range cfg.Embed {
		var err error
		if refs[i], err = embedAlone(ctx, s, timeout); err != nil {
			return Report{}, err
		}
	}

	// Every nonce is drawn before the first request, so the check knows them all.
	var chats []ChatResult
	for _, s := range cfg.Chat {
		for w := range s.Workers {
			for r := range cfg.Rounds {
				nonce, err := newNonce()
				if err != nil {
					return Report{}, err
				}
				chats = append(chats, ChatResult{
					ID: fmt.Sprintf("%s/w%d/r%d", s.Name, w, r), Stream: s.Name, Nonce: nonce,
				})
			}
		}
	}
	var embeds []EmbedResult
	for _, s := range cfg.Embed {
		for r := range cfg.Rounds {
			for i, in := range s.Inputs {
				embeds = append(embeds, EmbedResult{
					ID: fmt.Sprintf("%s/r%d/%d", s.Name, r, i), Stream: s.Name, Input: in, index: i,
				})
			}
		}
	}

	start := time.Now()
	var wg sync.WaitGroup
	next := 0
	for _, s := range cfg.Chat {
		for range s.Workers {
			mine := chats[next : next+cfg.Rounds]
			next += cfg.Rounds
			wg.Go(func() {
				for i := range mine {
					mine[i].chat(ctx, s.Chatter, start, timeout)
				}
			})
		}
	}
	next = 0
	for i, s := range cfg.Embed {
		mine := embeds[next : next+cfg.Rounds*len(s.Inputs)]
		next += len(mine)
		wg.Go(func() {
			for r := range cfg.Rounds {
				embedRound(ctx, s, refs[i], mine[r*len(s.Inputs):(r+1)*len(s.Inputs)], r, start, timeout)
			}
		})
	}
	wg.Wait()
	rep := Report{Chats: chats, Embeds: embeds, Elapsed: time.Since(start)}
	if cfg.InjectSwap {
		rep.Swapped = injectSwap(rep.Chats)
	}
	checkNonces(rep.Chats)
	return rep, nil
}

func (cfg Config) validate() error {
	switch {
	case cfg.Rounds < 1:
		return errors.New("concurrent: the run needs at least one round")
	case len(cfg.Chat) == 0 && len(cfg.Embed) == 0:
		return errors.New("concurrent: the run has no streams")
	}
	names := map[string]bool{}
	chatRequests := 0
	for _, s := range cfg.Chat {
		switch {
		case s.Name == "" || names[s.Name]:
			return fmt.Errorf("concurrent: chat stream %q: every stream needs a name of its own", s.Name)
		case s.Chatter == nil:
			return fmt.Errorf("concurrent: chat stream %s has no Chatter", s.Name)
		case s.Workers < 1:
			return fmt.Errorf("concurrent: chat stream %s needs at least one worker", s.Name)
		}
		names[s.Name] = true
		chatRequests += s.Workers * cfg.Rounds
	}
	if cfg.InjectSwap && chatRequests < 2 {
		return errors.New("concurrent: InjectSwap needs at least two chat requests")
	}
	for _, s := range cfg.Embed {
		switch {
		case s.Name == "" || names[s.Name]:
			return fmt.Errorf("concurrent: embedding stream %q: every stream needs a name of its own", s.Name)
		case s.Embedder == nil:
			return fmt.Errorf("concurrent: embedding stream %s has no Embedder", s.Name)
		case len(s.Inputs) < 2:
			return fmt.Errorf("concurrent: embedding stream %s needs at least two inputs", s.Name)
		}
		seen := map[string]bool{}
		for _, in := range s.Inputs {
			if seen[in] {
				// Two equal inputs have one embedding, so a vector couldn't be told from the other's.
				return fmt.Errorf("concurrent: embedding stream %s repeats the input %q", s.Name, in)
			}
			seen[in] = true
		}
		names[s.Name] = true
	}
	return nil
}

// chat sends r's request and records its answer and timing.
func (r *ChatResult) chat(ctx context.Context, c Chatter, start time.Time, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	sent := time.Now()
	r.Start = sent.Sub(start)
	r.Answer, r.Err = c.Chat(ctx, Prompt(r.Nonce))
	r.Duration = time.Since(sent)
}

// Prompt is the chat prompt that carries nonce. It asks for the nonce alone, so an answer that
// carries any other nonce got another request's answer. It says what the nonce is for:
// gpt-oss-120b refused a bare "reply with exactly this token" once in twelve, as if the nonce
// were a secret.
func Prompt(nonce string) string {
	return "This is a load test that checks each answer reaches the request that asked it. " +
		"Repeat the request ID below exactly, with nothing else in your reply.\n\nRequest ID: " + nonce
}

// newNonce returns a nonce no other request in the run carries: nonce- and 16 random hex
// digits, a token a model copies whole.
func newNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("concurrent: nonce: %w", err)
	}
	return noncePrefix + hex.EncodeToString(b), nil
}

// embedAlone embeds each of s's inputs in a request of its own, before the streams start: the
// embeddings a batch's vectors are checked against.
func embedAlone(ctx context.Context, s EmbedStream, timeout time.Duration) ([][]float32, error) {
	refs := make([][]float32, len(s.Inputs))
	for i, in := range s.Inputs {
		rctx, cancel := context.WithTimeout(ctx, timeout)
		v, err := s.Embedder.Embed(rctx, []string{in})
		cancel()
		switch {
		case err != nil:
			return nil, fmt.Errorf("concurrent: %s: embedding input %d alone: %w", s.Name, i, err)
		case len(v) != 1:
			return nil, fmt.Errorf("concurrent: %s: embedding input %d alone returned %d vectors", s.Name, i, len(v))
		}
		refs[i] = v[0]
	}
	return refs, nil
}

// embedRound embeds s's inputs in one batch, rotated by the round so each input sits at a new
// position in each batch, and checks each vector against refs.
func embedRound(ctx context.Context, s EmbedStream, refs [][]float32, out []EmbedResult, round int, start time.Time, timeout time.Duration) {
	n := len(s.Inputs)
	batch := make([]string, n)
	for i := range n {
		batch[i] = s.Inputs[(i+round)%n]
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	sent := time.Now()
	vecs, err := s.Embedder.Embed(ctx, batch)
	took := time.Since(sent)
	if err == nil && len(vecs) != n {
		err = fmt.Errorf("the batch of %d returned %d vectors", n, len(vecs))
	}
	for pos := range n {
		idx := (pos + round) % n
		r := &out[idx]
		r.Start, r.Duration, r.Err = sent.Sub(start), took, err
		if err != nil {
			continue
		}
		r.Cosine = Cosine(vecs[pos], refs[idx])
		r.Closest = closest(vecs[pos], refs)
	}
}

// closest returns the index of the vector in refs most similar to v.
func closest(v []float32, refs [][]float32) int {
	best, bestCos := 0, Cosine(v, refs[0])
	for i := 1; i < len(refs); i++ {
		if c := Cosine(v, refs[i]); c > bestCos {
			best, bestCos = i, c
		}
	}
	return best
}

// injectSwap swaps the answers of the first request of each of the first two streams, or of
// the first two requests when there is one stream, and returns their IDs.
func injectSwap(chats []ChatResult) []string {
	a, b := 0, 1
	for i := range chats {
		if chats[i].Stream != chats[0].Stream {
			b = i
			break
		}
	}
	chats[a].Answer, chats[b].Answer = chats[b].Answer, chats[a].Answer
	return []string{chats[a].ID, chats[b].ID}
}

// SplitSlots divides slots requests in flight among streams chat streams, as evenly as it can,
// the first streams taking one more when they don't divide evenly, so the streams together keep
// every slot busy.
func SplitSlots(slots, streams int) []int {
	if streams < 1 {
		return nil
	}
	out := make([]int, streams)
	for i := range out {
		out[i] = slots / streams
		if i < slots%streams {
			out[i]++
		}
	}
	return out
}
