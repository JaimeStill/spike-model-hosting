package swap

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// The Config's defaults. A load may download tens of gigabytes before it loads, so its bound
// is generous; an unload only stops an instance.
const (
	DefaultInterval      = time.Second
	DefaultLoadTimeout   = 2 * time.Hour
	DefaultUnloadTimeout = 5 * time.Minute
)

// Config is one swap: the router, and the models it ends with loaded.
type Config struct {
	// Router is the router's root URL.
	Router string
	// HTTP is the client the swap reads and commands the router with; nil is a plain client.
	HTTP *http.Client
	// Models are the incoming set's models, by router ID, loaded in this order.
	Models []string
	// Interval is the time between polls of GET /models; zero is DefaultInterval.
	Interval time.Duration
	// LoadTimeout bounds one model's load, its download included; zero is
	// DefaultLoadTimeout.
	LoadTimeout time.Duration
	// UnloadTimeout bounds one model's unload; zero is DefaultUnloadTimeout.
	UnloadTimeout time.Duration
}

// Step is one model's unload or load, timed from the request to the poll that saw it done.
type Step struct {
	Model string
	// Took is how long the step took; for a step that failed, how long until it failed.
	Took time.Duration
	// Err is why the step failed, or nil.
	Err error
}

// Report is what a swap did, in the order it did it.
type Report struct {
	// Kept are the incoming models the router had loaded already, left alone.
	Kept []string
	// Unloads and Loads are the steps, each in the order it ran. A swap that failed stops at
	// the step that failed, which is the last of its kind.
	Unloads, Loads []Step
	// Total is the whole swap, from the first read of the router to the final check.
	Total time.Duration
}

// Run swaps the router to cfg.Models: it unloads every loaded model the incoming set doesn't
// hold, then loads each of the set's models that isn't loaded, and checks the router ends with
// exactly the set loaded. It fails before any request that changes the router when the router
// doesn't list one of the set's models. A step that fails stops the swap, and a load is never
// sent while an unload is unfinished. The Report holds the steps run, even when Run fails.
func Run(ctx context.Context, cfg Config) (rep Report, err error) {
	cfg.HTTP = cmp.Or(cfg.HTTP, &http.Client{})
	cfg.Interval = cmp.Or(cfg.Interval, DefaultInterval)
	cfg.LoadTimeout = cmp.Or(cfg.LoadTimeout, DefaultLoadTimeout)
	cfg.UnloadTimeout = cmp.Or(cfg.UnloadTimeout, DefaultUnloadTimeout)
	start := time.Now()
	defer func() { rep.Total = time.Since(start) }()

	models, err := router.Models(ctx, cfg.HTTP, cfg.Router)
	if err != nil {
		return rep, err
	}
	status := make(map[string]string, len(models))
	for _, m := range models {
		status[m.ID] = m.Status
	}
	for _, id := range cfg.Models {
		if _, ok := status[id]; !ok {
			return rep, fmt.Errorf("the router doesn't list %s; its presets must name every set's models", id)
		}
	}
	var outgoing, incoming []string
	for _, m := range models {
		if m.Status != router.StatusUnloaded && !slices.Contains(cfg.Models, m.ID) {
			outgoing = append(outgoing, m.ID)
		}
	}
	for _, id := range cfg.Models {
		if status[id] == router.StatusLoaded {
			rep.Kept = append(rep.Kept, id)
		} else {
			incoming = append(incoming, id)
		}
	}

	for _, id := range outgoing {
		s := unload(ctx, cfg, id)
		rep.Unloads = append(rep.Unloads, s)
		if s.Err != nil {
			return rep, fmt.Errorf("unloading %s: %w", id, s.Err)
		}
	}
	for _, id := range incoming {
		s := load(ctx, cfg, id, status[id])
		rep.Loads = append(rep.Loads, s)
		if s.Err != nil {
			return rep, fmt.Errorf("loading %s: %w", id, s.Err)
		}
	}
	return rep, check(ctx, cfg)
}

