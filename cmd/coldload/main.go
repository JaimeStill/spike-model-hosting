// Command coldload times a router restart from the outside: how long until every model of a set
// is loaded again.
//
// Usage:
//
//	coldload [-router url] [-profile path] [-set name] [-interval d] [-timeout d]
//
// The set is the profile's set -set names, or SPIKE_SET, or else its startup set, the one the
// router loads when it starts. Start the tool before the restart, with the set's models loaded:
// it refuses to start otherwise. It polls GET /models every -interval and waits through the
// restart, as the coldload package's Watch describes, then prints when the router answered again
// and when each of the set's models was listed as loaded again, each from the restart's start,
// and the last poll that saw them all loaded before it, which bounds the start from below. It
// names the set, and the build before the restart and after it.
//
// The router comes from -router or SPIKE_ROUTER_URL. The tool only reads /models and /props; it
// never loads a model or starts a restart.
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
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/coldload"
	"github.com/JaimeStill/spike-model-hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/router"
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
  coldload [-router url] [-profile path] [-set name] [-interval d] [-timeout d]
`

// options are the tool's flags, after the environment's fallbacks.
type options struct {
	router, profile, set string
	interval, timeout    time.Duration
}

// run runs the tool with args, reading the environment through getenv, and returns its exit
// status.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	if err == nil {
		err = watch(ctx, opts, stdout)
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

// parse reads the flags in args, the router and the set falling back to SPIKE_ROUTER_URL and
// SPIKE_SET.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("coldload", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", "profiles/strix-halo.toml", "the profile the router serves")
	fs.StringVar(&o.set, "set", getenv("SPIKE_SET"), "the profile's set to time (SPIKE_SET); empty is the startup set")
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

// watch times one restart of the router, waiting for the set's models, and names the set and the
// build before and after it.
func watch(ctx context.Context, o options, stdout io.Writer) error {
	p, err := profile.Load(o.profile)
	if err != nil {
		return err
	}
	set, err := p.Set(o.set)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	hc := &http.Client{}
	if err := router.CheckLoaded(ctx, hc, o.router, set.Models...); err != nil {
		return err
	}
	build, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "router %s, build %s, set %s\n", o.router, build, set.Name); err != nil {
		return err
	}
	if err := coldload.Watch(ctx, o.router, set.Models, o.interval, o.timeout, stdout); err != nil {
		return err
	}
	after, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "build after the restart %s, set %s\n", after, set.Name)
	return err
}
