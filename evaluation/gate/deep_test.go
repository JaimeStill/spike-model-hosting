package gate_test

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
)

// fakeGuideLLM is a guidellm executable that records each call's arguments and HF_HOME, and
// answers each cell's scenario with testdata/guidellm/short-c4.json, a live run of gpt-oss-120b's
// short profile at 4 streams on the Framework trimmed to the figures evaluate reads, written where
// the scenario's output goes (beside it, <stem>.json).
type fakeGuideLLM struct {
	dir string
	// fail, when set, is a cell stem, such as agentic-c4, the fake exits 1 on.
	fail string
}

func newFakeGuideLLM(t *testing.T) *fakeGuideLLM { return &fakeGuideLLM{dir: t.TempDir()} }

// runner writes the fake's script as it is configured, and returns the GuideLLM that runs it.
func (f *fakeGuideLLM) runner() gate.GuideLLM {
	recorded, err := filepath.Abs("testdata/guidellm/short-c4.json")
	if err != nil {
		panic(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s|%%s\n' "$*" "$HF_HOME" >> %[1]q
[ "$1" = run ] && [ "$2" = -c ] || exit 2
scenario=$3
case $scenario in */%[2]s.scenario.json) exit 1 ;; esac
cp %[3]q "${scenario%%.scenario.json}.json"
`, filepath.Join(f.dir, "calls"), cmp.Or(f.fail, "none"), recorded)
	exe := filepath.Join(f.dir, "guidellm")
	if err := os.WriteFile(exe, []byte(script), 0o700); err != nil {
		panic(err)
	}
	return gate.GuideLLM{Exe: exe, HFHome: "/state/hf"}
}

// calls returns each call's arguments and HF_HOME, '|'-separated, in order.
func (f *fakeGuideLLM) calls(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "calls"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// near reports whether a and b agree to within 1e-9.
func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestDeepAgent runs gpt-oss-120b, set A's agent, through every stage to the deep stage: τ2
// airline and retail, each task twice, its user simulated through the pinned Claude Code, pass^1
// and pass^2 taken from the trials and recorded, never judged, with the simulator's calls rolled
// up; the GuideLLM grid on the model with the engine's own figures beside GuideLLM's; prefix
// reuse; and cold load, recorded not run without the architect's restart, naming its commands.
func TestDeepAgent(t *testing.T) {
	h := newHarness(t, gptOSS)
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
	if got := statuses(rep); !equal(got, want) || !rep.Passed || rep.Reached != gate.StageDeep {
		t.Fatalf("stages %q, passed %v, reached %q; want %q, passed, reached deep\n%+v", got, rep.Passed, rep.Reached, want, rep.Stage(gate.StageDeep))
	}

	// τ2: the recorded airline run passed task 22 twice, 37 never and 44 once, its simulator
	// called 26 times; retail 63 once, 99 twice and 102 once, 41 calls.
	for _, c := range []struct {
		domain       string
		pass1, pass2 float64
		calls        float64
	}{{"airline", 0.5, 1.0 / 3, 26}, {"retail", 2.0 / 3, 1.0 / 3, 41}} {
		ch := check(t, rep, gate.StageDeep, gate.RoleAgent, gate.Tau2Check(c.domain))
		v := ch.Values
		if !ch.Passed || ch.Floor != nil || !near(*ch.Measured, c.pass1) || !near(v["pass_1"], c.pass1) || !near(v["pass_2"], c.pass2) ||
			v["tasks"] != 3 || v["trials"] != 2 || v["samples"] != 6 || v["simulator_calls"] != c.calls || v["agent_errors"] != 0 ||
			v["simulator_errors"] != 0 || v["wall_s"] <= 0 {
			t.Errorf("%s %+v; want pass^1 %v and pass^2 %v over 3 tasks x 2 trials, recorded, unjudged", c.domain, ch, c.pass1, c.pass2)
		}
		if !strings.Contains(ch.Detail, fmt.Sprintf("Claude Code 2.1.301, reported claude-sonnet-5-5 (%v)", c.calls)) || !strings.Contains(ch.Detail, "tau2_"+c.domain+"@3-A") {
			t.Errorf("%s detail %q; want the simulator's Claude Code and reported model, and the task's version", c.domain, ch.Detail)
		}
	}
	tau2 := h.uv.deepCalls(t)
	if len(tau2) != 2 || !strings.Contains(tau2[0], "/tau2_quality.py eval ") {
		t.Fatalf("%d τ2 calls; want airline and retail:\n%s", len(tau2), strings.Join(tau2, "\n"))
	}
	for i, d := range gate.Tau2Domains {
		args, _, _ := strings.Cut(tau2[i], "|")
		for _, want := range []string{"--domain " + d, "--model " + gptOSS, "--trials 2", "--claude /pinned/claude",
			"--claude-config " + filepath.Join(h.uv.dir, "claude"), "agent-tau2-" + d + ".txt", "/deep/tau2/" + d} {
			if !strings.Contains(args, want) {
				t.Errorf("τ2 call %s lacks %q", args, want)
			}
		}
	}
	sim := check(t, rep, gate.StageDeep, gate.RoleAgent, "tau2 simulator")
	if *sim.Measured != 67 || sim.Values["retries"] != 0 || sim.Values["errors"] != 0 ||
		!strings.Contains(sim.Detail, "reported model: claude-sonnet-5-5 (67)") || !strings.Contains(sim.Detail, "claude -p, tools off") {
		t.Errorf("simulator %+v; want both domains' 67 calls rolled up, with the reported model", sim)
	}

	// The grid: four cells on the model, the prompt cut by its card's tokenizer, GuideLLM's
	// figures beside the engine's from /metrics before and after each cell.
	calls := h.guidellm.calls(t)
	if len(calls) != len(gate.GridCells) {
		t.Fatalf("%d GuideLLM calls; want %d", len(calls), len(gate.GridCells))
	}
	for i, cell := range gate.GridCells {
		args, hf, _ := strings.Cut(calls[i], "|")
		if hf != "/state/hf" || !strings.HasSuffix(args, "--disable-console") {
			t.Errorf("GuideLLM call %q, HF_HOME %q", args, hf)
		}
		scenario := strings.Fields(args)[2]
		var spec struct {
			Spec struct {
				Backend   struct{ Target, Model string }
				Tokenizer struct{ Model string }
				Profile   struct{ Streams int }
				Data      []struct {
					PromptTokens int `json:"prompt_tokens"`
					OutputTokens int `json:"output_tokens"`
				}
				Constraints []struct{ Count int }
			}
		}
		b, err := os.ReadFile(scenario)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &spec); err != nil {
			t.Fatal(err)
		}
		s := spec.Spec
		if s.Backend.Target != h.router.srv.URL || s.Backend.Model != gptOSS || s.Tokenizer.Model != "openai/gpt-oss-120b" ||
			s.Profile.Streams != cell.Streams || s.Data[0].PromptTokens != cell.PromptTokens || s.Data[0].OutputTokens != cell.OutputTokens ||
			s.Constraints[0].Count != cell.Requests {
			t.Errorf("%s scenario %+v", cell.Name(), s)
		}
		ch := check(t, rep, gate.StageDeep, "", cell.Name())
		v := ch.Values
		if !ch.Passed || !near(*ch.Measured, 68.2252252320765) || v["completed"] != 32 || v["errored"] != 0 ||
			!near(v["ttft_median_ms"], 3578.094720840454) || v["engine_prompt_tok_s"] != 500 || v["engine_gen_tok_s"] != 50 ||
			v["engine_cached_tokens"] != 50 {
			t.Errorf("%s %+v; want GuideLLM's figures and the engine's, recorded", cell.Name(), ch)
		}
	}

	// Prefix reuse: the fake caches all but a warm request's last ten words.
	p := check(t, rep, gate.StageDeep, "", "prefix reuse")
	if !p.Passed || p.Values["warm_prompt_n_max"] != 10 || p.Values["cold_prompt_n"] < gate.PrefixTokens || *p.Measured < 0.99 {
		t.Errorf("prefix reuse %+v; want the warm requests served from the cache", p)
	}

	// Cold load needs the architect's restart, and names the commands.
	cl := check(t, rep, gate.StageDeep, "", "cold load")
	if !cl.Passed || cl.Measured != nil || !strings.HasPrefix(cl.Detail, "not run: needs the architect's restart") ||
		!strings.Contains(cl.Detail, gate.ColdLoadWarm) || !strings.Contains(cl.Detail, gate.ColdLoadCold) {
		t.Errorf("cold load %+v; want not run, naming the restart commands", cl)
	}

	// Resumed from deep, the run reuses every gate and runs deep again.
	quality, set := len(h.uv.calls(t)), len(h.uv.setCalls(t))
	again := h.run(t, gate.StageDeep, rep)
	if got := statuses(again); !equal(got, want) || !again.Stage(gate.StageSet).Reused || again.Stage(gate.StageDeep).Reused {
		t.Errorf("resumed %q; want %q, set reused, deep run", got, want)
	}
	if len(h.uv.calls(t)) != quality || len(h.uv.setCalls(t)) != set || len(h.uv.deepCalls(t)) != 4 {
		t.Errorf("the resumed run reran an earlier stage, or not τ2")
	}
}

// TestDeepOnlyAfterGates shows the deep stage runs only once every gate passed: a failed set
// stage leaves it not run, and a run can't resume at it past that failure.
func TestDeepOnlyAfterGates(t *testing.T) {
	h := newHarness(t, gptOSS)
	// The vision model's answers cross in the set stage's rounds; capacity sends gpt-oss-120b's
	// alone.
	h.router.crossed = gemma26B
	rep := h.run(t, "", nil)
	if s := rep.Stage(gate.StageDeep); s.Status != gate.StatusNotRun || s.Reason != "set failed" {
		t.Fatalf("deep %+v after set %s; want not run, set failed", s, rep.Stage(gate.StageSet).Status)
	}
	if n := len(h.guidellm.calls(t)); n != 0 {
		t.Errorf("%d GuideLLM calls; want none", n)
	}
	if _, err := h.runner.Run(t.Context(), gate.StageDeep, rep); err == nil || !strings.Contains(err.Error(), "set fail") {
		t.Errorf("resuming at deep past a failed set: %v; want a refusal naming it", err)
	}
}

// TestDeepBroken shows a deep-stage measurement that couldn't be taken fails the stage, beside the
// ones that could: a τ2 domain whose run failed, one whose simulated user failed a call past its
// retries (the stock task carries on with a user who said nothing), and a GuideLLM cell that
// failed.
func TestDeepBroken(t *testing.T) {
	h := newHarness(t, gptOSS)
	h.uv.fail = "retail"
	// The airline recording, its first sample's simulator call failed.
	testdata := t.TempDir()
	if err := os.CopyFS(testdata, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	summary := filepath.Join(testdata, "tau2", "airline", gate.Tau2Summary)
	var log map[string]any
	b, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &log); err != nil {
		t.Fatal(err)
	}
	log["samples"].([]any)[0].(map[string]any)["simulator_errors"] = 1
	if b, err = json.Marshal(log); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(summary, b, 0o600); err != nil {
		t.Fatal(err)
	}
	h.uv.testdata = testdata
	h.runner.Tau2 = h.uv.runner()
	h.guidellm.fail = "agentic-c4"
	h.runner.Grid = h.guidellm.runner()

	rep := h.run(t, "", nil)
	if s := rep.Stage(gate.StageDeep); s.Status != gate.StatusFail || rep.Passed {
		t.Fatalf("deep %s, passed %v; want a failed deep stage", s.Status, rep.Passed)
	}
	airline := check(t, rep, gate.StageDeep, gate.RoleAgent, gate.Tau2Check("airline"))
	if airline.Passed || !strings.Contains(airline.Detail, "1 of the simulated user's calls failed") || airline.Values["pass_1"] != 0.5 {
		t.Errorf("airline %+v; want a measurement that couldn't be taken, its pass^1 still recorded", airline)
	}
	if retail := check(t, rep, gate.StageDeep, gate.RoleAgent, gate.Tau2Check("retail")); retail.Passed || !strings.Contains(retail.Detail, "couldn't measure") {
		t.Errorf("retail %+v; want a measurement that couldn't be taken", retail)
	}
	if sim := check(t, rep, gate.StageDeep, gate.RoleAgent, "tau2 simulator"); *sim.Measured != 26 || sim.Values["errors"] != 1 {
		t.Errorf("simulator %+v; want airline's 26 calls, one failed", sim)
	}
	for _, cell := range gate.GridCells {
		c := check(t, rep, gate.StageDeep, "", cell.Name())
		if c.Passed != (cell.Profile+fmt.Sprint(cell.Streams) != "agentic4") {
			t.Errorf("%s passed %v; want only agentic at 4 streams failed", cell.Name(), c.Passed)
		}
	}
	check(t, rep, gate.StageDeep, "", "prefix reuse")
}

// TestDeepColdLoad shows cold load, with the architect at hand, asking for each restart with its
// command, warm and then with the page cache dropped, and timing each until every model of the
// set is loaded again. The embedding model's deep stage is cold load alone: no τ2, no grid.
func TestDeepColdLoad(t *testing.T) {
	h := newHarness(t, embedding)
	h.runner.Restarts = true
	log := &restartLog{router: h.router}
	h.runner.Log = log
	rep := h.run(t, "", nil)
	if s := rep.Stage(gate.StageDeep); s.Status != gate.StatusPass || len(s.Checks) != 2 {
		t.Fatalf("deep %+v; want cold load's two restarts alone, passed", s)
	}
	for i, kind := range []string{"warm", "cold"} {
		c := check(t, rep, gate.StageDeep, "", "cold load "+kind)
		if c.Measured == nil || *c.Measured <= 0 || c.Values["router_up_s"] <= 0 || c.Values["embedding_loaded_s"] <= 0 ||
			c.Values["agent_loaded_s"] > *c.Measured || len(c.Values) != 5 {
			t.Errorf("cold load %s %+v; want each role's model timed loaded again", kind, c)
		}
		command := []string{gate.ColdLoadWarm, gate.ColdLoadCold}[i]
		if !strings.Contains(c.Detail, command) || !strings.Contains(log.text(), "restart the router now, on its host: "+command) {
			t.Errorf("cold load %s %q; want it asked for and recorded with `%s`", kind, c.Detail, command)
		}
	}
	if len(h.guidellm.calls(t)) != 0 || len(h.uv.deepCalls(t)) != 0 {
		t.Error("the embedding model's deep stage ran τ2 or the grid")
	}
}

// restartLog is a run's log that restarts the fake router each time the run asks for a restart,
// as the architect would.
type restartLog struct {
	router *fakeRouter
	mu     sync.Mutex
	b      strings.Builder
}

func (l *restartLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.Contains(string(p), "restart the router now") {
		go l.router.restart(20 * time.Millisecond)
	}
	return l.b.Write(p)
}

func (l *restartLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
