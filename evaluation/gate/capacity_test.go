package gate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
)

// agenticBodies returns the model's chat completions in the agentic shape: 512 out, as capacity's
// agent streams send them.
func (f *fakeRouter) agenticBodies(model string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, b := range f.nonced[model] {
		if b["max_tokens"] == 512.0 {
			out = append(out, b)
		}
	}
	return out
}

// TestCapacityAtN shows capacity puts each role model's own slots in flight, N from its profile
// entry and not a fixed 4, and judges each of the N against the role's floor at N: the agent's
// streams in the agentic shape by their decode, the audio role's transcriptions by their RTFx,
// the vision role's image questions and the embedding role's 32-token queries, N at once, by
// fitness's floors; no request ran out of context, and the pool's peak is read with N in flight.
func TestCapacityAtN(t *testing.T) {
	for _, n := range []int{2, 8} {
		for model, role := range map[string]string{gptOSS: gate.RoleAgent, gemma26B: gate.RoleVision, gemmaE4B: gate.RoleAudio, embedding: gate.RoleEmbedding} {
			t.Run(fmt.Sprintf("%s at %d", role, n), func(t *testing.T) {
				h := newHarness(t, model)
				h.runner.Profile.Model(model).Slots = n
				rep := h.run(t, "", nil)
				if s := rep.Stage(gate.StageCapacity); s.Status != gate.StatusPass {
					t.Fatalf("capacity %s; want pass\n%+v", s.Status, s.Checks)
				}
				name := map[string]string{gate.RoleAgent: "decode at N", gate.RoleVision: "image answer at N",
					gate.RoleAudio: "rtfx at N", gate.RoleEmbedding: "query 32 at N"}[role]
				c := check(t, rep, gate.StageCapacity, role, name)
				if c.Values["slots"] != float64(n) || c.Measured == nil || c.Floor == nil {
					t.Errorf("%s %+v; want judged at %d in flight", name, c, n)
				}
				key := "stream%d"
				if role == gate.RoleVision || role == gate.RoleEmbedding {
					key = "request%d"
				}
				for i := 1; i <= n; i++ {
					if _, ok := c.Values[fmt.Sprintf(key, i)]; !ok {
						t.Errorf("%s values %v lack %s: want each of the %d judged", name, c.Values, fmt.Sprintf(key, i), n)
					}
				}
				if _, ok := c.Values[fmt.Sprintf(key, n+1)]; ok {
					t.Errorf("%s values %v: more than %d requests", name, c.Values, n)
				}
				if role != gate.RoleEmbedding {
					if x := check(t, rep, gate.StageCapacity, role, "no crossed nonce"); !strings.HasPrefix(x.Detail, fmt.Sprintf("%d answers in flight", n)) {
						t.Errorf("capacity's nonce check %q; want %d answers in flight", x.Detail, n)
					}
				}
				if x := check(t, rep, gate.StageCapacity, role, "context held"); !x.Passed || x.Values["slots"] != float64(n) {
					t.Errorf("context held %+v; want passed at %d", x, n)
				}
				if p := check(t, rep, gate.StageCapacity, role, "peak pool free"); !p.Passed || p.Values["slots"] != float64(n) {
					t.Errorf("peak pool free %+v; want passed at %d in flight", p, n)
				}
				if role == gate.RoleAgent {
					// Each agent stream is the agentic shape: an 8192-token prompt (the fake's
					// tokens are words), the nonce request after it, 512 out.
					bodies := h.router.agenticBodies(gptOSS)
					if len(bodies) != n {
						t.Fatalf("%d agentic requests; want %d", len(bodies), n)
					}
					for _, b := range bodies {
						prompt := b["messages"].([]any)[0].(map[string]any)["content"].(string)
						if words := len(strings.Fields(prompt)); words < 8192 || !fakeNonce.MatchString(prompt) {
							t.Errorf("agentic prompt of %d words; want 8192 and more, carrying its nonce", words)
						}
					}
				}
			})
		}
	}
}

// TestCapacityContextExhausted shows a request that runs out of context fails capacity, by a
// check of its own naming the request, and the run stops there.
func TestCapacityContextExhausted(t *testing.T) {
	h := newHarness(t, gptOSS)
	h.router.exhaust = gptOSS
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity fail", "quality not run", "set not run", "deep not run"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q", got, want)
	}
	c := check(t, rep, gate.StageCapacity, gate.RoleAgent, "context held")
	if c.Passed || c.Values["exhausted"] != 4 || !strings.Contains(c.Detail, "4 ran out of context with 4 in flight") ||
		!strings.Contains(c.Detail, "agent/w0/r0") {
		t.Errorf("context held %+v; want a failure naming each of the 4 requests", c)
	}
}

