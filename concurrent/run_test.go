package concurrent_test

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/concurrent"
)

var embedInputs = []string{"alpha", "beta", "gamma", "delta"}

// setup is a run's streams over fakes: a harness stream named pi and a model-client chat stream
// named model-chat, both answering from b, with two workers each, and a model-client embedding
// stream whose batches sit batchCosine from the lone embeddings.
func setup(t *testing.T, b *backend, rounds int, batchCosine float64, swapEmbeds bool) (concurrent.Config, *fakeDriver) {
	t.Helper()
	d := &fakeDriver{b: b, stream: "pi"}
	return concurrent.Config{
		Rounds: rounds,
		Chat: []concurrent.ChatStream{
			{Name: "pi", Workers: 2, Chatter: concurrent.HarnessChat{Driver: d}},
			{Name: "model-chat", Workers: 2, Chatter: concurrent.ModelChat{Client: chatServer(t, b, "model-chat"), Model: "m"}},
		},
		Embed: []concurrent.EmbedStream{{
			Name:     "model-embed",
			Embedder: concurrent.ModelEmbed{Client: embedServer(t, embedInputs, batchCosine, swapEmbeds), Model: "e"},
			Inputs:   embedInputs,
		}},
	}, d
}

// TestRunPasses shows a run in which every endpoint answers its own request passes: every
// request was sent, with all four slots in flight at once, each nonce is the run's alone, and
// each answer carries its own.
func TestRunPasses(t *testing.T) {
	b := &backend{gate: newGate(4)}
	cfg, d := setup(t, b, 3, 1, false)
	rep, err := concurrent.Run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("the run failed:\n%s", strings.Join(rep.Failures(), "\n"))
	}
	if b.gate.timedOut.Load() {
		t.Error("the first four chat requests were never in flight at once")
	}
	if n := len(rep.Chats); n != 12 || b.count.Load() != 12 {
		t.Errorf("%d chat results, %d requests sent; want 12 of each", n, b.count.Load())
	}
	if d.opened.Load() != 6 || d.closed.Load() != 6 {
		t.Errorf("the harness stream opened %d sessions and closed %d, want one each per request (6)",
			d.opened.Load(), d.closed.Load())
	}
	seen := map[string]bool{}
	for _, c := range rep.Chats {
		if seen[c.Nonce] {
			t.Errorf("%s repeats the nonce %s", c.ID, c.Nonce)
		}
		seen[c.Nonce] = true
		if c.Answer != c.Nonce {
			t.Errorf("%s answered %q, want its nonce %s", c.ID, c.Answer, c.Nonce)
		}
	}
	if n := len(rep.Embeds); n < 12 || n%4 != 0 {
		t.Errorf("%d embedding results, want at least 3 rounds of 4", n)
	}
}

// TestEmbedRunsUntilChatsFinish shows an embedding stream keeps sending batches, each checked,
// for as long as a chat request is in flight, beyond its Rounds, and stops once the chats
// return.
func TestEmbedRunsUntilChatsFinish(t *testing.T) {
	const hold = 300 * time.Millisecond
	cfg, _ := setup(t, &backend{reply: func(n string) string { time.Sleep(hold); return n }}, 1, 1, false)
	cfg.Chat = cfg.Chat[1:2]
	cfg.Chat[0].Workers = 1
	rep, err := concurrent.Run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("the run failed:\n%s", strings.Join(rep.Failures(), "\n"))
	}
	if n := len(rep.Embeds) / len(embedInputs); n < 2 {
		t.Fatalf("the embedding stream sent %d batches while the chat took %s, want more than its one round", n, hold)
	}
	chat := rep.Chats[0]
	late := chat.Start + chat.Duration/2
	var last time.Duration
	for _, e := range rep.Embeds {
		last = max(last, e.Start)
	}
	if last < late {
		t.Errorf("the last batch started at %s, before the chat's second half (from %s): the stream stopped early", last, late)
	}
}

// TestRunNamesSwap shows that when the endpoints swap two requests' answers, one from each
// stream, the run fails and names both requests, each against the other.
func TestRunNamesSwap(t *testing.T) {
	b := &backend{swap: &swapper{}}
	cfg, _ := setup(t, b, 2, 1, false)
	rep, err := concurrent.Run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	pair := b.swap.swapped()
	if len(pair) != 2 {
		t.Fatal("the fake never swapped two answers")
	}
	if rep.Passed() {
		t.Fatal("the run passed despite the swap")
	}
	a, c := idOf(t, rep, pair[0]), idOf(t, rep, pair[1])
	want := []string{
		fmt.Sprintf("%s: the answer carries %s's nonce", a, c),
		fmt.Sprintf("%s: the answer doesn't carry its nonce", a),
		fmt.Sprintf("%s: the answer carries %s's nonce", c, a),
		fmt.Sprintf("%s: the answer doesn't carry its nonce", c),
	}
	assertFailures(t, rep, want)
}

