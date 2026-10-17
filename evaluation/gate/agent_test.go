package gate_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// recordedAgent are each BFCL fixture's scored samples, passes and model requests, as
// testdata/agent/<tier>'s Inspect log holds them; the samples are testdata/tiers/agent-<tier>.txt's.
// Each log is gpt-oss-120b's gated run's on the Framework with the events, messages and
// attachments dropped from its samples and every sample but the fixture's taken out: the first
// ten of the single-turn tier list and the first four of the multi-turn one.
// testdata/agent/bfcl-single-stopped is a run of its own of the same ten single-turn questions
// with a bar of 0.99, which early stopping ended, its early_stop.json beside the log. The logs'
// results keep the whole run's metrics, which ReadTier doesn't read.
var recordedAgent = map[string]struct{ samples, passes, requests int }{
	"bfcl-single":         {10, 7, 10},
	"bfcl-multi":          {4, 0, 49},
	"bfcl-single-stopped": {5, 3, 5},
}

// agentChecks are the checks the agent role's quality stage makes when every tier runs: one a
// BFCL tier, then one a consumer of the conform tier.
var agentChecks = []string{"agent-bfcl-single", "agent-bfcl-multi", "agent-conform-pi", "agent-conform-claude", "agent-conform-opencode"}

// checkNames returns the names of a stage's checks, in order.
func checkNames(s *gate.StageResult) []string {
	var names []string
	for _, c := range s.Checks {
		names = append(names, c.Name)
	}
	return names
}

// TestQualityAgent runs the agent model's quality stage with no bars: BFCL's single-turn tier,
// then its multi-turn tier, through uv, each scored from Inspect's log and recorded with its
// samples, passes, requests, data and releases, passing with no bar; then conform's pass^5
// through the consumers, each consumer judged against the comparison row, which the fake's
// consumers match. The conform run gets the set's roles, gpt-oss-120b's consumer reasoning, the
// row's k and the run directory's recordings, and leaves its table there.
func TestQualityAgent(t *testing.T) {
	h := newHarness(t, gptOSS)
	rep := h.run(t, "", nil)
	q := rep.Stage(gate.StageQuality)
	if q == nil || q.Status != gate.StatusPass || !equal(checkNames(q), agentChecks) {
		t.Fatalf("quality %+v; want passed, checks %q", q, agentChecks)
	}
	for i, at := range gate.AgentTiers[:2] {
		c, r := q.Checks[i], recordedAgent[at.Name]
		score := float64(r.passes) / float64(r.samples)
		if c.Role != gate.RoleAgent || c.Measured == nil || *c.Measured != score || c.Unit != "accuracy" || c.Floor != nil || !c.Passed {
			t.Errorf("%s %+v; want accuracy %v, no bar, passed", c.Name, c, score)
		}
		if c.Values["samples"] != float64(r.samples) || c.Values["passes"] != float64(r.passes) ||
			c.Values["requests"] != float64(r.requests) || c.Values["tier"] != float64(at.Tier) || c.Values["working_s"] <= 0 {
			t.Errorf("%s values %v; want %d samples, %d passed, %d requests, tier %d, working time", c.Name, c.Values, r.samples, r.passes, r.requests, at.Tier)
		}
		for _, w := range []string{fmt.Sprintf("tier %d", at.Tier), "no bar: recorded, never fails", "parallel tool calls allowed",
			"ShishirPatil/gorilla@dac44e7ac9db5ff26a01ab0c1ec5de5a1e703b7a", "inspect_evals " + gate.InspectEvalsVersion + " inspect_evals/bfcl@8-B", "inspect " + gate.InspectVersion} {
			if !strings.Contains(c.Detail, w) {
				t.Errorf("%s detail %q lacks %q", c.Name, c.Detail, w)
			}
		}
	}

	calls := h.uv.calls(t)
	if len(calls) != 2 {
		t.Fatalf("uv calls %q; want one a BFCL tier", calls)
	}
	for i, at := range gate.AgentTiers[:2] {
		want := fmt.Sprintf("run --project /module/evaluation/suites --locked --no-python-downloads --python /pinned/python python "+
			"/module/evaluation/suites/agent_quality.py eval --router %s --model %s --tier %s --list %s --output %s|/state/cache/huggingface|/state/cache/mteb|/state/cache/asr|/state/cache/inspect_evals",
			h.router.srv.URL, gptOSS, at.Name, filepath.Join(h.runner.Tiers, gate.AgentCheck(at.Name)+".txt"), filepath.Join(h.runner.Dir, "agent", at.Name, "logs"))
		if calls[i] != want {
			t.Errorf("call %d:\n got %s\nwant %s", i, calls[i], want)
		}
	}

	if len(h.conform.plans) != 1 {
		t.Fatalf("conform ran %d times; want once", len(h.conform.plans))
	}
	plan := h.conform.plans[0]
	if plan.Router != h.router.srv.URL || plan.Roles != h.runner.Set.Roles || plan.K != 5 || !equal(plan.Consumers, passk.ConsumerNames) ||
		plan.Wire != filepath.Join(h.runner.Dir, "conform", "wire") || plan.Stop == nil {
		t.Errorf("plan %+v; want the set's roles, k 5, every consumer, the run's recordings and the row's stop", plan)
	}
	if plan.Reasoning.Pi != h.runner.Profile.Model(gptOSS).Consumers.Pi {
		t.Errorf("reasoning %+v; want gpt-oss-120b's consumer reasoning", plan.Reasoning)
	}
	pi := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-pi")
	if !pi.Passed || *pi.Measured != 9 || *pi.Floor != 9 || pi.Op != gate.AtLeast || pi.Values["cell_audio-tool"] != 5 || pi.Values["tier"] != 3 {
		t.Errorf("pi %+v; want 9 of 9 against the row's 9, passed", pi)
	}
	claude := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-claude")
	if !claude.Passed || *claude.Measured != 6 || *claude.Floor != 6 || !strings.Contains(claude.Detail, "tool 0/5 (row 0/5)") {
		t.Errorf("claude %+v; want 6 of 9 against the row's 6, passed, its cells beside the row's", claude)
	}
	opencode := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-opencode")
	if !opencode.Passed || opencode.Floor != nil || *opencode.Measured != 8 || opencode.Values["invalid"] != 3 || !strings.Contains(opencode.Detail, "measured, not gated") {
		t.Errorf("opencode %+v; want 8 of 9 and 3 invalid recorded, not gated", opencode)
	}
	table, err := os.ReadFile(filepath.Join(h.runner.Dir, "conform", gate.ConformReport))
	if err != nil || !strings.Contains(string(table), "| pi | pi 1.0.0 | 5/5 |") || !strings.Contains(string(table), "b11669-620d2225c") {
		t.Errorf("conform table %q, %v; want the consumers' cells and the row's build", table, err)
	}
}

