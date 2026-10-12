// Command coldload times a router restart from the outside: how long until every model it served
// is loaded again.
//
// Usage:
//
//	coldload [-router url] [-interval d] [-timeout d]
//
// Start it before the restart. It polls GET /models every -interval and waits through the
// restart, as the coldload package's Watch describes, then prints when the router answered again
// and when each model was listed as loaded again, each from the restart's start, and the last
// poll that saw them all loaded before it, which bounds the start from below.
//
// The router comes from -router or SPIKE_ROUTER_URL. The tool only reads /models; it never loads
// a model or starts a restart.
//
// It exits 0 when every model loaded, 1 when -timeout passed first or the tool can't run, and 2
// on a usage error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/coldload"
)

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
  coldload [-router url] [-interval d] [-timeout d]
`

// options are the tool's flags, after the environment's fallbacks.
type options struct {
	router            string
	interval, timeout time.Duration
}

// run runs the tool with args, reading the environment through getenv, and returns its exit
// status.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	if err == nil {
		err = coldload.Watch(ctx, opts.router, opts.interval, opts.timeout, stdout)
		if err == nil {
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
		_, _ = fmt.Fprintf(stderr, "coldload: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "coldload: %v\n", err)
		return 1
	}
}

// parse reads the flags in args, the router falling back to SPIKE_ROUTER_URL.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("coldload", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.DurationVar(&o.interval, "interval", 250*time.Millisecond, "the time between polls")
	fs.DurationVar(&o.timeout, "timeout", 30*time.Minute, "the bound on the whole wait, restart included")
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
	case o.interval <= 0:
		return o, fmt.Errorf("%w: -interval must be positive", errUsage)
	case o.timeout <= 0:
		return o, fmt.Errorf("%w: -timeout must be positive", errUsage)
	}
	o.router = strings.TrimSuffix(o.router, "/")
	return o, nil
}
