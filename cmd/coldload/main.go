// Command coldload times a router restart from the outside: how long until every model it served
// is loaded again.
//
// Usage:
//
//	coldload [-router url] [-interval d] [-timeout d]
//
// Start it before the restart. It polls GET /models every -interval, and it first needs to see
// every listed model loaded: those are the models it waits for. The restart begins at the first
// poll after that which doesn't see them all loaded, whether the router refused the connection,
// answered with an error, or listed a model not loaded. The tool then records when the router
// answered /models again after refusing it, and when each model was listed as loaded again after
// it was gone (the router down, or the model listed not loaded), so a model a stopping router
// still lists counts for nothing; it stops once all of them are back. Every time is measured from the restart's start, so each is late by up to one
// -interval; the last poll that saw them all loaded before the restart is printed too, which
// bounds the start from below.
//
// The router comes from -router or SPIKE_ROUTER_URL. The tool only reads /models; it never loads
// a model or starts a restart.
//
// It exits 0 when every model loaded, 1 when -timeout passed first or the tool can't run, and 2
// on a usage error.
package main

import (
	"context"
	"encoding/json"
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

// watch polls the router until the restart it waits for has every model loaded again, and
// writes the times.
func watch(ctx context.Context, o options, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	hc := &http.Client{Timeout: max(o.interval, 2*time.Second)}
	var t tracker
	tick := time.NewTicker(o.interval)
	defer tick.Stop()
	announced := false
	for {
		status, err := poll(ctx, hc, o.router)
		if ctx.Err() != nil {
			return fmt.Errorf("gave up after %s: %s", o.timeout, t.progress())
		}
		if err := t.observe(time.Now(), status, err); err != nil {
			return err
		}
		if !announced && t.ready() {
			announced = true
			if _, err := fmt.Fprintf(stdout, "watching %s: %d models loaded; restart the router now\n", o.router, len(t.models)); err != nil {
				return err
			}
		}
		if t.done() {
			return t.write(stdout)
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
}

// poll returns each model's status from GET /models, or the error that kept it from answering.
func poll(ctx context.Context, hc *http.Client, router string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, router+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /models: %s", resp.Status)
	}
	var got struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		return nil, fmt.Errorf("GET /models: %w", err)
	}
	status := make(map[string]string, len(got.Data))
	for _, m := range got.Data {
		status[m.ID] = m.Status.Value
	}
	return status, nil
}

// tracker follows a restart through the polls it observes: it waits for every model loaded, then
// for the restart to start, then for every model loaded again.
type tracker struct {
	// models are the models the first poll listed, all loaded, sorted.
	models []string
	// lastSteady is the last poll before the restart that saw every model loaded.
	lastSteady time.Time
	// start is the first poll that didn't, which starts the restart; zero until then.
	start time.Time
	// down is whether a poll since start found the router down.
	down bool
	// up is the first poll the router answered after it was down; zero until then.
	up time.Time
	// gone is each model seen gone since start: the router down, or the model listed not loaded.
	gone map[string]bool
	// loaded is when each model was first listed as loaded after it was gone.
	loaded map[string]time.Time
}

// observe takes one poll's answer, at, or the error that kept the router from giving one.
func (t *tracker) observe(at time.Time, status map[string]string, err error) error {
	if t.models == nil {
		if err != nil {
			return fmt.Errorf("the router must be up, every model loaded, before the restart: %w", err)
		}
		for id, s := range status {
			if s != "loaded" {
				return fmt.Errorf("the router lists %s as %q; start the timer with every model loaded, before the restart", id, s)
			}
			t.models = append(t.models, id)
		}
		if len(t.models) == 0 {
			return errors.New("the router lists no models")
		}
		slices.Sort(t.models)
		t.lastSteady = at
		t.gone = map[string]bool{}
		t.loaded = map[string]time.Time{}
		return nil
	}
	if t.start.IsZero() {
		if err == nil && t.allLoaded(status) {
			t.lastSteady = at
			return nil
		}
		t.start = at
	}
	if err != nil {
		t.down = true
		for _, id := range t.models {
			t.gone[id] = true
		}
		return nil
	}
	if t.down && t.up.IsZero() {
		t.up = at
	}
	for _, id := range t.models {
		_, back := t.loaded[id]
		switch {
		case back:
		case status[id] != "loaded":
			t.gone[id] = true
		case t.gone[id]:
			t.loaded[id] = at
		}
	}
	return nil
}

// allLoaded reports whether status lists every model the tracker waits for as loaded.
func (t *tracker) allLoaded(status map[string]string) bool {
	for _, id := range t.models {
		if status[id] != "loaded" {
			return false
		}
	}
	return true
}

// ready reports whether the tracker has its models and waits for the restart.
func (t *tracker) ready() bool { return t.models != nil }

// done reports whether every model has loaded since the restart started.
func (t *tracker) done() bool {
	return !t.start.IsZero() && len(t.loaded) == len(t.models)
}

// progress says how far the restart got, for a timeout.
func (t *tracker) progress() string {
	switch {
	case t.models == nil:
		return "the router never listed every model loaded"
	case t.start.IsZero():
		return "no restart seen"
	case t.down && t.up.IsZero():
		return "the router never came back"
	}
	var missing []string
	for _, id := range t.models {
		if _, ok := t.loaded[id]; !ok {
			missing = append(missing, id)
		}
	}
	return fmt.Sprintf("%d of %d models loaded; not %s", len(t.loaded), len(t.models), strings.Join(missing, ", "))
}

// write writes the times, each from the restart's start: the router's return, then each model
// in the order it loaded, then the total.
func (t *tracker) write(w io.Writer) error {
	order := slices.Clone(t.models)
	slices.SortStableFunc(order, func(a, b string) int { return t.loaded[a].Compare(t.loaded[b]) })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "restart seen\t%s UTC\t(last steady poll %s before)\n",
		t.start.UTC().Format("15:04:05.000"), sec(t.start.Sub(t.lastSteady)))
	// A router seen answering throughout counts as up from the start.
	up := t.start
	if t.down {
		up = t.up
		_, _ = fmt.Fprintf(tw, "router up\t%s\t\n", sec(up.Sub(t.start)))
	} else {
		_, _ = fmt.Fprintln(tw, "router up\tnever seen down\t")
	}
	for _, id := range order {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t(%s after the router)\n", id, sec(t.loaded[id].Sub(t.start)), sec(t.loaded[id].Sub(up)))
	}
	_, _ = fmt.Fprintf(tw, "all loaded\t%s\t\n", sec(t.loaded[order[len(order)-1]].Sub(t.start)))
	return tw.Flush()
}

// sec formats d in seconds, to the tenth.
func sec(d time.Duration) string { return fmt.Sprintf("%.1fs", d.Seconds()) }
