package main_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bin is the evaluate tool, built once for the tests, which drive it as a user would.
var bin string

// root is the repository root, the working directory the tool runs in.
const root = "../../.."

// stepNames are the steps evaluate runs, in the order its usage lists them.
var stepNames = []string{"conform", "concurrent", "prefix", "coldload"}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "evaluate-tool")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "evaluate")
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

// TestUsage shows evaluate refuses a call that names no step, or one it doesn't know, with exit
// status 2 and its usage, and lists every step on -h.
func TestUsage(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"no step":      {nil, "no flags or step named"},
		"unknown step": {[]string{"nope"}, `unknown step "nope"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, errOut, code := run(t, nil, tc.args...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
	out, _, code := run(t, nil, "-h")
	if code != 0 || !strings.HasPrefix(out, "usage:") {
		t.Errorf("-h: exit %d, stdout %q; want 0 and the usage", code, out)
	}
	for _, s := range stepNames {
		if !strings.Contains(out, "\n  "+s+" ") {
			t.Errorf("-h lacks the %s step:\n%s", s, out)
		}
	}
}

// TestStepUsage shows each step's -h prints the step's own usage, under evaluate's name, and a
// step's usage error names the step.
func TestStepUsage(t *testing.T) {
	for _, s := range stepNames {
		t.Run(s, func(t *testing.T) {
			out, _, code := run(t, nil, s, "-h")
			if code != 0 || !strings.HasPrefix(out, "usage:\n  evaluate "+s+" [-router url]") {
				t.Errorf("-h: exit %d, stdout %q; want 0 and the step's usage", code, out)
			}
			_, errOut, code := run(t, nil, s, "-nope")
			if code != 2 || !strings.HasPrefix(errOut, "evaluate "+s+": ") {
				t.Errorf("-nope: exit %d, stderr %q; want 2, naming the step", code, errOut)
			}
		})
	}
}

// TestGatedUsage shows a call that starts with a flag runs the gated stages, which refuse a call
// that names no model, both ways of naming one, an unknown role or stage, or a trailing step.
func TestGatedUsage(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"no model":      {[]string{"-router", "http://127.0.0.1:1"}, "-role or -model"},
		"both":          {[]string{"-router", "http://127.0.0.1:1", "-role", "agent", "-model", "x"}, "-role or -model"},
		"unknown role":  {[]string{"-router", "http://127.0.0.1:1", "-role", "reasoning"}, `role "reasoning"`},
		"unknown stage": {[]string{"-router", "http://127.0.0.1:1", "-role", "agent", "-from", "nope"}, `-from "nope"`},
		"trailing step": {[]string{"-router", "http://127.0.0.1:1", "prefix"}, "unexpected arguments"},
		"no router":     {[]string{"-role", "agent"}, "no router"},
		"set run from":  {[]string{"-router", "http://127.0.0.1:1", "-set", "a", "-from", "capacity"}, "-from may name only set"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, errOut, code := run(t, nil, tc.args...)
			if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage:") {
				t.Errorf("exit %d, stderr %q; want 2, naming %q, with the usage", code, errOut, tc.want)
			}
		})
	}
	out, _, code := run(t, nil, "-role", "agent", "-h")
	if code != 0 || !strings.HasPrefix(out, "usage:\n  evaluate [-router url]") {
		t.Errorf("-h: exit %d, stdout %q; want 0 and the gated run's usage", code, out)
	}
}

// TestGatedRefusesUnloadedModel shows the gated run refuses a model the router lists unloaded,
// before any stage, writing no report.
func TestGatedRefusesUnloadedModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("a request past GET /models: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"ggml-org/gpt-oss-120b-GGUF:MXFP4","status":{"value":"unloaded"}}]}`)
	}))
	defer srv.Close()
	state := t.TempDir()
	// The quality and deep stages' tools are named, so finding them asks no mise, which the bare
	// environment lacks; the run refuses before it would start one.
	tools := []string{"-uv", "/nonexistent/uv", "-python", "/nonexistent/python",
		"-pi", "/nonexistent/pi", "-claude", "/nonexistent/claude", "-opencode", "/nonexistent/opencode", "-guidellm", "/nonexistent/guidellm"}
	_, errOut, code := run(t, []string{"SPIKE_ROUTER_URL=" + srv.URL}, append([]string{"-role", "agent", "-state", state}, tools...)...)
	if code != 1 || !strings.Contains(errOut, "ggml-org/gpt-oss-120b-GGUF:MXFP4 as unloaded") {
		t.Errorf("exit %d, stderr %q; want 1, naming the model unloaded", code, errOut)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Errorf("a refused run wrote %d entries under the state directory", len(entries))
	}
}

// TestGatedSetRunRefusesUnloaded shows -set with no model is a set run, which refuses a set
// whose models the router doesn't all list loaded, before any stage, writing no report.
func TestGatedSetRunRefusesUnloaded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("a request past GET /models: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"ggml-org/gpt-oss-120b-GGUF:MXFP4","status":{"value":"loaded"}}]}`)
	}))
	defer srv.Close()
	state := t.TempDir()
	tools := []string{"-uv", "/nonexistent/uv", "-python", "/nonexistent/python"}
	_, errOut, code := run(t, []string{"SPIKE_ROUTER_URL=" + srv.URL}, append([]string{"-set", "a", "-from", "set", "-state", state}, tools...)...)
	if code != 1 || !strings.Contains(errOut, "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0 as unlisted") {
		t.Errorf("exit %d, stderr %q; want 1, naming set A's first model not loaded", code, errOut)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Errorf("a refused run wrote %d entries under the state directory", len(entries))
	}
}
