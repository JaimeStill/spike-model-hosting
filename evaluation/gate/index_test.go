package gate_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// indexed gives h's runner a results index under a state root of its own, and returns the root.
func (h *harness) indexed(t *testing.T) string {
	t.Helper()
	state := t.TempDir()
	index, err := gate.LoadIndex(state)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.Index = index
	return state
}

// runAs runs h's runner in the run directory name under state's evaluate/, then saves the index
// and reads it back from disk, as the next evaluate would.
func (h *harness) runAs(t *testing.T, state, name, from string, prior *gate.Report) *gate.Report {
	t.Helper()
	h.runner.Dir = filepath.Join(state, "evaluate", name)
	rep := h.run(t, from, prior)
	if err := h.runner.Index.Save(); err != nil {
		t.Fatal(err)
	}
	index, err := gate.LoadIndex(state)
	if err != nil {
		t.Fatal(err)
	}
	h.runner.Index = index
	return rep
}

// reusedFrom returns, for each of the stage's checks, "<model> <role> <name> <run>", the run being
// the one the check was reused from, or "-" for a check measured in the report's run.
func reusedFrom(rep *gate.Report, stage string) []string {
	var out []string
	if s := rep.Stage(stage); s != nil {
		for _, c := range s.Checks {
			out = append(out, strings.Join([]string{c.Model, c.Role, c.Name, cmpOr(c.ReusedFrom, "-")}, " "))
		}
	}
	return out
}

func cmpOr(s, or string) string {
	if s == "" {
		return or
	}
	return s
}

// wantReused fails t unless every check of stage is reused from run when keep says so of it, and
// measured in the report's run otherwise.
func wantReused(t *testing.T, rep *gate.Report, stage, run string, keep func(gate.Check) bool) {
	t.Helper()
	s := rep.Stage(stage)
	if s == nil || len(s.Checks) == 0 {
		t.Fatalf("no %s checks in %q", stage, statuses(rep))
	}
	for _, c := range s.Checks {
		want := ""
		if keep(c) {
			want = run
		}
		if c.ReusedFrom != want {
			t.Errorf("%s %s %s %s: reused from %q; want %q", stage, c.Model, c.Role, c.Name, c.ReusedFrom, want)
		}
	}
}

// byRole keeps a check that judges a role: every one but the gates of the whole set's pool.
func byRole(c gate.Check) bool { return c.Role != "" }

// TestIndexReusesSameKeys runs a set run's screen twice on the same keys: the second reuses each
// model's fitness and capacity records and every quality unit, from the index alone once the first
// run's directory is removed, and says so, naming the run; it measures the pool's gates over the
// whole set again (fitness's warmed peak, the quality pass's two gates, over one crossing round),
// and sends no suite, tool call or harness trial.
func TestIndexReusesSameKeys(t *testing.T) {
	h := newHarness(t, "")
	state := h.indexed(t)
	first := h.runAs(t, state, "run1", "", nil)
	if got := statuses(first); !equal(got, screenStatuses) || first.Keys == nil || len(first.Keys.Models) != 4 {
		t.Fatalf("first run %q, keys %+v; want a passed screen, keyed by set A's four models", got, first.Keys)
	}
	for _, s := range first.Stages {
		if reusedAnyCheck(s.Checks) {
			t.Errorf("the first run reused %q from an empty index", reusedFrom(first, s.Name))
		}
	}
	if err := os.RemoveAll(filepath.Join(state, "evaluate", "run1")); err != nil {
		t.Fatal(err)
	}
	calls, toolCalls, trials := len(h.uv.calls(t)), h.router.count("messages"), len(h.conform.plans)

	rep := h.runAs(t, state, "run2", "", nil)
	if got := statuses(rep); !equal(got, screenStatuses) || !rep.Finalist {
		t.Fatalf("second run %q; want a passed screen, a finalist", got)
	}
	if rep.Keys.Set != first.Keys.Set || rep.Keys.Method != first.Keys.Method {
		t.Errorf("keys %+v; want the first run's %+v", rep.Keys, first.Keys)
	}
	if s := rep.Stage(gate.StageAdmission); s.Status != gate.StatusPass || reusedAnyCheck(s.Checks) {
		t.Errorf("admission %+v; want it judged again, on paper", s)
	}
	for _, stage := range []string{gate.StageFitness, gate.StageCapacity, gate.StageQuality} {
		wantReused(t, rep, stage, "run1", byRole)
	}
	if w := check(t, rep, gate.StageFitness, "", "warmed pool free"); !w.Passed {
		t.Errorf("warmed peak %+v; want it judged from the run's own warm-up", w)
	}
	if x := check(t, rep, gate.StageQuality, "", "no crossed answer"); !x.Passed || x.Values["rounds"] != 1 {
		t.Errorf("crossing %+v; want one round over a pass that reused every unit", x)
	}
	if n := len(h.uv.calls(t)); n != calls {
		t.Errorf("the second run ran %d suites; want none", n-calls)
	}
	if h.router.count("messages") != toolCalls || len(h.conform.plans) != trials {
		t.Error("the second run resent fitness's tool call or harness fit's trials")
	}

	var b bytes.Buffer
	if err := gate.WriteSummary(&b, rep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(reused from run1)", "checks reused from run1)", "[reused from run1]"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, b.String())
		}
	}
	// The report keeps where each check came from.
	dir := t.TempDir()
	if err := gate.WriteReport(dir, rep); err != nil {
		t.Fatal(err)
	}
	got, err := gate.ReadReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c := check(t, got, gate.StageCapacity, gate.RoleAgent, "no crossed nonce"); c.ReusedFrom != "run1" || got.Keys == nil || got.Keys.Set != rep.Keys.Set {
		t.Errorf("read back %+v, keys %+v; want the source run and the keys kept", c, got.Keys)
	}
}

