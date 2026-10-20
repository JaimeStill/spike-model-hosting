// Package sweepstep is evaluate's sweep step, which chooses one model's slots and c on the
// router (the evaluation package gate's Sweep), and restores the committed presets after; or, in
// its set mode, sweeps a whole set and commits each model's choice (gate's SetSweep).
//
// Usage:
//
//	evaluate sweep [-router url] [-profile name|path] [-set name] (-role role | -model id) [-ssh dest] [-floors path] [-targets path] [-uv path] [-python path] [-state dir]
//	evaluate sweep -set name [-from role] [-router url] [-profile name|path] [-ssh dest] [-floors path] [-targets path] [-uv path] [-python path] [-state dir]
//	evaluate sweep -restore [-router url] [-profile name|path] [-set name] [-ssh dest]
//
// The model is -model, by router ID, or the model the set assigns -role: agent (the profile's
// chat role; chat is accepted), vision, audio or embedding. The set is -set, or SPIKE_SET, or
// else the profile's startup set, which must assign the model a role.
//
// The sweep puts the model at N slots for N of 1, 2, 4, 8 and 16 until a step fails, then one
// bisection between the last step that held and that failure. Each step's c is N times the
// model's per-session target, the largest of its roles' in -targets (the module root's
// evaluation/targets.toml by default), rounded up to a multiple of 256 and capped at the card's
// context; a step the cap leaves a session short of its target fails on context with no request.
// Each other step renders the profile with the model at N and c, installs it as the router's
// presets file over ssh on -ssh (by default the router URL's host), written beside
// /etc/llama-router/models.ini and renamed over it, has the router read it again with
// GET /models?reload=1, loads the model again and checks its server runs at N and c, then reloads
// and warms the set as a gated run does and runs the capacity stage on the model at N, every role
// the set assigns it, judged by the floors at N in -floors (the module root's
// evaluation/floors.toml by default). The set's other models stay loaded at their own settings.
//
// Each step records every stream's rate against the floors at N, the median and 95th-percentile
// time to first token and the total throughput of the N requests, the pool at the warmed peak
// with system RAM, and any request that ran out of context. The chosen N is the smaller of the
// largest N that held every floor, the pool floor and every session's context, and the knee,
// past which total throughput gains under 10% a step; the summary gives both, and which bound.
//
// The sweep refuses to start unless every model of the set is loaded and the installed presets
// are the committed render, by SHA-256. Once started, it ends by restoring the committed render
// on every exit, an error and an interrupt (SIGINT or SIGTERM) among them: it installs the
// render, checks the installed file's SHA-256 on the host is the render's, has the router read it
// again and waits until every model of the set serves at its committed setting. A restore that
// fails is reported loudly, RESTORE FAILED. -restore runs the restore alone. Neither writes any
// file on the host but the presets file and its temporary one, and neither runs sudo.
//
// The warm-up's peak media are found through the suites project, with uv and Python from -uv
// and -python, or SPIKE_UV and SPIKE_PYTHON, or else the consumers mise environment's pins, into
// the run directory's warm/. The report is <state>/evaluate/<UTC time>-sweep-<model>/sweep.json,
// the state directory being -state, or SPIKE_STATE_DIR, or else the module root's .state/; a
// summary of it goes to stdout, and a line as each step starts to stderr. The sweep records
// nothing in the results index.
//
// The set mode, -set with no -role or -model, sweeps each model of the set in role order, agent,
// vision, audio and embedding (a model the set assigns two roles once, at the larger target), from
// -from's model when given. After each model's sweep and its restore, it writes the chosen N and
// that step's c into the profile file -profile names, editing only the model's slots, c,
// slots_reason and c_reason (the reasons citing the sweep's N, bound, build and date), and
// installs the new render as the committed one, checked as a restore is; the next model's sweep
// starts from it, so each model is swept against the settings already chosen. It stops at the
// first model that chooses no N, or whose sweep, commit or install fails, the models before it
// committed and installed. Each model's report is its own run directory's, as a one-model sweep's;
// the summary ends with a line a model. The profile's edit is left for the caller to commit.
//
// It exits 0 when it chose an N of at least 1 and the restore verified (in the set mode, for every
// model, each installed), or -restore verified; 1 when no N held, the sweep, the restore or an
// install failed, or it can't run; and 2 on a usage error.
package sweepstep

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/consumers"
	"github.com/JaimeStill/spike-model-hosting/evaluation/gate"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// errUsage marks an error in how the step was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

