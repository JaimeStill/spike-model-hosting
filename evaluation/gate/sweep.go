package gate

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
	"github.com/JaimeStill/spike-model-hosting/hosting/swap"
)

// SweepSteps are the slots a sweep doubles through, stopping at the first that fails, before its
// one bisection between the last that held and that first failure.
var SweepSteps = []int{1, 2, 4, 8, 16}

// KneeGain is the knee's threshold: a step whose total throughput gains under it over the step
// before puts the knee at that step before.
const KneeGain = 0.10

// ctxStep is what a step's c is rounded up to a multiple of, so the context the server allocates
// is the context the step renders, whatever padding it applies.
const ctxStep = 256

// The bounds a sweep step can fail on: a floor at N (each request's rate, or another of
// capacity's checks at N, a crossed nonce or the embedding tolerance), the pool's floor (at the
// warmed peak or capacity's own), or a session's context. With the knee and the sweep's last
// step, they are what bound a sweep's chosen N.
const (
	BoundFloor   = "floor"
	BoundPool    = "pool"
	BoundContext = "context"
	BoundKnee    = "knee"
	BoundTop     = "top"
)

// SweepFile is a sweep's report's file name in its run directory: not ReportFile, so LatestRun
// never takes a sweep for a run to resume.
const SweepFile = "sweep.json"

// SweepSubject is the name a sweep's run directory takes in place of a model's: sweep-<model>.
func SweepSubject(model string) string { return "sweep-" + model }

// defaultRestoreTimeout bounds the restore that ends a sweep: the install, the reload, and the
// swept model's load again.
const defaultRestoreTimeout = 15 * time.Minute

// Sweep chooses one model's slots and c on the router (Run), the rest of its set loaded and idle
// at their own settings, and restores the committed render after (Restore).
type Sweep struct {
	// Runner is the model's run: its router, its committed profile and set, the model swept
	// (Model, which the set assigns at least one role), its floors, footprint and media, and its
	// warm-up's peak finder, tier lists and run directory. Each step runs capacity on a copy of
	// it whose profile is the step's.
	Runner *Runner
	// Presets is the router's presets file, which each step and the restore install.
	Presets Presets
	// Targets are the roles' per-session targets (evaluation/targets.toml).
	Targets Targets
	// RestoreTimeout bounds the restore; zero is 15 minutes. The restore runs on a context of its
	// own, so the interrupt that ends a sweep doesn't stop it.
	RestoreTimeout time.Duration
}

// Setting is a model's slots and context: what a step renders, or what its server reports it
// runs at.
type Setting struct {
	Slots int `json:"slots"`
	C     int `json:"c"`
}

// SweepReport is one sweep of one model.
type SweepReport struct {
	Router    string    `json:"router"`
	Build     string    `json:"build"`
	HostClass string    `json:"host_class"`
	Set       string    `json:"set"`
	Model     string    `json:"model"`
	Roles     []string  `json:"roles"`
	Started   time.Time `json:"started"`
	// Target is the model's per-session target, in tokens: the largest of its roles' (TargetRole),
	// from Targets. CardContext caps each step's c, and Committed is the model's setting in the
	// committed profile.
	Target      int     `json:"target"`
	TargetRole  string  `json:"target_role"`
	CardContext int     `json:"card_context"`
	Committed   Setting `json:"committed"`
	// Steps are the steps run, in the order they ran.
	Steps []SweepStep `json:"steps"`
	// Held is the largest N whose step held every bound, 0 when none did; HeldBy, the bounds the
	// first step above it failed on, empty when none above it failed.
	Held   int      `json:"held"`
	HeldBy []string `json:"held_by,omitempty"`
	// Knee is the largest N before total throughput gains under KneeGain a step, 0 when it never
	// does over the steps measured.
	Knee int `json:"knee"`
	// Chosen is the smaller of Held and Knee (Held, when there is no knee), 0 when no step held;
	// Bounds are what bound it: BoundKnee, the bounds of HeldBy, or BoundTop when the sweep's last
	// step held.
	Chosen int      `json:"chosen"`
	Bounds []string `json:"bounds"`
	// Restore is the restore that ended the sweep, nil when the sweep refused to start.
	Restore *Restore `json:"restore,omitempty"`
	// Install, in a set sweep (SetSweep), is the install after the restore of the profile with
	// the chosen setting committed: the render the set's next sweep starts from. Nil outside a
	// set sweep, or when it stopped before the install.
	Install *Restore `json:"install,omitempty"`
	// Err is why the sweep stopped before it chose, empty when it chose.
	Err string `json:"err,omitempty"`
}

