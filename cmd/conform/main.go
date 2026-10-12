// Command conform runs clutch's conformance suite k times for each consumer over the router, and
// judges each consumer by pass^k against the bar for a gpt-oss implementation.
//
// Usage:
//
//	conform [-router url] [-k n] [-consumers list] [-pi path] [-claude path] [-opencode path] [-state dir] [-timeout d]
//
// The consumers are Pi on its llama.cpp provider, Claude Code over the router's Anthropic
// messages, and OpenCode with gpt-oss-120b's interleaved setting on; the passk package says how
// each cell is built. Each trial runs every consumer's nine capabilities once, consumer after
// consumer, and the run makes -k trials. Every consumer's model requests go through a recording
// proxy (wire.Recorder) in front of the router, one per consumer and trial, whose recording is
// saved under the state directory's conform/wire/; a model request the router answers with an
// error counts as invalid.
//
// The output is a Markdown table, a row per consumer and a column per capability, each cell the
// trials that passed out of k, then each consumer's pass^k, invalid requests and verdict against
// the bar, and the reason for every trial that didn't pass.
//
// The router comes from -router or SPIKE_ROUTER_URL. Every model the suite sends to must be
// loaded already: the tool checks GET /models first and never loads one. Each consumer is its
// flag, or SPIKE_PI, SPIKE_CLAUDE or SPIKE_OPENCODE, defaulting to the one the consumers mise
// environment pins. Their state lives under -state, or SPIKE_STATE_DIR, which defaults to the
// repository's untracked .state/: the suite's under conform/, and Claude Code's configuration
// directory at claude/, which must hold the architect's subscription login, made once with
//
//	CLAUDE_CONFIG_DIR=$PWD/.state/claude MISE_ENV=consumers mise exec -- claude
//
// then /login.
//
// It exits 0 when every consumer meets the bar, 1 when one doesn't or the run can't start, and 2
// on a usage error.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/passk"
	"github.com/JaimeStill/spike-model-hosting/router"
	"github.com/JaimeStill/spike-model-hosting/wire"
)

// consumerNames are the consumers the tool runs, in the order it runs and reports them.
var consumerNames = []string{"pi", "claude", "opencode"}

// errUsage marks an error in how the tool was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// usage is the tool's usage, printed on -h and after a usage error.
const usage = `usage:
  conform [-router url] [-k n] [-consumers list] [-pi path] [-claude path] [-opencode path] [-state dir] [-timeout d]
`

// options are the tool's flags, after the environment's fallbacks.
type options struct {
	router, state string
	k             int
	consumers     []string
	commands      map[string]string
	timeout       time.Duration
}

// run runs the tool with args, reading the environment through getenv, and returns its exit
// status.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
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
		_, _ = fmt.Fprintf(stderr, "conform: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "conform: %v\n", err)
		return 1
	}
}

