package gate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// The stages, in the order a run takes them. Admission, fitness and capacity are built; quality
// is built for the embedding role; set and deep, and quality for the other roles, are named so a
// run reports them, and later work fills them in.
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

// Runner runs the stages for one model of a profile's set against the router.
type Runner struct {
	// Router is the router's root URL; HTTP, the client that reaches it (nil for a default one).
	Router string
	HTTP   *http.Client
	// Profile and Set are the profile the router serves and the set Model is evaluated in.
	Profile *profile.Profile
	Set     *profile.Set
	// Model is the model evaluated, by router ID: one the set assigns at least one role.
	Model     string
	Floors    Floors
	Footprint Footprint
	Media     Media
	// SampleEvery spaces the footprint's readings while capacity's requests are in flight; zero
	// is two seconds.
	SampleEvery time.Duration
	// Log takes a line as each stage starts, for a reader watching a long run; nil discards it.
	Log io.Writer
	// Bars are the quality stage's bars, by tier or task name (evaluation/bars.toml); a task
	// without one is recorded and never fails.
	Bars tier.Bars
	// MTEB runs the embedding role's MTEB tasks; Dir is the run's directory, under which each
	// task's MTEB output is kept, in mteb/<task>.
	MTEB MTEB
	Dir  string
}

// Roles returns the roles the set assigns Model, in the order Roles lists them.
func (r *Runner) Roles() []string {
	assigned := map[string]string{
		RoleAgent: r.Set.Roles.Chat, RoleVision: r.Set.Roles.Vision,
		RoleAudio: r.Set.Roles.Audio, RoleEmbedding: r.Set.Roles.Embedding,
	}
	var out []string
	for _, role := range Roles {
		if assigned[role] == r.Model {
			out = append(out, role)
		}
	}
	return out
}

// Run runs the stages in order and returns the run's report, stopping at the first stage that
// fails: every later stage is reported not run, naming it. Before any stage it refuses a model
// the router doesn't list as loaded, with an error wrapping ErrNotLoaded, and reads the router's
// build.
//
// from names the stage to start at; empty starts at admission. A run from a later stage reuses
// prior's results for every stage before it: prior must be a run of the same model and set in
// which each of them passed.
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
	roles := r.Roles()
	if len(roles) == 0 {
		return nil, fmt.Errorf("set %s assigns %s no role", r.Set.Name, r.Model)
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
	if start > 0 {
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
	}
	// A stage with a function may still have nothing built for the run's roles.
	builtFor := map[string]func(*Report) bool{StageQuality: qualityBuilt}
	failed := ""
	for _, name := range Stages[start:] {
		run, built := stages[name]
		if b, ok := builtFor[name]; ok && built {
			built = b(rep)
		}
		switch {
		case failed != "":
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotRun, Reason: failed + " failed"})
			continue
		case !built:
			rep.Stages = append(rep.Stages, StageResult{Name: name, Status: StatusNotBuilt, Reason: "not built yet"})
			continue
		}
		if r.Log != nil {
			_, _ = fmt.Fprintf(r.Log, "evaluate: %s %s\n", name, r.Model)
		}
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

// checkLoaded refuses the run unless the router lists Model loaded.
func (r *Runner) checkLoaded(ctx context.Context) error {
	status, err := router.Statuses(ctx, r.HTTP, r.Router)
	if err != nil {
		return err
	}
	s, ok := status[r.Model]
	if !ok {
		s = "unlisted"
	}
	if s != router.StatusLoaded {
		return fmt.Errorf("%w: the router lists %s as %s; evaluate never loads a model, so load it (or swap its set in) first",
			ErrNotLoaded, r.Model, s)
	}
	return nil
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
