package main_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// bin is the coldload tool, built once for the tests, which drive it as a user would.
var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "coldload-tool")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "coldload")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir) // a temporary directory left behind fails no test
	os.Exit(code)
}

// run runs the tool with args and env (added to a bare environment), and returns its standard
// output, standard error and exit status.
func run(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("run %v: %v", args, err)
	}
	return out.String(), errOut.String(), code
}

// TestUsage shows the tool refuses a call it can't run with exit status 2 and its usage, and
// prints its usage on -h.
func TestUsage(t *testing.T) {
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
			_, errOut, code := run(t, tc.env, tc.args...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
	out, _, code := run(t, nil, "-h")
	if code != 0 || !strings.Contains(out, "usage:") {
		t.Errorf("-h: exit %d, stdout %q; want 0 and the usage", code, out)
	}
}

// scripted is a router whose /models answers follow a script, one step a poll, the last
// repeated; a step of "" answers 503, as a router that is down.
type scripted struct {
	mu    sync.Mutex
	steps []string
	polls int
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	// A step names each model's status, as "a=loaded,b=loading".
	var data []string
	for pair := range strings.SplitSeq(step, ",") {
		id, status, _ := strings.Cut(pair, "=")
		data = append(data, fmt.Sprintf(`{"id":%q,"status":{"value":%q}}`, id, status))
	}
	_, _ = fmt.Fprintf(w, `{"data":[%s]}`, strings.Join(data, ","))
}

// TestRestart shows the tool waits through a restart and prints the router's return and each
// model's load, in the order they loaded.
func TestRestart(t *testing.T) {
	srv := httptest.NewServer(&scripted{steps: []string{
		"a=loaded,b=loaded", "a=loaded,b=loaded", "a=loaded,b=unloading", "", "",
		"a=loading,b=loading", "a=loading,b=loaded", "a=loaded,b=loaded",
	}})
	defer srv.Close()
	out, errOut, code := run(t, nil, "-router", srv.URL, "-interval", "5ms")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"2 models loaded; restart the router now", "restart seen", "router up", "all loaded"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if a, b := strings.Index(out, "\na "), strings.Index(out, "\nb "); a < 0 || b < 0 || b > a {
		t.Errorf("want b listed before a, as it loaded first:\n%s", out)
	}
}

// TestTimeout shows the tool gives up after -timeout, saying how far the restart got.
func TestTimeout(t *testing.T) {
	srv := httptest.NewServer(&scripted{steps: []string{"a=loaded", ""}})
	defer srv.Close()
	_, errOut, code := run(t, nil, "-router", srv.URL, "-interval", "5ms", "-timeout", "200ms")
	if code != 1 || !strings.Contains(errOut, "never came back") {
		t.Errorf("exit %d, stderr %q; want 1, saying the router never came back", code, errOut)
	}
}