// SweepStep is one step of a sweep: the model at N slots and context C, rendered from the
// profile with that override and applied through the router's reload.
type SweepStep struct {
	N int `json:"n"`
	C int `json:"c"`
	// Bisection marks the step between the last that held and the first that failed.
	Bisection bool `json:"bisection,omitempty"`
	// Capped marks a step whose N sessions at the target need more than the card's context: it
	// fails on context without a request.
	Capped bool `json:"capped,omitempty"`
	// SHA256 is the step's render's; Served, what each of the set's models' servers reported
	// they ran at once it was applied: the swept model at the step's, the rest at their own.
	SHA256 string             `json:"sha256,omitempty"`
	Served map[string]Setting `json:"served,omitempty"`
	// Warm is the step's reload and warm-up, the set's pool at its warmed peak with system RAM.
	Warm *Warm `json:"warm,omitempty"`
	// Checks are the warmed pool's and each role's capacity checks at N: every stream's rate
	// against its floor at N in its values, the pool at capacity's peak, and any request that ran
	// out of context.
	Checks []Check `json:"checks,omitempty"`
	// Rates are each role's total throughput and time to first token at N.
	Rates []RoleRate `json:"rates,omitempty"`
	// Held says the step held every bound; Failed names the bounds it failed on.
	Held   bool     `json:"held"`
	Failed []string `json:"failed,omitempty"`
	Detail string   `json:"detail,omitempty"`
	WallS  float64  `json:"wall_s"`
}

// RoleRate is one role's total throughput and time to first token over its N requests at once:
// the agent's and vision role's throughput is the sum of the N streams' decode (llama.cpp's
// predicted_per_second, tok/s), the audio role's the sum of their RTFx (x), the embedding role's
// the sum of its N 32-token queries' tokens a second at the client (tok/s). Time to first token
// is each request's prompt evaluation, llama.cpp's prompt_ms, its media's encoding with it; for
// the embedding role, each query's wall time at the client, its one answer.
type RoleRate struct {
	Role        string  `json:"role"`
	Throughput  float64 `json:"throughput"`
	Unit        string  `json:"unit"`
	TTFTMedianS float64 `json:"ttft_median_s"`
	TTFTP95S    float64 `json:"ttft_p95_s"`
	// Measured says every one of the N requests answered, so the figures are the N's; the knee
	// reads only a measured step.
	Measured bool `json:"measured"`
}

// Restore is the committed render's install after a sweep, or alone (Sweep.Restore).
type Restore struct {
	// SHA256 is the committed render's; Installed, the installed file's after the install, read
	// on the host.
	SHA256    string `json:"sha256"`
	Installed string `json:"installed,omitempty"`
	// Served is what each of the set's models' servers report they run at after the reload.
	Served map[string]Setting `json:"served,omitempty"`
	// Verified says the installed file's SHA-256 is the committed render's and every model of the
	// set serves at its committed setting; Err says why not.
	Verified bool   `json:"verified"`
	Err      string `json:"err,omitempty"`
}

// ErrRestore marks a restore that failed: the router's presets may not be the committed render.
var ErrRestore = errors.New("RESTORE FAILED")