// Reload unloads each of cfg.Models and loads it again, so every one starts from a fresh load:
// a model keeps the largest working memory it has needed until it is reloaded. It fails before
// any request that changes the router unless the router lists every one of them loaded, and
// leaves every other model as it is. Every unload finishes before the first load, in cfg.Models'
// order, and the reload ends by checking each is listed loaded. The Report holds the steps run,
// even when Reload fails; it keeps none.
func Reload(ctx context.Context, cfg Config) (rep Report, err error) {
	cfg.HTTP = cmp.Or(cfg.HTTP, &http.Client{})
	cfg.Interval = cmp.Or(cfg.Interval, DefaultInterval)
	cfg.LoadTimeout = cmp.Or(cfg.LoadTimeout, DefaultLoadTimeout)
	cfg.UnloadTimeout = cmp.Or(cfg.UnloadTimeout, DefaultUnloadTimeout)
	start := time.Now()
	defer func() { rep.Total = time.Since(start) }()

	if err := router.CheckLoaded(ctx, cfg.HTTP, cfg.Router, cfg.Models...); err != nil {
		return rep, fmt.Errorf("a reload starts from every model loaded: %w", err)
	}
	for _, id := range cfg.Models {
		s := unload(ctx, cfg, id)
		rep.Unloads = append(rep.Unloads, s)
		if s.Err != nil {
			return rep, fmt.Errorf("unloading %s: %w", id, s.Err)
		}
	}
	for _, id := range cfg.Models {
		s := load(ctx, cfg, id, router.StatusUnloaded)
		rep.Loads = append(rep.Loads, s)
		if s.Err != nil {
			return rep, fmt.Errorf("loading %s: %w", id, s.Err)
		}
	}
	return rep, router.CheckLoaded(ctx, cfg.HTTP, cfg.Router, cfg.Models...)
}

// unload unloads id and polls until the router lists it unloaded.
func unload(ctx context.Context, cfg Config, id string) Step {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, cfg.UnloadTimeout)
	defer cancel()
	err := router.Unload(ctx, cfg.HTTP, cfg.Router, id)
	if err == nil {
		err = poll(ctx, cfg, id, func(m router.Model) (bool, error) {
			return m.Status == router.StatusUnloaded, nil
		})
	}
	return Step{Model: id, Took: time.Since(start), Err: err}
}

// load loads id, whose status the swap's first read gave, and polls until the router lists it
// loaded, or unloaded with its load failed. A model already loading or downloading is waited
// for, not asked to load again.
func load(ctx context.Context, cfg Config, id, status string) Step {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, cfg.LoadTimeout)
	defer cancel()
	var err error
	if status == router.StatusUnloaded {
		err = router.Load(ctx, cfg.HTTP, cfg.Router, id)
	}
	if err == nil {
		err = poll(ctx, cfg, id, func(m router.Model) (bool, error) {
			if m.Status == router.StatusUnloaded && m.Failed {
				return false, fmt.Errorf("the router failed to load it (exit code %d)", m.ExitCode)
			}
			return m.Status == router.StatusLoaded, nil
		})
	}
	return Step{Model: id, Took: time.Since(start), Err: err}
}

// poll reads GET /models every interval until done says id's listing is done, or fails, or ctx
// ends. A read that fails is retried at the next poll: a router busy starting an instance may
// answer late.
func poll(ctx context.Context, cfg Config, id string, done func(router.Model) (bool, error)) error {
	tick := time.NewTicker(cfg.Interval)
	defer tick.Stop()
	last := "never read"
	for {
		models, err := router.Models(ctx, cfg.HTTP, cfg.Router)
		switch {
		case err != nil && ctx.Err() != nil:
			// The read the deadline cut short says nothing of the model; the last one does.
		case err != nil:
			last = err.Error()
		default:
			last = "not listed"
			for _, m := range models {
				if m.ID != id {
					continue
				}
				last = fmt.Sprintf("listed %q", m.Status)
				ok, err := done(m)
				if err != nil || ok {
					return err
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w; last %s", ctx.Err(), last)
		case <-tick.C:
		}
	}
}

// check fails unless the router lists exactly cfg.Models loaded, naming each model that differs.
func check(ctx context.Context, cfg Config) error {
	models, err := router.Models(ctx, cfg.HTTP, cfg.Router)
	if err != nil {
		return err
	}
	var wrong []string
	for _, m := range models {
		want := slices.Contains(cfg.Models, m.ID)
		switch {
		case want && m.Status != router.StatusLoaded:
			wrong = append(wrong, fmt.Sprintf("%s is %q, not loaded", m.ID, m.Status))
		case !want && m.Status != router.StatusUnloaded:
			wrong = append(wrong, fmt.Sprintf("%s is %q, outside the set", m.ID, m.Status))
		}
	}
	if len(wrong) > 0 {
		return errors.New("the router didn't end with exactly the set loaded: " + strings.Join(wrong, "; "))
	}
	return nil
}
