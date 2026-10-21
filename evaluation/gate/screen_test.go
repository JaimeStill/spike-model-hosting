package gate_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// variant makes f answer a screen of kind's unit (asr, vision or agent) given a bar with a run its
// early stop ended on side after the unit's list's first keep samples, its bound then bound, and a
// full run's rest with the others: <kind>/<unit>-stopped and <kind>/<unit>-rest, cut from the
// recorded <kind>/<unit> in a copy of testdata f then answers from. An ASR log carries the stop in
// its results, as asr_quality.py's WERStop leaves it; a pass-rate tier's runs carry it in
// early_stop.json beside the logs, as pass_stop.py's PassStop leaves it. A vision task none of
// whose samples are the rest's is left out of the rest, as vision_quality.py leaves it out.
func (f *fakeUV) variant(t *testing.T, kind, unit string, keep int, side string, bound float64) {
	t.Helper()
	if f.testdata == "" {
		f.testdata = t.TempDir()
		if err := os.CopyFS(f.testdata, os.DirFS("testdata")); err != nil {
			t.Fatal(err)
		}
	}
	list, err := tier.ReadListFile(filepath.Join(f.testdata, "tiers", kind+"-"+unit+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	screen, rest := list.IDs[:keep], list.IDs[keep:]
	src := filepath.Join(f.testdata, kind, unit)
	stopped, more := src+"-stopped", src+"-rest"
	reason := fmt.Sprintf("the 95%% bound %.4f decided it, side %s", bound, side)
	bounds := map[string]map[string]string{
		"asr":    {gate.StopFail: "lower_bound", gate.StopPass: "upper_bound"},
		"vision": {gate.StopFail: "upper_bound", gate.StopPass: "lower_bound"},
		"agent":  {gate.StopFail: "upper_bound", gate.StopPass: "lower_bound"},
	}[kind]
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{stopped, more} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	logName := regexp.MustCompile(`^\d{4}-.*\.json$`)
	for _, e := range entries {
		if !logName.MatchString(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for dir, ids := range map[string][]string{stopped: screen, more: rest} {
			var log map[string]any
			if err := json.Unmarshal(b, &log); err != nil {
				t.Fatal(err)
			}
			var cut []any
			for _, s := range log["samples"].([]any) {
				if slices.Contains(ids, fmt.Sprint(s.(map[string]any)["id"])) {
					cut = append(cut, s)
				}
			}
			if dir == more && len(cut) == 0 {
				continue
			}
			log["samples"] = cut
			if results, ok := log["results"].(map[string]any); ok && kind == "asr" {
				delete(results, "early_stopping")
				if dir == stopped {
					var stops []any
					for _, id := range rest {
						stops = append(stops, map[string]any{"id": id, "epoch": 1, "reason": reason})
					}
					results["early_stopping"] = map[string]any{"manager": "wer_bounds", "early_stops": stops,
						"metadata": map[string]any{"stopped": true, "side": side, bounds[side]: bound}}
				}
			}
			out, err := json.Marshal(log)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, e.Name()), out, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if kind != "asr" {
		account, err := json.Marshal(map[string]any{"stopped": rest, "side": side, "reason": reason, bounds[side]: bound})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stopped, "early_stop.json"), account, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// evaluated reports whether the fake has run a quality suite: a call that isn't the warm-up's.
func (f *fakeUV) evaluated() bool {
	b, _ := os.ReadFile(filepath.Join(f.dir, "calls"))
	for line := range strings.SplitSeq(string(b), "\n") {
		if line != "" && !peakCall.MatchString(line) {
			return true
		}
	}
	return false
}

// rewire points every suite of h's runner at its fake uv as it is now configured.
func (h *harness) rewire() {
	uv := h.uv.runner()
	h.runner.MTEB, h.runner.ASR, h.runner.Vision, h.runner.Agent, h.runner.Tau2, h.runner.Peaks = uv, uv, uv, uv, uv, uv
}

// screenStatuses are a passed screen's stages.
var screenStatuses = []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set not run", "deep not run"}

// TestScreenStopsOnEitherSide shows a screen's tier stops passed once its bound clears its bar, the
// stop recorded with its side, bound and samples, and the next tier runs; one stops failed once its
// bound falls short, and the role's tiers end there; a tier whose bounds straddle its bar runs its
// whole list; and an MTEB task, a continuous score, runs whole whatever its bar.
func TestScreenStopsOnEitherSide(t *testing.T) {
	t.Run("vision", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		h.uv.variant(t, "vision", "vstar", 4, gate.StopPass, 0.6)
		h.rewire()
		h.runner.Bars = tier.Bars{"vision-vstar": {Value: 0.5}, "vision-docvqa": {Value: 0.1}}
		rep := h.run(t, "", nil)
		if got := statuses(rep); !equal(got, screenStatuses) || rep.Mode != gate.ModeScreen || !rep.Finalist {
			t.Fatalf("stages %q, mode %q, finalist %v; want a passed screen, a finalist", got, rep.Mode, rep.Finalist)
		}
		v := check(t, rep, gate.StageQuality, gate.RoleVision, "vision-vstar")
		if s := v.Stop; !v.Passed || s == nil || s.Side != gate.StopPass || s.Bound != "mean score lower bound" || s.Ran != 4 || s.Size != 10 ||
			*s.Value != 0.6 || v.Values["mean_score_lower_bound"] != 0.6 || v.Values["samples"] != 4 {
			t.Errorf("vision-vstar %+v, stop %+v; want passed by the mean score's lower bound 0.6 after 4 of 10", v, v.Stop)
		}
		if !strings.Contains(v.Detail, "early stop, passed, after 4 of 10: the 95% bound 0.6000 decided it, side pass") {
			t.Errorf("vision-vstar detail %q; want the stop, its side and its reason", v.Detail)
		}
		// The tier after a passed stop runs; DocVQA's bounds straddle its bar through its list.
		d := check(t, rep, gate.StageQuality, gate.RoleVision, "vision-docvqa")
		if !d.Passed || d.Stop != nil || d.Values["samples"] != float64(recordedVision["docvqa"].samples) || !strings.Contains(d.Detail, "early stop: undecided") {
			t.Errorf("vision-docvqa %+v; want its whole list run, undecided, and passed", d)
		}
		check(t, rep, gate.StageQuality, gate.RoleVision, "vision-mmmu")

		// The report keeps the mode, the finalist and the stop.
		dir := t.TempDir()
		if err := gate.WriteReport(dir, rep); err != nil {
			t.Fatal(err)
		}
		got, err := gate.ReadReport(dir)
		if err != nil {
			t.Fatal(err)
		}
		if s := check(t, got, gate.StageQuality, gate.RoleVision, "vision-vstar").Stop; got.Mode != gate.ModeScreen || !got.Finalist ||
			s == nil || s.Side != gate.StopPass || s.Ran != 4 || *s.Value != 0.6 {
			t.Errorf("read back mode %q finalist %v stop %+v; want the screen's, its stop kept", got.Mode, got.Finalist, s)
		}
	})
	t.Run("vision fails", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		h.uv.variant(t, "vision", "vstar", 3, gate.StopFail, 0.4)
		h.rewire()
		h.runner.Bars = tier.Bars{"vision-vstar": {Value: 0.9}}
		rep := h.run(t, "", nil)
		v := check(t, rep, gate.StageQuality, gate.RoleVision, "vision-vstar")
		if s := v.Stop; v.Passed || s == nil || s.Side != gate.StopFail || s.Bound != "pass rate upper bound" || s.Ran != 3 || *s.Value != 0.4 {
			t.Errorf("vision-vstar %+v, stop %+v; want failed by the pass rate's upper bound 0.4 after 3", v, v.Stop)
		}
		if names := checkNames(units(rep.Stage(gate.StageQuality))); !equal(names, []string{"vision-probe", "vision-vstar"}) || rep.Finalist {
			t.Errorf("quality %q, finalist %v; want the tiers ended at V*Bench, no finalist", names, rep.Finalist)
		}
	})
	t.Run("audio", func(t *testing.T) {
		h := newHarness(t, gemmaE4B)
		h.uv.variant(t, "asr", "ami", 4, gate.StopPass, 0.2)
		h.rewire()
		h.runner.Bars = tier.Bars{"asr-ami": {Value: 0.5, AtMost: true}}
		rep := h.run(t, "", nil)
		a := check(t, rep, gate.StageQuality, gate.RoleAudio, "asr-ami")
		if s := a.Stop; !a.Passed || s == nil || s.Side != gate.StopPass || s.Bound != "WER upper bound" || s.Ran != 4 || s.Size != 10 ||
			a.Values["wer_upper_bound"] != 0.2 || a.Values["utterances"] != 4 || !strings.Contains(a.Detail, "early stop, passed, after 4 of 10") {
			t.Errorf("asr-ami %+v, stop %+v; want passed by the WER's upper bound 0.2 after 4 of 10", a, a.Stop)
		}
		// The sets after it run.
		check(t, rep, gate.StageQuality, gate.RoleAudio, "asr-fleurs-en")
		if !rep.Finalist {
			t.Errorf("stages %q; want a finalist", statuses(rep))
		}
	})
	t.Run("mteb", func(t *testing.T) {
		h := newHarness(t, embedding)
		h.runner.Bars = tier.Bars{"STSBenchmark": {Value: 0.5}}
		rep := h.run(t, "", nil)
		c := check(t, rep, gate.StageQuality, gate.RoleEmbedding, "STSBenchmark")
		if !c.Passed || c.Stop != nil || !strings.Contains(c.Detail, "early stop n/a: a continuous score, run in full") {
			t.Errorf("STSBenchmark %+v; want run whole, judged, no stop", c)
		}
		for _, call := range h.uv.calls(t) {
			if strings.Contains(call, "--bar") {
				t.Errorf("MTEB call %s; want no bar, the task run whole", call)
			}
		}
	})
}

// stageLog is a run's Log that, at the line starting the quality stage, counts the speed
// requests (fitness's chats and tool call, capacity's nonce requests) the router has served.
type stageLog struct {
	h      *harness
	mu     sync.Mutex
	before int
	seen   bool
}

// speed counts the requests fitness and capacity send: chats, tool calls and capacity's
// requests. The quality pass's crossing rounds send capacity's requests too, so they are counted
// apart, by the gates' own count.
func (l *stageLog) speed() int {
	return l.h.router.count("chat") + l.h.router.count("messages")
}

func (l *stageLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if strings.HasPrefix(string(p), "evaluate: quality ") {
		l.before, l.seen = l.speed(), true
	}
	return len(p), nil
}

// TestScreenSetRun screens set A as a set run: admission, fitness and capacity model by model,
// alone, each check naming its model; then every role's quality tiers at once (the fake holds each
// suite's first call until all four kinds have started), with no speed request sent while they
// run; the pool's least free memory and the crossing rounds over that pass, folded into the
// quality stage; and the set and deep stages left to the full run.
func TestScreenSetRun(t *testing.T) {
	h := newHarness(t, "")
	h.uv.barrier = 4
	h.rewire()
	log := &stageLog{h: h}
	h.runner.Log = log
	rep := h.run(t, "", nil)
	if got := statuses(rep); !equal(got, screenStatuses) || rep.Mode != gate.ModeScreen || !rep.Finalist || rep.Model != "" {
		t.Fatalf("stages %q, mode %q, finalist %v; want a set run's passed screen\n%+v", got, rep.Mode, rep.Finalist, rep.Stages)
	}
	if r := rep.Stage(gate.StageSet).Reason; !strings.Contains(r, "stands in for the set stage's concurrent phase") {
		t.Errorf("set's reason %q; want the concurrent pass named as its stand-in", r)
	}
	if r := rep.Stage(gate.StageDeep).Reason; !strings.Contains(r, "full run") {
		t.Errorf("deep's reason %q; want it left to the full run", r)
	}

	// The speed floors are each model's, measured alone in fitness and capacity.
	for role, model := range roleModels {
		for _, stage := range []string{gate.StageFitness, gate.StageCapacity} {
			s := rep.Stage(stage)
			i := slices.IndexFunc(s.Checks, func(c gate.Check) bool { return c.Role == role })
			if i < 0 || s.Checks[i].Model != model {
				t.Errorf("%s: no %s check naming %s", stage, role, model)
			}
		}
	}
	if d := check(t, rep, gate.StageFitness, gate.RoleAgent, "decode"); d.Model != gptOSS || *d.Measured != 50 {
		t.Errorf("agent decode %+v; want gpt-oss-120b's, 50 tok/s", d)
	}
	if lic := check(t, rep, gate.StageAdmission, "", "licence"); lic.Model == "" {
		t.Errorf("licence %+v; want it naming its model", lic)
	}
	if n := len(rep.Stage(gate.StageFitness).Checks) - 1; n <= len(roleModels) {
		t.Errorf("%d fitness checks beside the warmed peak; want each model's", n)
	}
	if !log.seen || log.speed() != log.before {
		t.Errorf("speed requests: %d before the quality pass, %d after it; want none sent during it", log.before, log.speed())
	}

	// Every role's quality ran, and no unit carries a speed floor.
	q := rep.Stage(gate.StageQuality)
	for _, c := range units(q).Checks {
		if c.Unit == "tok/s" || c.Unit == "ms" || c.Unit == "s" || c.Model != "" {
			t.Errorf("quality check %+v; want no speed floor and no model's own", c)
		}
	}
	for _, c := range []struct{ role, name string }{
		{gate.RoleAgent, "agent-conform"}, {gate.RoleVision, "vision-mmmu"}, {gate.RoleAudio, "asr-fleurs-en"}, {gate.RoleEmbedding, "NFCorpus"},
	} {
		check(t, rep, gate.StageQuality, c.role, c.name)
	}
	x := check(t, rep, gate.StageQuality, "", "no crossed answer")
	for role := range roleModels {
		if x.Values["checked_"+role] < 1 {
			t.Errorf("crossing %+v; want %s checked", x, role)
		}
	}
	p := check(t, rep, gate.StageQuality, "", "peak pool free")
	if !x.Passed || !p.Passed || *p.Measured != 3333 || !strings.Contains(p.Detail, "every role's quality tiers ran at once") {
		t.Errorf("gates %+v, %+v; want both passed over every role's tiers at once", x, p)
	}

	// No alone figure or slowdown: they are the full run's.
	for _, s := range rep.Stages {
		for _, c := range s.Checks {
			if strings.HasSuffix(c.Name, " alone") || strings.Contains(c.Name, "slowdown") || c.Name == "load" {
				t.Errorf("%s ran %s in a screen", s.Name, c.Name)
			}
		}
	}
	if calls := h.uv.setCalls(t); len(calls) != 0 {
		t.Errorf("a screen ran %d set stage calls", len(calls))
	}
	var sum strings.Builder
	if err := gate.WriteSummary(&sum, rep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"screen: passed every gate, a finalist", "ok   " + gptOSS + ": agent decode: 50 tok/s"} {
		if !strings.Contains(sum.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, sum.String())
		}
	}
}

// TestScreenModelRun screens one model, set A's vision model: its role's tiers run with the set's
// other models loaded, the pass's pool and crossing rounds over every role's model, and fail the
// screen's quality stage, and so its finalist, when the pool falls under its floor or an answer
// crosses while the tiers run.
func TestScreenModelRun(t *testing.T) {
	h := newHarness(t, gemma26B)
	rep := h.run(t, "", nil)
	if got := statuses(rep); !equal(got, screenStatuses) || !rep.Finalist {
		t.Fatalf("stages %q; want a passed screen", got)
	}
	x := check(t, rep, gate.StageQuality, "", "no crossed answer")
	for role := range roleModels {
		if x.Values["checked_"+role] < 1 {
			t.Errorf("crossing %+v; want %s's model checked beside the model's tiers", x, role)
		}
	}
	if p := check(t, rep, gate.StageQuality, "", "peak pool free"); !strings.Contains(p.Detail, "the model's quality tiers ran, the set's other models loaded") {
		t.Errorf("peak pool free %q; want the model's pass named", p.Detail)
	}

	t.Run("pool", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		h.runner.Footprint = footprintFunc(func() int {
			if h.uv.evaluated() {
				return 2700
			}
			return 3333
		})
		rep := h.run(t, "", nil)
		p := check(t, rep, gate.StageQuality, "", "peak pool free")
		if p.Passed || *p.Measured != 2700 || rep.Finalist || rep.Stage(gate.StageSet).Reason != "quality failed" {
			t.Errorf("peak pool free %+v, finalist %v, set %q; want the screen failed at quality", p, rep.Finalist, rep.Stage(gate.StageSet).Reason)
		}
	})
	t.Run("crossing", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		// Capacity sends the vision model's requests and the embedding model's alone, so only the
		// quality pass's rounds reach the audio model.
		h.router.crossed = gemmaE4B
		rep := h.run(t, "", nil)
		x := check(t, rep, gate.StageQuality, "", "no crossed answer")
		if x.Passed || !strings.Contains(x.Detail, "audio/w0/r0: the answer carries "+crossedNonce) || rep.Finalist {
			t.Errorf("crossing %+v, finalist %v; want a failure naming the audio request", x, rep.Finalist)
		}
	})
}