// Run sweeps the model: N through SweepSteps until a step fails, then one bisection between the
// last step that held and that failure (the integer midpoint, none when they are adjacent). Each
// step's c is N times the model's per-session target, rounded up to a multiple of 256 and capped
// at the card's context; a step the cap leaves a session short of its target fails on context
// with no request sent. Each other step renders the committed profile with the model at N and c
// (profile.Profile.Override), installs it (Presets), has the router read it again
// (router.ReloadPresets), loads the model again at its new setting, checks its server runs at it,
// then reloads and warms the whole set (Warm) and runs capacity on the model at N for each of its
// roles, the set's other models loaded and idle at their own settings.
//
// A step holds when every floor at N, the pool's floor (at the warmed peak and capacity's peak)
// and every session's context hold. The chosen N is the smaller of the largest N that held and
// the knee, the largest N before total throughput gains under KneeGain a step; the report gives
// both, and which bound.
//
// Run refuses before any step unless the router lists every model of the set loaded and the
// installed presets are the committed render, by SHA-256. Once it has started, it ends by
// restoring the committed render, on every exit, an error and a cancelled ctx among them
// (Restore); a restore that fails is the run's error, wrapping ErrRestore.
func (s *Sweep) Run(ctx context.Context) (rep *SweepReport, err error) {
	r := s.Runner
	if r.HTTP == nil {
		r.HTTP = &http.Client{}
	}
	m := r.model()
	if r.Model == "" || m == nil {
		return nil, fmt.Errorf("a sweep sweeps one model of the profile; %q is none", r.Model)
	}
	roles := r.Roles()
	if len(roles) == 0 {
		return nil, fmt.Errorf("set %s assigns %s no role", r.Set.Name, r.Model)
	}
	committed, err := render(r.Profile)
	if err != nil {
		return nil, err
	}
	if err := r.checkLoaded(ctx, true); err != nil {
		return nil, err
	}
	build, err := router.Build(ctx, r.HTTP, r.Router)
	if err != nil {
		return nil, err
	}
	installed, err := s.Presets.Sum(ctx)
	if err != nil {
		return nil, err
	}
	if installed != sum(committed) {
		return nil, fmt.Errorf("the installed presets' SHA-256 is %s, not the committed render's %s: evaluate sweep -restore installs the committed render", installed, sum(committed))
	}

	rep = &SweepReport{
		Router: r.Router, Build: build, HostClass: r.Profile.HostClass, Set: r.Set.Name, Model: r.Model,
		Roles: roles, Started: time.Now().UTC(), CardContext: m.CardContext, Committed: Setting{Slots: m.Slots, C: m.C},
	}
	rep.Target, rep.TargetRole = s.target(roles)
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cmp.Or(s.RestoreTimeout, defaultRestoreTimeout))
		defer cancel()
		res, rerr := s.Restore(rctx)
		rep.Restore = res
		if err != nil {
			rep.Err = err.Error()
		}
		err = errors.Join(err, rerr)
	}()
	if rep.Target < 1 {
		return rep, fmt.Errorf("no per-session target for %s's roles %q", r.Model, roles)
	}

	r.peaks = map[string]Peak{}
	held, failed := 0, 0
	step := func(n int, bisection bool) error {
		st, err := s.step(ctx, roles, rep.Target, n, bisection)
		rep.Steps = append(rep.Steps, st)
		switch {
		case err != nil:
			return err
		case st.Held:
			held = max(held, n)
		case failed == 0 || n < failed:
			failed = n
		}
		return nil
	}
	for _, n := range SweepSteps {
		if err := step(n, false); err != nil {
			return rep, err
		}
		if failed > 0 {
			break
		}
	}
	if held > 0 && failed-held > 1 {
		if err := step((held+failed)/2, true); err != nil {
			return rep, err
		}
	}
	chooseN(rep)
	return rep, nil
}

// target returns the largest per-session target of roles, in tokens, and the role it is.
func (s *Sweep) target(roles []string) (int, string) {
	best, role := 0, ""
	for _, ro := range roles {
		if t := s.Targets.Of(ro).Tokens(); t > best {
			best, role = t, ro
		}
	}
	return best, role
}

