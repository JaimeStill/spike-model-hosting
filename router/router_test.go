package router_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/router"
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
