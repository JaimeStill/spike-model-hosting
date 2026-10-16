// Package coldloadstep is evaluate's coldload step, which times a router restart from the outside:
// how long until every model of a set is loaded again.
//
// Usage:
//
//	evaluate coldload [-router url] [-profile name|path] [-set name] [-interval d] [-timeout d]
//
// The set is the profile's set -set names, or SPIKE_SET, or else its startup set, the one the
// router loads when it starts. Start the step before the restart, with the set's models loaded:
// it refuses to start otherwise. It polls GET /models every -interval and waits through the
// restart, as the coldload package's Watch describes, then prints when the router answered again
// and when each of the set's models was listed as loaded again, each from the restart's start,
// and the last poll that saw them all loaded before it, which bounds the start from below. It
// names the set, and the build before the restart and after it.
//
// The router comes from -router or SPIKE_ROUTER_URL. The step only reads /models and /props; it
// never loads a model or starts a restart.
//
// It exits 0 when every model loaded, 1 when -timeout passed first or the step can't run, and 2
// on a usage error.
package coldloadstep

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/coldload"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// errUsage marks an error in how the step was called, which exits 2 rather than 1.
var errUsage = errors.New("usage")

// usage is the step's usage, printed on -h and after a usage error.
const usage = `usage:
  evaluate coldload [-router url] [-profile name|path] [-set name] [-interval d] [-timeout d]
`

// options are the step's flags, after the environment's fallbacks.
type options struct {
	router, profile, set string
	interval, timeout    time.Duration
}

// Run runs the step with args, reading the environment through getenv, and returns its exit
// status.
func Run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
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
		_, _ = fmt.Fprintf(stderr, "evaluate coldload: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "evaluate coldload: %v\n", err)
		return 1
	}
}

// parse reads the flags in args, the router and the set falling back to SPIKE_ROUTER_URL and
// SPIKE_SET.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("evaluate coldload", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", profile.HostStrixHalo, "the profile the router serves: a host class, found under the module root's devices/, or a TOML file's path")
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
	p, _, err := profile.Open(o.profile)
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
