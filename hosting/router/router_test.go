package router_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// fake is a router that answers GET /models and GET /props, and 404s anything else.
func fake(t *testing.T, models, props string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/models" && models != "":
			_, _ = w.Write([]byte(models))
		case r.Method == http.MethodGet && r.URL.Path == "/props" && props != "":
			_, _ = w.Write([]byte(props))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

const models = `{"data":[
	{"id":"a","status":{"value":"loaded"}},
	{"id":"b","status":{"value":"unloaded"}}]}`

func TestStatuses(t *testing.T) {
	got, err := router.Statuses(t.Context(), http.DefaultClient, fake(t, models, ""))
	if want := map[string]string{"a": "loaded", "b": "unloaded"}; err != nil || !maps.Equal(got, want) {
		t.Errorf("Statuses = %v, %v; want %v", got, err, want)
	}
	if _, err := router.Statuses(t.Context(), http.DefaultClient, fake(t, "", "")); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a router that 404s /models: err %v, want the status", err)
	}
	if _, err := router.Statuses(t.Context(), http.DefaultClient, fake(t, "not json", "")); err == nil {
		t.Error("a /models that isn't JSON gave no error")
	}
}

// TestCheckLoaded shows the check passes only when every model named is listed as loaded, and
// otherwise names the first that isn't, with its status, a model not listed at all included.
func TestCheckLoaded(t *testing.T) {
	base := fake(t, models, "")
	if err := router.CheckLoaded(t.Context(), http.DefaultClient, base, "a"); err != nil {
		t.Errorf("a loaded: %v", err)
	}
	for ids, want := range map[string]string{"a,b": `b as "unloaded"`, "c": `c as ""`} {
		err := router.CheckLoaded(t.Context(), http.DefaultClient, base, strings.Split(ids, ",")...)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckLoaded(%s) = %v, want an error naming %s", ids, err, want)
		}
	}
}

func TestBuild(t *testing.T) {
	got, err := router.Build(t.Context(), http.DefaultClient, fake(t, "", `{"build_info":"b9999-abc"}`))
	if err != nil || got != "b9999-abc" {
		t.Errorf("Build = %q, %v; want b9999-abc", got, err)
	}
	if _, err := router.Build(t.Context(), http.DefaultClient, fake(t, "", "")); err == nil {
		t.Error("a router that 404s /props gave no error")
	}
}

// TestModelsFailed shows Models reads a failed load's flag and exit code beside the status.
func TestModelsFailed(t *testing.T) {
	base := fake(t, `{"data":[{"id":"a","status":{"value":"unloaded","failed":true,"exit_code":1}}]}`, "")
	got, err := router.Models(t.Context(), http.DefaultClient, base)
	want := []router.Model{{ID: "a", Status: router.StatusUnloaded, Failed: true, ExitCode: 1}}
	if err != nil || len(got) != 1 || got[0] != want[0] {
		t.Errorf("Models = %+v, %v; want %+v", got, err, want)
	}
}

// TestLoadUnload shows Load and Unload post the model to their endpoints, and fail on an answer
// that isn't success, carrying the router's message.
func TestLoadUnload(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || r.Method != http.MethodPost {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		got = append(got, r.URL.Path+" "+in.Model)
		switch in.Model {
		case "refused":
			http.Error(w, `{"error":{"message":"model not found"}}`, http.StatusBadRequest)
		case "quiet":
			_, _ = w.Write([]byte(`{"success":false}`))
		default:
			_, _ = w.Write([]byte(`{"success":true}`))
		}
	}))
	t.Cleanup(srv.Close)
	if err := router.Load(t.Context(), http.DefaultClient, srv.URL, "a"); err != nil {
		t.Errorf("Load(a): %v", err)
	}
	if err := router.Unload(t.Context(), http.DefaultClient, srv.URL, "b"); err != nil {
		t.Errorf("Unload(b): %v", err)
	}
	if want := []string{"/models/load a", "/models/unload b"}; !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
	if err := router.Load(t.Context(), http.DefaultClient, srv.URL, "refused"); err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Errorf("Load(refused) = %v, want the router's message", err)
	}
	if err := router.Unload(t.Context(), http.DefaultClient, srv.URL, "quiet"); err == nil {
		t.Error("Unload(quiet): an answer without success gave no error")
	}
}

// TestReloadPresets shows ReloadPresets asks GET /models?reload=1 and fails on the router's error.
func TestReloadPresets(t *testing.T) {
	var got []string
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.RequestURI())
		if fail {
			http.Error(w, "bad presets", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(models))
	}))
	t.Cleanup(srv.Close)
	if err := router.ReloadPresets(t.Context(), http.DefaultClient, srv.URL); err != nil {
		t.Fatalf("ReloadPresets: %v", err)
	}
	if want := []string{"GET /models?reload=1"}; !slices.Equal(got, want) {
		t.Errorf("requests %q, want %q", got, want)
	}
	fail = true
	if err := router.ReloadPresets(t.Context(), http.DefaultClient, srv.URL); err == nil || !strings.Contains(err.Error(), "bad presets") {
		t.Errorf("ReloadPresets on a failing router = %v, want the router's message", err)
	}
}

// TestServedAt shows ServedAt reads a model's total_slots and n_ctx from GET /props?model=, never
// loading it.
func TestServedAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" || r.URL.Query().Get("model") != "m:Q8_0" || r.URL.Query().Get("autoload") != "false" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"total_slots":8,"default_generation_settings":{"n_ctx":15648},"build_info":"b1"}`))
	}))
	t.Cleanup(srv.Close)
	got, err := router.ServedAt(t.Context(), http.DefaultClient, srv.URL, "m:Q8_0")
	if err != nil {
		t.Fatal(err)
	}
	if want := (router.Served{Slots: 8, NCtx: 15648}); got != want {
		t.Errorf("ServedAt = %+v, want %+v", got, want)
	}
}
