package gate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// The stages, in the order a run takes them.
const (
	StageAdmission = "admission"
	StageFitness   = "fitness"
	StageCapacity  = "capacity"
	StageQuality   = "quality"
	StageSet       = "set"
	StageDeep      = "deep"
)

// Stages are every stage, in order.
var Stages = []string{StageAdmission, StageFitness, StageCapacity, StageQuality, StageSet, StageDeep}

// ErrNotLoaded is the error of a run refused because the router doesn't list its model loaded.
var ErrNotLoaded = errors.New("model not loaded")

// Runner runs the stages for one model of a profile's set against the router, or, with no Model,
// for every model of the set together: a set run.
type Runner struct {
	// Router is the router's root URL; HTTP, the client that reaches it (nil for a default one).
	Router string
	HTTP   *http.Client
	// Profile and Set are the profile the router serves and the set Model is evaluated in.
	Profile *profile.Profile
	Set     *profile.Set
	// Model is the model evaluated, by router ID: one the set assigns at least one role. Empty
	// makes the run a set run, which takes every model of the set through the stages, across
	// every role the set assigns.
	Model     string
	Floors    Floors
	Footprint Footprint
	Media     Media
	// SampleEvery spaces the footprint's readings while capacity's requests are in flight, and
	// while a screen's quality tiers or the set stage's tiers run; zero is two seconds.
	SampleEvery time.Duration
	// CrossEvery spaces the rounds of nonce requests over a screen's quality pass and the set
	// stage's concurrent phase; zero is 15 seconds.
	CrossEvery time.Duration
	// LoadWorkers is how many workers each of the set's other roles sends its load with, one
	// request after another, in a model run's set stage; zero is one. One worker a role keeps the
	// other models resident and interleaving with the model's tiers, which is what the stage
	// gates: memory (KV is fixed when a model loads) and crossing. The tier-1 concurrency, four,
	// slowed the vision model's set stage 3.17x on set A, against 1.68x in a set run.
	LoadWorkers int
	// Log takes a line as each stage starts, for a reader watching a long run; nil discards it.
	Log io.Writer
	// Bars are the quality stage's bars, by tier or task name (evaluation/bars.toml); a task
	// without one is recorded and never fails.
	Bars tier.Bars
	// MTEB runs the embedding role's MTEB tasks, and ASR the audio role's ASR sets; Dir is the
	// run's directory, under which each task's MTEB output is kept, in mteb/<task>, and each
	// set's Inspect log, in asr/<set>.
	MTEB MTEB
	ASR  ASR
	// Vision runs the vision role's tiers, each tier's Inspect logs kept in the run's
	// vision/<tier>, and Agent the agent role's BFCL tiers, kept in its agent/<tier>.
	Vision Vision
	Agent  Agent
	Dir    string
	// Conform runs the agent role's conform tier, harness fit, through the harnesses, its
	// recordings and table kept in the run's conform/.
	Conform Conform
	// Tiers is the directory of the tier lists the ASR sets, vision tiers, BFCL tiers and τ2
	// domains run (TiersDir, under the module root).
	Tiers string
	// Tau2 runs the deep stage's τ2 domains, each domain's Inspect log kept in the run's
	// deep/tau2/<domain>, and Grid its GuideLLM cells, kept in its deep/grid.
	Tau2 Tau2
	Grid Grid
	// Restarts says the architect is at hand to restart the router when the deep stage's cold load
	// asks; without it, cold load is recorded not run.
	Restarts bool
	// PollEvery spaces cold load's polls of /models; zero is 250 milliseconds. The reload before a
	// run polls at it too, where zero is the swap package's default.
	PollEvery time.Duration
	// Peaks finds the largest image and the longest clip the tier lists send, which the warm-up
	// before a run sends the vision and audio roles' models (Warm).
	Peaks Peaks
	// Settle is the wait after the reload before the idle reading; zero is 30 seconds.
	Settle time.Duration
	// Full makes the run a full run, a finalist's: the screen's records, the rest of each
	// tier, the set stage and the deep stage. Without it the run is a screen, which ends at the
	// quality stage.
	Full bool
	// Index is evaluate's index of passed results: with one, the run reuses each unit whose key
	// matches a passed entry, and records each unit it passes, for the caller to save. Fresh
	// reuses nothing, measuring every unit again, and still records. Without an Index, the run
	// reuses and records nothing.
	Index *Index
	Fresh bool

	// screen is the screen a full run continues.
	screen *Report
	// keys are the run's keys, read once its build is.
	keys *Keys
}

