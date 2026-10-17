// Package conformstep is evaluate's conform step, which runs clutch's conformance suite k times for
// each consumer over the router, and judges each consumer by pass^k against the bar for a gpt-oss
// implementation.
//
// Usage:
//
//	evaluate conform [-router url] [-profile name|path] [-set name] [-k n] [-consumers list] [-pi path] [-claude path] [-opencode path] [-state dir] [-timeout d]
//
// The consumers are Pi on its llama.cpp provider, Claude Code over the router's Anthropic messages,
// and OpenCode with the chat model's interleaved setting on; the passk package says how each cell
// is built. Every model they run on is one of the profile's set -set names, or SPIKE_SET, by its
// roles: the chat model for every session, the vision model for images, the audio model for the
// audio tool. With neither, the set is the profile's startup set. Each trial runs every consumer's
// nine capabilities once, consumer after consumer, and the run makes -k trials. Every consumer's
// model requests go through a recording proxy (wire.Recorder) in front of the router, one per
// consumer and trial, whose recording is saved under the state directory's conform/wire/; a model
// request the router answers with an error counts as invalid.
//
// The output names the build and the set it ran on, then a Markdown table, a row per consumer and a
// column per capability, each cell the trials that passed out of k, then each consumer's pass^k,
// invalid requests and verdict against the bar, and the reason for every trial that didn't pass.
//
// The router comes from -router or SPIKE_ROUTER_URL. Every model of the set must be loaded
// already: the step checks GET /models first and never loads one. Each consumer is its
// flag, or SPIKE_PI, SPIKE_CLAUDE or SPIKE_OPENCODE, defaulting to the one the consumers mise
// environment pins. Their state lives under -state, or SPIKE_STATE_DIR, which defaults to the
// repository's untracked .state/: the suite's under conform/, and Claude Code's configuration
// directory at claude/, which must hold the architect's subscription login, made once with
//
//	CLAUDE_CONFIG_DIR=$PWD/.state/claude MISE_ENV=consumers mise -C evaluation exec -- claude
//
// then /login.
//
// It exits 0 when every consumer meets the bar, 1 when one doesn't or the run can't start, and 2
// on a usage error.
package conformstep

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/consumers"
	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// errUsage marks an error in how the step was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

// usage is the step's usage, printed on -h and after a usage error.
const usage = `usage:
  evaluate conform [-router url] [-profile name|path] [-set name] [-k n] [-consumers list] [-pi path] [-claude path] [-opencode path] [-state dir] [-timeout d]
`

// options are the step's flags, after the environment's fallbacks.
type options struct {
	router, profile, set, state string
	k                           int
	consumers                   []string
	commands                    map[string]string
	timeout                     time.Duration
}

// Run runs the step with args, reading the environment through getenv, and returns its exit
// status.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	if err == nil {
		var rows []passk.Row
		rows, err = runAll(ctx, opts, stdout, stderr)
		if err == nil {
			for _, r := range rows {
				if !r.MeetsBar() {
					return 1
				}
			}
			return 0
		}
	}
	// A failed write to stderr has nowhere left to be reported, so its errors are dropped.
	switch {
	case errors.Is(err, flag.ErrHelp):
		if _, err := fmt.Fprint(stdout, usage); err != nil {
			return 1
		}
		return 0
	case errors.Is(err, errUsage):
		_, _ = fmt.Fprintf(stderr, "evaluate conform: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "evaluate conform: %v\n", err)
		return 1
	}
}

