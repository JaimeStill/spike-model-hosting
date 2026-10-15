// Command swap swaps the router from the set of models it has loaded to another of the profile's
// sets, over the router's own API, and times each step.
//
// Usage:
//
//	swap [-router url] [-profile path] [-interval d] [-load-timeout d] <set>
//
// It unloads every loaded model the incoming set doesn't share, then loads the set's models
// that aren't loaded, as the swap package describes, and checks the router ends with exactly
// the set loaded. A model the router hasn't the files of downloads through the router's own
// Hugging Face fetch as it loads, which -load-timeout bounds, per model. It prints the router's
// build, the set it swapped from, when the loaded models were exactly one of the profile's sets,
// and each model's step with its time, then the total.
//
// The router comes from -router or SPIKE_ROUTER_URL. The set must be one the profile names; the
// tool refuses any other before it reads the router.
//
// It exits 0 when the router ends with exactly the set loaded, 1 when a step fails or the tool
// can't run, and 2 on a usage error, an unknown set among them.
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
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JaimeStill/spike-model-hosting/profile"
	"github.com/JaimeStill/spike-model-hosting/router"
	"github.com/JaimeStill/spike-model-hosting/swap"
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
  swap [-router url] [-profile path] [-interval d] [-load-timeout d] <set>
`

// options are the tool's flags and argument, after the environment's fallbacks.
type options struct {
	router, profile, set string
	interval, timeout    time.Duration
}

// run runs the tool with args, reading the environment through getenv, and returns its exit
// status.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	opts, err := parse(args, getenv)
	if err == nil {
		err = swapTo(ctx, opts, stdout)
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
		_, _ = fmt.Fprintf(stderr, "swap: %v\n%s", err, usage)
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "swap: %v\n", err)
		return 1
	}
}

// parse reads the flags and the set in args, the router falling back to SPIKE_ROUTER_URL.
func parse(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("swap", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	var o options
	fs.StringVar(&o.router, "router", getenv("SPIKE_ROUTER_URL"), "the router's root URL (SPIKE_ROUTER_URL)")
	fs.StringVar(&o.profile, "profile", "profiles/strix-halo.toml", "the profile the router serves")
	fs.DurationVar(&o.interval, "interval", swap.DefaultInterval, "the time between polls of the router's models")
	fs.DurationVar(&o.timeout, "load-timeout", swap.DefaultLoadTimeout, "the bound on one model's load, its download included")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return o, err
		}
		return o, fmt.Errorf("%w: %v", errUsage, err)
	}
	switch {
	case fs.NArg() != 1:
		return o, fmt.Errorf("%w: name one set to swap to", errUsage)
	case o.router == "":
		return o, fmt.Errorf("%w: no router: set -router or SPIKE_ROUTER_URL", errUsage)
	case o.interval <= 0:
		return o, fmt.Errorf("%w: -interval must be positive", errUsage)
	case o.timeout <= 0:
		return o, fmt.Errorf("%w: -load-timeout must be positive", errUsage)
	}
	o.set = fs.Arg(0)
	o.router = strings.TrimSuffix(o.router, "/")
	return o, nil
}

// swapTo swaps the router to the set and writes what it did.
func swapTo(ctx context.Context, o options, stdout io.Writer) error {
	p, err := profile.Load(o.profile)
	if err != nil {
		return err
	}
	set, err := p.Set(o.set)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	hc := &http.Client{}
	build, err := router.Build(ctx, hc, o.router)
	if err != nil {
		return err
	}
	status, err := router.Statuses(ctx, hc, o.router)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "router %s, build %s\nswap to set %s from %s\n\n", o.router, build, set.Name, from(p, status)); err != nil {
		return err
	}
	rep, err := swap.Run(ctx, swap.Config{
		Router: o.router, HTTP: hc, Models: set.Models, Interval: o.interval, LoadTimeout: o.timeout,
	})
	if werr := write(stdout, rep); werr != nil {
		return errors.Join(err, werr)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "\nswapped to set %s in %s, build %s\n", set.Name, sec(rep.Total), build)
	return err
}

// from names what the router has loaded before the swap: the profile's set whose models are
// exactly the loaded ones, or else the loaded models themselves.
func from(p *profile.Profile, status map[string]string) string {
	var loaded []string
	for id, s := range status {
		if s == router.StatusLoaded {
			loaded = append(loaded, id)
		}
	}
	slices.Sort(loaded)
	for _, s := range p.Sets {
		if slices.Equal(slices.Sorted(slices.Values(s.Models)), loaded) {
			return "set " + s.Name
		}
	}
	if len(loaded) == 0 {
		return "no model loaded"
	}
	return "no set: " + strings.Join(loaded, ", ")
}

// write writes the swap's steps, each with its time, and its total.
func write(w io.Writer, rep swap.Report) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "STEP\tMODEL\tTOOK\tRESULT")
	for _, id := range rep.Kept {
		_, _ = fmt.Fprintf(tw, "keep\t%s\t-\tshared, left loaded\n", id)
	}
	for _, steps := range []struct {
		verb  string
		steps []swap.Step
	}{{"unload", rep.Unloads}, {"load", rep.Loads}} {
		for _, s := range steps.steps {
			result := "ok"
			if s.Err != nil {
				result = "FAIL: " + s.Err.Error()
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", steps.verb, s.Model, sec(s.Took), result)
		}
	}
	_, _ = fmt.Fprintf(tw, "total\t\t%s\t\n", sec(rep.Total))
	return tw.Flush()
}

// sec formats d in seconds, to the tenth.
func sec(d time.Duration) string { return fmt.Sprintf("%.1fs", d.Seconds()) }