func reusedAnyCheck(checks []gate.Check) bool {
	return slices.ContainsFunc(checks, func(c gate.Check) bool { return c.ReusedFrom != "" })
}

// TestIndexChangedSection changes one model's c, which changes its preset section: a set run's
// next screen reruns only that model's roles, and harness fit, under the set's key, and reuses
// every other model's records; a model's full run of another role reuses its own records and
// reruns the set stage, which the same keys reuse whole.
func TestIndexChangedSection(t *testing.T) {
	t.Run("set run", func(t *testing.T) {
		h := newHarness(t, "")
		state := h.indexed(t)
		first := h.runAs(t, state, "run1", "", nil)
		calls := len(h.uv.calls(t))
		trials := len(h.conform.plans)

		h.runner.Profile.Model(embedding).C /= 2
		rep := h.runAs(t, state, "run2", "", nil)
		if got := statuses(rep); !equal(got, screenStatuses) {
			t.Fatalf("second run %q; want a passed screen", got)
		}
		for i, k := range rep.Keys.Models {
			if changed := k.Section != first.Keys.Models[i].Section; changed != (k.Model == embedding) || k.Build != first.Keys.Models[i].Build {
				t.Errorf("%s's key %+v against %+v; want only the embedding model's section changed", k.Model, k, first.Keys.Models[i])
			}
		}
		if rep.Keys.Set == first.Keys.Set {
			t.Error("the set's key didn't change with one of its models'")
		}
		for _, stage := range []string{gate.StageFitness, gate.StageCapacity} {
			wantReused(t, rep, stage, "run1", func(c gate.Check) bool { return c.Role != "" && c.Model != embedding })
		}
		wantReused(t, rep, gate.StageQuality, "run1", func(c gate.Check) bool {
			return c.Role != "" && c.Role != gate.RoleEmbedding && !strings.HasPrefix(c.Name, gate.AgentCheck(gate.ConformTier))
		})
		ran := h.uv.calls(t)[calls:]
		if len(ran) != len(gate.EmbeddingTasks) || !strings.Contains(ran[0], "mteb_quality.py") {
			t.Errorf("the second run ran %q; want the embedding model's MTEB tasks alone", ran)
		}
		if len(h.conform.plans) != trials+1 {
			t.Errorf("harness fit ran %d times; want once more, its set changed", len(h.conform.plans)-trials)
		}
	})

	t.Run("full run", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		state := h.indexed(t)
		h.runner.Full = true
		first := h.runAs(t, state, "run1", "", nil)
		if !first.Passed || first.Mode != gate.ModeFull || reusedAnyCheck(first.Stage(gate.StageSet).Checks) {
			t.Fatalf("first full run %q; want passed, its set stage measured", statuses(first))
		}
		setCalls := len(h.uv.setCalls(t))

		// The same keys: every keyed stage reused, the set stage whole; deep, never kept, measured again.
		same := h.runAs(t, state, "run2", "", nil)
		if !same.Passed {
			t.Fatalf("second full run %q; want passed", statuses(same))
		}
		wantReused(t, same, gate.StageQuality, "run1", byRole)
		wantReused(t, same, gate.StageSet, "run1", func(gate.Check) bool { return true })
		wantReused(t, same, gate.StageDeep, "run1", func(gate.Check) bool { return false })
		if n := len(h.uv.setCalls(t)); n != setCalls {
			t.Errorf("the set stage ran %d suites on the same keys; want none", n-setCalls)
		}

		// Another model's section changed: the model's own records reused, the set stage rerun.
		h.runner.Profile.Model(embedding).C /= 2
		setCalls = len(h.uv.setCalls(t))
		rep := h.runAs(t, state, "run3", "", nil)
		if !rep.Passed {
			t.Fatalf("third full run %q; want passed", statuses(rep))
		}
		wantReused(t, rep, gate.StageQuality, "run1", byRole)
		wantReused(t, rep, gate.StageSet, "", func(gate.Check) bool { return false })
		if n := len(h.uv.setCalls(t)); n == setCalls {
			t.Error("the set stage ran no suite after a set model's section changed")
		}
		// The screen this full run began with reused its fitness and capacity records.
		wantReused(t, rep, gate.StageCapacity, "run1", byRole)
	})
}

