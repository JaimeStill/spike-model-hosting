package gate_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// setUnits are set A's tier 1 units, by role, in the order the stage records them.
var setUnits = map[string][]string{
	gate.RoleAgent:     {"agent-bfcl-single"},
	gate.RoleVision:    {"vision-probe", "vision-vstar"},
	gate.RoleAudio:     {"asr-librispeech-clean"},
	gate.RoleEmbedding: {"STSBenchmark"},
}

// roleModels are set A's role models.
var roleModels = map[string]string{
	gate.RoleAgent: gptOSS, gate.RoleVision: gemma26B, gate.RoleAudio: gemmaE4B, gate.RoleEmbedding: embedding,
}

func TestTierOne(t *testing.T) {
	for role, want := range setUnits {
		if got := gate.TierOne(role); !slices.Equal(got, want) {
			t.Errorf("TierOne(%s) = %q; want %q", role, got, want)
		}
	}
}

// TestSetRun runs set A's set stage as a set run: every stage before it is reported not run,
// each role's tier 1 runs on its model alone and then at once, without its bar even where one
// would fail it, and the stage passes on the pool's floor and no crossed answer.
func TestSetRun(t *testing.T) {
	h := newHarness(t, "")
	// A bar V*Bench's recorded score falls short of: the quality stage would fail it and stop it early.
	h.runner.Bars = tier.Bars{"vision-vstar": {Value: 0.99}}
	rep := h.run(t, "", nil)
	want := []string{"admission not run", "fitness not run", "capacity not run", "quality not run", "set pass", "deep not run"}
	if got := statuses(rep); !equal(got, want) {
		t.Fatalf("stages %q; want %q\n%+v", got, want, rep.Stages)
	}
	if rep.Model != "" || !equal(rep.Roles, gate.Roles) || !rep.Passed || rep.Reached != gate.StageSet {
		t.Errorf("model %q roles %q passed %v reached %q; want a set run of every role, passed at set", rep.Model, rep.Roles, rep.Passed, rep.Reached)
	}
	if r := rep.Stage(gate.StageQuality).Reason; !strings.Contains(r, "set run") {
		t.Errorf("quality's reason %q; want it to name the set run", r)
	}

	for role, units := range setUnits {
		for _, u := range units {
			a := check(t, rep, gate.StageSet, role, u+" alone")
			c := check(t, rep, gate.StageSet, role, u+" concurrent")
			if !a.Passed || !c.Passed || a.Measured == nil || c.Measured == nil || a.Floor != nil || c.Floor != nil {
				t.Errorf("%s %s: alone %+v, concurrent %+v; want both recorded, unjudged", role, u, a, c)
			}
			if c.Values["alone_score"] != *a.Measured || c.Values["score_change"] != *c.Measured-*a.Measured ||
				c.Values["alone_wall_s"] != a.Values["wall_s"] || c.Values["slowdown"] <= 0 {
				t.Errorf("%s %s concurrent values %v; want the alone score, its change, the alone wall and the slowdown", role, u, c.Values)
			}
		}
		s := check(t, rep, gate.StageSet, role, "tier 1 slowdown")
		if s.Measured == nil || *s.Measured <= 0 || s.Values["alone_wall_s"] <= 0 || s.Values["concurrent_wall_s"] <= 0 ||
			s.Values["alone_pool_free_min_mib"] != 3333 {
			t.Errorf("%s slowdown %+v; want both walls, their ratio, and the alone phase's pool", role, s)
		}
	}
	if v := check(t, rep, gate.StageSet, gate.RoleVision, "vision-vstar concurrent"); !strings.Contains(v.Detail, "never judged") || v.Values["samples"] != 10 {
		t.Errorf("V*Bench concurrent %+v; want all of the test tier's 10 run, recorded, not judged", v)
	}

	x := check(t, rep, gate.StageSet, "", "no crossed answer")
	for _, role := range []string{gate.RoleAgent, gate.RoleVision, gate.RoleAudio} {
		if x.Values["checked_"+role] != x.Values["rounds"] {
			t.Errorf("crossing %+v; want one %s answer a round", x, role)
		}
	}
	if !x.Passed || x.Values["rounds"] < 1 || x.Values["checked_embedding"] < 8*x.Values["rounds"] {
		t.Errorf("crossing %+v; want passed, with the embedding batch each round", x)
	}
	p := check(t, rep, gate.StageSet, "", "peak pool free")
	if !p.Passed || *p.Measured != 3333 || *p.Floor != 2816 {
		t.Errorf("peak pool free %+v; want 3333 against 2816", p)
	}

	// Each unit ran twice, once a phase, on its role's model, with no bar.
	calls := h.uv.setCalls(t)
	if len(calls) != 2*5 {
		t.Fatalf("%d set stage uv calls; want 10:\n%s", len(calls), strings.Join(calls, "\n"))
	}
	for _, phase := range []string{"alone", "concurrent"} {
		for role, units := range setUnits {
			for _, u := range units {
				unit := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(u, "agent-"), "vision-"), "asr-")
				n := 0
				for _, c := range calls {
					args, _, _ := strings.Cut(c, "|")
					if strings.Contains(args, "/set/"+phase+"/") && strings.Contains(args, "/"+unit) {
						n++
						if !strings.Contains(args, "--model "+roleModels[role]+" ") || strings.Contains(args, "--bar") {
							t.Errorf("%s %s: %s; want %s's model, no bar", phase, u, args, role)
						}
					}
				}
				if n != 1 {
					t.Errorf("%s %s ran %d times; want once", phase, u, n)
				}
			}
		}
	}
	if n := len(h.uv.calls(t)); n != 0 {
		t.Errorf("a set run ran %d quality calls", n)
	}
}

