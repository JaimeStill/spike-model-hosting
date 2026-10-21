package swap_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/hosting/swap"
)

// fakeRouter is a router in router mode whose loads and unloads take effect a few polls of
// GET /models after they are asked for, as the real router's do in the background. It records
// every command, and every load asked for while an unload was unfinished.
type fakeRouter struct {
	mu sync.Mutex
	// order is the order GET /models lists the models in.
	order []string
	// status is each model's status value.
	status map[string]string
	// failed are the models whose loads fail, listed unloaded with failed set afterwards.
	failed map[string]bool
	// failedNow are the models whose last load failed.
	failedNow map[string]bool
	// stuck are the models whose unloads never finish.
	stuck map[string]bool
	// sideEffect loads a second model when the first loads, as a router that autoloads would.
	sideEffect map[string]string
	// pending is each model's polls left until its command takes effect, and the status it
	// ends in.
	pending map[string]int
	ends    map[string]string
	// commands are the POSTs, as "load id" or "unload id", in the order they came.
	commands []string
	// early are the loads asked for while an unload was unfinished.
	early []string
}

// polls is how many GET /models a command takes to take effect.
const polls = 3

func newFake(loaded, unloaded []string) *fakeRouter {
	f := &fakeRouter{
		status: map[string]string{}, failed: map[string]bool{}, failedNow: map[string]bool{},
		stuck: map[string]bool{}, sideEffect: map[string]string{}, pending: map[string]int{}, ends: map[string]string{},
	}
	for _, id := range loaded {
		f.order = append(f.order, id)
		f.status[id] = "loaded"
	}
	for _, id := range unloaded {
		f.order = append(f.order, id)
		f.status[id] = "unloaded"
	}
	return f
}