// TestIndexBuildChange shows a new router build changes every model's key: the next run reuses
// nothing.
func TestIndexBuildChange(t *testing.T) {
	h := newHarness(t, gptOSS)
	state := h.indexed(t)
	first := h.runAs(t, state, "run1", "", nil)
	calls := len(h.uv.calls(t))
	h.router.build = "b11715-0a1b2c3d4"
	rep := h.runAs(t, state, "run2", "", nil)
	if got := statuses(rep); !equal(got, screenStatuses) {
		t.Fatalf("second run %q; want a passed screen", got)
	}
	for _, k := range rep.Keys.Models {
		if k.Build != "b11715-0a1b2c3d4" {
			t.Errorf("%s's key %+v; want the new build", k.Model, k)
		}
	}
	if rep.Keys.Set == first.Keys.Set {
		t.Error("the set's key didn't change with the build")
	}
	for _, stage := range gate.Stages {
		if s := rep.Stage(stage); s != nil && reusedAnyCheck(s.Checks) {
			t.Errorf("%s reused %q on a new build", stage, reusedFrom(rep, stage))
		}
	}
	if len(h.uv.calls(t)) == calls {
		t.Error("the second run ran no suite")
	}
}

// TestIndexFailedNotReused shows a unit that failed is never kept: the next run measures it again
// and reuses the units before it that passed; a role whose fitness failed is measured again too.
func TestIndexFailedNotReused(t *testing.T) {
	t.Run("quality", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		state := h.indexed(t)
		h.uv.fail = "vstar"
		h.rewire()
		first := h.runAs(t, state, "run1", "", nil)
		if c := check(t, first, gate.StageQuality, gate.RoleVision, "vision-vstar"); c.Passed || first.Finalist {
			t.Fatalf("vision-vstar %+v; want it failed, no finalist", c)
		}
		for _, e := range h.runner.Index.Entries {
			for _, c := range e.Checks {
				if !c.Passed {
					t.Errorf("the index kept %s %s's failed %s", e.Stage, e.Unit, c.Name)
				}
			}
			if e.Unit == "vision-vstar" {
				t.Errorf("the index kept the failed V*Bench: %+v", e)
			}
		}

		h.uv.fail = ""
		h.rewire()
		rep := h.runAs(t, state, "run2", "", nil)
		if !rep.Finalist {
			t.Fatalf("second run %q; want a finalist", statuses(rep))
		}
		wantReused(t, rep, gate.StageQuality, "run1", func(c gate.Check) bool { return c.Name == "vision-probe" })
		wantReused(t, rep, gate.StageCapacity, "run1", byRole)
	})
	t.Run("fitness", func(t *testing.T) {
		h := newHarness(t, gptOSS)
		state := h.indexed(t)
		h.router.decode = 15
		if first := h.runAs(t, state, "run1", "", nil); first.Stage(gate.StageFitness).Status != gate.StatusFail {
			t.Fatalf("first run %q; want fitness failed", statuses(first))
		}
		h.router.decode = 50
		toolCalls := h.router.count("messages")
		rep := h.runAs(t, state, "run2", "", nil)
		if got := statuses(rep); !equal(got, screenStatuses) {
			t.Fatalf("second run %q; want a passed screen", got)
		}
		wantReused(t, rep, gate.StageFitness, "", func(gate.Check) bool { return false })
		if h.router.count("messages") == toolCalls {
			t.Error("fitness's tool call wasn't resent after its role failed")
		}
	})
}