// loads fails t unless each of the set's roles other than the vision model's carried load from
// workers workers in rep's set stage, answered, recorded and unjudged.
func loads(t *testing.T, rep *gate.Report, workers float64) {
	t.Helper()
	for _, role := range []string{gate.RoleAgent, gate.RoleAudio, gate.RoleEmbedding} {
		l := check(t, rep, gate.StageSet, role, "load")
		if !l.Passed || l.Measured == nil || l.Values["errors"] != 0 || l.Values["workers"] != workers || l.Values["window_s"] <= 0 || l.Floor != nil {
			t.Errorf("%s load %+v; want answered requests from %v workers over the window, recorded, unjudged", role, l, workers)
		}
	}
}

// TestSetModelRun shows a model's run takes the set stage after quality: the model's tier 1
// runs once, under load from the set's other role models, beside its quality tier 1 as the alone
// figure, which it never reruns; the other roles run no tier, and their load stops with the
// model's tiers. It resumes from the set stage.
func TestSetModelRun(t *testing.T) {
	h := newHarness(t, gemma26B)
	first := h.run(t, "", nil)
	if s := first.Stage(gate.StageSet); s.Status != gate.StatusPass {
		t.Fatalf("set %s; want pass\n%+v", s.Status, s.Checks)
	}
	// The load stopped when the model's tiers finished: nothing reaches the other models after.
	after := h.router.count("capacity") + h.router.count("embeddings")
	time.Sleep(50 * time.Millisecond)
	if n := h.router.count("capacity") + h.router.count("embeddings"); n != after {
		t.Errorf("%d requests after the run returned; want the load stopped with the tiers", n-after)
	}

	// Only the model's own units ran, once, in the concurrent phase, on its model, with no bar.
	calls := h.uv.setCalls(t)
	if len(calls) != len(setUnits[gate.RoleVision]) {
		t.Fatalf("%d set stage uv calls; want the vision tier 1's %d:\n%s", len(calls), len(setUnits[gate.RoleVision]), strings.Join(calls, "\n"))
	}
	for _, c := range calls {
		args, _, _ := strings.Cut(c, "|")
		if !strings.Contains(args, "/set/concurrent/vision/") || !strings.Contains(args, "--model "+gemma26B+" ") || strings.Contains(args, "--bar") {
			t.Errorf("set stage call %s; want the vision tier on %s, concurrent, no bar", args, gemma26B)
		}
	}

	// Each concurrent unit carries its quality record as the alone figure; no unit runs alone.
	for _, u := range setUnits[gate.RoleVision] {
		q := check(t, first, gate.StageQuality, gate.RoleVision, u)
		c := check(t, first, gate.StageSet, gate.RoleVision, u+" concurrent")
		if c.Values["alone_score"] != *q.Measured || c.Values["alone_wall_s"] != q.Values["wall_s"] ||
			c.Values["slowdown"] != c.Values["wall_s"]/q.Values["wall_s"] {
			t.Errorf("%s concurrent values %v; want quality's score %v and wall %v as the alone figure", u, c.Values, *q.Measured, q.Values["wall_s"])
		}
	}
	for _, c := range first.Stage(gate.StageSet).Checks {
		if strings.HasSuffix(c.Name, " alone") {
			t.Errorf("a model run's set stage ran %s", c.Name)
		}
	}
	s := check(t, first, gate.StageSet, gate.RoleVision, "tier 1 slowdown")
	qWall := check(t, first, gate.StageQuality, gate.RoleVision, "vision-probe").Values["wall_s"] +
		check(t, first, gate.StageQuality, gate.RoleVision, "vision-vstar").Values["wall_s"]
	if s.Measured == nil || s.Values["alone_wall_s"] != qWall || !strings.Contains(s.Detail, "its quality tier 1") {
		t.Errorf("slowdown %+v; want the concurrent wall over quality's tier 1 wall, %v", s, qWall)
	}

	// The other roles carried load, one worker each by default, recorded as load, not as scored
	// tiers.
	loads(t, first, 1)
	// The fake answers a chat in well under a millisecond, and the tiers' uv calls each take longer.
	if l := check(t, first, gate.StageSet, gate.RoleAgent, "load"); l.Values["requests"] < 1 || *l.Measured <= 0 {
		t.Errorf("agent load %+v; want answered requests", l)
	}
	for _, c := range first.Stage(gate.StageSet).Checks {
		if c.Role == gate.RoleVision && c.Name == "load" {
			t.Error("the model's own role carried load")
		}
	}
	check(t, first, gate.StageSet, "", "no crossed answer")
	if p := check(t, first, gate.StageSet, "", "peak pool free"); !strings.Contains(p.Detail, "under load") {
		t.Errorf("peak pool free %q; want it over the window the model's tier ran under load", p.Detail)
	}

	quality := len(h.uv.calls(t))
	h.runner.LoadWorkers = 2
	rep := h.run(t, gate.StageSet, first)
	loads(t, rep, 2)
	want := []string{"admission pass", "fitness pass", "capacity pass", "quality pass", "set pass", "deep pass"}
	if got := statuses(rep); !equal(got, want) || !rep.Stage(gate.StageQuality).Reused || rep.Stage(gate.StageSet).Reused {
		t.Fatalf("resumed %q; want %q, quality reused, set run", got, want)
	}
	if len(h.uv.calls(t)) != quality || len(h.uv.setCalls(t)) != 2*len(calls) {
		t.Errorf("the resumed run reran quality, or not the set stage")
	}
	if c := check(t, rep, gate.StageSet, gate.RoleVision, "vision-vstar concurrent"); c.Values["alone_wall_s"] !=
		check(t, first, gate.StageQuality, gate.RoleVision, "vision-vstar").Values["wall_s"] {
		t.Errorf("resumed V*Bench %v; want the resumed run's quality record as the alone figure", c.Values)
	}

	// A run whose quality failed can't be resumed past it.
	failed := *first
	failed.Stages = slices.Clone(first.Stages[:4])
	failed.Stages[3].Status = gate.StatusFail
	if _, err := h.runner.Run(t.Context(), gate.StageSet, &failed); err == nil || !strings.Contains(err.Error(), "quality fail") {
		t.Errorf("resuming past a failed quality: %v; want an error naming it", err)
	}

	// Nor one whose quality holds no record of a tier 1 unit: there is no alone figure to reuse.
	missing := *first
	missing.Stages = slices.Clone(first.Stages[:4])
	q := missing.Stages[3]
	q.Checks = slices.DeleteFunc(slices.Clone(q.Checks), func(c gate.Check) bool { return c.Name == "vision-vstar" })
	missing.Stages[3] = q
	before := len(h.uv.setCalls(t))
	if _, err := h.runner.Run(t.Context(), gate.StageSet, &missing); err == nil ||
		!strings.Contains(err.Error(), "can't resume from set") || !strings.Contains(err.Error(), "vision-vstar") {
		t.Errorf("resuming without a quality tier 1 record: %v; want a refusal naming the unit", err)
	}
	if len(h.uv.setCalls(t)) != before {
		t.Error("the refused run ran the set stage")
	}
}