func (f *fakeRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/models":
		f.advance()
		var data []map[string]any
		for _, id := range f.order {
			st := map[string]any{"value": f.status[id]}
			if f.failedNow[id] {
				st["failed"], st["exit_code"] = true, 1
			}
			data = append(data, map[string]any{"id": id, "status": st})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case r.Method == http.MethodPost && (r.URL.Path == "/models/load" || r.URL.Path == "/models/unload"):
		var in struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		verb := strings.TrimPrefix(r.URL.Path, "/models/")
		f.commands = append(f.commands, verb+" "+in.Model)
		if verb == "load" {
			for id, end := range f.ends {
				if end == "unloaded" {
					f.early = append(f.early, in.Model+" while "+id+" unloads")
				}
			}
			f.status[in.Model] = "loading"
			f.failedNow[in.Model] = false
			f.pending[in.Model], f.ends[in.Model] = polls, "loaded"
			if f.failed[in.Model] {
				f.ends[in.Model] = "failed"
			}
		} else {
			// A model stays listed loaded while its instance stops.
			f.pending[in.Model], f.ends[in.Model] = polls, "unloaded"
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	default:
		http.NotFound(w, r)
	}
}

// advance counts one poll down on every pending command, and applies those that come due.
func (f *fakeRouter) advance() {
	for id, n := range f.pending {
		if f.stuck[id] && f.ends[id] == "unloaded" {
			continue
		}
		if n > 1 {
			f.pending[id] = n - 1
			continue
		}
		switch f.ends[id] {
		case "failed":
			f.status[id], f.failedNow[id] = "unloaded", true
		default:
			f.status[id] = f.ends[id]
		}
		if other, ok := f.sideEffect[id]; ok && f.ends[id] == "loaded" {
			f.status[other] = "loaded"
		}
		delete(f.pending, id)
		delete(f.ends, id)
	}
}

func (f *fakeRouter) start(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return srv.URL
}

func config(base string, models ...string) swap.Config {
	return swap.Config{Router: base, Models: models, Interval: time.Millisecond, UnloadTimeout: 5 * time.Second, LoadTimeout: 5 * time.Second}
}

func names(steps []swap.Step) []string {
	var ids []string
	for _, s := range steps {
		ids = append(ids, s.Model)
	}
	return ids
}

// TestSwap shows a swap unloads the outgoing models the incoming set doesn't share, then loads
// the rest, never asking for a load while an unload is unfinished, leaves a shared model alone,
// times each step, and ends with exactly the incoming set loaded.
func TestSwap(t *testing.T) {
	f := newFake([]string{"a", "shared", "c"}, []string{"d", "e"})
	rep, err := swap.Run(t.Context(), config(f.start(t), "shared", "d", "e"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"unload a", "unload c", "load d", "load e"}; !slices.Equal(f.commands, want) {
		t.Errorf("commands %q, want %q", f.commands, want)
	}
	if len(f.early) > 0 {
		t.Errorf("loads asked for while an unload was unfinished: %q", f.early)
	}
	if !slices.Equal(rep.Kept, []string{"shared"}) || !slices.Equal(names(rep.Unloads), []string{"a", "c"}) || !slices.Equal(names(rep.Loads), []string{"d", "e"}) {
		t.Errorf("report kept %q, unloaded %q, loaded %q; want shared, a c, d e", rep.Kept, names(rep.Unloads), names(rep.Loads))
	}
	for _, s := range slices.Concat(rep.Unloads, rep.Loads) {
		if s.Took <= 0 || s.Err != nil {
			t.Errorf("step %s: took %s, err %v; want a time and no error", s.Model, s.Took, s.Err)
		}
	}
	if rep.Total <= 0 {
		t.Error("the report has no total")
	}
	for id, want := range map[string]string{"a": "unloaded", "shared": "loaded", "c": "unloaded", "d": "loaded", "e": "loaded"} {
		if f.status[id] != want {
			t.Errorf("%s ends %q, want %q", id, f.status[id], want)
		}
	}
}

// TestSwapUnlistedModel shows a swap to a model the router doesn't list fails before it sends
// any command.
func TestSwapUnlistedModel(t *testing.T) {
	f := newFake([]string{"a"}, []string{"b"})
	_, err := swap.Run(t.Context(), config(f.start(t), "b", "missing"))
	if err == nil || !strings.Contains(err.Error(), "doesn't list missing") {
		t.Errorf("Run = %v, want an error naming the missing model", err)
	}
	if len(f.commands) != 0 {
		t.Errorf("commands %q, want none", f.commands)
	}
}

// TestSwapLoadFails shows a model whose load fails is reported by name, with the router's exit
// code, and stops the swap.
func TestSwapLoadFails(t *testing.T) {
	f := newFake([]string{"a"}, []string{"b", "c", "d"})
	f.failed["c"] = true
	rep, err := swap.Run(t.Context(), config(f.start(t), "b", "c", "d"))
	if err == nil || !strings.Contains(err.Error(), "loading c") || !strings.Contains(err.Error(), "exit code 1") {
		t.Errorf("Run = %v, want an error naming c and its exit code", err)
	}
	last := rep.Loads[len(rep.Loads)-1]
	if last.Model != "c" || last.Err == nil {
		t.Errorf("last load %s, err %v; want c, failed", last.Model, last.Err)
	}
	if slices.Contains(f.commands, "load d") {
		t.Errorf("commands %q: d loaded after c failed", f.commands)
	}
}

// TestSwapUnloadUnfinished shows a swap whose unload never finishes fails naming the model and
// what the router last listed, and sends no load.
func TestSwapUnloadUnfinished(t *testing.T) {
	f := newFake([]string{"a"}, []string{"b"})
	f.stuck["a"] = true
	cfg := config(f.start(t), "b")
	cfg.UnloadTimeout = 50 * time.Millisecond
	_, err := swap.Run(t.Context(), cfg)
	if err == nil || !strings.Contains(err.Error(), "unloading a") || !strings.Contains(err.Error(), `listed "loaded"`) {
		t.Errorf("Run = %v, want an error naming a, still loaded", err)
	}
	if slices.Contains(f.commands, "load b") {
		t.Errorf("commands %q: b loaded before a's unload finished", f.commands)
	}
}

// TestSwapFinalCheck shows a swap fails when the router doesn't end with exactly the incoming
// set loaded, naming the model outside it.
func TestSwapFinalCheck(t *testing.T) {
	f := newFake(nil, []string{"a", "b", "stray"})
	f.sideEffect["b"] = "stray"
	_, err := swap.Run(t.Context(), config(f.start(t), "a", "b"))
	if want := `stray is "loaded", outside the set`; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Run = %v, want an error containing %q", err, want)
	}
}

// TestSwapNothingToDo shows a swap to the set already loaded sends no command.
func TestSwapNothingToDo(t *testing.T) {
	f := newFake([]string{"a", "b"}, []string{"c"})
	rep, err := swap.Run(t.Context(), config(f.start(t), "a", "b"))
	if err != nil || len(f.commands) != 0 || !slices.Equal(rep.Kept, []string{"a", "b"}) {
		t.Errorf("Run = %+v, %v, commands %q; want a and b kept, no command", rep, err, f.commands)
	}
}

// TestSwapRouterDown shows a swap fails, sending nothing, when the router answers with an error.
func TestSwapRouterDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	_, err := swap.Run(t.Context(), config(srv.URL, "a"))
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("Run = %v, want the router's 503", err)
	}
}