// parse reads the flags in args, each falling back to its environment variable.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("conform", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	o := options{commands: map[string]string{}}
	var consumers string
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.IntVar(&o.k, "k", 5, "the trials each consumer runs")
	fs.StringVar(&consumers, "consumers", strings.Join(consumerNames, ","), "the consumers to run, comma-separated")
	cmds := map[string]*string{}
	for _, name := range consumerNames {
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
		if !slices.Contains(consumerNames, c) {
			return o, fmt.Errorf("%w: unknown consumer %q (known: %s)", errUsage, c, strings.Join(consumerNames, ", "))
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
	hc := &http.Client{}
	// The consumers' providers can't carry autoload=false, so the run never starts against a model
	// a request would load.
	if err := router.CheckLoaded(ctx, hc, o.router, passk.Models...); err != nil {
		return nil, err
	}
	build, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return nil, err
	}
	for _, name := range o.consumers {
		if o.commands[name], err = resolveTool(ctx, o.commands[name], name); err != nil {
			return nil, err
		}
	}
	root := filepath.Join(o.state, "conform")
	if err := os.MkdirAll(filepath.Join(root, "wire"), 0o700); err != nil {
		return nil, err
	}
	opts := conform.Options{
		// The consumers mise environment's pins run, and a consumer that drifts from them runs
		// too, its version marked with the pin.
		AnyVersion: true,
		Timeout:    o.timeout,
		Progress: func(c conform.Cell, capability string) {
			_, _ = fmt.Fprintf(stderr, "-- %s %s: %s\n", time.Now().Format(time.TimeOnly), c.Harness, cmp.Or(capability, "checking the consumer"))
		},
	}
	start := time.Now()
	trials := map[string][]passk.Trial{}
	for k := 1; k <= o.k; k++ {
		for _, name := range o.consumers {
			_, _ = fmt.Fprintf(stderr, "== trial %d/%d: %s\n", k, o.k, name)
			t, err := trial(ctx, o, root, name, k, opts)
			if err != nil {
				return nil, err
			}
			trials[name] = append(trials[name], t)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	rows := make([]passk.Row, len(o.consumers))
	for i, name := range o.consumers {
		rows[i] = passk.Tally(name, trials[name])
	}
	return rows, write(stdout, rows, trials, o, build, time.Since(start))
}

// trial runs one consumer's cell once, through a Recorder of its own, and saves the recording.
func trial(ctx context.Context, o options, root, name string, k int, opts conform.Options) (passk.Trial, error) {
	target, err := url.Parse(o.router)
	if err != nil {
		return passk.Trial{}, err
	}
	rec := wire.New(target)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return passk.Trial{}, err
	}
	srv := &http.Server{Handler: rec, ReadHeaderTimeout: time.Minute}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	cfg := passk.Config{
		Command:      o.commands[name],
		Router:       "http://" + ln.Addr().String(),
		Direct:       o.router,
		State:        root,
		ClaudeConfig: filepath.Join(o.state, "claude"),
	}
	var cell conform.Cell
	switch name {
	case "pi":
		cell = passk.Pi(cfg)
	case "claude":
		cell = passk.Claude(cfg)
	default:
		if cell, err = passk.OpenCode(cfg); err != nil {
			return passk.Trial{}, err
		}
	}
	rep := conform.Run(ctx, []conform.Cell{cell}, opts)[0]
	path := filepath.Join(root, "wire", fmt.Sprintf("%s-%d-%s.jsonl", name, k, time.Now().Format("20060102T150405")))
	if err := save(rec, path); err != nil {
		return passk.Trial{}, err
	}
	return passk.Trial{Report: rep, Invalid: passk.Invalid(rec.Exchanges())}, nil
}

// save writes rec's exchanges to path, credentials redacted.
func save(rec *wire.Recorder, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	return errors.Join(rec.WriteJSONL(f), f.Close())
}

// write writes the report: the table, each consumer's verdict, and the reason for every trial
// that didn't pass and every invalid request.
func write(w io.Writer, rows []passk.Row, trials map[string][]passk.Trial, o options, build string, elapsed time.Duration) error {
	host, _ := os.Hostname()
	var b strings.Builder
	fmt.Fprintf(&b, "build %s, router %s, ran from %s, %d trials, %s\n\n", build, o.router, host, o.k, elapsed.Round(time.Second))
	b.WriteString("| Consumer | Version |")
	for _, c := range conform.Capabilities {
		b.WriteString(" " + c.Name + " |")
	}
	b.WriteString(" pass^k | invalid |\n|---|---|")
	b.WriteString(strings.Repeat("---|", len(conform.Capabilities)+2) + "\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s |", r.Consumer, cmp.Or(r.Version, "-"))
		for _, c := range r.Cells {
			fmt.Fprintf(&b, " %s |", cellText(c, r.K))
		}
		fmt.Fprintf(&b, " %d/%d (%.1f%%) | %d |\n", r.Passed(), len(r.Cells), 100*r.PassK(), r.Invalid)
	}
	b.WriteString("\n")
	for _, r := range rows {
		verdict := "meets the bar"
		if !r.MeetsBar() {
			verdict = "FAILS the bar"
		}
		fmt.Fprintf(&b, "%s: pass^%d %d/%d (%.1f%%), %d invalid: %s (0 invalid and pass^k >= %.0f%%)\n",
			r.Consumer, r.K, r.Passed(), len(r.Cells), 100*r.PassK(), r.Invalid, verdict, 100*passk.Bar)
	}
	for _, r := range rows {
		for _, s := range r.Skipped {
			fmt.Fprintf(&b, "\n%s skipped a trial: %s", r.Consumer, oneLine(s))
		}
		for _, c := range r.Cells {
			for _, reason := range c.Reasons {
				fmt.Fprintf(&b, "\n%s %s: %s", r.Consumer, c.Capability, oneLine(reason))
			}
		}
		for k, t := range trials[r.Consumer] {
			for _, x := range t.Invalid {
				fmt.Fprintf(&b, "\n%s trial %d invalid: %s %s -> %d %s", r.Consumer, k+1, x.Method, x.Path, x.Status, truncate(oneLine(x.Response), 200))
			}
		}
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// cellText is a capability's trials that passed out of k, with n/a or skip noted when every
// trial that didn't pass was one.
func cellText(c passk.Cell, k int) string {
	s := fmt.Sprintf("%d/%d", c.Pass, k)
	switch {
	case c.NA > 0 && c.NA == k-c.Pass:
		s += " n/a"
	case c.Skip > 0 && c.Skip == k-c.Pass:
		s += " skip"
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// resolveTool returns a consumer's executable: given, or else the one named name that the
// consumers mise environment pins, rather than whatever the user's PATH finds first.
func resolveTool(ctx context.Context, given, name string) (string, error) {
	if given != "" {
		return given, nil
	}
	cmd := exec.CommandContext(ctx, "mise", "which", name)
	cmd.Env = append(os.Environ(), "MISE_ENV=consumers")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("finding the pinned %s (MISE_ENV=consumers mise which %s): %w; set -%s or SPIKE_%s",
			name, name, err, name, strings.ToUpper(name))
	}
	return strings.TrimSpace(string(out)), nil
}