// TestSetModelRunGates shows a model run's set stage fails on the pool under its floor while
// its tiers run under load, and on a background role whose load is never answered.
func TestSetModelRunGates(t *testing.T) {
	t.Run("pool", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		var mu sync.Mutex
		inSet := false
		h.runner.Footprint = footprintFunc(func() int {
			mu.Lock()
			defer mu.Unlock()
			// The set stage's load and crossing send nonce requests to the other roles' models;
			// capacity sends them only to the model's.
			if inSet || h.router.count("capacity true "+gptOSS)+h.router.count("capacity false "+gptOSS) > 0 {
				inSet = true
				return 2700
			}
			return 3333
		})
		rep := h.run(t, "", nil)
		if s := rep.Stage(gate.StageSet).Status; s != gate.StatusFail || rep.Passed {
			t.Errorf("set %s, passed %v; want failed", s, rep.Passed)
		}
		if p := check(t, rep, gate.StageSet, "", "peak pool free"); p.Passed || *p.Measured != 2700 {
			t.Errorf("peak pool free %+v; want a failure at 2700", p)
		}
	})
	t.Run("load refused", func(t *testing.T) {
		h := newHarness(t, gemma26B)
		first := h.run(t, "", nil)
		h.router.refuse = gemmaE4B
		rep := h.run(t, gate.StageSet, first)
		l := check(t, rep, gate.StageSet, gate.RoleAudio, "load")
		if l.Passed || l.Values["requests"] != 0 || l.Values["errors"] < 1 || !strings.Contains(l.Detail, "none of "+gemmaE4B) {
			t.Errorf("audio load %+v; want a failure, none answered", l)
		}
		if rep.Stage(gate.StageSet).Status != gate.StatusFail {
			t.Error("the set stage passed a load that was never answered")
		}
	})
}