// TestFullRun continues a finalist's screen: the full run reuses the screen's admission, fitness,
// capacity and warm-up, reuses every unit the screen ran whole, runs the rest of the one the
// screen stopped passed, its own list without a bar, and judges the whole list, then runs the set
// stage's alone figures, concurrent phase and slowdowns, and the deep stage.
func TestFullRun(t *testing.T) {
	cases := []struct {
		model, kind, unit, role, name, samples string
		bar                                    tier.Bar
	}{
		{gemma26B, "vision", "vstar", gate.RoleVision, "vision-vstar", "samples", tier.Bar{Value: 0.5}},
		{gemmaE4B, "asr", "ami", gate.RoleAudio, "asr-ami", "utterances", tier.Bar{Value: 0.5, AtMost: true}},
		{gptOSS, "agent", "bfcl-single", gate.RoleAgent, "agent-bfcl-single", "samples", tier.Bar{Value: 0.5}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			h := newHarness(t, tc.model)
			h.uv.variant(t, tc.kind, tc.unit, 4, gate.StopPass, 0.6)
			h.rewire()
			h.runner.Bars = tier.Bars{tc.name: tc.bar}
			screen := h.run(t, "", nil)
			if !screen.Finalist {
				t.Fatalf("screen %q; want a finalist", statuses(screen))
			}
			screened := len(h.uv.calls(t))
			reloads := len(h.router.commands())

			h.runner.Full = true
			rep := h.run(t, "", screen)
			want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
			if got := statuses(rep); !equal(got, want) || rep.Mode != gate.ModeFull || !rep.Finalist || !rep.Passed ||
				rep.Screened == nil || !rep.Screened.Equal(screen.Started) || rep.From != "" {
				t.Fatalf("full run %q, mode %q, finalist %v, screened %v; want every stage passed, continuing the screen", got, rep.Mode, rep.Finalist, rep.Screened)
			}
			for _, s := range rep.Stages[:3] {
				if !s.Reused {
					t.Errorf("%s not reused from the screen", s.Name)
				}
			}
			if rep.Stage(gate.StageQuality).Reused || rep.Warm == nil || !rep.Warm.Reused || len(h.router.commands()) != reloads {
				t.Errorf("quality reused %v, warm %+v; want quality continued and the screen's warm-up kept, nothing reloaded", rep.Stage(gate.StageQuality).Reused, rep.Warm)
			}

			// The stopped unit: the screen's 4 and the rest's 6, judged whole.
			c := check(t, rep, gate.StageQuality, tc.role, tc.name)
			if !c.Passed || c.Stop != nil || c.Values[tc.samples] != 10 || c.Values["screen_samples"] != 4 || c.Values["screen_wall_s"] <= 0 ||
				!strings.Contains(c.Detail, "full run: the screen's samples and the rest, the whole list judged") ||
				!strings.Contains(c.Detail, "the screen's 4 of 10 reused, early stop pass there") {
				t.Errorf("%s %+v; want the whole list of 10, the screen's 4 reused", tc.name, c)
			}
			// Exactly one more suite call: the rest, its own list, no bar.
			calls := h.uv.calls(t)
			if len(calls) != screened+1 {
				t.Fatalf("calls %q; want the screen's %d and the rest's one", calls[screened:], screened)
			}
			args, _, _ := strings.Cut(calls[screened], "|")
			restList := filepath.Join(h.runner.Dir, tc.kind, tc.unit, "rest", "list.txt")
			if tc.kind == "asr" {
				restList = filepath.Join(h.runner.Dir, "asr", tc.unit, "rest", "list.txt")
			}
			if !strings.Contains(args, restList) || strings.Contains(args, "--bar") {
				t.Errorf("rest call %s; want the rest's list %s, no bar", args, restList)
			}
			full, _ := tier.ReadListFile(filepath.Join("testdata", "tiers", tc.kind+"-"+tc.unit+".txt"))
			if rest, err := tier.ReadListFile(restList); err != nil || !slices.Equal(rest.IDs, full.IDs[4:]) || rest.Dataset != full.Dataset {
				t.Errorf("rest list %+v, %v; want the list's last 6 under its header", rest, err)
			}
			// Every other unit is the screen's record, and the screen's two gates are carried.
			for _, u := range units(rep.Stage(gate.StageQuality)).Checks {
				if u.Name != tc.name && !strings.Contains(u.Detail, "full run: the screen's record") {
					t.Errorf("%s detail %q; want the screen's record reused", u.Name, u.Detail)
				}
			}
			check(t, rep, gate.StageQuality, "", "no crossed answer")
			check(t, rep, gate.StageQuality, "", "peak pool free")
			// The set stage's slowdown and the deep stage are the full run's.
			check(t, rep, gate.StageSet, tc.role, "tier 1 slowdown")
			if s := rep.Stage(gate.StageDeep); s.Status != gate.StatusPass || len(s.Checks) == 0 {
				t.Errorf("deep %+v; want it run", s)
			}

			// A full run resumes at deep from a full run, and not from a screen.
			if _, err := h.runner.Run(t.Context(), gate.StageDeep, screen); err == nil || !strings.Contains(err.Error(), "screen run, not a full") {
				t.Errorf("resuming a full run from a screen: %v; want a refusal", err)
			}
			again := h.run(t, gate.StageDeep, rep)
			if again.Mode != gate.ModeFull || again.From != gate.StageDeep || !again.Stage(gate.StageSet).Reused {
				t.Errorf("resumed full run mode %q from %q; want a full run from deep, set reused", again.Mode, again.From)
			}
		})
	}
}