// TestQualityAgentBars judges a BFCL tier's accuracy against its floor: a tier under its floor
// passes it to agent_quality.py, whose early stop ends the tier once the pass rate's upper bound
// is below it, failing the check, the stage and the run before the next tier and conform; a tier
// over its floor runs in full and passes.
func TestQualityAgentBars(t *testing.T) {
	h := newHarness(t, gptOSS)
	h.runner.Bars = tier.Bars{"agent-bfcl-single": {Value: 0.99}}
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality fail", "set not run", "deep not run"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q", got, want)
	}
	q := rep.Stage(gate.StageQuality)
	r := recordedAgent["bfcl-single-stopped"]
	if len(q.Checks) != 1 {
		t.Fatalf("checks %q; want the single-turn tier's alone, the stage ended there", checkNames(q))
	}
	c := q.Checks[0]
	if c.Passed || *c.Floor != 0.99 || c.Values["samples"] != float64(r.samples) || !strings.Contains(c.Detail, fmt.Sprintf("early stop after %d: the 95%% upper bound", r.samples)) {
		t.Errorf("agent-bfcl-single %+v; want stopped early after %d, failed against >= 0.99", c, r.samples)
	}
	if _, ok := c.Values["pass_rate_upper_bound"]; !ok {
		t.Errorf("values %v; want the bound", c.Values)
	}
	if calls := h.uv.calls(t); len(calls) != 1 || !strings.HasSuffix(strings.Split(calls[0], "|")[0], "--bar 0.99") {
		t.Errorf("calls %q; want the single-turn tier's alone, with its bar", calls)
	}
	if len(h.conform.plans) != 0 {
		t.Error("conform ran after a BFCL tier failed")
	}

	h = newHarness(t, gptOSS)
	h.runner.Bars = tier.Bars{"agent-bfcl-multi": {Value: 0}}
	rep = h.run(t, "", nil)
	if c := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-bfcl-multi"); !c.Passed || *c.Floor != 0 || !strings.Contains(c.Detail, "early stop: ran in full") {
		t.Errorf("agent-bfcl-multi %+v; want a pass against >= 0, run in full", c)
	}
	if !rep.Passed {
		t.Errorf("run failed; want passed\n%+v", rep.Stages)
	}
}