// Roles returns the roles the set assigns Model, in the order Roles lists them; for a set run,
// every role the set assigns.
func (r *Runner) Roles() []string {
	var out []string
	for _, a := range r.assignments() {
		if r.Model == "" || a.model == r.Model {
			out = append(out, a.role)
		}
	}
	return out
}

// SetSubject is the name a set run's directory takes in place of a model's: set-<name>.
func SetSubject(set string) string { return "set-" + set }

// Why a screen reports the set and deep stages not run.
const (
	screenSetReason  = "a screen: its quality stage's concurrent pass stands in for the set stage's concurrent phase; the alone figures and slowdowns are the full run's"
	screenDeepReason = "a screen: the deep stage is a finalist's, in its full run"
)

// Run runs the run's stages in order and returns its report, stopping at the first stage that
// fails: every later stage is reported not run, naming it. Before any stage it refuses a model
// the router doesn't list as loaded, with an error wrapping ErrNotLoaded, and reads the router's
// build.
//
// A run is a screen unless Full is set: admission, fitness and capacity, then the quality stage,
// every role's tiers at once (quality), the set and deep stages reported not run. A screen that
// passes every stage makes its model, or its set's models, a finalist (Report.Finalist). A model
// run's stages are its model's; a set run (no Model) runs admission, fitness, capacity and deep
// model by model, each model's checks naming it, and the quality stage across every role.
//
// A full run continues a finalist's screen: prior, when it is one, or else a screen Run takes
// first, returning that screen's report alone when it isn't a finalist. It reuses the screen's
// admission, fitness and capacity and its warm-up, continues its quality stage (the rest of each
// tier the screen stopped), then runs the set stage, alone figures, the concurrent phase and
// slowdowns, and the deep stage.
//
// A run that resumes nothing starts from freshly loaded models (Warm): once admission has
// passed on paper, and before any stage that sends a request, it reloads the set over the
// router's API, reads the idle pool, drives each of the set's models to its high-water mark and
// reads the pool at that warmed peak, which fitness judges against the pool's floor. Such a run
// refuses unless every model of the set is loaded, since the reload unloads and loads them all;
// it never loads a model it didn't find loaded. A reload that fails returns the report so far
// with its error. A resumed or continued run reloads nothing and keeps the warm-up of the run it
// resumes.
//
// from names the stage to start at; empty starts at admission, or a full run at its
// continuation. A run from a later stage reuses prior's results for every stage before it: prior
// must be a run of the same model (or set run) and set, and of the same mode, in which each of
// them passed. A screen starts no later than quality, and a full run resumes only at set or deep.
// A model's full run from set also needs prior's quality records of the model's tier 1 units,
// which its set stage reuses as the alone figure.
func (r *Runner) Run(ctx context.Context, from string, prior *Report) (*Report, error) {
	if r.HTTP == nil {
		r.HTTP = &http.Client{}
	}
	r.screen = nil
	if !r.Full {
		return r.run(ctx, ModeScreen, from, prior)
	}
	if from != "" {
		if i := slices.Index(Stages, from); i >= 0 && i <= slices.Index(Stages, StageQuality) {
			return nil, fmt.Errorf("a full run resumes only at set or deep; from %s, screen again and continue the screen", from)
		}
		return r.run(ctx, ModeFull, from, prior)
	}
	screen := prior
	if screen == nil {
		rep, err := r.run(ctx, ModeScreen, "", nil)
		if err != nil || !rep.Finalist {
			return rep, err
		}
		screen = rep
	}
	switch {
	case screen.Mode != ModeScreen:
		return nil, fmt.Errorf("a full run continues a screen, and the run given is a %s run", cmp.Or(screen.Mode, "pre-screen"))
	case !screen.Finalist:
		return nil, fmt.Errorf("the screen of %s reached %s and didn't pass every gate: no finalist, so no full run", cmp.Or(screen.Model, SetSubject(screen.Set)), screen.Reached)
	}
	r.screen = screen
	return r.run(ctx, ModeFull, StageQuality, screen)
}

