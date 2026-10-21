package gate_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// recordedVision are each vision fixture's scored samples, passes and the sum of their scores, as
// testdata/vision/<tier>'s Inspect logs hold them; a benchmark's samples are
// testdata/tiers/vision-<tier>.txt's. Each log is the gated run's with the events, messages and
// attachments dropped from its samples and every sample but the fixture's taken out: the probe's
// five; the first ten of V*Bench's tier list, from both of its tasks; DocVQA's first three and
// one with an ANLS between 0.5 and 1; MMMU's first four. testdata/vision/vstar-stopped is a run
// of its own of the same ten V*Bench questions with a bar of 0.99, which early stopping ended
// after six, its early_stop.json beside the logs: its spatial task's samples were all stopped, so
// Inspect logged that task with no samples. The logs' results keep the whole run's metrics, which
// ReadTier doesn't read.
var recordedVision = map[string]struct {
	samples, passes int
	sum             float64
}{
	"probe":         {5, 5, 5},
	"vstar":         {10, 6, 6},
	"docvqa":        {4, 3, 2.75},
	"mmmu":          {4, 3, 3},
	"vstar-stopped": {6, 2, 2},
}

// visionLog is a vision fixture's Inspect log, decoded, for a test to edit.
func visionLog(t *testing.T, tierName string) (string, map[string]any) {
	t.Helper()
	dir := filepath.Join("testdata", "vision", tierName)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("%s: %v, %d entries", dir, err, len(entries))
	}
	b, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var log map[string]any
	if err := json.Unmarshal(b, &log); err != nil {
		t.Fatal(err)
	}
	return entries[0].Name(), log
}

// TestQualityVision runs the vision model's quality stage with no bars: each tier runs, cheapest
// first, through uv, its score read from Inspect's logs and recorded with its samples, passes,
// dataset, tasks and releases, and, with no bar, it passes; the probe passes on all five of its
// repeats. MMMU records the card's MMMU Pro figure beside it.
func TestQualityVision(t *testing.T) {
	h := newHarness(t, gemma26B)
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set not run", "deep not run"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q\n%+v", got, want, rep.Stages)
	}
	q := units(rep.Stage(gate.StageQuality))
	if len(q.Checks) != len(gate.VisionTiers) {
		t.Fatalf("%d quality checks; want one a tier, %d", len(q.Checks), len(gate.VisionTiers))
	}
	for i, vt := range gate.VisionTiers {
		c, r := q.Checks[i], recordedVision[vt.Name]
		score := r.sum / float64(r.samples)
		if c.Name != gate.VisionCheck(vt.Name) || c.Role != gate.RoleVision || c.Measured == nil || *c.Measured != score || c.Unit != vt.Metric {
			t.Errorf("check %d %+v; want %s's %s %v", i, c, gate.VisionCheck(vt.Name), vt.Metric, score)
		}
		if c.Floor != nil || !c.Passed {
			t.Errorf("%s: floor %v passed %v; want no bar, passed", vt.Name, c.Floor, c.Passed)
		}
		if c.Values["samples"] != float64(r.samples) || c.Values["passes"] != float64(r.passes) || c.Values["tier"] != float64(vt.Tier) {
			t.Errorf("%s: values %v; want %d samples, %d passed, tier %d", vt.Name, c.Values, r.samples, r.passes, vt.Tier)
		}
		wants := []string{"inspect " + gate.InspectVersion, fmt.Sprintf("tier %d", vt.Tier)}
		if vt.Name == "probe" {
			wants = append(wants, "pass^5")
		} else {
			wants = append(wants, "no bar: recorded, never fails", "inspect_evals "+gate.InspectEvalsVersion, "dataset ", "reasoning off")
		}
		for _, w := range wants {
			if !strings.Contains(c.Detail, w) {
				t.Errorf("%s: detail %q lacks %q", vt.Name, c.Detail, w)
			}
		}
		_, card := c.Values["card_score"]
		if card != (vt.Name == "mmmu") {
			t.Errorf("%s: card_score recorded %v; want it on mmmu alone", vt.Name, card)
		}
	}
	vstar := check(t, rep, gate.StageQuality, gate.RoleVision, "vision-vstar")
	for _, w := range []string{"craigwu/vstar_bench@d9ae62c903da0c98336e85c5ee89cd863b04b4da", "vstar_bench_attribute_recognition@2-A", "vstar_bench_spatial_relationship_reasoning@2-A"} {
		if !strings.Contains(vstar.Detail, w) {
			t.Errorf("vision-vstar detail %q lacks %q", vstar.Detail, w)
		}
	}
	if d := check(t, rep, gate.StageQuality, gate.RoleVision, "vision-docvqa").Detail; !strings.Contains(d, "mean ANLS") {
		t.Errorf("vision-docvqa detail %q; want its mean ANLS and pass rate", d)
	}

	// Each tier ran once, in order, through the suites project: the probe with its image, written
	// into the run directory from the runner's media, and each benchmark with its tier list.
	calls := h.uv.calls(t)
	if len(calls) != len(gate.VisionTiers) {
		t.Fatalf("uv calls %q; want one a tier", calls)
	}
	for i, vt := range gate.VisionTiers {
		dir := filepath.Join(h.runner.Dir, "vision", vt.Name)
		input := "--list " + filepath.Join(h.runner.Tiers, gate.VisionCheck(vt.Name)+".txt")
		if vt.Name == "probe" {
			input = "--image " + filepath.Join(dir, "shapes.png")
		}
		want := fmt.Sprintf("run --project /module/evaluation/suites --locked --no-python-downloads --python /pinned/python python "+
			"/module/evaluation/suites/vision_quality.py eval --router %s --model %s --tier %s --output %s --slots 4 %s|/state/cache/huggingface|/state/cache/mteb|/state/cache/asr|/state/cache/inspect_evals",
			h.router.srv.URL, gemma26B, vt.Name, filepath.Join(dir, "logs"), input)
		if calls[i] != want {
			t.Errorf("call %d:\n got %s\nwant %s", i, calls[i], want)
		}
	}
	if b, err := os.ReadFile(filepath.Join(h.runner.Dir, "vision", "probe", "shapes.png")); err != nil || string(b) != "png" {
		t.Errorf("the probe's image %q, %v; want the media's", b, err)
	}
}