// TestSetStageAtOwnSlots shows a set run's stages run each model at its own slots, from its
// profile entry: each suite that keeps requests in flight, in the quality stage and the set
// stage's phases alike, is given its model's slots as --slots (the ASR task, one clip at a time,
// none); and the run states the set's sessions supported, its agent model's slots, confirmed by
// the passed set stage.
func TestSetStageAtOwnSlots(t *testing.T) {
	slots := map[string]int{gptOSS: 2, gemma26B: 3, gemmaE4B: 5, embedding: 6}
	h := newHarness(t, "")
	for m, n := range slots {
		h.runner.Profile.Model(m).Slots = n
	}
	h.runner.Full = true
	rep := h.run(t, "", nil)
	if s := rep.Stage(gate.StageSet); s == nil || s.Status != gate.StatusPass {
		t.Fatalf("stages %q; want the set stage passed", statuses(rep))
	}
	script := regexp.MustCompile(`/(\w+)_quality\.py`)
	model := regexp.MustCompile(`--model (\S+)`)
	given := regexp.MustCompile(`--slots (\d+)`)
	calls := append(h.uv.calls(t), h.uv.setCalls(t)...)
	if len(h.uv.setCalls(t)) == 0 {
		t.Fatal("the set stage ran no suite")
	}
	for _, c := range calls {
		args, _, _ := strings.Cut(c, "|")
		s, m, g := script.FindStringSubmatch(args), model.FindStringSubmatch(args), given.FindStringSubmatch(args)
		switch {
		case s == nil || m == nil:
			t.Errorf("call %s: no suite or model", args)
		case s[1] == "asr":
			if g != nil {
				t.Errorf("ASR call %s: --slots %s; want none, one clip at a time", args, g[1])
			}
		case g == nil || g[1] != fmt.Sprint(slots[m[1]]):
			t.Errorf("call %s: want --slots %d, %s's slots", args, slots[m[1]], m[1])
		}
	}

	want := gate.Sessions{Supported: 2, Model: gptOSS, Confirmed: true, Basis: "the set stage passed with every model at its own slots"}
	if rep.Sessions == nil || *rep.Sessions != want {
		t.Errorf("sessions %+v; want %+v", rep.Sessions, want)
	}
	var sum bytes.Buffer
	if err := gate.WriteSummary(&sum, rep); err != nil {
		t.Fatal(err)
	}
	if line := "sessions supported: 2, " + gptOSS + "'s slots; confirmed: the set stage passed"; !strings.Contains(sum.String(), line) {
		t.Errorf("summary lacks %q:\n%s", line, sum.String())
	}
}

// TestSessionsUnconfirmed shows a set run's screen states its sessions supported unconfirmed,
// since its set stage is the full run's, and a model run states none.
func TestSessionsUnconfirmed(t *testing.T) {
	h := newHarness(t, "")
	rep := h.run(t, "", nil)
	if s := rep.Sessions; s == nil || s.Supported != 4 || s.Confirmed || !strings.Contains(s.Basis, "the set stage didn't run: a screen") {
		t.Errorf("a screen's sessions %+v; want 4, unconfirmed, the set stage a full run's", s)
	}
	var sum bytes.Buffer
	if err := gate.WriteSummary(&sum, rep); err != nil {
		t.Fatal(err)
	}
	if line := "sessions supported: 4, " + gptOSS + "'s slots; not confirmed: "; !strings.Contains(sum.String(), line) {
		t.Errorf("summary lacks %q:\n%s", line, sum.String())
	}
	if rep := newHarness(t, gptOSS).run(t, "", nil); rep.Sessions != nil {
		t.Errorf("a model run's sessions %+v; want none", rep.Sessions)
	}
}

// cancelOn is a run's log that cancels the run when a line holding line is written.
type cancelOn struct {
	line   string
	cancel context.CancelFunc
}

func (c cancelOn) Write(b []byte) (int, error) {
	if strings.Contains(string(b), c.line) {
		c.cancel()
	}
	return len(b), nil
}

// TestSessionsInterrupted shows a set run interrupted once its quality stage begins still states
// its sessions supported, its agent model's slots, unconfirmed: the run didn't reach the set
// stage.
func TestSessionsInterrupted(t *testing.T) {
	h := newHarness(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.runner.Log = cancelOn{line: "evaluate: quality ", cancel: cancel}
	rep, err := h.runner.Run(ctx, "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v; want cancelled", err)
	}
	if s := rep.Sessions; s == nil || s.Supported != 4 || s.Model != gptOSS || s.Confirmed || s.Basis != "the run didn't reach the set stage" {
		t.Errorf("an interrupted run's sessions %+v; want 4, gpt-oss-120b's, unconfirmed", s)
	}
}
