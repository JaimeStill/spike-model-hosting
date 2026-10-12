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

// bin is the concurrent tool, built once for the tests, which drive it as a user would.
var bin string

// root is the repository root, the working directory the tool runs in.
const root = "../.."

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "concurrent-tool")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "concurrent")
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
		"no router":    {nil, nil, "no router"},
		"zero rounds":  {[]string{"SPIKE_ROUTER_URL=http://127.0.0.1:1"}, []string{"-rounds", "0"}, "-rounds"},
		"unknown flag": {nil, []string{"-router", "http://127.0.0.1:1", "-nope"}, "-nope"},
		"argument":     {nil, []string{"-router", "http://127.0.0.1:1", "extra"}, "unexpected arguments"},
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

// TestRefusesUnloadedModel shows the tool sends nothing to a router on which a model the run
// needs isn't loaded: it never loads one.
func TestRefusesUnloadedModel(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			posts.Add(1)
			http.Error(w, "unexpected", http.StatusTeapot)
			return
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"ggml-org/gpt-oss-120b-GGUF:MXFP4","status":{"value":"unloaded"}},
			{"id":"ggml-org/embeddinggemma-2-GGUF:Q8_0","status":{"value":"loaded"}}]}`))
	}))
	defer srv.Close()
	_, errOut, code := run(t, []string{"SPIKE_ROUTER_URL=" + srv.URL})
	if code != 1 || !strings.Contains(errOut, `gpt-oss-120b-GGUF:MXFP4 as "unloaded"`) {
		t.Errorf("exit %d, stderr %q; want 1, naming the unloaded model", code, errOut)
	}
	if n := posts.Load(); n != 0 {
		t.Errorf("the router got %d requests beyond GET, want 0", n)
	}
}