// TestSwapWaitsForLoading shows a swap waits for an incoming model the router is already
// loading, rather than ask for it again, and unloads an outgoing model that is still loading.
func TestSwapWaitsForLoading(t *testing.T) {
	f := newFake(nil, []string{"b"})
	for _, id := range []string{"a", "stale"} {
		f.order = append(f.order, id)
		f.status[id], f.pending[id], f.ends[id] = "loading", polls, "loaded"
	}
	rep, err := swap.Run(t.Context(), config(f.start(t), "a", "b"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"unload stale", "load b"}; !slices.Equal(f.commands, want) {
		t.Errorf("commands %q, want %q: a waited for, not asked to load again", f.commands, want)
	}
	if !slices.Equal(names(rep.Loads), []string{"a", "b"}) {
		t.Errorf("loads %q, want a then b, each a step", names(rep.Loads))
	}
}

// TestReload shows a reload unloads each of the set's models and loads it again, every unload
// before the first load, leaves a model outside the set alone, times each step, and refuses a
// set the router doesn't list wholly loaded before it sends any command.
func TestReload(t *testing.T) {
	f := newFake([]string{"a", "b", "other"}, []string{"d"})
	rep, err := swap.Reload(t.Context(), config(f.start(t), "a", "b"))
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if want := []string{"unload a", "unload b", "load a", "load b"}; !slices.Equal(f.commands, want) {
		t.Errorf("commands %q, want %q", f.commands, want)
	}
	if len(f.early) > 0 {
		t.Errorf("loads asked for while an unload was unfinished: %q", f.early)
	}
	if !slices.Equal(names(rep.Unloads), []string{"a", "b"}) || !slices.Equal(names(rep.Loads), []string{"a", "b"}) || len(rep.Kept) != 0 {
		t.Errorf("report kept %q, unloaded %q, loaded %q; want none, a b, a b", rep.Kept, names(rep.Unloads), names(rep.Loads))
	}
	for _, s := range slices.Concat(rep.Unloads, rep.Loads) {
		if s.Took <= 0 || s.Err != nil {
			t.Errorf("step %s: took %s, err %v; want a time and no error", s.Model, s.Took, s.Err)
		}
	}
	for id, want := range map[string]string{"a": "loaded", "b": "loaded", "other": "loaded", "d": "unloaded"} {
		if f.status[id] != want {
			t.Errorf("%s ends %q, want %q", id, f.status[id], want)
		}
	}

	g := newFake([]string{"a"}, []string{"d"})
	if _, err := swap.Reload(t.Context(), config(g.start(t), "a", "d")); err == nil || !strings.Contains(err.Error(), "lists d as") {
		t.Errorf("Reload of a set not wholly loaded = %v; want an error naming d", err)
	}
	if len(g.commands) != 0 {
		t.Errorf("commands %q, want none", g.commands)
	}
}