// TestQualityVisionBars judges each benchmark's score against its floor: a tier under its floor
// passes it to vision_quality.py, whose early stop ends the tier once the pass rate's upper
// bound is below it, failing the check, the stage and the run before the next tier; a tier over
// its floor runs in full and passes.
func TestQualityVisionBars(t *testing.T) {
	h := newHarness(t, gemma26B)
	h.runner.Bars = tier.Bars{"vision-vstar": {Value: 0.99}, "vision-probe": {Value: 0.1, AtMost: true}}
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality fail", "set not run", "deep not run"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q", got, want)
	}
	q := units(rep.Stage(gate.StageQuality))
	if len(q.Checks) != 2 {
		t.Fatalf("quality checks %+v; want the probe's and V*Bench's, the stage ended there", q.Checks)
	}
	// The probe ignores the bars: it is pass^5.
	if probe := q.Checks[0]; !probe.Passed || probe.Floor != nil {
		t.Errorf("vision-probe %+v; want passed, unbarred", probe)
	}
	r := recordedVision["vstar-stopped"]
	vstar := q.Checks[1]
	if vstar.Passed || vstar.Op != gate.AtLeast || *vstar.Floor != 0.99 || !strings.Contains(vstar.Detail, fmt.Sprintf("early stop, failed, after %d of 10: the 95%% upper bound", r.samples)) ||
		vstar.Stop == nil || vstar.Stop.Side != gate.StopFail || vstar.Stop.Ran != r.samples {
		t.Errorf("vision-vstar %+v; want stopped early, failed against >= 0.99", vstar)
	}
	if _, ok := vstar.Values["pass_rate_upper_bound"]; !ok || vstar.Values["samples"] != float64(r.samples) {
		t.Errorf("vision-vstar values %v; want the bound and the %d samples run", vstar.Values, r.samples)
	}
	calls := h.uv.calls(t)
	if len(calls) != 2 || !strings.HasSuffix(strings.Split(calls[1], "|")[0], "--bar 0.99") || strings.Contains(calls[0], "--bar") {
		t.Errorf("calls %q; want the probe's without a bar, V*Bench's with 0.99, and no more", calls)
	}

	h = newHarness(t, gemma26B)
	h.runner.Bars = tier.Bars{"vision-docvqa": {Value: 0.1}}
	rep = h.run(t, "", nil)
	if c := check(t, rep, gate.StageQuality, gate.RoleVision, "vision-docvqa"); !c.Passed || *c.Floor != 0.1 || !strings.Contains(c.Detail, "early stop: undecided") {
		t.Errorf("vision-docvqa %+v; want a pass against >= 0.1, its whole list run undecided", c)
	}
	if !rep.Passed {
		t.Errorf("run failed; want passed\n%+v", rep.Stages)
	}
}