// parse reads the flags in args, each falling back to its environment variable.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("evaluate conform", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	o := options{commands: map[string]string{}}
	var consumers string
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", profile.HostStrixHalo, "the profile the router serves: a host class, found under the module root's devices/, or a TOML file's path")
	fs.StringVar(&o.set, "set", getenv("SPIKE_SET"), "the profile's set to run on (SPIKE_SET); empty is the startup set")
	fs.IntVar(&o.k, "k", 5, "the trials each consumer runs")
	fs.StringVar(&consumers, "consumers", strings.Join(passk.ConsumerNames, ","), "the consumers to run, comma-separated")
	cmds := map[string]*string{}
	for _, name := range passk.ConsumerNames {
		env := "SPIKE_" + strings.ToUpper(name)
		cmds[name] = fs.String(name, getenv(env), "the "+name+" executable ("+env+"); empty asks mise for the pinned one")
	}
	fs.StringVar(&o.state, "state", cmp.Or(getenv("SPIKE_STATE_DIR"), ".state"), "the consumers' untracked state directory (SPIKE_STATE_DIR)")
	fs.DurationVar(&o.timeout, "timeout", 10*time.Minute, "the bound on one capability")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, err
		}
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	for c := range strings.SplitSeq(consumers, ",") {
		if c = strings.TrimSpace(c); c == "" {
			continue
		}
		if !slices.Contains(passk.ConsumerNames, c) {
			return o, fmt.Errorf("%w: unknown consumer %q (known: %s)", errUsage, c, strings.Join(passk.ConsumerNames, ", "))
		}
		if !slices.Contains(o.consumers, c) {
			o.consumers = append(o.consumers, c)
		}
	}
	switch {
	case fs.NArg() != 0:
		return o, fmt.Errorf("%w: unexpected arguments %q", errUsage, fs.Args())
	case o.router == "":
		return o, fmt.Errorf("%w: no router: set -router or SPIKE_ROUTER_URL", errUsage)
	case o.k < 1:
		return o, fmt.Errorf("%w: -k must be at least 1", errUsage)
	case len(o.consumers) == 0:
		return o, fmt.Errorf("%w: -consumers names none", errUsage)
	}
	for name, p := range cmds {
		o.commands[name] = *p
	}
	o.router = strings.TrimSuffix(o.router, "/")
	// The consumers run in working directories of their own and read the paths their drivers
	// give them from there, so the state directory is made absolute.
	state, err := filepath.Abs(o.state)
	if err != nil {
		return o, err
	}
	o.state = state
	return o, nil
}

// runAll checks the router, runs the trials, and writes the report.
func runAll(ctx context.Context, o options, stdout, stderr io.Writer) ([]passk.Row, error) {
	p, _, err := profile.Open(o.profile)
	if err != nil {
		return nil, err
	}
	set, err := p.Set(o.set)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUsage, err)
	}
	hc := &http.Client{}
	// The consumers' providers can't carry autoload=false, so the run never starts against a model
	// a request would load.
	if err := router.CheckLoaded(ctx, hc, o.router, set.Models...); err != nil {
		return nil, err
	}
	build, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return nil, err
	}
	for _, name := range o.consumers {
		if o.commands[name], err = consumers.Executable(ctx, o.commands[name], name); err != nil {
			return nil, err
		}
	}
	root := filepath.Join(o.state, "conform")
	runner := passk.Runner{
		Commands: o.commands, State: root, ClaudeConfig: filepath.Join(o.state, "claude"),
		Timeout: o.timeout, Log: stderr,
	}
	// Pi and OpenCode run the chat model at its consumer reasoning setting; a valid profile lists
	// every role model.
	res, err := runner.Run(ctx, passk.Plan{
		Router: o.router, Roles: set.Roles, Reasoning: p.Model(set.Roles.Chat).Consumers,
		K: o.k, Consumers: o.consumers, Wire: filepath.Join(root, "wire"),
	})
	if err != nil {
		return nil, err
	}
	return res.Rows, write(stdout, res, o, build, set.Name)
}

// write writes the report: the table, each consumer's verdict, and the reason for every trial
// that didn't pass and every invalid request.
func write(w io.Writer, res passk.Result, o options, build, set string) error {
	host, _ := os.Hostname()
	if _, err := fmt.Fprintf(w, "build %s, set %s, router %s, ran from %s, %d trials, %s\n\n", build, set, o.router, host, o.k, res.Elapsed.Round(time.Second)); err != nil {
		return err
	}
	if err := passk.WriteTable(w, res.Rows); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, r := range res.Rows {
		verdict := "meets the bar"
		if !r.MeetsBar() {
			verdict = "FAILS the bar"
		}
		fmt.Fprintf(&b, "%s: pass^%d %d/%d (%.1f%%), %d invalid: %s (0 invalid and pass^k >= %.0f%%)\n",
			r.Consumer, r.K, r.Passed(), len(r.Cells), 100*r.PassK(), r.Invalid, verdict, 100*passk.Bar)
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return passk.WriteReasons(w, res.Rows, res.Trials)
}
