package main_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// runColdload runs evaluate's coldload step from the repository root with args and env, as run
// does.
func runColdload(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return run(t, env, append([]string{"coldload"}, args...)...)
}

// alias names set A's models, and one outside it, by a letter, which the scripted router's steps
// use.
var alias = map[string]string{
	"g": "ggml-org/gpt-oss-120b-GGUF:MXFP4",
	"v": "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0",
	"s": "ggml-org/gemma-4-E4B-it-GGUF:Q8_0",
	"e": "ggml-org/embeddinggemma-2-GGUF:Q8_0",
	"x": "example/outside-GGUF:Q4_0",
}

// setA is a step listing set A's models with status, and the model outside it unloaded.
func setA(status string) string {
	return "g=" + status + ",v=" + status + ",s=" + status + ",e=" + status + ",x=unloaded"
}

// TestColdloadUsage shows the step refuses a call it can't run with exit status 2 and its usage,
// and prints its usage on -h.
func TestColdloadUsage(t *testing.T) {
	cases := map[string]struct {
		env  []string
		args []string
		want string
	}{
		"no router":     {nil, nil, "no router"},
		"zero interval": {[]string{"SPIKE_ROUTER_URL=http://127.0.0.1:1"}, []string{"-interval", "0s"}, "-interval"},
		"zero timeout":  {nil, []string{"-router", "http://127.0.0.1:1", "-timeout", "0s"}, "-timeout"},
		"unknown flag":  {nil, []string{"-router", "http://127.0.0.1:1", "-nope"}, "-nope"},
		"argument":      {nil, []string{"-router", "http://127.0.0.1:1", "extra"}, "unexpected arguments"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, errOut, code := runColdload(t, tc.env, tc.args...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
	out, _, code := runColdload(t, nil, "-h")
	if code != 0 || !strings.Contains(out, "usage:") {
		t.Errorf("-h: exit %d, stdout %q; want 0 and the usage", code, out)
	}
}

// scripted is a router whose /models answers follow a script, one step a poll, the last
// repeated; a step of "" answers 503, as a router that is down. /props answers its build.
type scripted struct {
	mu    sync.Mutex
	steps []string
	polls int
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/props" {
		_, _ = fmt.Fprint(w, `{"build_info":"b3-test"}`)
		return
	}
	if r.URL.Path != "/models" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	step := s.steps[min(s.polls, len(s.steps)-1)]
	s.polls++
	s.mu.Unlock()
	if step == "" {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	// A step names each model's status by its alias, as "g=loaded,v=loading".
	var data []string
	for pair := range strings.SplitSeq(step, ",") {
		id, status, _ := strings.Cut(pair, "=")
		data = append(data, fmt.Sprintf(`{"id":%q,"status":{"value":%q}}`, alias[id], status))
	}
	_, _ = fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(data, ","))
}

// TestColdloadRestart shows the step waits through a restart for set A's models, ignoring a model
// outside the set, and prints the router's return and each model's load, in the order they
// loaded, with the set and the build.
func TestColdloadRestart(t *testing.T) {
	srv := httptest.NewServer(&scripted{steps: []string{
		// The step's check that the set is loaded, then the first poll of the watch.
		setA("loaded"), setA("loaded"), setA("loaded"),
		"g=loaded,v=unloading,s=loaded,e=loaded,x=unloaded", "", "",
		setA("loading"), "g=loading,v=loaded,s=loaded,e=loaded,x=unloaded", setA("loaded"),
	}})
	defer srv.Close()
	out, errOut, code := runColdload(t, nil, "-router", srv.URL, "-interval", "5ms")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		"build b3-test, set a\n", "4 models loaded; restart the router now", "restart seen", "router up", "all loaded",
		"build after the restart b3-test, set a",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if g, v := strings.Index(out, "\n"+alias["g"]), strings.Index(out, "\n"+alias["v"]); g < 0 || v < 0 || v > g {
		t.Errorf("want the 26B listed before gpt-oss, as it loaded first:\n%s", out)
	}
	if strings.Contains(out, alias["x"]) {
		t.Errorf("the model outside the set is listed:\n%s", out)
	}
}

// TestColdloadRefuses shows the step refuses to start when one of the set's models isn't loaded, or
// the set is one the profile doesn't name.
func TestColdloadRefuses(t *testing.T) {
	srv := httptest.NewServer(&scripted{steps: []string{"g=loaded,v=loading,s=loaded,e=loaded"}})
	defer srv.Close()
	_, errOut, code := runColdload(t, nil, "-router", srv.URL, "-interval", "5ms")
	if code != 1 || !strings.Contains(errOut, alias["v"]+` as "loading"`) {
		t.Errorf("exit %d, stderr %q; want 1, naming the model loading", code, errOut)
	}
	_, errOut, code = runColdload(t, []string{"SPIKE_SET=z"}, "-router", srv.URL)
	if code != 2 || !strings.Contains(errOut, `no set "z"`) {
		t.Errorf("exit %d, stderr %q; want 2, naming the unknown set", code, errOut)
	}
}

// TestColdloadTimeout shows the step gives up after -timeout, saying how far the restart got.
func TestColdloadTimeout(t *testing.T) {
	srv := httptest.NewServer(&scripted{steps: []string{setA("loaded"), setA("loaded"), setA("loaded"), ""}})
	defer srv.Close()
	_, errOut, code := runColdload(t, nil, "-router", srv.URL, "-interval", "5ms", "-timeout", "200ms")
	if code != 1 || !strings.Contains(errOut, "never came back") {
		t.Errorf("exit %d, stderr %q; want 1, saying the router never came back", code, errOut)
	}
}
