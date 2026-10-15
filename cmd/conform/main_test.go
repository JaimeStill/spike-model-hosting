package main_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// bin is the conform tool, built once for the tests, which drive it as a user would.
var bin string

// root is the repository root, the working directory the tool runs in.
const root = "../.."

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "conform-tool")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "conform")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir) // a temporary directory left behind fails no test
	os.Exit(code)
}

// run runs the tool from the repository root with args and env (added to a bare environment),
// and returns its standard output, standard error and exit status.
func run(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = root
	// A nil Env would inherit this process's environment, SPIKE_ROUTER_URL and the consumer
	// variables with it; a non-nil slice holds the tool to exactly env.
	cmd.Env = append([]string{}, env...)
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
			_, errOut, code := run(t, tc.env, tc.args...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
	out, _, code := run(t, nil, "-h")
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

// TestRefusesUnloadedModel shows the tool sends nothing to a router on which a model the suite
// needs isn't loaded: it never loads one.
func TestRefusesUnloadedModel(t *testing.T) {
	var others atomic.Int32
	srv := router(t, "unloaded", &others)
	_, errOut, code := run(t, []string{"SPIKE_ROUTER_URL=" + srv.URL})
	if code != 1 || !strings.Contains(errOut, `gpt-oss-120b-GGUF:MXFP4 as "unloaded"`) {
		t.Errorf("exit %d, stderr %q; want 1, naming the unloaded model", code, errOut)
	}
	if n := others.Load(); n != 0 {
		t.Errorf("the router got %d other requests, want 0", n)
	}
}

// TestRefusesUnknownSet shows the tool refuses a set the profile doesn't name, before it reads
// the router.
func TestRefusesUnknownSet(t *testing.T) {
	var others atomic.Int32
	srv := router(t, "loaded", &others)
	_, errOut, code := run(t, []string{"SPIKE_ROUTER_URL=" + srv.URL, "SPIKE_SET=z"})
	if code != 2 || !strings.Contains(errOut, `no set "z"`) {
		t.Errorf("exit %d, stderr %q; want 2, naming the unknown set", code, errOut)
	}
}

// TestReportsSkippedConsumers shows a consumer whose cell can't run is reported, not dropped:
// every trial skipped, every cell 0 of k, pass^k 0 and the bar failed, with the reason, and the
// build named.
func TestReportsSkippedConsumers(t *testing.T) {
	var others atomic.Int32
	srv := router(t, "loaded", &others)
	state := t.TempDir()
	out, errOut, code := run(t, []string{"SPIKE_ROUTER_URL=" + srv.URL}, "-k", "2", "-state", state,
		"-pi", "/nonexistent/pi", "-claude", "/nonexistent/claude", "-opencode", "/nonexistent/opencode")
	if code != 1 {
		t.Errorf("exit %d, want 1: no consumer meets the bar\nstderr: %s", code, errOut)
	}
	for _, want := range []string{
		"build b0-test, set a,",
		"| pi | - | 0/2 skip | 0/2 skip |",
		"| opencode | - |",
		"pi: pass^2 0/9 (0.0%), 0 invalid: FAILS the bar",
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
