// Package gatestep is evaluate's gated run, which takes one model, or a set, through the
// evaluation package gate's stages: admission, fitness, capacity and quality, then set and deep.
//
// Usage:
//
//	evaluate [-router url] [-profile name|path] [-set name] [-role role | -model id] [-full] [-from stage] [-run dir] [-ssh dest] [-floors path] [-bars path] [-uv path] [-python path] [-pi path] [-claude path] [-opencode path] [-guidellm path] [-restart] [-state dir]
//
// The model is -model, by router ID, or the model the set assigns -role: agent (the profile's
// chat role; chat is accepted), vision, audio or embedding. The set is -set, or SPIKE_SET, or
// else the profile's startup set; with -model and no set named, it is the startup set when that
// assigns the model a role, or else the first set that does. The run judges each role the set
// assigns the model.
//
// A run is a screen by default: admission, fitness and capacity, each model's speed floors
// measured alone, then the quality stage, each tier its screening fifth and on until its 95%
// bound decides it, passed or failed, the roles' tiers at once, with the pool's least free memory
// and the crossing rounds over that pass, which stand in for the set stage's concurrent phase. The
// set and deep stages are left to the full run. A screen that passes every stage makes the model a
// finalist, and the report says so. -full runs the full run: given -run, it continues the finalist
// screen in that run directory, reusing its records and adding the rest of each tier the screen
// stopped, the set stage's alone figures, concurrent phase and slowdowns, and the deep stage;
// without -run it screens first, in a new run directory, and continues the screen when it makes a
// finalist. Its report replaces the screen's, in the same directory, naming the screen it
// continued.
//
// A set run names -set on the command line and neither -role nor -model: its screen takes every
// model of the set through admission, fitness and capacity in turn, alone, each check naming its
// model, then every role's quality tiers at once; its full run's set stage runs every role's
// tier 1 alone and then at once, and its deep stage each model's part, then cold load once. It
// needs every model of the set loaded. Its report is <state>/evaluate/<UTC time>-set-<name>/report.json.
//
// The run stops at the first stage that fails. -from starts it at a later stage, reusing the
// results of the stages before it from an earlier run of the same model (or set run) and mode:
// the run directory -run names, or else the newest under the state directory. Each stage before
// -from must have passed there, and the resumed run's report replaces that run's, in the same
// directory. A screen starts no later than quality; a full run (-full -from) resumes only at set
// or deep, from a full run, and a model's from set also needs that run's quality records of the
// model's tier 1, which the set stage reuses as its alone figure.
//
// The report is <state>/evaluate/<UTC time>-<model>/report.json, the state directory being
// -state, or SPIKE_STATE_DIR, or else the module root's untracked .state/. A summary of it goes
// to stdout, and a line as each stage starts to stderr. The floors are -floors, the module
// root's evaluation/floors.toml by default, and the quality stage's bars -bars, the module root's
// evaluation/bars.toml by default. The pool is read over ssh with amdgpu_top, on -ssh, by default
// the router URL's host.
//
// The embedding role's quality stage runs MTEB, and the audio, vision and agent roles' run Inspect
// AI on the ASR sets', vision tiers' and BFCL tiers' lists (evaluation/tiers), through the uv
// project evaluation/suites, with uv and Python from -uv and -python, or SPIKE_UV and
// SPIKE_PYTHON, or else the consumers mise environment's pins. MTEB's output for each task goes
// in the run directory's mteb/<task>, Inspect's JSON log for each ASR set in its asr/<set>, each
// vision tier's logs in its vision/<tier>/logs and each BFCL tier's in its agent/<tier>/logs; the
// datasets download into the state directory's cache/. The set stage runs the same suites on
// tier 1s, its output under the run directory's set/alone/ (a set run's) and set/concurrent/,
// laid out as the quality stage's. A full run's rest of a tier the screen stopped runs from the
// tier's directory's rest/, its list there as list.txt. The agent role's conform tier, harness fit, runs every pinned
// harness (Pi, Claude Code and OpenCode), each -pi, -claude and -opencode, or SPIKE_PI,
// SPIKE_CLAUDE and SPIKE_OPENCODE, or else the consumers mise environment's pin, their state
// under the state directory's conform/, Claude Code's login in its claude/ (as evaluate conform
// keeps them), and the run's recordings and conform table, the fit profile, in the run
// directory's conform/.
//
// The deep stage, reached in a full run once every gate passed, runs the agent role's τ2 airline and retail
// through the same uv project, their simulated user the pinned Claude Code (-claude,
// SPIKE_CLAUDE or the pin) on the state directory's claude/ login, each domain's Inspect log and
// its summary in the run directory's deep/tau2/<domain>; then, for a generative model, the
// GuideLLM grid, with GuideLLM from -guidellm, or SPIKE_GUIDELLM, or else the consumers mise
// environment's pin, its tokenizers fetched into the state directory's hf/ and its files in the
// run directory's deep/grid; then prefix reuse; then cold load. Cold load restarts the router
// twice, root commands the run can't take: with -restart, the architect is at hand, and the run
// asks for each restart on stderr, naming its command, and times it; without it, cold load is
// recorded not run, naming the commands. -full -from deep runs the deep stage alone again on a full
// run whose gates all passed.
//
// The router comes from -router or SPIKE_ROUTER_URL. The model must be loaded already, and, for a
// run that resumes nothing, every model of the set: the run checks GET /models first, and refuses
// an unloaded model before any stage; every request carries autoload=false, so it never loads
// one. A run that resumes nothing starts from freshly loaded models: once admission passes it
// unloads and loads the set's models over the router's API,
// waits 30 s, reads the idle pool, sends each model its high-water requests (an 8192-token
// prompt to each generative model, the vision role's largest image and the audio role's longest
// clip in the tier lists, found through the suites project into the run directory's warm/, the
// embedding role's largest input), and reads the pool at that warmed peak, which the pool's floor
// is judged at. Each reading carries the host's system RAM: used and available, and each model
// process's RSS. A run resumed with -from on the same run directory, or a full run continuing its
// screen, reloads nothing.
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
	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// Usage is the run's usage line, which evaluate's own usage lists.
