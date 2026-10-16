// Package gatestep is evaluate's gated run, which takes one model through the evaluation
// package gate's stages: admission, fitness, capacity, quality (built for the embedding, audio
// and vision roles), then set and deep, which are not built yet.
//
// Usage:
//
//	evaluate [-router url] [-profile name|path] [-set name] (-role role | -model id) [-from stage] [-run dir] [-ssh dest] [-floors path] [-bars path] [-uv path] [-python path] [-state dir]
//
// The model is -model, by router ID, or the model the set assigns -role: agent (the profile's
// chat role; chat is accepted), vision, audio or embedding. The set is -set, or SPIKE_SET, or
// else the profile's startup set; with -model and no set named, it is the startup set when that
// assigns the model a role, or else the first set that does. The run judges each role the set
// assigns the model.
//
// The run stops at the first stage that fails. -from starts it at a later stage, reusing the
// results of the stages before it from an earlier run of the same model: the run directory -run
// names, or else the model's newest under the state directory. Each stage before -from must
// have passed there, and the resumed run's report replaces that run's, in the same directory.
//
// The report is <state>/evaluate/<UTC time>-<model>/report.json, the state directory being
// -state, or SPIKE_STATE_DIR, or else the module root's untracked .state/. A summary of it goes
// to stdout, and a line as each stage starts to stderr. The floors are -floors, the module
// root's evaluation/floors.toml by default, and the quality stage's bars -bars, the module
// root's evaluation/bars.toml by default. The pool is read over ssh with amdgpu_top, on -ssh,
// by default the router URL's host.
//
// The embedding role's quality stage runs MTEB, and the audio and vision roles' run Inspect AI
// on the ASR sets' and vision tiers' lists (evaluation/tiers), through the uv project
// evaluation/suites, with uv and Python from -uv and -python, or SPIKE_UV and SPIKE_PYTHON, or
// else the consumers mise environment's pins. MTEB's output for each task goes in the run
// directory's mteb/<task>, Inspect's JSON log for each ASR set in its asr/<set>, and each vision
// tier's logs in its vision/<tier>/logs; the datasets download into the state directory's cache/.
//
// The router comes from -router or SPIKE_ROUTER_URL. The model must be loaded already: the run
// checks GET /models first, and refuses an unloaded model before any stage; every request
// carries autoload=false, so it never loads one.
//
// It exits 0 when every stage that ran passed, 1 when one failed or the run can't start, and 2
// on a usage error.
package gatestep

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
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// Usage is the run's usage line, which evaluate's own usage lists.
const Usage = "evaluate [-router url] [-profile name|path] [-set name] (-role role | -model id) [-from stage] [-run dir] [-ssh dest] [-floors path] [-bars path] [-uv path] [-python path] [-state dir]"

const usage = "usage:\n  " + Usage + "\n"

// errUsage marks an error in how the run was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

type options struct {
	router, profile, set, role, model, from, run, ssh, floors, bars, uv, python, state string
}

// Run runs the gated stages with args, reading the environment through getenv, and returns its
// exit status.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	passed := false
	if err == nil {
		passed, err = run(ctx, opts, stdout, stderr)
		if err == nil && passed {
			return 0
		}
	}
	switch {
	case err == nil:
		return 1
	case errors.Is(err, flag.ErrHelp):
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "evaluate: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "evaluate: %v\n", err)
		return 1
	}
}

func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", profile.HostStrixHalo, "the profile the router serves: a host class or a TOML file's path")
	fs.StringVar(&o.set, "set", getenv("SPIKE_SET"), "the profile's set (SPIKE_SET); empty is the startup set")
	fs.StringVar(&o.role, "role", "", "the role whose model to evaluate: agent, vision, audio or embedding")
	fs.StringVar(&o.model, "model", "", "the model to evaluate, by router ID")
	fs.StringVar(&o.from, "from", "", "the stage to start at, reusing an earlier run's results before it")
	fs.StringVar(&o.run, "run", "", "with -from, the run directory to resume; default the model's newest")
	fs.StringVar(&o.ssh, "ssh", "", "the ssh destination the pool is read on; default the router's host")
	fs.StringVar(&o.floors, "floors", "", "the floors file; default the module root's evaluation/floors.toml")
	fs.StringVar(&o.bars, "bars", "", "the quality stage's bars file; default the module root's evaluation/bars.toml")
	fs.StringVar(&o.uv, "uv", getenv("SPIKE_UV"), "the uv executable (SPIKE_UV); empty asks mise for the pinned one")
	fs.StringVar(&o.python, "python", getenv("SPIKE_PYTHON"), "the Python uv runs (SPIKE_PYTHON); empty asks mise for the pinned one")
	fs.StringVar(&o.state, "state", getenv("SPIKE_STATE_DIR"), "the state directory (SPIKE_STATE_DIR); default the module root's .state")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, err
		}
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	switch {
	case fs.NArg() != 0:
		return o, fmt.Errorf("%w: unexpected arguments %q", errUsage, fs.Args())
	case o.router == "":
		return o, fmt.Errorf("%w: no router: set -router or SPIKE_ROUTER_URL", errUsage)
	case (o.role == "") == (o.model == ""):
		return o, fmt.Errorf("%w: name the model to evaluate with -role or -model, one of them", errUsage)
	case o.from != "" && !slices.Contains(gate.Stages, o.from):
		return o, fmt.Errorf("%w: -from %q is not one of the stages %q", errUsage, o.from, gate.Stages)
	case o.run != "" && o.from == "":
		return o, fmt.Errorf("%w: -run names the run -from resumes; give -from too", errUsage)
	}
	if o.role != "" {
		role, err := gate.ParseRole(o.role)
		if err != nil {
			return o, fmt.Errorf("%w: %v", errUsage, err)
		}
		o.role = role
	}
	o.router = strings.TrimSuffix(o.router, "/")
	if o.ssh == "" {
		u, err := url.Parse(o.router)
		if err != nil || u.Hostname() == "" {
			return o, fmt.Errorf("%w: the router URL %q names no host to read the pool on; set -ssh", errUsage, o.router)
		}
		o.ssh = u.Hostname()
	}
	return o, nil
}