// TestQualityAgentConform judges each gated consumer cell by cell against the comparison row:
// Claude Code losing one audio-tool trial, a cell the row holds at 5/5, fails it and the stage,
// naming the cell; OpenCode doing worse than its row is recorded and never fails.
func TestQualityAgentConform(t *testing.T) {
	h := newHarness(t, gptOSS)
	lost := conformReport("claude", map[string]conform.Status{"tool": conform.Fail, "skill": conform.Fail, "dropped-image": conform.NA, "audio-tool": conform.Fail})
	h.conform.trials["claude"][3] = passk.Trial{Report: lost}
	for i := range h.conform.trials["opencode"] {
		h.conform.trials["opencode"][i] = passk.Trial{Report: conformReport("opencode", map[string]conform.Status{"vision": conform.Fail})}
	}
	rep := h.run(t, "", nil)
	q := rep.Stage(gate.StageQuality)
	if q.Status != gate.StatusFail || !equal(checkNames(q), agentChecks) {
		t.Fatalf("quality %+v; want failed, every check made", q)
	}
	claude := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-claude")
	if claude.Passed || *claude.Measured != 5 || !strings.Contains(claude.Detail, "under the row: audio-tool 4/5 under the row's 5/5") {
		t.Errorf("claude %+v; want failed on audio-tool", claude)
	}
	if c := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-opencode"); !c.Passed || *c.Measured != 8 {
		t.Errorf("opencode %+v; want recorded, 8 of 9, passed", c)
	}
	if c := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-pi"); !c.Passed {
		t.Errorf("pi %+v; want passed", c)
	}
}

// TestQualityAgentConformStops runs the conform tier on passk's own runner with consumers whose
// executables don't exist, as evaluate conform's tests do: every trial is skipped and sends
// nothing, so after the first trial, the screening fifth, Pi can't reach its row and the tier
// stops. Pi fails, out of reach; Claude Code fails too, its evidence cut short; OpenCode is
// recorded.
func TestQualityAgentConformStops(t *testing.T) {
	h := newHarness(t, gptOSS)
	state := t.TempDir()
	h.runner.Conform = passk.Runner{
		Commands: map[string]string{"pi": "/nonexistent/pi", "claude": "/nonexistent/claude", "opencode": "/nonexistent/opencode"},
		State:    filepath.Join(state, "conform"), ClaudeConfig: filepath.Join(state, "claude"),
	}
	rep := h.run(t, "", nil)
	q := rep.Stage(gate.StageQuality)
	if q.Status != gate.StatusFail || !equal(checkNames(q), agentChecks) {
		t.Fatalf("quality %+v; want failed, a check a consumer", q)
	}
	pi := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-pi")
	if pi.Passed || pi.Values["trials"] != 1 || !strings.Contains(pi.Detail, "stopped early: pi's exchange can reach at most 4/5 after 1 trials") {
		t.Errorf("pi %+v; want failed, stopped after its first trial", pi)
	}
	if c := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-claude"); c.Passed || !strings.Contains(c.Detail, "stopped early") {
		t.Errorf("claude %+v; want failed, stopped early", c)
	}
	if c := check(t, rep, gate.StageQuality, gate.RoleAgent, "agent-conform-opencode"); !c.Passed {
		t.Errorf("opencode %+v; want recorded", c)
	}
	table, err := os.ReadFile(filepath.Join(h.runner.Dir, "conform", gate.ConformReport))
	if err != nil || !strings.Contains(string(table), "stopped early: pi's exchange") || !strings.Contains(string(table), "pi skipped a trial") {
		t.Errorf("conform table %q, %v; want the stop and the skips", table, err)
	}
}

// TestQualityAgentBroken shows a BFCL tier uv fails on or whose log doesn't match its list, a
// conform run that errs, or a set model unloaded before conform fails its check, naming why, and
// ends the stage there.
func TestQualityAgentBroken(t *testing.T) {
	cases := map[string]struct {
		setup func(*harness)
		check string
		want  string
	}{
		"uv fails": {func(h *harness) { h.uv.fail = "bfcl-multi"; h.runner.Agent = h.uv.runner() },
			"agent-bfcl-multi", "uv run agent_quality.py eval --tier bfcl-multi"},
		"list differs": {func(h *harness) { h.runner.Tiers = withExtraID(t, h.runner.Tiers, "agent-bfcl-single") },
			"agent-bfcl-single", "aren't the tier's"},
		"a ceiling": {func(h *harness) { h.runner.Bars = tier.Bars{"agent-bfcl-single": {Value: 0.5, AtMost: true}} },
			"agent-bfcl-single", "its bar belongs under [bars]"},
		"conform errs":       {func(h *harness) { h.conform.err = errors.New("no cell") }, "agent-conform", "no cell"},
		"set model unloaded": {func(h *harness) { h.router.status[gemmaE4B] = "unloaded" }, "agent-conform", gemmaE4B},
		"no comparison":      {func(h *harness) { h.runner.Comparison = nil }, "agent-conform", "no conform runner, comparison row"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, gptOSS)
			tc.setup(h)
			rep := h.run(t, "", nil)
			q := rep.Stage(gate.StageQuality)
			if q.Status != gate.StatusFail {
				t.Fatalf("quality %+v; want failed", q)
			}
			last := q.Checks[len(q.Checks)-1]
			if last.Name != tc.check || last.Passed || !strings.Contains(last.Detail, tc.want) {
				t.Errorf("last check %+v; want %s failed, naming %q", last, tc.check, tc.want)
			}
		})
	}
}