const Usage = "evaluate [-router url] [-profile name|path] [-set name] [-role role | -model id] [-full] [-from stage] [-run dir] [-ssh dest] [-floors path] [-bars path] [-uv path] [-python path] [-pi path] [-claude path] [-opencode path] [-guidellm path] [-restart] [-state dir]"

const usage = "usage:\n  " + Usage + "\n"

// errUsage marks an error in how the run was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

type options struct {
	router, profile, set, role, model, from, run, ssh, floors, bars, uv, python, guidellm, state string
	// restart says the architect is at hand to restart the router for the deep stage's cold load.
	restart bool
	// full makes the run a full run, a finalist's, continuing its screen; without it the run is a
	// screen.
	full bool
	// consumers are the conform tier's consumer executables, by name; empty asks mise.
	consumers map[string]string
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
	o := options{consumers: map[string]string{}}
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", profile.HostStrixHalo, "the profile the router serves: a host class or a TOML file's path")
	fs.StringVar(&o.set, "set", getenv("SPIKE_SET"), "the profile's set (SPIKE_SET); empty is the startup set")
	fs.StringVar(&o.role, "role", "", "the role whose model to evaluate: agent, vision, audio or embedding")
	fs.StringVar(&o.model, "model", "", "the model to evaluate, by router ID")
	fs.BoolVar(&o.full, "full", false, "run the full run, a finalist's: continue the screen -run names, or screen first")
	fs.StringVar(&o.from, "from", "", "the stage to start at, reusing an earlier run's results before it")
	fs.StringVar(&o.run, "run", "", "with -from, the run directory to resume, default the newest; with -full, the screen to continue")
	fs.StringVar(&o.ssh, "ssh", "", "the ssh destination the pool is read on; default the router's host")
	fs.StringVar(&o.floors, "floors", "", "the floors file; default the module root's evaluation/floors.toml")
	fs.StringVar(&o.bars, "bars", "", "the quality stage's bars file; default the module root's evaluation/bars.toml")
	fs.StringVar(&o.uv, "uv", getenv("SPIKE_UV"), "the uv executable (SPIKE_UV); empty asks mise for the pinned one")
	fs.StringVar(&o.python, "python", getenv("SPIKE_PYTHON"), "the Python uv runs (SPIKE_PYTHON); empty asks mise for the pinned one")
	fs.StringVar(&o.guidellm, "guidellm", getenv("SPIKE_GUIDELLM"), "the GuideLLM executable (SPIKE_GUIDELLM); empty asks mise for the pinned one")
	fs.BoolVar(&o.restart, "restart", false, "the architect restarts the router when the deep stage's cold load asks; without it, cold load is recorded not run")
	consumerFlags := map[string]*string{}
	for _, name := range passk.ConsumerNames {
		env := "SPIKE_" + strings.ToUpper(name)
		consumerFlags[name] = fs.String(name, getenv(env), "the "+name+" executable ("+env+"); empty asks mise for the pinned one")
	}
	fs.StringVar(&o.state, "state", getenv("SPIKE_STATE_DIR"), "the state directory (SPIKE_STATE_DIR); default the module root's .state")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, err
		}
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	for name, p := range consumerFlags {
		o.consumers[name] = *p
	}
	// A set run needs -set on the command line: SPIKE_SET alone, with a model forgotten, would
	// start one.
	setNamed := false
	fs.Visit(func(f *flag.Flag) { setNamed = setNamed || f.Name == "set" })
	switch {
	case fs.NArg() != 0:
		return o, fmt.Errorf("%w: unexpected arguments %q", errUsage, fs.Args())
	case o.router == "":
		return o, fmt.Errorf("%w: no router: set -router or SPIKE_ROUTER_URL", errUsage)
	case o.role != "" && o.model != "":
		return o, fmt.Errorf("%w: name the model to evaluate with -role or -model, one of them", errUsage)
	case o.role == "" && o.model == "" && !setNamed:
		return o, fmt.Errorf("%w: name the model to evaluate with -role or -model, or a set run with -set alone", errUsage)
	case o.from != "" && !slices.Contains(gate.Stages, o.from):
		return o, fmt.Errorf("%w: -from %q is not one of the stages %q", errUsage, o.from, gate.Stages)
	case o.full && o.from != "" && o.from != gate.StageSet && o.from != gate.StageDeep:
		return o, fmt.Errorf("%w: -full resumes only at set or deep; a full run before them continues its screen (-full -run)", errUsage)
	case !o.full && (o.from == gate.StageSet || o.from == gate.StageDeep):
		return o, fmt.Errorf("%w: a screen ends at quality; -from %s needs -full", errUsage, o.from)
	case o.run != "" && o.from == "" && !o.full:
		return o, fmt.Errorf("%w: -run names the run -from resumes or -full continues; give one of them too", errUsage)
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

	setRun := model == ""
	subject := model
	if setRun {
		subject = gate.SetSubject(set.Name)
	}
	var prior *gate.Report
	dir := ""
	resumes := o.from != "" && o.from != gate.Stages[0]
	if resumes || (o.full && o.run != "") {
		dir = o.run
		if dir == "" {
			if dir, err = gate.LatestRun(state, subject); err != nil {
				return false, err
			}
			if dir == "" {
				return false, fmt.Errorf("no earlier run of %s under %s to resume from %s", subject, state, o.from)
			}
		}
		if prior, err = gate.ReadReport(dir); err != nil {
			return false, err
		}
	}

	// The run directory is named before the run, since the quality stage writes into it.
	if dir == "" {
		dir = gate.RunDir(state, subject, time.Now())
	}
	r := &gate.Runner{
		Router: o.router, Profile: p, Set: set, Model: model, Floors: floors,
		Footprint: gate.SSH{Dest: o.ssh}, Media: media, Log: stderr, Bars: bars, Dir: dir, Restarts: o.restart, Full: o.full,
	}
	roles := r.Roles()
	if len(roles) > 0 {
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
		if slices.Contains(roles, gate.RoleAgent) {
			// τ2's simulated user is the pinned Claude Code on the login conform's Claude Code uses.
			if suites.Claude, err = consumers.Executable(ctx, o.consumers["claude"], "claude"); err != nil {
				return false, err
			}
			if suites.ClaudeConfig, err = filepath.Abs(filepath.Join(state, "claude")); err != nil {
				return false, err
			}
		}
		r.MTEB, r.ASR, r.Vision, r.Agent, r.Tau2, r.Peaks, r.Tiers = suites, suites, suites, suites, suites, suites, filepath.Join(root, gate.TiersDir)
	}
	if slices.Contains(roles, gate.RoleAgent) {
		if r.Conform, err = conformRunner(ctx, o, state, stderr); err != nil {
			return false, err
		}
	}
	generative := false
	for _, m := range set.Roles.Models() {
		if pm := p.Model(m); (setRun || m == model) && pm != nil && pm.Kind == profile.KindGenerative {
			generative = true
		}
	}
	if generative {
		exe, err := consumers.Executable(ctx, o.guidellm, "guidellm")
		if err != nil {
			return false, err
		}
		r.Grid = gate.GuideLLM{Exe: exe, HFHome: filepath.Join(state, "hf"), Log: stderr}
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

// conformRunner returns the agent role's conform tier's runner, the harnesses' executables
// found, their state under the state directory as evaluate conform keeps it.
func conformRunner(ctx context.Context, o options, state string, log io.Writer) (gate.Conform, error) {
	// The harnesses run in working directories of their own and read the paths their drivers give
	// them from there, so the state directory is made absolute.
	state, err := filepath.Abs(state)
	if err != nil {
		return nil, err
	}
	commands := map[string]string{}
	for _, name := range passk.ConsumerNames {
		if commands[name], err = consumers.Executable(ctx, o.consumers[name], name); err != nil {
			return nil, err
		}
	}
	return passk.Runner{
		Commands: commands, State: filepath.Join(state, "conform"), ClaudeConfig: filepath.Join(state, "claude"),
		Timeout: conformTimeout, Log: log,
	}, nil
}

// conformTimeout bounds one conform capability, as evaluate conform's -timeout does by default.
const conformTimeout = 10 * time.Minute

// pick returns the set and the model the options name; the model is empty for a set run.
func pick(p *profile.Profile, o options) (*profile.Set, string, error) {
	if o.role == "" && o.model == "" {
		set, err := p.Set(o.set)
		return set, "", err
	}
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