// TestIndexMethodAndFresh shows a changed bar, part of the method every entry is kept under,
// reruns everything, and so does -fresh (Runner.Fresh), which still records what it measured.
func TestIndexMethodAndFresh(t *testing.T) {
	h := newHarness(t, embedding)
	state := h.indexed(t)
	h.runAs(t, state, "run1", "", nil)

	h.runner.Bars = tier.Bars{"STSBenchmark": {Value: 0.1}}
	bars := h.runAs(t, state, "run2", "", nil)
	for _, stage := range gate.Stages {
		if s := bars.Stage(stage); s != nil && reusedAnyCheck(s.Checks) {
			t.Errorf("%s reused %q under a changed bar", stage, reusedFrom(bars, stage))
		}
	}

	h.runner.Fresh = true
	fresh := h.runAs(t, state, "run3", "", nil)
	for _, stage := range gate.Stages {
		if s := fresh.Stage(stage); s != nil && reusedAnyCheck(s.Checks) {
			t.Errorf("%s reused %q in a fresh run", stage, reusedFrom(fresh, stage))
		}
	}
	h.runner.Fresh = false
	rep := h.runAs(t, state, "run4", "", nil)
	wantReused(t, rep, gate.StageQuality, "run3", byRole)
}

// TestIndexFullContinuesReusedScreen continues a screen that reused a stopped tier from an
// earlier run: the rest reads that run's screen output, beside the screen's own directory, and the
// whole list is judged; once that run's directory is gone, the tier can't be continued, and says to
// screen again.
func TestIndexFullContinuesReusedScreen(t *testing.T) {
	h := newHarness(t, gemma26B)
	h.uv.variant(t, "vision", "vstar", 4, gate.StopPass, 0.6)
	h.rewire()
	h.runner.Bars = tier.Bars{"vision-vstar": {Value: 0.5}}
	state := h.indexed(t)
	h.runAs(t, state, "run1", "", nil)
	screen := h.runAs(t, state, "run2", "", nil)
	if c := check(t, screen, gate.StageQuality, gate.RoleVision, "vision-vstar"); c.ReusedFrom != "run1" || c.Stop == nil {
		t.Fatalf("vision-vstar %+v; want the stopped tier reused from run1", c)
	}

	h.runner.Full = true
	rep := h.runAs(t, state, "run2", "", screen)
	c := check(t, rep, gate.StageQuality, gate.RoleVision, "vision-vstar")
	if !rep.Passed || !c.Passed || c.Stop != nil || c.Values["samples"] != 10 || c.Values["screen_samples"] != 4 {
		t.Fatalf("full run %q, vision-vstar %+v; want the whole list judged, run1's 4 and the rest", statuses(rep), c)
	}
	if _, err := os.Stat(filepath.Join(state, "evaluate", "run2", "vision", "vstar", "rest", "list.txt")); err != nil {
		t.Errorf("the rest's list: %v; want it in the full run's own directory", err)
	}

	if err := os.RemoveAll(filepath.Join(state, "evaluate", "run1")); err != nil {
		t.Fatal(err)
	}
	h.runner.Fresh = true
	gone := h.runAs(t, state, "run2", "", screen)
	if c := check(t, gone, gate.StageQuality, gate.RoleVision, "vision-vstar"); c.Passed || !strings.Contains(c.Detail, "from run run1, whose output the rest continues is gone") ||
		!strings.Contains(c.Detail, "screen again with -fresh") {
		t.Errorf("vision-vstar %+v; want a failure naming run1's output gone", c)
	}
}
