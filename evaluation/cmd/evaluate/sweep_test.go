package main_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestSweepUsage shows the sweep step refuses a call it can't run with exit status 2 and its
// usage: no router, no model or both ways of naming one, a model with -restore, an unknown role,
// or a model the set assigns no role.
func TestSweepUsage(t *testing.T) {
	router := []string{"-router", "http://127.0.0.1:1"}
	cases := map[string]struct {
		args []string
		want string
	}{
		"no router":     {[]string{"-role", "audio"}, "no router"},
		"no model":      {router, "-role or -model"},
		"both":          {append([]string{"-role", "audio", "-model", "x"}, router...), "-role or -model"},
		"restore model": {append([]string{"-restore", "-role", "audio"}, router...), "-restore restores"},
		"unknown role":  {append([]string{"-role", "reasoning"}, router...), `role "reasoning"`},
		"not in set":    {append([]string{"-set", "a", "-model", "example/none-GGUF:Q4_0"}, router...), "assigns example/none-GGUF:Q4_0 no role"},
		"argument":      {append([]string{"-role", "audio", "extra"}, router...), "unexpected arguments"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, errOut, code := run(t, nil, append([]string{"sweep"}, tc.args...)...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
}

// TestSweepRefusesUnloaded shows the sweep refuses a set the router doesn't list loaded before it
// reads or writes the presets file, writing no report: its ssh, a missing executable, is never
// run.
func TestSweepRefusesUnloaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("a request past GET /models: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"ggml-org/gemma-4-E4B-it-GGUF:Q8_0","status":{"value":"loaded"}}]}`)
	}))
	defer srv.Close()
	state := t.TempDir()
	_, errOut, code := run(t, []string{"SPIKE_ROUTER_URL=" + srv.URL, "PATH=/nonexistent"},
		"sweep", "-role", "audio", "-state", state, "-uv", "/nonexistent/uv", "-python", "/nonexistent/python")
	if code != 1 || !strings.Contains(errOut, "as unlisted") {
		t.Errorf("exit %d, stderr %q; want 1, naming a model of the set not loaded", code, errOut)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Errorf("a refused sweep wrote %d entries under the state directory", len(entries))
	}
}