// run runs the stages from from (empty for all) in mode, reusing prior's before it.
func (r *Runner) run(ctx context.Context, mode, from string, prior *Report) (*Report, error) {
	start := 0
	if from != "" {
		start = slices.Index(Stages, from)
		if start < 0 {
			return nil, fmt.Errorf("no stage %q; the stages are %q", from, Stages)
		}
	}
	if mode == ModeScreen && start > slices.Index(Stages, StageQuality) {
		return nil, fmt.Errorf("a screen ends at quality; resume from %s in a full run", from)
	}
	setRun := r.Model == ""
	roles := r.Roles()
	if len(roles) == 0 {
		return nil, fmt.Errorf("set %s assigns %s no role", r.Set.Name, cmp.Or(r.Model, "any model"))
	}
	fresh := start == 0
	if err := r.checkLoaded(ctx, fresh); err != nil {
		return nil, err
	}
	build, err := router.Build(ctx, r.HTTP, r.Router)
	if err != nil {
		return nil, err
	}
	r.keys = nil
	if r.Index != nil {
		if r.keys, err = r.keysOf(build); err != nil {
			return nil, err
		}
	}
	rep := &Report{
		Router: r.Router, Build: build, HostClass: r.Profile.HostClass, Set: r.Set.Name,
		Model: r.Model, Roles: roles, Mode: mode, Started: time.Now().UTC(), Keys: r.keys,
	}
	defer r.record(rep)
	if start > 0 {
		if r.screen != nil {
			screened := r.screen.Started
			rep.Screened = &screened
		} else {
			rep.From = from
		}
		if err := reuse(rep, prior, start); err != nil {
			return nil, err
		}
		if prior.Warm != nil {
			w := *prior.Warm
			w.Reused = true
			rep.Warm = &w
		}
		// A model's full run takes its set stage's alone figures from the quality stage, so a run
		// resumed at it needs them in the run it resumes.
		if Stages[start] == StageSet && !setRun {
			if _, err := qualityTierOne(rep.Stage(StageQuality), roles); err != nil {
				return nil, fmt.Errorf("can't resume from set: %w, which its set stage reuses as the alone figure; continue the screen again", err)
			}
		}
	}
	stages := map[string]func(context.Context, *Report) []Check{
		StageAdmission: r.admission,
		StageFitness:   r.fitness,
		StageCapacity:  r.capacity,
		StageQuality:   r.quality,
		StageSet:       r.set,
		StageDeep:      r.deep,
	}
	failed := ""
	for _, name := range Stages[start:] {
		switch {
		case failed != "":
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotRun, Reason: failed + " failed"})
			continue
		case mode == ModeScreen && name == StageSet:
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotRun, Reason: screenSetReason})
			continue
		case mode == ModeScreen && name == StageDeep:
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotRun, Reason: screenDeepReason})
			continue
		}
		if fresh && rep.Warm == nil && name != StageAdmission {
			w, err := r.warm(ctx)
			rep.Warm = w
			if err != nil {
				return rep, err
			}
		}
		run := stages[name]
		r.logf("evaluate: %s %s (%s)\n", name, cmp.Or(r.Model, SetSubject(r.Set.Name)), mode)
		began := time.Now()
		// The set stage is reused whole, under the set's key: every model of the set unchanged.
		checks, ok := []Check(nil), false
		if name == StageSet {
			checks, ok = r.reusable(mode, r.setEntry(StageSet, "", "", cmp.Or(r.Model, SetSubject(r.Set.Name))))
		}
		if !ok {
			checks = run(ctx, rep)
		}
		res := StageResult{Name: name, Status: StatusPass, Checks: checks, WallS: time.Since(began).Seconds()}
		for _, c := range checks {
			if !c.Passed {
				res.Status, failed = StatusFail, name
			}
		}
		rep.Stages = append(rep.Stages, res)
		rep.Reached = name
		if err := ctx.Err(); err != nil {
			return rep, err
		}
	}
	rep.Passed = failed == ""
	// A screen that passed every stage makes a finalist; a full run is one's.
	rep.Finalist = rep.Passed || mode == ModeFull
	rep.Ceiling = r.ceiling(rep)
	return rep, nil
}