// step runs one step at n slots, its c from target.
func (s *Sweep) step(ctx context.Context, roles []string, target, n int, bisection bool) (st SweepStep, err error) {
	r := s.Runner
	m := r.model()
	need := n * target
	st = SweepStep{N: n, C: min(roundUp(need, ctxStep), m.CardContext), Bisection: bisection}
	began := time.Now()
	defer func() { st.WallS = time.Since(began).Seconds() }()
	if need > m.CardContext {
		st.Capped, st.Failed = true, []string{BoundContext}
		st.Detail = fmt.Sprintf("%d sessions of %d tokens need c = %d, over the card's %d: capped there, a session holds %d, short of its target",
			n, target, need, m.CardContext, m.CardContext/n)
		return st, nil
	}

	prof, err := r.Profile.Override(r.Model, n, st.C)
	if err != nil {
		return st, err
	}
	data, err := render(prof)
	if err != nil {
		return st, err
	}
	data = append(fmt.Appendf(nil, "; evaluate sweep's step: %s at %d slots, c = %d, not the committed render.\n; evaluate sweep -restore installs the committed render.\n", r.Model, n, st.C), data...)
	st.SHA256 = sum(data)
	r.logf("evaluate: sweep %s at %d slots, c = %d\n", r.Model, n, st.C)
	if st.Served, err = s.apply(ctx, prof, data); err != nil {
		return st, err
	}

	sub := *r
	sub.Profile = prof
	w, err := sub.warm(ctx)
	st.Warm = w
	if err != nil {
		return st, err
	}
	st.Checks = []Check{sub.warmedCheck(w)}
	c := &client{base: r.Router, hc: r.HTTP}
	for _, role := range roles {
		run := sub.capAt(ctx, c, role)
		st.Checks = append(st.Checks, run.checks...)
		st.Rates = append(st.Rates, rateOf(role, run, r.Media))
	}
	if err := ctx.Err(); err != nil {
		return st, err
	}
	st.Failed = bounds(st.Checks)
	st.Held = len(st.Failed) == 0
	return st, nil
}

// apply installs data, the render of prof, checks the installed file is it by its SHA-256, has
// the router read it again, and waits until every model of the set serves at prof's setting
// (settle).
func (s *Sweep) apply(ctx context.Context, prof *profile.Profile, data []byte) (map[string]Setting, error) {
	r := s.Runner
	if err := s.Presets.Install(ctx, data); err != nil {
		return nil, err
	}
	got, err := s.Presets.Sum(ctx)
	if err != nil {
		return nil, err
	}
	if got != sum(data) {
		return nil, fmt.Errorf("the installed presets' SHA-256 is %s, not the render's %s", got, sum(data))
	}
	if err := router.ReloadPresets(ctx, r.HTTP, r.Router); err != nil {
		return nil, err
	}
	return s.settle(ctx, prof)
}

// settle waits, after the router read its presets again, until each of the set's models is
// unloaded (its section changed, so the router unloaded it) or serves at prof's setting, loads
// those unloaded again (swap.Run), and checks each serves at prof's setting: its slots, and its
// server's context its c. It returns what each serves at.
func (s *Sweep) settle(ctx context.Context, prof *profile.Profile) (map[string]Setting, error) {
	r := s.Runner
	every := cmp.Or(r.PollEvery, 500*time.Millisecond)
	wctx, cancel := context.WithTimeout(ctx, swap.DefaultUnloadTimeout)
	defer cancel()
	for _, id := range r.Set.Models {
		want := setting(prof, id)
		for {
			status, err := router.Statuses(wctx, r.HTTP, r.Router)
			if err != nil {
				return nil, err
			}
			if status[id] == router.StatusUnloaded {
				break
			}
			if status[id] == router.StatusLoaded {
				if at, err := router.ServedAt(wctx, r.HTTP, r.Router, id); err == nil && (Setting{at.Slots, at.NCtx}) == want {
					break
				}
			}
			select {
			case <-wctx.Done():
				return nil, fmt.Errorf("waiting for %s to unload or serve at %d slots and c = %d after the reload: %w", id, want.Slots, want.C, wctx.Err())
			case <-time.After(every):
			}
		}
	}
	if _, err := swap.Run(ctx, swap.Config{Router: r.Router, HTTP: r.HTTP, Models: r.Set.Models, Interval: r.PollEvery}); err != nil {
		return nil, fmt.Errorf("loading set %s again after the reload: %w", r.Set.Name, err)
	}
	served := map[string]Setting{}
	for _, id := range r.Set.Models {
		at, err := router.ServedAt(ctx, r.HTTP, r.Router, id)
		if err != nil {
			return served, err
		}
		got, want := Setting{at.Slots, at.NCtx}, setting(prof, id)
		served[id] = got
		if got != want {
			return served, fmt.Errorf("the router serves %s at %d slots and c = %d, not the presets' %d and %d", id, got.Slots, got.C, want.Slots, want.C)
		}
	}
	return served, nil
}

