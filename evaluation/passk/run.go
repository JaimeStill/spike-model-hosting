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

	// runCell runs one cell once; nil is clutch's conform.Run. Tests replace it to script
	// trials without a harness (export_test.go).
	runCell func(context.Context, conform.Cell, conform.Options) conform.Report
}

// Plan is one run of k trials: the router, the set's roles, the chat model's consumer reasoning,
// the harnesses, where each trial's recording goes, and whether a harness drops at its first
// failed trial.
type Plan struct {
	// Router is the router's root URL.
	Router    string
	Roles     profile.SetRoles
	Reasoning profile.ConsumerReasoning
	// K is the trials each harness runs; Harnesses, the harnesses, in the order they run.
	K         int
	Harnesses []Harness
	// Wire is the directory each harness's trial's recording is saved in, as
	// <harness>-<trial>-<time>.jsonl.
	Wire string
	// Drop, when set, drops a harness at its first trial that fails the fit bar (Judge): it
	// runs no more trials, since none could bring it back. Unset, every harness runs all K.
	Drop bool
}

// Result is a run's trials, by harness, each harness's Row and Fit, in the plan's order, its
// wall time, and each harness's share of it.
type Result struct {
	Rows    []Row
	Fits    []Fit
	Trials  map[string][]Trial
	Elapsed time.Duration
	// Wall is each harness's time in its trials, by name.
	Wall map[string]time.Duration
}

// Run runs plan: within each trial every harness's cell once, harness after harness, each
// through a wire.Recorder of its own in front of the router, its recording saved under
// plan.Wire. With plan.Drop, a harness that fails the fit bar in a trial runs no more, and the
// run ends once every harness has dropped or run all k. It fails when a cell can't be built or
// a recording can't be saved, or when ctx ends.
func (r Runner) Run(ctx context.Context, plan Plan) (Result, error) {
	res := Result{Trials: map[string][]Trial{}, Wall: map[string]time.Duration{}}
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
	dropped := map[string]bool{}
	for k := 1; k <= plan.K && len(dropped) < len(plan.Harnesses); k++ {
		for _, h := range plan.Harnesses {
			if dropped[h.Name] {
				continue
			}
			r.logf("== trial %d/%d: %s\n", k, plan.K, h.Name)
			began := time.Now()
			t, err := r.trial(ctx, plan, h, k, opts)
			res.Wall[h.Name] += time.Since(began)
			if err != nil {
				return res, err
			}
			res.Trials[h.Name] = append(res.Trials[h.Name], t)
			if err := ctx.Err(); err != nil {
				return res, err
			}
			if plan.Drop {
				if f := Judge(h.Name, res.Trials[h.Name], plan.K); f.Failed() {
					dropped[h.Name] = true
					r.logf("== %s %s\n", h.Name, f.Verdict())
				}
			}
		}
	}
	res.Elapsed = time.Since(start)
	for _, h := range plan.Harnesses {
		f := Judge(h.Name, res.Trials[h.Name], plan.K)
		res.Rows = append(res.Rows, f.Row)
		res.Fits = append(res.Fits, f)
	}
	return res, nil
}

func (r Runner) logf(format string, args ...any) {
	if r.Log != nil {
		_, _ = fmt.Fprintf(r.Log, format, args...)
	}
}

// trial runs one harness's cell once, through a Recorder of its own, and saves the recording.
func (r Runner) trial(ctx context.Context, plan Plan, h Harness, k int, opts conform.Options) (Trial, error) {
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
		Command:      r.Commands[h.Name],
		Router:       "http://" + ln.Addr().String(),
		Direct:       plan.Router,
		State:        r.State,
		ClaudeConfig: r.ClaudeConfig,
		Roles:        plan.Roles,
		Reasoning:    plan.Reasoning,
	}
	cell, err := h.Cell(cfg)
	if err != nil {
		return Trial{}, err
	}
	var rep conform.Report
	if r.runCell != nil {
		rep = r.runCell(ctx, cell, opts)
	} else {
		rep = conform.Run(ctx, []conform.Cell{cell}, opts)[0]
	}
	path := filepath.Join(plan.Wire, fmt.Sprintf("%s-%d-%s.jsonl", h.Name, k, time.Now().Format("20060102T150405")))
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