// run sets up the runner, runs it, and writes its report; passed is whether every stage that
// ran passed.
func run(ctx context.Context, o options, stdout, stderr io.Writer) (passed bool, err error) {
	p, _, err := profile.Open(o.profile)
	if err != nil {
		return false, err
	}
	set, model, err := pick(p, o)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errUsage, err)
	}
	wd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	root, err := profile.ModuleRoot(wd)
	if err != nil {
		return false, err
	}
	floors, err := gate.LoadFloors(cmp.Or(o.floors, filepath.Join(root, "evaluation", "floors.toml")))
	if err != nil {
		return false, err
	}
	bars, err := tier.LoadBars(cmp.Or(o.bars, filepath.Join(root, "evaluation", "bars.toml")))
	if err != nil {
		return false, err
	}
	media, err := gate.LoadMedia(root)
	if err != nil {
		return false, err
	}
	state := cmp.Or(o.state, filepath.Join(root, ".state"))

	var prior *gate.Report
	dir := ""
	if o.from != "" && o.from != gate.Stages[0] {
		dir = o.run
		if dir == "" {
			if dir, err = gate.LatestRun(state, model); err != nil {
				return false, err
			}
			if dir == "" {
				return false, fmt.Errorf("no earlier run of %s under %s to resume from %s", model, state, o.from)
			}
		}
		if prior, err = gate.ReadReport(dir); err != nil {
			return false, err
		}
	}

	// The run directory is named before the run, since the quality stage writes into it.
	if dir == "" {
		dir = gate.RunDir(state, model, time.Now())
	}
	r := &gate.Runner{
		Router: o.router, Profile: p, Set: set, Model: model, Floors: floors,
		Footprint: gate.SSH{Dest: o.ssh}, Media: media, Log: stderr, Bars: bars, Dir: dir,
	}
	roles := r.Roles()
	if slices.Contains(roles, gate.RoleEmbedding) || slices.Contains(roles, gate.RoleAudio) || slices.Contains(roles, gate.RoleVision) {
		uv, err := consumers.Executable(ctx, o.uv, "uv")
		if err != nil {
			return false, err
		}
		python, err := consumers.Executable(ctx, o.python, "python")
		if err != nil {
			return false, err
		}
		suites := gate.UV{
			Exe: uv, Python: python, Project: filepath.Join(root, gate.SuitesDir),
			Cache: filepath.Join(state, "cache"), Log: stderr,
		}
		r.MTEB, r.ASR, r.Vision, r.Tiers = suites, suites, suites, filepath.Join(root, gate.TiersDir)
	}
	rep, err := r.Run(ctx, o.from, prior)
	if rep == nil {
		return false, err
	}
	if werr := gate.WriteReport(dir, rep); werr != nil {
		return false, errors.Join(err, werr)
	}
	if werr := gate.WriteSummary(stdout, rep); werr != nil {
		return false, errors.Join(err, werr)
	}
	if _, werr := fmt.Fprintf(stdout, "report: %s\n", filepath.Join(dir, gate.ReportFile)); werr != nil {
		return false, errors.Join(err, werr)
	}
	return rep.Passed, err
}

// pick returns the set and the model the options name.
func pick(p *profile.Profile, o options) (*profile.Set, string, error) {
	if o.role != "" {
		set, err := p.Set(o.set)
		if err != nil {
			return nil, "", err
		}
		model := map[string]string{
			gate.RoleAgent: set.Roles.Chat, gate.RoleVision: set.Roles.Vision,
			gate.RoleAudio: set.Roles.Audio, gate.RoleEmbedding: set.Roles.Embedding,
		}[o.role]
		if model == "" {
			return nil, "", fmt.Errorf("set %s assigns no model the %s role", set.Name, o.role)
		}
		return set, model, nil
	}
	assigns := func(s *profile.Set) bool { return slices.Contains(s.Roles.Models(), o.model) }
	if o.set != "" {
		set, err := p.Set(o.set)
		if err != nil {
			return nil, "", err
		}
		if !assigns(set) {
			return nil, "", fmt.Errorf("set %s assigns %s no role", set.Name, o.model)
		}
		return set, o.model, nil
	}
	if s := p.StartupSet(); s != nil && assigns(s) {
		return s, o.model, nil
	}
	for i := range p.Sets {
		if assigns(&p.Sets[i]) {
			return &p.Sets[i], o.model, nil
		}
	}
	return nil, "", fmt.Errorf("no set of the profile assigns %s a role", o.model)
}