// setting is model id's slots and c in prof.
func setting(prof *profile.Profile, id string) Setting {
	m := prof.Model(id)
	if m == nil {
		return Setting{}
	}
	return Setting{Slots: m.Slots, C: m.C}
}

// Restore installs the committed render, the runner's profile's own RenderPresets, checks the
// installed file's SHA-256 on the host is the render's, has the router read it again, and waits
// until every model of the set is loaded and serves at its committed setting (loading again the
// models the reload unloaded). A failure wraps ErrRestore: the router's presets, or a model's
// setting, may not be the committed ones.
func (s *Sweep) Restore(ctx context.Context) (*Restore, error) {
	r := s.Runner
	if r.HTTP == nil {
		r.HTTP = &http.Client{}
	}
	data, err := render(r.Profile)
	if err != nil {
		return &Restore{Err: err.Error()}, fmt.Errorf("%w: %v", ErrRestore, err)
	}
	res := &Restore{SHA256: sum(data)}
	fail := func(err error) (*Restore, error) {
		res.Err = err.Error()
		return res, fmt.Errorf("%w: %v; run evaluate sweep -restore, or install the committed render by the runbook", ErrRestore, err)
	}
	r.logf("evaluate: restore the committed presets, sha256 %s\n", res.SHA256)
	if err := s.Presets.Install(ctx, data); err != nil {
		return fail(err)
	}
	if res.Installed, err = s.Presets.Sum(ctx); err != nil {
		return fail(err)
	}
	if res.Installed != res.SHA256 {
		return fail(fmt.Errorf("the installed presets' SHA-256 is %s, not the committed render's %s", res.Installed, res.SHA256))
	}
	if err := router.ReloadPresets(ctx, r.HTTP, r.Router); err != nil {
		return fail(err)
	}
	if res.Served, err = s.settle(ctx, r.Profile); err != nil {
		return fail(err)
	}
	res.Verified = true
	return res, nil
}