// usage is the step's usage, printed on -h and after a usage error.
const usage = `usage:
  evaluate sweep [-router url] [-profile name|path] [-set name] (-role role | -model id) [-ssh dest] [-floors path] [-targets path] [-uv path] [-python path] [-state dir]
  evaluate sweep -set name [-from role] [-router url] [-profile name|path] [-ssh dest] [-floors path] [-targets path] [-uv path] [-python path] [-state dir]
  evaluate sweep -restore [-router url] [-profile name|path] [-set name] [-ssh dest]
`

// options are the step's flags, after the environment's fallbacks.
type options struct {
	router, profile, set, role, model, from, ssh, floors, targets, uv, python, state string
	// restore runs the restore alone; whole sweeps the whole set, -set given with no model.
	restore, whole bool
}

// Run runs the step with args, reading the environment through getenv, and returns its exit
// status.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	ok := false
	if err == nil {
		ok, err = run(ctx, opts, stdout, stderr)
		if err == nil && ok {
			return 0
		}
	}
	// A failed write to stderr has nowhere left to be reported, so its errors are dropped.
	switch {
	case err == nil:
		return 1
	case errors.Is(err, flag.ErrHelp):
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "evaluate sweep: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "evaluate sweep: %v\n", err)
		return 1
	}
}

func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("evaluate sweep", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", profile.HostStrixHalo, "the profile the router serves: a host class or a TOML file's path")
	fs.StringVar(&o.set, "set", getenv("SPIKE_SET"), "the profile's set (SPIKE_SET); empty is the startup set")
	fs.StringVar(&o.role, "role", "", "the role whose model to sweep: agent, vision, audio or embedding")
	fs.StringVar(&o.model, "model", "", "the model to sweep, by router ID")
	fs.StringVar(&o.from, "from", "", "in the set mode, the role whose model to start at: agent, vision, audio or embedding")
	fs.BoolVar(&o.restore, "restore", false, "only restore the committed presets and verify them")
	fs.StringVar(&o.ssh, "ssh", "", "the ssh destination the presets file and the pool are on; default the router's host")
	fs.StringVar(&o.floors, "floors", "", "the floors file; default the module root's evaluation/floors.toml")
	fs.StringVar(&o.targets, "targets", "", "the per-session targets file; default the module root's evaluation/targets.toml")
	fs.StringVar(&o.uv, "uv", getenv("SPIKE_UV"), "the uv executable (SPIKE_UV); empty asks mise for the pinned one")
	fs.StringVar(&o.python, "python", getenv("SPIKE_PYTHON"), "the Python uv runs (SPIKE_PYTHON); empty asks mise for the pinned one")
	fs.StringVar(&o.state, "state", getenv("SPIKE_STATE_DIR"), "the state directory (SPIKE_STATE_DIR); default the module root's .state")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, err
		}
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	setGiven := false
	fs.Visit(func(f *flag.Flag) { setGiven = setGiven || f.Name == "set" })
	o.whole = !o.restore && o.role == "" && o.model == "" && setGiven
	switch {
	case fs.NArg() != 0:
		return o, fmt.Errorf("%w: unexpected arguments %q", errUsage, fs.Args())
	case o.router == "":
		return o, fmt.Errorf("%w: no router: set -router or SPIKE_ROUTER_URL", errUsage)
	case o.restore && (o.role != "" || o.model != ""):
		return o, fmt.Errorf("%w: -restore restores the set's presets; it takes no -role or -model", errUsage)
	case !o.restore && !o.whole && (o.role == "") == (o.model == ""):
		return o, fmt.Errorf("%w: name the model to sweep with -role or -model, one of them, the set to sweep whole with -set alone, or give -restore", errUsage)
	case o.from != "" && !o.whole:
		return o, fmt.Errorf("%w: -from starts a set sweep, -set with no -role or -model", errUsage)
	}
	for _, r := range []*string{&o.role, &o.from} {
		if *r == "" {
			continue
		}
		role, err := gate.ParseRole(*r)
		if err != nil {
			return o, fmt.Errorf("%w: %v", errUsage, err)
		}
		*r = role
	}
	o.router = strings.TrimSuffix(o.router, "/")
	if o.ssh == "" {
		u, err := url.Parse(o.router)
		if err != nil || u.Hostname() == "" {
			return o, fmt.Errorf("%w: the router URL %q names no host for the presets file; set -ssh", errUsage, o.router)
		}
		o.ssh = u.Hostname()
	}
	return o, nil
}