// TestQualityVisionBroken shows a tier uv fails on, whose logs another Inspect or inspect_evals
// wrote or don't match its tier list, or whose bar is a ceiling, fails its check, naming why, and
// ends the stage there.
func TestQualityVisionBroken(t *testing.T) {
	cases := map[string]struct {
		fail, inspect, evals, want string
		bars                       tier.Bars
		extraID                    bool
	}{
		"uv fails":      {fail: "docvqa", want: "uv run vision_quality.py eval --tier docvqa"},
		"other inspect": {inspect: "0.3.200", want: "Inspect 0.3.200's, not the pinned " + gate.InspectVersion + "'s"},
		"other evals":   {evals: "0.20.0", want: "inspect_evals 0.20.0's, not the pinned " + gate.InspectEvalsVersion + "'s"},
		"a ceiling":     {bars: tier.Bars{"vision-docvqa": {Value: 0.2, AtMost: true}}, want: "its bar belongs under [bars]"},
		"list differs":  {extraID: true, want: "aren't the tier's"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, gemma26B)
			h.uv.fail, h.uv.inspect, h.uv.evals = tc.fail, tc.inspect, tc.evals
			h.runner.Vision, h.runner.Bars = h.uv.runner(), tc.bars
			if tc.extraID {
				h.runner.Tiers = withExtraID(t, h.runner.Tiers, "vision-docvqa")
			}
			rep := h.run(t, "", nil)
			q := units(rep.Stage(gate.StageQuality))
			if q.Status != gate.StatusFail {
				t.Fatalf("quality %+v; want failed", q)
			}
			// Another Inspect fails the probe, the first tier, and another inspect_evals V*Bench,
			// the first benchmark; the rest fail DocVQA, the third.
			name := "vision-docvqa"
			switch {
			case tc.inspect != "":
				name = "vision-probe"
			case tc.evals != "":
				name = "vision-vstar"
			}
			last := q.Checks[len(q.Checks)-1]
			if last.Name != name || last.Passed || !strings.Contains(last.Detail, tc.want) {
				t.Errorf("last check %+v; want %s failed, naming %q", last, name, tc.want)
			}
		})
	}
}

// TestQualityVisionProbe fails the probe, and the stage there, when one of its five repeats
// doesn't name every shape and colour: pass^5, with no bar.
func TestQualityVisionProbe(t *testing.T) {
	h := newHarness(t, gemma26B)
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	name, log := visionLog(t, "probe")
	log["samples"].([]any)[2].(map[string]any)["scores"] = map[string]any{"names_shapes": map[string]any{"value": "I"}}
	b, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vision", "probe", name), b, 0o600); err != nil {
		t.Fatal(err)
	}
	h.uv.testdata = dir
	h.runner.Vision = h.uv.runner()
	rep := h.run(t, "", nil)
	q := units(rep.Stage(gate.StageQuality))
	if q.Status != gate.StatusFail || len(q.Checks) != 1 {
		t.Fatalf("quality %+v; want failed at the probe, its only check", q)
	}
	if c := q.Checks[0]; c.Passed || *c.Measured != 0.8 || c.Values["passes"] != 4 {
		t.Errorf("vision-probe %+v; want failed, 4 of 5", c)
	}
}

// TestReadTier reads a log's scores, choice()'s C and I as 1 and 0 and ANLS as it is, and
// refuses a log of a run that didn't succeed, with a sample that errored, went unscored or
// scored something else, or beside a log of another tier.
func TestReadTier(t *testing.T) {
	write := func(t *testing.T, tierName string, edit func(log map[string]any)) string {
		t.Helper()
		name, log := visionLog(t, tierName)
		edit(log)
		b, err := json.Marshal(log)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	for _, name := range []string{"mmmu", "docvqa"} {
		r := recordedVision[name]
		res, err := gate.ReadTier(write(t, name, func(map[string]any) {}))
		if err != nil || len(res.IDs) != r.samples || res.Passes() != r.passes || res.Score() != r.sum/float64(r.samples) || res.Tier != name {
			t.Errorf("%s: ReadTier = %+v, %v; want %d samples, %d passed, score %v", name, res, err, r.samples, r.passes, r.sum/float64(r.samples))
		}
	}
	sample := func(log map[string]any) map[string]any { return log["samples"].([]any)[0].(map[string]any) }
	for name, edit := range map[string]func(map[string]any){
		"run errored":  func(log map[string]any) { log["status"] = "error" },
		"sample error": func(log map[string]any) { sample(log)["error"] = map[string]any{"message": "boom"} },
		"unscored":     func(log map[string]any) { delete(sample(log), "scores") },
		"another score": func(log map[string]any) {
			sample(log)["scores"] = map[string]any{"choice": map[string]any{"value": "P"}}
		},
		"no time": func(log map[string]any) { delete(sample(log)["output"].(map[string]any), "time") },
	} {
		if _, err := gate.ReadTier(write(t, "mmmu", edit)); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
	mixed := write(t, "mmmu", func(map[string]any) {})
	other, log := visionLog(t, "docvqa")
	b, _ := json.Marshal(log)
	if err := os.WriteFile(filepath.Join(mixed, other), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.ReadTier(mixed); err == nil {
		t.Error("two tiers' logs: read without error")
	}
}
