package main_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// runConform runs evaluate's conform step from the repository root with args and env, as run does.
func runConform(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return run(t, env, append([]string{"conform"}, args...)...)
}

// TestConformUsage shows the step refuses a call it can't run with exit status 2 and its usage, and
// prints its usage on -h.
func TestConformUsage(t *testing.T) {
	const router = "SPIKE_ROUTER_URL=http://127.0.0.1:1"
	cases := map[string]struct {
		env  []string
		args []string
		want string
	}{
		"no router":        {nil, nil, "no router"},
		"zero trials":      {[]string{router}, []string{"-k", "0"}, "-k"},
		"unknown consumer": {[]string{router}, []string{"-consumers", "pi,codex"}, `"codex"`},
		"no consumer":      {[]string{router}, []string{"-consumers", ","}, "names none"},
		"argument":         {[]string{router}, []string{"extra"}, "unexpected arguments"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, errOut, code := runConform(t, tc.env, tc.args...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
	out, _, code := runConform(t, nil, "-h")
	if code != 0 || !strings.HasPrefix(out, "usage:") {
		t.Errorf("-h: exit %d, stdout %q; want 0 and the usage", code, out)
	}
}

// router is a fake router that answers GET /props and GET /models, listing models with the
// status given, and counts every other request.
func router(t *testing.T, status string, others *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/props":
			_, _ = w.Write([]byte(`{"build_info":"b0-test"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"ggml-org/gpt-oss-120b-GGUF:MXFP4","status":{"value":"` + status + `"}},
				{"id":"ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0","status":{"value":"loaded"}},
				{"id":"ggml-org/gemma-4-E4B-it-GGUF:Q8_0","status":{"value":"loaded"}},
				{"id":"ggml-org/embeddinggemma-2-GGUF:Q8_0","status":{"value":"loaded"}}]}`))
		default:
			others.Add(1)
			http.Error(w, "unexpected", http.StatusTeapot)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestConformRefusesUnloadedModel shows the step sends nothing to a router on which a model the
// suite needs isn't loaded: it never loads one.
func TestConformRefusesUnloadedModel(t *testing.T) {
	var others atomic.Int32
	srv := router(t, "unloaded", &others)
	_, errOut, code := runConform(t, []string{"SPIKE_ROUTER_URL=" + srv.URL})
	if code != 1 || !strings.Contains(errOut, `gpt-oss-120b-GGUF:MXFP4 as "unloaded"`) {
		t.Errorf("exit %d, stderr %q; want 1, naming the unloaded model", code, errOut)
	}
	if n := others.Load(); n != 0 {
		t.Errorf("the router got %d other requests, want 0", n)
	}
}

// TestConformRefusesUnknownSet shows the step refuses a set the profile doesn't name, before it
// reads the router.
func TestConformRefusesUnknownSet(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected", http.StatusTeapot)
	}))
	defer srv.Close()
	_, errOut, code := runConform(t, []string{"SPIKE_ROUTER_URL=" + srv.URL, "SPIKE_SET=z"})
	if code != 2 || !strings.Contains(errOut, `no set "z"`) {
		t.Errorf("exit %d, stderr %q; want 2, naming the unknown set", code, errOut)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("the router got %d requests, want 0", n)
	}
}

// TestConformReportsSkippedConsumers shows a consumer whose cell can't run is reported, not
// dropped: every trial skipped, every cell 0 of k, not fitting at its first trial, with the reason,
// and the build named.
func TestConformReportsSkippedConsumers(t *testing.T) {
	var others atomic.Int32
	srv := router(t, "loaded", &others)
	state := t.TempDir()
	out, errOut, code := runConform(t, []string{"SPIKE_ROUTER_URL=" + srv.URL}, "-k", "2", "-state", state,
		"-pi", "/nonexistent/pi", "-claude", "/nonexistent/claude", "-opencode", "/nonexistent/opencode")
	if code != 1 {
		t.Errorf("exit %d, want 1: no consumer meets the bar\nstderr: %s", code, errOut)
	}
	for _, want := range []string{
		"build b0-test, set a,",
		"| pi | - | 0/2 skip | 0/2 skip |",
		"| opencode | - |",
		"pi: does not fit: failed trial 1 of 2: skipped: need the pi executable",
		"claude skipped a trial: need the claude executable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	if n := others.Load(); n != 0 {
		t.Errorf("the router got %d other requests, want 0", n)
	}
}