// TestInjectSwap shows the run's own injected swap fails an otherwise passing run, naming the
// two requests it swapped, one from each stream.
func TestInjectSwap(t *testing.T) {
	cfg, _ := setup(t, &backend{}, 1, 1, false)
	cfg.InjectSwap = true
	rep, err := concurrent.Run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Swapped) != 2 {
		t.Fatalf("Swapped = %v, want two requests", rep.Swapped)
	}
	a, c := rep.Swapped[0], rep.Swapped[1]
	if streamOf(a) == streamOf(c) {
		t.Errorf("the swap took %s and %s, from one stream; want one from each", a, c)
	}
	assertFailures(t, rep, []string{
		fmt.Sprintf("%s: the answer carries %s's nonce", a, c),
		fmt.Sprintf("%s: the answer doesn't carry its nonce", a),
		fmt.Sprintf("%s: the answer carries %s's nonce", c, a),
		fmt.Sprintf("%s: the answer doesn't carry its nonce", c),
	})
}

// TestRunAnswerChecks shows each way an answer fails on its own: without its nonce, with a
// nonce no request sent beside its own, and with its own nonce beside another request's.
func TestRunAnswerChecks(t *testing.T) {
	const stranger = "nonce-0123456789abcdef"
	cases := map[string]struct {
		reply func(nonce string) string
		want  string
	}{
		"no nonce":      {func(string) string { return "ready" }, "doesn't carry its nonce"},
		"unknown nonce": {func(n string) string { return n + " " + stranger }, "carries " + stranger + ", which no request sent"},
		"prefix only":   {func(n string) string { return n[:len(n)-1] }, "doesn't carry its nonce"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, _ := setup(t, &backend{reply: tc.reply}, 1, 1, false)
			rep, err := concurrent.Run(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			f := rep.Failures()
			if len(f) != len(rep.Chats) {
				t.Fatalf("%d failures for %d chats:\n%s", len(f), len(rep.Chats), strings.Join(f, "\n"))
			}
			for _, line := range f {
				if !strings.Contains(line, tc.want) {
					t.Errorf("failure %q doesn't say %q", line, tc.want)
				}
			}
		})
	}

	t.Run("another's nonce beside its own", func(t *testing.T) {
		// Every answer carries its own nonce and the first nonce the backend saw.
		var (
			mu    sync.Mutex
			first string
		)
		b := &backend{}
		b.reply = func(n string) string {
			mu.Lock()
			defer mu.Unlock()
			if first == "" {
				first = n
			}
			return n + " " + first
		}
		cfg, _ := setup(t, b, 1, 1, false)
		cfg.Chat = cfg.Chat[1:2]
		cfg.Chat[0].Workers = 1
		cfg.Rounds = 2
		rep, err := concurrent.Run(t.Context(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		assertFailures(t, rep, []string{"model-chat/w0/r1: the answer carries model-chat/w0/r0's nonce"})
	})
}

// TestEmbedSwap shows a batch that returns two inputs' vectors in each other's places fails,
// naming both vectors, each as the other input's.
func TestEmbedSwap(t *testing.T) {
	cfg, _ := setup(t, &backend{}, 1, 1, true)
	// Without chat streams the embedding stream sends its one round alone, so the fake's swap
	// lands in one batch only.
	cfg.Chat = nil
	rep, err := concurrent.Run(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Round 0 sends the inputs in order, so the fake swaps inputs 0 and 1.
	assertFailures(t, rep, []string{
		"model-embed/r0/0: the vector is input 1's, not its own input 0's",
		"model-embed/r0/1: the vector is input 0's, not its own input 1's",
	})
}

// TestEmbedTolerance shows a batched vector passes within EmbedTolerance of its lone embedding
// and fails beyond it, in every round, whichever position the round's rotation gives it.
func TestEmbedTolerance(t *testing.T) {
	cases := map[string]struct {
		cosine float64
		pass   bool
	}{
		"exact":             {1, true},
		"within tolerance":  {1 - concurrent.EmbedTolerance/2, true},
		"beyond tolerance":  {1 - 2*concurrent.EmbedTolerance, false},
		"another direction": {0.5, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, _ := setup(t, &backend{}, 3, tc.cosine, false)
			rep, err := concurrent.Run(t.Context(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range rep.Embeds {
				if math.Abs(e.Cosine-tc.cosine) > 1e-6 {
					t.Errorf("%s: cosine %v, want %v", e.ID, e.Cosine, tc.cosine)
				}
				if e.Passed() != tc.pass {
					t.Errorf("%s: Passed = %v at cosine %v, want %v", e.ID, e.Passed(), e.Cosine, tc.pass)
				}
			}
			if rep.Passed() != tc.pass {
				t.Errorf("the run's Passed = %v, want %v:\n%s", rep.Passed(), tc.pass, strings.Join(rep.Failures(), "\n"))
			}
		})
	}
}

func TestCosine(t *testing.T) {
	cases := map[string]struct {
		a, b []float32
		want float64
	}{
		"same":       {[]float32{1, 2, 3}, []float32{2, 4, 6}, 1},
		"orthogonal": {[]float32{1, 0}, []float32{0, 3}, 0},
		"opposite":   {[]float32{1, 1}, []float32{-1, -1}, -1},
		"45 degrees": {[]float32{1, 0}, []float32{1, 1}, math.Sqrt2 / 2},
		"lengths":    {[]float32{1, 0}, []float32{1, 0, 0}, 0},
		"zero":       {[]float32{0, 0}, []float32{1, 0}, 0},
	}
	for name, tc := range cases {
		if got := concurrent.Cosine(tc.a, tc.b); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: Cosine = %v, want %v", name, got, tc.want)
		}
	}
}

func TestSplitSlots(t *testing.T) {
	cases := []struct {
		slots, streams int
		want           []int
	}{
		{4, 2, []int{2, 2}},
		{4, 3, []int{2, 1, 1}},
		{5, 2, []int{3, 2}},
		{4, 1, []int{4}},
		{4, 0, nil},
	}
	for _, tc := range cases {
		if got := concurrent.SplitSlots(tc.slots, tc.streams); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitSlots(%d, %d) = %v, want %v", tc.slots, tc.streams, got, tc.want)
		}
	}
}

// TestRunRefusesConfig shows Run refuses a Config it can't check before sending anything.
func TestRunRefusesConfig(t *testing.T) {
	b := &backend{}
	base, _ := setup(t, b, 1, 1, false)
	cases := map[string]func(c *concurrent.Config){
		"no rounds":       func(c *concurrent.Config) { c.Rounds = 0 },
		"no streams":      func(c *concurrent.Config) { c.Chat, c.Embed = nil, nil },
		"repeated name":   func(c *concurrent.Config) { c.Embed[0].Name = "pi" },
		"no workers":      func(c *concurrent.Config) { c.Chat[0].Workers = 0 },
		"one input":       func(c *concurrent.Config) { c.Embed[0].Inputs = c.Embed[0].Inputs[:1] },
		"repeated input":  func(c *concurrent.Config) { c.Embed[0].Inputs = []string{"a", "b", "a"} },
		"swap of one":     func(c *concurrent.Config) { c.Chat = c.Chat[:1]; c.Chat[0].Workers = 1; c.InjectSwap = true },
		"chat no chatter": func(c *concurrent.Config) { c.Chat[0].Chatter = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base
			cfg.Chat = slices.Clone(base.Chat)
			cfg.Embed = slices.Clone(base.Embed)
			mutate(&cfg)
			if _, err := concurrent.Run(t.Context(), cfg); err == nil {
				t.Fatal("Run accepted the config")
			}
		})
	}
	if n := b.count.Load(); n != 0 {
		t.Errorf("the endpoints got %d chat requests, want 0", n)
	}
}

// idOf returns the ID of the request in rep that carried nonce.
func idOf(t *testing.T, rep concurrent.Report, nonce string) string {
	t.Helper()
	for _, c := range rep.Chats {
		if c.Nonce == nonce {
			return c.ID
		}
	}
	t.Fatalf("no request carried %s", nonce)
	return ""
}

func streamOf(id string) string { s, _, _ := strings.Cut(id, "/"); return s }

// assertFailures checks rep's failures are exactly want, each line matched by its prefix.
func assertFailures(t *testing.T, rep concurrent.Report, want []string) {
	t.Helper()
	got := rep.Failures()
	if len(got) != len(want) {
		t.Fatalf("failures:\n%s\nwant %d:\n%s", strings.Join(got, "\n"), len(want), strings.Join(want, "\n"))
	}
	for _, w := range want {
		if !slices.ContainsFunc(got, func(g string) bool { return strings.HasPrefix(g, w) }) {
			t.Errorf("no failure starts %q; failures:\n%s", w, strings.Join(got, "\n"))
		}
	}
}