// perModel runs fn, a stage that judges one model (admission's model checks, fitness, capacity,
// deep's model parts), for the run: on Model for a model run; for a set run, on each of the set's
// models in turn, alone, as its own model run would, each check naming its model, and each model's
// output under the run directory's <Slug(model)>/.
func (r *Runner) perModel(ctx context.Context, rep *Report, fn func(*Runner, context.Context, *Report) []Check) []Check {
	if r.Model != "" {
		return fn(r, ctx, rep)
	}
	var checks []Check
	var seen []string
	for _, a := range r.assignments() {
		if slices.Contains(seen, a.model) {
			continue
		}
		seen = append(seen, a.model)
		sub := *r
		sub.Model, sub.Dir = a.model, filepath.Join(r.Dir, Slug(a.model))
		subRep := *rep
		subRep.Model, subRep.Roles = a.model, sub.Roles()
		for _, c := range fn(&sub, ctx, &subRep) {
			c.Model = a.model
			checks = append(checks, c)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return checks
}

// reuse copies prior's results for the stages before Stages[start] into rep.
func reuse(rep, prior *Report, start int) error {
	if prior == nil {
		return fmt.Errorf("resuming from %s needs the run to resume, and there is none", Stages[start])
	}
	if prior.Model != rep.Model || prior.Set != rep.Set {
		return fmt.Errorf("the run to resume is of %s in set %s, not %s in set %s", prior.Model, prior.Set, rep.Model, rep.Set)
	}
	// A full run continues a screen (rep.Screened) or resumes a full run; a screen resumes a screen.
	want := rep.Mode
	if rep.Screened != nil {
		want = ModeScreen
	}
	if prior.Mode != want {
		return fmt.Errorf("the run to resume is a %s run, not a %s", cmp.Or(prior.Mode, "pre-screen"), want)
	}
	for _, name := range Stages[:start] {
		s := prior.Stage(name)
		if s == nil || s.Status != StatusPass {
			status := "absent"
			if s != nil {
				status = s.Status
			}
			return fmt.Errorf("can't resume from %s: the run to resume has %s %s, not passed", Stages[start], name, status)
		}
		reused := *s
		reused.Reused = true
		rep.Stages = append(rep.Stages, reused)
		rep.Reached = name
	}
	return nil
}

// checkLoaded refuses the run unless the router lists Model loaded, or, for a run that reloads
// the set first (whole), every model of the set.
func (r *Runner) checkLoaded(ctx context.Context, whole bool) error {
	status, err := router.Statuses(ctx, r.HTTP, r.Router)
	if err != nil {
		return err
	}
	models := []string{r.Model}
	if whole || r.Model == "" {
		models = r.Set.Models
	}
	for _, m := range models {
		s, ok := status[m]
		if !ok {
			s = "unlisted"
		}
		if s != router.StatusLoaded {
			return fmt.Errorf("%w: the router lists %s as %s; evaluate reloads only a set it finds loaded and never loads a model, so load it (or swap its set in) first",
				ErrNotLoaded, m, s)
		}
	}
	return nil
}

// logf writes a line to Log, when there is one.
func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		_, _ = fmt.Fprintf(r.Log, format, args...)
	}
}

// model returns the profile's entry for Model.
func (r *Runner) model() *profile.Model { return r.Profile.Model(r.Model) }

// ceiling compares the run's first measured decode with the model's bandwidth ceiling, or
// returns nil when the run measured none or the profile lacks a figure it needs.
func (r *Runner) ceiling(rep *Report) *Ceiling {
	m := r.model()
	fit := rep.Stage(StageFitness)
	if m == nil || fit == nil || m.ActiveParamsB == 0 || r.Profile.MemoryBandwidthGBs == 0 {
		return nil
	}
	for _, c := range fit.Checks {
		v, ok := c.Values["decode_tok_s"]
		if !ok || v <= 0 {
			continue
		}
		bytes := m.ActiveParamsB * 1e9 * m.BitsPerWeight / 8
		ceil := r.Profile.MemoryBandwidthGBs * 1e9 / bytes
		return &Ceiling{
			DecodeTokS: v, Source: strings.TrimSpace(c.Role + " " + c.Name),
			ActiveParamsB: m.ActiveParamsB, BitsPerWeight: m.BitsPerWeight, BytesPerToken: bytes,
			BandwidthGBs: r.Profile.MemoryBandwidthGBs, CeilingTokS: ceil, Efficiency: v / ceil,
		}
	}
	return nil
}

// buildNumber parses a llama.cpp build, such as b11452 or the build_info b11669-620d2225c,
// into its number.
func buildNumber(build string) (int, error) {
	digits, _, _ := strings.Cut(strings.TrimPrefix(build, "b"), "-")
	n, err := strconv.Atoi(digits)
	if !strings.HasPrefix(build, "b") || err != nil {
		return 0, fmt.Errorf("%q is not a llama.cpp build such as b11452", build)
	}
	return n, nil
}