// run sets up the sweep and runs it, or the restore alone, and writes its report; ok is whether
// it chose an N and the restore verified, or the restore alone verified.
func run(ctx context.Context, o options, stdout, stderr io.Writer) (ok bool, err error) {
	p, path, err := profile.Open(o.profile)
	if err != nil {
		return false, err
	}
	set, err := p.Set(o.set)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errUsage, err)
	}
	model, err := pick(set, o)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errUsage, err)
	}
	sw := &gate.Sweep{
		Runner:  &gate.Runner{Router: o.router, Profile: p, Set: set, Model: model, Log: stderr},
		Presets: gate.SSHPresets{Dest: o.ssh},
	}
	if o.restore {
		res, err := sw.Restore(ctx)
		if werr := gate.WriteRestoreSummary(stdout, res); werr != nil {
			return false, errors.Join(err, werr)
		}
		return res.Verified, err
	}

	wd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	root, err := profile.ModuleRoot(wd)
	if err != nil {
		return false, err
	}
	r := sw.Runner
	if r.Floors, err = gate.LoadFloors(cmp.Or(o.floors, filepath.Join(root, "evaluation", "floors.toml"))); err != nil {
		return false, err
	}
	if sw.Targets, err = gate.LoadTargets(cmp.Or(o.targets, filepath.Join(root, "evaluation", "targets.toml"))); err != nil {
		return false, err
	}
	if r.Media, err = gate.LoadMedia(root); err != nil {
		return false, err
	}
	state := cmp.Or(o.state, filepath.Join(root, ".state"))
	uv, err := consumers.Executable(ctx, o.uv, "uv")
	if err != nil {
		return false, err
	}
	python, err := consumers.Executable(ctx, o.python, "python")
	if err != nil {
		return false, err
	}
	r.Peaks = gate.UV{Exe: uv, Python: python, Project: filepath.Join(root, gate.SuitesDir), Cache: filepath.Join(state, "cache"), Log: stderr}
	r.Tiers, r.Footprint = filepath.Join(root, gate.TiersDir), gate.SSH{Dest: o.ssh}
	if o.whole {
		return runSet(ctx, sw, path, state, o.from, stdout)
	}
	dir := gate.RunDir(state, gate.SweepSubject(model), time.Now())
	r.Dir = dir

	rep, err := sw.Run(ctx)
	if rep == nil {
		return false, err
	}
	if werr := gate.WriteSweep(dir, rep); werr != nil {
		return false, errors.Join(err, werr)
	}
	if werr := gate.WriteSweepSummary(stdout, rep); werr != nil {
		return false, errors.Join(err, werr)
	}
	if _, werr := fmt.Fprintf(stdout, "report: %s\n", filepath.Join(dir, gate.SweepFile)); werr != nil {
		return false, errors.Join(err, werr)
	}
	return rep.Chosen > 0 && rep.Restore != nil && rep.Restore.Verified, err
}

// pick returns the model the options name in set, empty for -restore and the set mode.
func pick(set *profile.Set, o options) (string, error) {
	switch {
	case o.restore || o.whole:
		return "", nil
	case o.role != "":
		model := map[string]string{
			gate.RoleAgent: set.Roles.Chat, gate.RoleVision: set.Roles.Vision,
			gate.RoleAudio: set.Roles.Audio, gate.RoleEmbedding: set.Roles.Embedding,
		}[o.role]
		if model == "" {
			return "", fmt.Errorf("set %s assigns no model the %s role", set.Name, o.role)
		}
		return model, nil
	case !slices.Contains(set.Roles.Models(), o.model):
		return "", fmt.Errorf("set %s assigns %s no role", set.Name, o.model)
	}
	return o.model, nil
}

// runSet runs the set mode: sw's set swept whole (gate.SetSweep), committing each model's choice
// into the profile file at path; ok is whether every model chose an N and was installed.
func runSet(ctx context.Context, sw *gate.Sweep, path, state, from string, stdout io.Writer) (ok bool, err error) {
	set := &gate.SetSweep{
		Runner: sw.Runner, Presets: sw.Presets, Targets: sw.Targets, Profile: gate.FileProfile{Path: path}, From: from,
		DirOf: func(model string) string { return gate.RunDir(state, gate.SweepSubject(model), time.Now()) },
	}
	reps, err := set.Run(ctx)
	if werr := gate.WriteSetSweepSummary(stdout, sw.Runner.Set.Name, reps); werr != nil {
		return false, errors.Join(err, werr)
	}
	if _, werr := fmt.Fprintf(stdout, "profile: %s, each chosen setting written; commit it\n", path); werr != nil {
		return false, errors.Join(err, werr)
	}
	return err == nil && len(reps) > 0, err
}
