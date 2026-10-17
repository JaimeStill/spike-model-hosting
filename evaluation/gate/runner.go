package gate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// The stages, in the order a run takes them. Admission, fitness, capacity, quality and set are
// built; deep is named so a run reports it, and later work fills it in.
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
// the set stage for the set itself: a set run.
type Runner struct {
	// Router is the router's root URL; HTTP, the client that reaches it (nil for a default one).
	Router string
	HTTP   *http.Client
	// Profile and Set are the profile the router serves and the set Model is evaluated in.
	Profile *profile.Profile
	Set     *profile.Set
	// Model is the model evaluated, by router ID: one the set assigns at least one role. Empty
	// makes the run a set run, which takes the set stage alone, across every role the set
	// assigns.
	Model     string
	Floors    Floors
	Footprint Footprint
	Media     Media
	// SampleEvery spaces the footprint's readings while capacity's requests are in flight, and
	// the set stage's tiers; zero is two seconds.
	SampleEvery time.Duration
	// CrossEvery spaces the set stage's rounds of nonce requests while every role's tier 1 runs
	// at once; zero is 15 seconds.
	CrossEvery time.Duration
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
	// Conform runs the agent role's conform tier through the consumers, its recordings and table
	// kept in the run's conform/, judged against Comparison, gpt-oss-120b's row
	// (evaluation/comparison.toml).
	Conform    Conform
	Comparison *passk.Comparison
	// Tiers is the directory of the tier lists the ASR sets, vision tiers and BFCL tiers run
	// (TiersDir, under the module root).
	Tiers string
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

// setRunReason is why a set run reports the stages before the set stage not run.
const setRunReason = "a set run: each role's model takes it in its own run"

// Run runs the stages in order and returns the run's report, stopping at the first stage that
// fails: every later stage is reported not run, naming it. Before any stage it refuses a model
// the router doesn't list as loaded, with an error wrapping ErrNotLoaded, and reads the router's
// build.
//
// from names the stage to start at; empty starts at admission. A run from a later stage reuses
// prior's results for every stage before it: prior must be a run of the same model and set in
// which each of them passed.
//
// A set run (no Model) starts at the set stage, from empty or naming it, and needs no prior: it
// reports every stage before it not run, since each is a model's, and refuses the run unless the
// router lists every model of the set loaded.
func (r *Runner) Run(ctx context.Context, from string, prior *Report) (*Report, error) {
	if r.HTTP == nil {
		r.HTTP = &http.Client{}
	}
	start := 0
	if from != "" {
		start = slices.Index(Stages, from)
		if start < 0 {
			return nil, fmt.Errorf("no stage %q; the stages are %q", from, Stages)
		}
	}
	setRun := r.Model == ""
	if setRun {
		if from != "" && from != StageSet {
			return nil, fmt.Errorf("a set run takes the set stage alone, not from %s", from)
		}
		start = slices.Index(Stages, StageSet)
	}
	roles := r.Roles()
	if len(roles) == 0 {
		return nil, fmt.Errorf("set %s assigns %s no role", r.Set.Name, cmp.Or(r.Model, "any model"))
	}
	if err := r.checkLoaded(ctx); err != nil {
		return nil, err
	}
	build, err := router.Build(ctx, r.HTTP, r.Router)
	if err != nil {
		return nil, err
	}
	rep := &Report{
		Router: r.Router, Build: build, HostClass: r.Profile.HostClass, Set: r.Set.Name,
		Model: r.Model, Roles: roles, Started: time.Now().UTC(),
	}
	switch {
	case setRun:
		rep.From = from
		for _, name := range Stages[:start] {
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotRun, Reason: setRunReason})
		}
	case start > 0:
		rep.From = from
		if err := reuse(rep, prior, start); err != nil {
			return nil, err
		}
	}
	stages := map[string]func(context.Context, *Report) []Check{
		StageAdmission: r.admission,
		StageFitness:   r.fitness,
		StageCapacity:  r.capacity,
		StageQuality:   r.quality,
		StageSet:       r.set,
	}
	failed := ""
	for _, name := range Stages[start:] {
		run, built := stages[name]
		switch {
		case failed != "":
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotRun, Reason: failed + " failed"})
			continue
		case !built:
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotBuilt, Reason: "not built yet"})
			continue
		}
		r.logf("evaluate: %s %s\n", name, cmp.Or(r.Model, SetSubject(r.Set.Name)))
		began := time.Now()
		checks := run(ctx, rep)
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
	rep.Ceiling = r.ceiling(rep)
	return rep, nil
}

// reuse copies prior's results for the stages before Stages[start] into rep.
func reuse(rep, prior *Report, start int) error {
	if prior == nil {
		return fmt.Errorf("resuming from %s needs the run to resume, and there is none", Stages[start])
	}
	if prior.Model != rep.Model || prior.Set != rep.Set {
		return fmt.Errorf("the run to resume is of %s in set %s, not %s in set %s", prior.Model, prior.Set, rep.Model, rep.Set)
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

// checkLoaded refuses the run unless the router lists Model loaded, or, for a set run, every
// model of the set.
func (r *Runner) checkLoaded(ctx context.Context) error {
	status, err := router.Statuses(ctx, r.HTTP, r.Router)
	if err != nil {
		return err
	}
	models := []string{r.Model}
	if r.Model == "" {
		models = r.Set.Models
	}
	for _, m := range models {
		s, ok := status[m]
		if !ok {
			s = "unlisted"
		}
		if s != router.StatusLoaded {
			return fmt.Errorf("%w: the router lists %s as %s; evaluate never loads a model, so load it (or swap its set in) first",
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
