package main_test

import (
	"bytes"
	"errors"
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
		"no step":      {nil, "no step named"},
		"unknown step": {[]string{"nope"}, `unknown step "nope"`},
		"flag first":   {[]string{"-router", "http://127.0.0.1:1", "prefix"}, `unknown step "-router"`},
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
