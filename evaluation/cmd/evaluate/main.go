// Command evaluate is the one entry point for the evaluation area's instruments, run from the
// laptop against the router SPIKE_ROUTER_URL names.
//
// Usage:
//
//	evaluate [-set name] (-role role | -model id) [flags]
//	evaluate -set name [flags]
//	evaluate <step> [flags]
//	evaluate <step> -h
//
// Called with flags first, evaluate screens one model, or, with -set and no model, a set: the gated
// stages admission, fitness, capacity and quality, stopping at the first that fails, each tier
// early stopped on either side of its bar and the roles' tiers at once. With -full it runs a
// finalist's full run, the rest of each tier, the set stage and the deep stage, continuing its
// screen. It writes the run's report under the module root's .state/evaluate/ (package gatestep,
// over the evaluation package gate).
//
// A step is one instrument, named as the first argument; the flags after it are the step's own:
//
//   - conform runs clutch's conformance suite k times per harness and judges each harness's fit
//     (package conformstep).
//   - concurrent runs Pi, Claude Code and the model client against the router at once and checks
//     every answer reached its own request (package concurrentstep).
//   - prefix measures prefix-cache reuse for each generative model of a set (package prefixstep).
//   - coldload times a router restart until every model of a set is loaded again (package
//     coldloadstep).
//   - sweep chooses one model's slots and c on the router, writing each step's presets and
//     restoring the committed render after, -restore alone restoring it (package sweepstep).
//
// An interrupt, SIGINT or SIGTERM, cancels the run or the step. Each step's package documents its
// flags, its environment fallbacks, and its exit status: 0 when it passes, 1 when it fails or can't
// run, and 2 on a usage error, as the gated run's does. evaluate itself exits 2 when neither flags
// nor a step, or an unknown step, are named, and 0 on -h.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/JaimeStill/spike-model-hosting/evaluation/cmd/evaluate/internal/coldloadstep"
	"github.com/JaimeStill/spike-model-hosting/evaluation/cmd/evaluate/internal/concurrentstep"
	"github.com/JaimeStill/spike-model-hosting/evaluation/cmd/evaluate/internal/conformstep"
	"github.com/JaimeStill/spike-model-hosting/evaluation/cmd/evaluate/internal/gatestep"
	"github.com/JaimeStill/spike-model-hosting/evaluation/cmd/evaluate/internal/prefixstep"
	"github.com/JaimeStill/spike-model-hosting/evaluation/cmd/evaluate/internal/sweepstep"
)

// step is one instrument evaluate runs by name.
type step struct {
	name, summary string
	// run runs the step with the arguments after its name, reading the environment through
	// getenv, and returns its exit status.
	run func(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int
}

// steps are the instruments, in the order the usage lists them.
var steps = []step{
	{"conform", "run clutch's conformance suite k times per harness, judged by fit", conformstep.Run},
	{"concurrent", "run Pi, Claude Code and the model client at once, each answer checked against its request", concurrentstep.Run},
	{"prefix", "measure prefix-cache reuse for each generative model of a set", prefixstep.Run},
	{"coldload", "time a router restart until every model of a set is loaded again", coldloadstep.Run},
	{"sweep", "choose a model's slots and c, then restore the committed presets", sweepstep.Run},
}

func main() {
	// SIGTERM cancels a step as an interrupt does, so a sweep restores the presets on either.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// usage is evaluate's usage, listing the steps, printed on -h and after a usage error.
func usage() string {
	var b strings.Builder
	b.WriteString("usage:\n  " + gatestep.Usage + "\n  evaluate <step> [flags]\n  evaluate <step> -h\n\n")
	b.WriteString("Flags first screen one model through the gated stages: admission, fitness, capacity,\nthen quality. -full runs a finalist's full run: the rest of each tier, set, then deep.\n-set with no model runs the set's models together.\n\nsteps:\n")
	for _, s := range steps {
		fmt.Fprintf(&b, "  %-11s %s\n", s.name, s.summary)
	}
	return b.String()
}

// run runs the gated stages when args start with a flag, or else the step args names with the
// arguments after it, and returns its exit status.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintf(stderr, "evaluate: no flags or step named\n%s", usage())
		return 2
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		if _, err := io.WriteString(stdout, usage()); err != nil {
			return 1
		}
		return 0
	}
	if strings.HasPrefix(args[0], "-") {
		return gatestep.Run(ctx, args, getenv, stdout, stderr)
	}
	for _, s := range steps {
		if s.name == args[0] {
			return s.run(ctx, args[1:], getenv, stdout, stderr)
		}
	}
	// A failed write to stderr has nowhere left to be reported, so its error is dropped.
	_, _ = fmt.Fprintf(stderr, "evaluate: unknown step %q\n%s", args[0], usage())
	return 2
}