// TestSetGates shows the set stage fails on the pool under its floor while every tier runs at
// once, on an answer carrying another request's nonce, and on a set model not loaded.
func TestSetGates(t *testing.T) {
	t.Run("pool", func(t *testing.T) {
		h := newHarness(t, "")
		// Only the crossing probe, in the concurrent phase, sends nonce requests in a set run.
		h.runner.Footprint = footprintFunc(func() int {
			if h.router.count("capacity") > 0 {
				return 2700
			}
			return 3333
		})
		rep := h.run(t, "", nil)
		if s := rep.Stage(gate.StageSet).Status; s != gate.StatusFail || rep.Passed {
			t.Errorf("set %s, passed %v; want failed", s, rep.Passed)
		}
		if p := check(t, rep, gate.StageSet, "", "peak pool free"); p.Passed || *p.Measured != 2700 {
			t.Errorf("peak pool free %+v; want a failure at 2700", p)
		}
		if s := check(t, rep, gate.StageSet, gate.RoleAudio, "tier 1 slowdown"); s.Values["alone_pool_free_min_mib"] != 3333 {
			t.Errorf("audio's alone pool %v; want 3333, the alone phase's, ungated", s.Values)
		}
		if r := rep.Stage(gate.StageDeep).Reason; r != "set failed" {
			t.Errorf("deep's reason %q; want set failed", r)
		}
	})
	t.Run("crossing", func(t *testing.T) {
		h := newHarness(t, "")
		h.router.crossed = gemmaE4B
		rep := h.run(t, "", nil)
		x := check(t, rep, gate.StageSet, "", "no crossed answer")
		if x.Passed || !strings.Contains(x.Detail, "crossed: round 1: audio/w0/r0: the answer carries "+crossedNonce) {
			t.Errorf("crossing %+v; want a failure naming the audio request and the foreign nonce", x)
		}
		if rep.Stage(gate.StageSet).Status != gate.StatusFail {
			t.Error("the set stage passed a crossed answer")
		}
	})
	t.Run("unanswered", func(t *testing.T) {
		// An empty answer carries no other request's content: recorded unanswered, not crossed;
		// but a role with none checked proves nothing, and fails.
		h := newHarness(t, "")
		h.router.empty = gemma26B
		rep := h.run(t, "", nil)
		x := check(t, rep, gate.StageSet, "", "no crossed answer")
		if x.Passed || x.Values["crossed"] != 0 || x.Values["unanswered"] != x.Values["rounds"] ||
			!strings.Contains(x.Detail, "nothing checked for vision") || !strings.Contains(x.Detail, "vision/w0/r0: the answer's text is empty") {
			t.Errorf("crossing %+v; want no crossing, every vision answer unanswered, and a failure for nothing checked", x)
		}
	})
	t.Run("cut off", func(t *testing.T) {
		// An answer that hit its max_tokens before any nonce carries nothing to cross: recorded
		// unanswered, naming why, not crossed.
		h := newHarness(t, "")
		h.router.cutOff = gemma26B
		rep := h.run(t, "", nil)
		x := check(t, rep, gate.StageSet, "", "no crossed answer")
		if x.Values["crossed"] != 0 || x.Values["unanswered"] != x.Values["rounds"] ||
			!strings.Contains(x.Detail, "vision/w0/r0: the answer hit its max_tokens after 10 tokens, with no nonce") {
			t.Errorf("crossing %+v; want no crossing and every vision answer unanswered as cut off", x)
		}
	})
	t.Run("cut off crossed", func(t *testing.T) {
		// A cut-off answer that carries a nonce not its own is still crossed.
		h := newHarness(t, "")
		h.router.cutOff, h.router.crossed = gemma26B, gemma26B
		rep := h.run(t, "", nil)
		x := check(t, rep, gate.StageSet, "", "no crossed answer")
		if x.Passed || x.Values["crossed"] == 0 || !strings.Contains(x.Detail, "crossed: round 1: vision/w0/r0: the answer carries "+crossedNonce) {
			t.Errorf("crossing %+v; want a failure naming the cut-off vision answer's foreign nonce", x)
		}
	})
	t.Run("unloaded", func(t *testing.T) {
		h := newHarness(t, gptOSS)
		first := h.run(t, "", nil)
		h.router.status[gemmaE4B] = "unloaded"
		rep := h.run(t, gate.StageSet, first)
		if c := check(t, rep, gate.StageSet, "", "set loaded"); c.Passed || !strings.Contains(c.Detail, gemmaE4B) {
			t.Errorf("set loaded %+v; want a failure naming the unloaded model", c)
		}
		// A set run refuses before any stage.
		h.runner.Model = ""
		if _, err := h.runner.Run(t.Context(), "", nil); !errors.Is(err, gate.ErrNotLoaded) {
			t.Errorf("a set run with a model unloaded: %v; want ErrNotLoaded", err)
		}
	})
	t.Run("from", func(t *testing.T) {
		h := newHarness(t, "")
		if _, err := h.runner.Run(t.Context(), gate.StageCapacity, nil); err == nil {
			t.Error("a set run ran from capacity")
		}
	})
}

// footprintFunc is a pool whose free memory free returns, out of a 98304MiB pool.
type footprintFunc func() int

func (f footprintFunc) Read(context.Context) (gate.Pool, error) {
	return gate.Pool{UsedMiB: 98304 - f(), TotalMiB: 98304}, nil
}
