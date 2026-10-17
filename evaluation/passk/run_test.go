package passk_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// TestRunDrops runs harnesses whose executables don't exist, so every trial is skipped and sends
// nothing: with Drop, each harness drops at its first trial, which fails the bar, and the run
// ends there, each trial's recording saved and every harness judged as not fitting. Without
// Drop the run makes every trial.
func TestRunDrops(t *testing.T) {
	state, wireDir := t.TempDir(), t.TempDir()
	r := passk.Runner{
		Commands: map[string]string{"pi": "/nonexistent/pi", "claude": "/nonexistent/claude", "opencode": "/nonexistent/opencode"},
		State:    state, ClaudeConfig: t.TempDir(),
	}
	plan := passk.Plan{
		Router: "http://127.0.0.1:1", Roles: profile.SetRoles{Chat: "chat", Vision: "vision", Audio: "audio", Embedding: "embed"},
		K: 5, Harnesses: passk.Harnesses, Wire: wireDir, Drop: true,
	}
	res, err := r.Run(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fits) != 3 || len(res.Rows) != 3 {
		t.Fatalf("fits %+v; want one a harness", res.Fits)
	}
	for i, f := range res.Fits {
		if f.Row.Consumer != passk.ConsumerNames[i] || f.Row.K != 1 || f.Fits || f.Trial != 1 || len(f.Row.Skipped) != 1 {
			t.Errorf("%s: %+v; want dropped at its first trial, skipped", f.Row.Consumer, f)
		}
		if _, ok := res.Wall[f.Row.Consumer]; !ok {
			t.Errorf("%s: no wall time", f.Row.Consumer)
		}
	}
	if entries, err := os.ReadDir(wireDir); err != nil || len(entries) != 3 {
		t.Errorf("recordings %v, %v; want one a trial, 3", entries, err)
	}

	pi, _ := passk.LookupHarness("pi")
	plan.Drop, plan.K, plan.Harnesses = false, 2, []passk.Harness{pi}
	res, err = r.Run(t.Context(), plan)
	if err != nil || len(res.Rows) != 1 || res.Rows[0].K != 2 || res.Fits[0].Trial != 1 {
		t.Errorf("no Drop: %+v, %v; want Pi's 2 trials, judged failed at the first", res, err)
	}
}

// scripted is a run of harnesses with scripted trials: each harness's cell records the router
// URL its trials send to, and each trial's report is script's for that harness and trial, which
// may send model requests to it first. calls counts each harness's trials.
type scripted struct {
	routers map[string]string
	calls   map[string]int
}

// harnesses returns a Harness a name, its cell carrying no harness, only its name.
func (s *scripted) harnesses(names ...string) []passk.Harness {
	var hs []passk.Harness
	for _, name := range names {
		hs = append(hs, passk.Harness{Name: name, Cell: func(cfg passk.Config) (conform.Cell, error) {
			s.routers[name] = cfg.Router
			return conform.Cell{Harness: name, Provider: "llama.cpp"}, nil
		}})
	}
	return hs
}

// runner returns a Runner whose trials are script's, by harness and trial from 1.
func (s *scripted) runner(t *testing.T, script func(router, harness string, trial int) conform.Report) passk.Runner {
	r := passk.Runner{State: t.TempDir(), ClaudeConfig: t.TempDir()}
	return passk.WithConform(r, func(_ context.Context, c conform.Cell, _ conform.Options) conform.Report {
		s.calls[c.Harness]++
		return script(s.routers[c.Harness], c.Harness, s.calls[c.Harness])
	})
}

// TestRunDropsEach drops each harness at its own first failed trial while the others run on: a
// passes every trial and fits; b fails tool in trial 1 and runs no more; c passes trial 1 and
// then sends a model request the router answers with 500, and drops at trial 2. The run ends
// once a has run all k, and records each harness's wall time. When every harness fails its first
// trial, the run ends after that trial, none fitting.
func TestRunDropsEach(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"does not match the expected peg-native format"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	s := &scripted{routers: map[string]string{}, calls: map[string]int{}}
	r := s.runner(t, func(router, h string, trial int) conform.Report {
		switch {
		case h == "b":
			return report(map[string]conform.Status{"tool": conform.Fail})
		case h == "c" && trial == 2:
			resp, err := http.Post(router+"/v1/chat/completions", "application/json", strings.NewReader("{}"))
			if err == nil {
				_ = resp.Body.Close()
			}
		}
		return report(nil)
	})
	plan := passk.Plan{Router: upstream.URL, K: 5, Harnesses: s.harnesses("a", "b", "c"), Wire: t.TempDir(), Drop: true}
	res, err := r.Run(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if s.calls["a"] != 5 || s.calls["b"] != 1 || s.calls["c"] != 2 {
		t.Errorf("trials %v; want a 5, b 1, c 2", s.calls)
	}
	a, b, c := res.Fits[0], res.Fits[1], res.Fits[2]
	if !a.Fits || a.Row.K != 5 {
		t.Errorf("a %+v; want fits after 5 trials", a)
	}
	if b.Fits || b.Trial != 1 || b.Row.K != 1 || !strings.HasPrefix(b.Failures[0], "tool FAIL") {
		t.Errorf("b %+v; want dropped at trial 1 on tool", b)
	}
	if c.Fits || c.Trial != 2 || c.Row.Invalid != 1 || !strings.Contains(c.Failures[0], "invalid: POST /v1/chat/completions -> 500") {
		t.Errorf("c %+v; want dropped at trial 2 on its invalid request", c)
	}
	for _, name := range []string{"a", "b", "c"} {
		if res.Wall[name] <= 0 {
			t.Errorf("%s: wall %v; want its trials' time", name, res.Wall[name])
		}
	}

	s = &scripted{routers: map[string]string{}, calls: map[string]int{}}
	r = s.runner(t, func(_, _ string, _ int) conform.Report {
		return report(map[string]conform.Status{"vision": conform.Fail})
	})
	plan.Harnesses = s.harnesses("a", "b", "c")
	if res, err = r.Run(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if s.calls["a"] != 1 || s.calls["b"] != 1 || s.calls["c"] != 1 || slices.ContainsFunc(res.Fits, func(f passk.Fit) bool { return f.Fits }) {
		t.Errorf("all failing: trials %v, fits %+v; want one trial each, none fitting", s.calls, res.Fits)
	}
}
