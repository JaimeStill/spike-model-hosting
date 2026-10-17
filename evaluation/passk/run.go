package passk

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// ConsumerNames are the consumers a run can take, in the order it runs and reports them.
var ConsumerNames = []string{"pi", "claude", "opencode"}

// Runner runs consumers' cells over the router: the executables, where their state lives, and
// how long one capability may take.
type Runner struct {
	// Commands are the consumers' executables, by name.
	Commands map[string]string
	// State is the consumers' state directory, absolute: each keeps its sessions,
	// configuration and cache under it (Config.State). ClaudeConfig is Claude Code's
	// configuration directory, which holds its subscription login.
	State, ClaudeConfig string
	// Timeout bounds one capability.
	Timeout time.Duration
	// Log takes a line as each trial and capability starts; nil discards it.
	Log io.Writer
}

// Plan is one run of k trials: the router, the set's roles, the chat model's consumer reasoning,
// the consumers, where each trial's recording goes, and when to stop early.
type Plan struct {
	// Router is the router's root URL.
	Router    string
	Roles     profile.SetRoles
	Reasoning profile.ConsumerReasoning
	// K is the trials each consumer runs; Consumers, the consumers, in the order they run.
	K         int
	Consumers []string
	// Wire is the directory each consumer's trial's recording is saved in, as
	// <consumer>-<trial>-<time>.jsonl.
	Wire string
	// Stop, when set, is asked after each consumer's trial, once every consumer has run its
	// first, whether the run can end: a non-empty reason ends it there.
	Stop func(trials map[string][]Trial) string
}

// Result is a run's trials, by consumer, each consumer's Row, and why it stopped early, if it
// did.
type Result struct {
	Rows    []Row
	Trials  map[string][]Trial
	Stopped string
	Elapsed time.Duration
}

// Run runs plan: within each trial every consumer's cell once, consumer after consumer, each
// through a wire.Recorder of its own in front of the router, its recording saved under
// plan.Wire. Every consumer runs its first trial, the run's screening fifth at k = 5; after that
// plan.Stop may end it after any consumer's trial. It fails when a cell can't be built or a
// recording can't be saved, or when ctx ends.
func (r Runner) Run(ctx context.Context, plan Plan) (Result, error) {
	res := Result{Trials: map[string][]Trial{}}
	if err := os.MkdirAll(plan.Wire, 0o700); err != nil {
		return res, err
	}
	opts := conform.Options{
		// The consumers mise environment's pins run, and a consumer that drifts from them runs
		// too, its version marked with the pin.
		AnyVersion: true,
		Timeout:    r.Timeout,
		Progress: func(c conform.Cell, capability string) {
			r.logf("-- %s %s: %s\n", time.Now().Format(time.TimeOnly), c.Harness, cmp.Or(capability, "checking the consumer"))
		},
	}
	start := time.Now()
trials:
	for k := 1; k <= plan.K; k++ {
		for i, name := range plan.Consumers {
			r.logf("== trial %d/%d: %s\n", k, plan.K, name)
			t, err := r.trial(ctx, plan, name, k, opts)
			if err != nil {
				return res, err
			}
			res.Trials[name] = append(res.Trials[name], t)
			if err := ctx.Err(); err != nil {
				return res, err
			}
			first := k == 1 && i < len(plan.Consumers)-1
			if plan.Stop != nil && !first {
				if res.Stopped = plan.Stop(res.Trials); res.Stopped != "" {
					break trials
				}
			}
		}
	}
	res.Elapsed = time.Since(start)
	for _, name := range plan.Consumers {
		res.Rows = append(res.Rows, Tally(name, res.Trials[name]))
	}
	return res, nil
}

func (r Runner) logf(format string, args ...any) {
	if r.Log != nil {
		_, _ = fmt.Fprintf(r.Log, format, args...)
	}
}

// trial runs one consumer's cell once, through a Recorder of its own, and saves the recording.
func (r Runner) trial(ctx context.Context, plan Plan, name string, k int, opts conform.Options) (Trial, error) {
	target, err := url.Parse(plan.Router)
	if err != nil {
		return Trial{}, err
	}
	rec := wire.New(target)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Trial{}, err
	}
	srv := &http.Server{Handler: rec, ReadHeaderTimeout: time.Minute}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	cfg := Config{
		Command:      r.Commands[name],
		Router:       "http://" + ln.Addr().String(),
		Direct:       plan.Router,
		State:        r.State,
		ClaudeConfig: r.ClaudeConfig,
		Roles:        plan.Roles,
		Reasoning:    plan.Reasoning,
	}
	var cell conform.Cell
	switch name {
	case "pi":
		cell, err = Pi(cfg)
	case "claude":
		cell = Claude(cfg)
	case "opencode":
		cell, err = OpenCode(cfg)
	default:
		err = fmt.Errorf("no consumer %q; the consumers are %q", name, ConsumerNames)
	}
	if err != nil {
		return Trial{}, err
	}
	rep := conform.Run(ctx, []conform.Cell{cell}, opts)[0]
	path := filepath.Join(plan.Wire, fmt.Sprintf("%s-%d-%s.jsonl", name, k, time.Now().Format("20060102T150405")))
	if err := save(rec, path); err != nil {
		return Trial{}, err
	}
	return Trial{Report: rep, Invalid: Invalid(rec.Exchanges())}, nil
}

// save writes rec's exchanges to path, credentials redacted.
func save(rec *wire.Recorder, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	return errors.Join(rec.WriteJSONL(f), f.Close())
}
