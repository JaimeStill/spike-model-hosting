package concurrent_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JaimeStill/spike-harness-driver/harness"
	"github.com/JaimeStill/spike-harness-driver/model"
)

// nonceRE finds the nonce in a prompt, as a fake model reads it.
var nonceRE = regexp.MustCompile(`nonce-[0-9a-f]{16}`)

// fakeWait bounds how long a fake holds a request for others to arrive.
const fakeWait = 10 * time.Second

// backend is the fake model behind a test's chat endpoints: it answers each prompt with the
// prompt's nonce, after its gate and its swapper, when it has them, have their way.
type backend struct {
	gate  *gate
	swap  *swapper
	reply func(nonce string) string // nil echoes the nonce
	count atomic.Int32
}

func (b *backend) answer(stream, prompt string) string {
	b.count.Add(1)
	nonce := nonceRE.FindString(prompt)
	if b.gate != nil {
		b.gate.pass()
	}
	if b.swap != nil {
		nonce = b.swap.exchange(stream, nonce)
	}
	if b.reply != nil {
		return b.reply(nonce)
	}
	return nonce
}

// gate holds the first n requests until all n have arrived, which shows they were in flight
// at once; later requests pass straight through.
type gate struct {
	n        int
	mu       sync.Mutex
	arrived  int
	open     chan struct{}
	timedOut atomic.Bool
}

func newGate(n int) *gate { return &gate{n: n, open: make(chan struct{})} }

func (g *gate) pass() {
	g.mu.Lock()
	g.arrived++
	if g.arrived == g.n {
		close(g.open)
	}
	g.mu.Unlock()
	select {
	case <-g.open:
	case <-time.After(fakeWait):
		g.timedOut.Store(true)
	}
}

// swapper swaps the answers of the first request it sees with the first that arrives after
// it from another stream: each gets the other's nonce.
type swapper struct {
	mu      sync.Mutex
	first   string // the waiting request's stream
	nonce   string
	waiting chan string
	done    bool
	// pair is the two swapped nonces, first the earlier request's.
	pair []string
}

func (s *swapper) exchange(stream, nonce string) string {
	s.mu.Lock()
	switch {
	case s.done || (s.waiting != nil && s.first == stream):
		s.mu.Unlock()
		return nonce
	case s.waiting == nil:
		ch := make(chan string, 1)
		s.first, s.nonce, s.waiting = stream, nonce, ch
		s.mu.Unlock()
		select {
		case other := <-ch:
			return other
		case <-time.After(fakeWait):
			return nonce
		}
	default:
		s.done, s.pair = true, []string{s.nonce, nonce}
		first, ch := s.nonce, s.waiting
		s.mu.Unlock()
		ch <- nonce
		return first
	}
}

func (s *swapper) swapped() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pair
}

// chatServer is an OpenAI chat completions endpoint answering from b as stream.
func chatServer(t *testing.T, b *backend, stream string) *model.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) != 1 {
			http.Error(w, "want one message", http.StatusBadRequest)
			return
		}
		reply := map[string]any{"choices": []any{map[string]any{
			"message":       map[string]any{"content": b.answer(stream, body.Messages[0].Content)},
			"finish_reason": "stop",
		}}}
		_ = json.NewEncoder(w).Encode(reply)
	}))
	t.Cleanup(srv.Close)
	return newClient(t, srv.URL)
}

func newClient(t *testing.T, base string) *model.Client {
	t.Helper()
	c, err := model.New(model.Config{BaseURL: base + "/v1", HTTP: &http.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// embedServer is an OpenAI embeddings endpoint. Input i of inputs embeds as the unit vector
// along axis i; in a batch of more than one, the vector turns toward a spare axis until its
// cosine to the lone vector is batchCosine, and with swap set, the batch's first two vectors
// trade places.
func embedServer(t *testing.T, inputs []string, batchCosine float64, swap bool) *model.Client {
	t.Helper()
	axis := map[string]int{}
	for i, in := range inputs {
		axis[in] = i
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		vecs := make([][]float32, len(body.Input))
		for i, in := range body.Input {
			v := make([]float32, len(inputs)+1)
			if len(body.Input) == 1 {
				v[axis[in]] = 1
			} else {
				v[axis[in]] = float32(batchCosine)
				v[len(inputs)] = float32(math.Sqrt(1 - batchCosine*batchCosine))
			}
			vecs[i] = v
		}
		if swap && len(vecs) > 1 {
			vecs[0], vecs[1] = vecs[1], vecs[0]
		}
		data := make([]any, len(vecs))
		for i, v := range vecs {
			data[i] = map[string]any{"index": i, "embedding": v}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return newClient(t, srv.URL)
}

// fakeDriver is a harness.Driver whose sessions answer from b as stream, the way Pi's driver
// stands in a run.
type fakeDriver struct {
	b      *backend
	stream string
	opened atomic.Int32
	closed atomic.Int32
}

func (d *fakeDriver) Open(ctx context.Context, _ harness.Options) (*harness.Session, error) {
	d.opened.Add(1)
	c := &fakeConn{d: d, events: make(chan harness.Event, 2)}
	return harness.NewSession(ctx, "fake", c, nil)
}

type fakeConn struct {
	d      *fakeDriver
	events chan harness.Event
	once   sync.Once
}

func (c *fakeConn) Prompt(_ context.Context, req harness.Request) error {
	go func() {
		c.events <- harness.Event{Kind: harness.EventMessageEnd, Text: c.d.b.answer(c.d.stream, req.Text), StopReason: "stop"}
		c.events <- harness.Event{Kind: harness.EventEnded}
	}()
	return nil
}

func (c *fakeConn) Cancel(context.Context) error { return nil }
func (c *fakeConn) Events() <-chan harness.Event { return c.events }
func (c *fakeConn) Err() error                   { return nil }

func (c *fakeConn) Close() error {
	c.once.Do(func() {
		c.d.closed.Add(1)
		close(c.events)
	})
	return nil
}