// render is prof's presets.
func render(prof *profile.Profile) ([]byte, error) {
	var b bytes.Buffer
	if err := prof.RenderPresets(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// roundUp rounds n up to a multiple of step.
func roundUp(n, step int) int { return (n + step - 1) / step * step }

// bounds names the bounds checks failed on, in BoundFloor, BoundPool, BoundContext order: a
// failed context check, a failed pool check (the warmed pool or capacity's peak), and any other
// failed check, a floor at N. A request that ran out of context answered nothing, so the floor
// checks it fails with it are its context's: a step that ran out of context names no floor.
func bounds(checks []Check) []string {
	failed := map[string]bool{}
	for _, c := range checks {
		switch {
		case c.Passed:
		case c.Name == "context held":
			failed[BoundContext] = true
		case c.Name == "peak pool free" || c.Name == "warmed pool free":
			failed[BoundPool] = true
		default:
			failed[BoundFloor] = true
		}
	}
	if failed[BoundContext] {
		delete(failed, BoundFloor)
	}
	var out []string
	for _, b := range []string{BoundFloor, BoundPool, BoundContext} {
		if failed[b] {
			out = append(out, b)
		}
	}
	return out
}

// rateOf is role's total throughput and time to first token over its capacity run (RoleRate).
func rateOf(role string, run capRun, media Media) RoleRate {
	rr := RoleRate{Role: role, Unit: "tok/s"}
	var ttft []float64
	if role == RoleEmbedding {
		rr.Measured = len(run.loaded) > 0
		for _, q := range run.loaded {
			if q.err != nil {
				rr.Measured = false
				continue
			}
			rr.Throughput += queryTokens / q.wall.Seconds()
			ttft = append(ttft, q.wall.Seconds())
		}
	} else if rec := run.rec; rec != nil {
		rr.Measured = rec.errorCount == 0 && len(rec.done) > 0
		if role == RoleAudio {
			rr.Unit = "x"
		}
		for _, c := range rec.done {
			if role == RoleAudio {
				rr.Throughput += media.AudioSeconds / c.Wall.Seconds()
			} else {
				rr.Throughput += c.Timings.PredictedPerSecond
			}
			ttft = append(ttft, c.Timings.PromptMS/1000)
		}
	}
	rr.TTFTMedianS, rr.TTFTP95S = median(ttft), p95(ttft)
	return rr
}

// median is xs's median, 0 for none.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(xs))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// p95 is xs's 95th percentile by nearest rank, 0 for none.
func p95(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(xs))
	return s[int(math.Ceil(0.95*float64(len(s))))-1]
}

// chooseN sets the report's held N, knee, chosen N and its bounds from its steps.
func chooseN(rep *SweepReport) {
	steps := slices.Clone(rep.Steps)
	slices.SortFunc(steps, func(a, b SweepStep) int { return a.N - b.N })
	rep.Held, rep.HeldBy = 0, nil
	for _, st := range steps {
		if st.Held {
			rep.Held = st.N
		}
	}
	for _, st := range steps {
		if st.N > rep.Held && !st.Held {
			rep.HeldBy = st.Failed
			break
		}
	}
	rep.Knee = knee(steps, rep.Roles)
	limit := rep.HeldBy
	if len(limit) == 0 {
		limit = []string{BoundTop}
	}
	switch {
	case rep.Held == 0:
		rep.Chosen, rep.Bounds = 0, rep.HeldBy
	case rep.Knee > 0 && rep.Knee < rep.Held:
		rep.Chosen, rep.Bounds = rep.Knee, []string{BoundKnee}
	case rep.Knee == rep.Held:
		rep.Chosen, rep.Bounds = rep.Held, append([]string{BoundKnee}, limit...)
	default:
		rep.Chosen, rep.Bounds = rep.Held, limit
	}
}

// knee returns the smallest of each role's knee over steps, sorted by N: the N of the measured
// step before the first measured step whose total throughput gains under KneeGain over it; 0
// when no role's does.
func knee(steps []SweepStep, roles []string) int {
	out := 0
	for _, role := range roles {
		var prev *RoleRate
		prevN := 0
		for _, st := range steps {
			i := slices.IndexFunc(st.Rates, func(rr RoleRate) bool { return rr.Role == role && rr.Measured })
			if i < 0 {
				continue
			}
			rr := st.Rates[i]
			if prev != nil && rr.Throughput < prev.Throughput*(1+KneeGain) {
				if out == 0 || prevN < out {
					out = prevN
				}
				break
			}
			prev, prevN = &rr, st.N
		}
	}
	return out
}

// WriteSweep writes rep as dir/SweepFile, making dir.
func WriteSweep(dir string, rep *SweepReport) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, SweepFile), append(b, '\n'), 0o600)
}