// TestFullRunScreensFirst shows a full run given no screen runs one first, then continues it in
// the same run; one whose screen fails returns the screen's report alone, no finalist; and a
// screen that isn't a finalist can't be continued.
func TestFullRunScreensFirst(t *testing.T) {
	h := newHarness(t, gemma26B)
	h.uv.variant(t, "vision", "vstar", 4, gate.StopPass, 0.6)
	h.rewire()
	h.runner.Bars = tier.Bars{"vision-vstar": {Value: 0.5}}
	h.runner.Full = true
	rep := h.run(t, "", nil)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
	if got := statuses(rep); !equal(got, want) || rep.Mode != gate.ModeFull || rep.Screened == nil {
		t.Fatalf("full run %q, mode %q; want the screen run first and continued", got, rep.Mode)
	}
	var barred, rest int
	for _, c := range h.uv.calls(t) {
		switch {
		case strings.Contains(c, "--tier vstar ") && strings.Contains(c, "--bar 0.5"):
			barred++
		case strings.Contains(c, "/vision/vstar/rest/list.txt"):
			rest++
		}
	}
	if barred != 1 || rest != 1 {
		t.Errorf("V*Bench ran %d times screened and %d as the rest; want once each", barred, rest)
	}

	failing := newHarness(t, gemma26B)
	failing.runner.Bars = tier.Bars{"vision-vstar": {Value: 0.99}}
	failing.runner.Full = true
	screen := failing.run(t, "", nil)
	if screen.Mode != gate.ModeScreen || screen.Finalist || screen.Stage(gate.StageDeep).Reason != "quality failed" {
		t.Errorf("a failed screen's full run: mode %q finalist %v %q; want the screen's report alone", screen.Mode, screen.Finalist, statuses(screen))
	}
	if _, err := failing.runner.Run(t.Context(), "", screen); err == nil || !strings.Contains(err.Error(), "no finalist") {
		t.Errorf("continuing a failed screen: %v; want a refusal naming no finalist", err)
	}
	if _, err := failing.runner.Run(t.Context(), gate.StageQuality, screen); err == nil || !strings.Contains(err.Error(), "resumes only at set or deep") {
		t.Errorf("a full run from quality: %v; want a refusal", err)
	}
	failing.runner.Full = false
	if _, err := failing.runner.Run(t.Context(), gate.StageSet, screen); err == nil || !strings.Contains(err.Error(), "a screen ends at quality") {
		t.Errorf("a screen from set: %v; want a refusal", err)
	}
}
