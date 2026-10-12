package coldload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/JaimeStill/spike-model-hosting/router"
)

// Watch polls GET /models on the router at base every interval until the restart it waits for
// has every model loaded again, and writes the times to w: first a line saying it is ready for
// the restart, then the Tracker's. It fails when the first poll doesn't list every model loaded,
// or when timeout passes first, saying how far the restart got. It only reads /models; it never
// loads a model or starts a restart.
func Watch(ctx context.Context, base string, interval, timeout time.Duration, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hc := &http.Client{Timeout: max(interval, 2*time.Second)}
	var t Tracker
	tick := time.NewTicker(interval)
	defer tick.Stop()
	announced := false
	for {
		status, err := router.Statuses(ctx, hc, base)
		if ctx.Err() != nil {
			return fmt.Errorf("gave up after %s: %s", timeout, t.Progress())
		}
		if err := t.Observe(time.Now(), status, err); err != nil {
			return err
		}
		if !announced && t.Ready() {
			announced = true
			if _, err := fmt.Fprintf(w, "watching %s: %d models loaded; restart the router now\n", base, len(t.models)); err != nil {
				return err
			}
		}
		if t.Done() {
			return t.Write(w)
		}
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
}

// Tracker follows a restart through the polls it observes; its zero value is ready to use: it waits for every model loaded, then
// for the restart to start, then for every model loaded again.
type Tracker struct {
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

// Observe takes one poll's answer, at, or the error that kept the router from giving one.
func (t *Tracker) Observe(at time.Time, status map[string]string, err error) error {
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
func (t *Tracker) allLoaded(status map[string]string) bool {
	for _, id := range t.models {
		if status[id] != "loaded" {
			return false
		}
	}
	return true
}

// Ready reports whether the tracker has its models and waits for the restart.
func (t *Tracker) Ready() bool { return t.models != nil }

// Done reports whether every model has loaded since the restart started.
func (t *Tracker) Done() bool {
	return !t.start.IsZero() && len(t.loaded) == len(t.models)
}

// Progress says how far the restart got, for a timeout.
func (t *Tracker) Progress() string {
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

// Write writes the times, each from the restart's start: the router's return, then each model
// in the order it loaded, then the total.
func (t *Tracker) Write(w io.Writer) error {
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