// WriteSweepSummary writes rep for a reader: its header, a line a step in N order, the held N,
// the knee and the chosen N with its bounds, and the restore.
func WriteSweepSummary(w io.Writer, rep *SweepReport) error {
	var b strings.Builder
	fmt.Fprintf(&b, "sweep of %s, set %s, build %s, roles %s\n", rep.Model, rep.Set, rep.Build, strings.Join(rep.Roles, ", "))
	fmt.Fprintf(&b, "per-session target %d tokens (%s), card context %d, committed %d slots and c = %d\n\n",
		rep.Target, rep.TargetRole, rep.CardContext, rep.Committed.Slots, rep.Committed.C)
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "N\tc\tresult\tslowest at N\tthroughput\tTTFT p50/p95 s\twarmed pool free\tpeak pool free\tRAM used/avail\t")
	steps := slices.Clone(rep.Steps)
	slices.SortFunc(steps, func(a, b SweepStep) int { return a.N - b.N })
	for _, st := range steps {
		result := "held"
		if !st.Held {
			result = "failed " + strings.Join(st.Failed, ", ")
		}
		if st.Bisection {
			result += " (bisection)"
		}
		slowest, warmed, peak := "-", "-", "-"
		var rates, ttft []string
		for _, c := range st.Checks {
			switch {
			case c.Name == "warmed pool free" && c.Measured != nil:
				warmed = fmt.Sprintf("%.0f MiB", *c.Measured)
			case c.Name == "peak pool free" && c.Measured != nil:
				peak = fmt.Sprintf("%.0f MiB", *c.Measured)
			case strings.HasSuffix(c.Name, " at N") && c.Measured != nil:
				slowest = fmt.Sprintf("%s %s %s (floor %s)", num(*c.Measured), c.Unit, c.Name, num(*c.Floor))
			}
		}
		for _, rr := range st.Rates {
			rates = append(rates, fmt.Sprintf("%s %s", num(rr.Throughput), rr.Unit))
			ttft = append(ttft, fmt.Sprintf("%.2f/%.2f", rr.TTFTMedianS, rr.TTFTP95S))
		}
		ram := "-"
		if st.Warm != nil && st.Warm.Peak != nil && st.Warm.Peak.RAM != nil {
			ram = fmt.Sprintf("%d/%d MiB", st.Warm.Peak.RAM.UsedMiB, st.Warm.Peak.RAM.AvailableMiB)
		}
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t\n", st.N, st.C, result, slowest,
			cmp.Or(strings.Join(rates, "; "), "-"), cmp.Or(strings.Join(ttft, "; "), "-"), warmed, peak, ram)
		if st.Detail != "" {
			_, _ = fmt.Fprintf(tw, "\t\t%s\t\t\t\t\t\t\t\n", st.Detail)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	b.WriteString("\n")
	heldBy := "the sweep's last step held"
	if len(rep.HeldBy) > 0 {
		heldBy = "the next step failed " + strings.Join(rep.HeldBy, ", ")
	}
	kn := "none"
	if rep.Knee > 0 {
		kn = fmt.Sprint(rep.Knee)
	}
	fmt.Fprintf(&b, "held: N = %d (%s); knee: %s; chosen: N = %d, bound by %s\n", rep.Held, heldBy, kn, rep.Chosen, cmp.Or(strings.Join(rep.Bounds, ", "), "none"))
	if rep.Err != "" {
		fmt.Fprintf(&b, "stopped: %s\n", rep.Err)
	}
	writeRestore(&b, rep.Restore)
	if res := rep.Install; res != nil {
		if res.Verified {
			fmt.Fprintf(&b, "installed: the profile committed with %s at its chosen setting, sha256 %s, installed and verified; every model of the set serves at its committed setting\n", rep.Model, res.SHA256)
		} else {
			fmt.Fprintf(&b, "INSTALL FAILED: %s; the committed render is sha256 %s, the installed file %s\n", res.Err, res.SHA256, cmp.Or(res.Installed, "unread"))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeRestore writes a restore's line, loud when it failed.
func writeRestore(b *strings.Builder, res *Restore) {
	switch {
	case res == nil:
	case res.Verified:
		fmt.Fprintf(b, "restored: the committed render, sha256 %s, installed and verified; every model of the set serves at its committed setting\n", res.SHA256)
	default:
		fmt.Fprintf(b, "RESTORE FAILED: %s; the committed render is sha256 %s, the installed file %s\n", res.Err, res.SHA256, cmp.Or(res.Installed, "unread"))
	}
}

// WriteRestoreSummary writes a restore alone, as evaluate sweep -restore reports it.
func WriteRestoreSummary(w io.Writer, res *Restore) error {
	var b strings.Builder
	writeRestore(&b, res)
	_, err := io.WriteString(w, b.String())
	return err
}
