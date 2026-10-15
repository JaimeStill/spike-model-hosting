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

// bin is the profile tool, built once for the tests, which drive it as a user would.
var bin string

// root is the repository root, the working directory the tool runs in.
const root = "../.."

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "profile-tool")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "profile")
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

// TestEngines shows the tool lists each profile's engine, its upstream repository and its
// minimum build, one per line.
func TestEngines(t *testing.T) {
	out, errOut, code := run(t, nil, "engines")
	if code != 0 {
		t.Fatalf("engines: exit %d: %s", code, errOut)
	}
	if want := "strix-halo llama.cpp ggml-org/llama.cpp b11452\n"; out != want {
		t.Errorf("engines = %q; want %q", out, want)
	}
}

func TestValidate(t *testing.T) {
	out, errOut, code := run(t, nil, "validate", "strix-halo")
	if code != 0 || out != "profiles/strix-halo.toml: valid\n" {
		t.Errorf("validate strix-halo = %q, exit %d (%s); want valid, exit 0", out, code, errOut)
	}

	bad := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(bad, []byte(`host_class = "laptop"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut, code = run(t, nil, "validate", bad)
	if code != 1 || !strings.Contains(errOut, "host_class") {
		t.Errorf("validate invalid = exit %d, %q; want exit 1 naming host_class", code, errOut)
	}
}

// TestSet shows the tool describes a set for the scripts: its name, the model each role goes to,
// and each of its models with its kind and card, the startup set when none is named, and refuses
// a set the profile doesn't name.
func TestSet(t *testing.T) {
	want := `set a
chat ggml-org/gpt-oss-120b-GGUF:MXFP4
vision ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0
audio ggml-org/gemma-4-E4B-it-GGUF:Q8_0
embedding ggml-org/embeddinggemma-2-GGUF:Q8_0
model ggml-org/gpt-oss-120b-GGUF:MXFP4 generative https://huggingface.co/openai/gpt-oss-120b
model ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0 generative https://huggingface.co/google/gemma-4-26B-A4B-it
model ggml-org/gemma-4-E4B-it-GGUF:Q8_0 generative https://huggingface.co/google/gemma-4-E4B-it
model ggml-org/embeddinggemma-2-GGUF:Q8_0 embedding https://huggingface.co/google/embeddinggemma-2
`
	for name, tc := range map[string]struct {
		env  []string
		args []string
	}{
		"startup":     {nil, []string{"set", "strix-halo"}},
		"flag":        {nil, []string{"set", "-set", "a", "strix-halo"}},
		"environment": {[]string{"SPIKE_SET=a"}, []string{"set", "strix-halo"}},
	} {
		t.Run(name, func(t *testing.T) {
			out, errOut, code := run(t, tc.env, tc.args...)
			if code != 0 || out != want {
				t.Errorf("exit %d (%s), output:\n%s\nwant:\n%s", code, errOut, out, want)
			}
		})
	}
	_, errOut, code := run(t, nil, "set", "-set", "z", "strix-halo")
	if code != 2 || !strings.Contains(errOut, `no set "z"`) {
		t.Errorf("set -set z: exit %d, %q; want 2, naming the unknown set", code, errOut)
	}
}

// TestRenderPresets shows the tool writes the profile's presets.
func TestRenderPresets(t *testing.T) {
	out, errOut, code := run(t, nil, "render", "presets", "strix-halo")
	if code != 0 {
		t.Fatalf("render presets: exit %d: %s", code, errOut)
	}
	want, err := os.ReadFile(filepath.Join(root, "profile/testdata/strix-halo.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if out != string(want) {
		t.Errorf("render presets differs from profile/testdata/strix-halo.ini:\n%s", out)
	}
}

// TestRenderUnit shows the unit's host values come from flags, or else the environment, and
// that the unit doesn't render without them.
func TestRenderUnit(t *testing.T) {
	want, err := os.ReadFile(filepath.Join(root, "profile/testdata/strix-halo.service"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		env  []string
		args []string
	}{
		{"flags", nil, []string{"-user", "svc", "-hf-home", "/srv/hf", "-models-dir", "/srv/models"}},
		{"environment", []string{"SPIKE_SERVICE_USER=svc", "SPIKE_HF_HOME=/srv/hf", "SPIKE_MODELS_DIR=/srv/models"}, nil},
		{"flags over environment", []string{"SPIKE_SERVICE_USER=other", "SPIKE_HF_HOME=/srv/hf", "SPIKE_MODELS_DIR=/srv/models"}, []string{"-user", "svc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append(append([]string{"render", "unit"}, tt.args...), "strix-halo")
			out, errOut, code := run(t, tt.env, args...)
			if code != 0 {
				t.Fatalf("render unit: exit %d: %s", code, errOut)
			}
			if out != string(want) {
				t.Errorf("render unit differs from profile/testdata/strix-halo.service:\n%s", out)
			}
		})
	}

	out, errOut, code := run(t, nil, "render", "unit", "-user", "svc", "strix-halo")
	if code != 1 || out != "" || !strings.Contains(errOut, "host HF_HOME: required") {
		t.Errorf("render unit without HF_HOME = exit %d, %q, %q; want exit 1 naming HF_HOME, no output", code, out, errOut)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"pins"},
		{"render"},
		{"render", "compose", "strix-halo"},
		{"validate"},
		{"render", "unit", "-bogus", "strix-halo"},
	} {
		_, errOut, code := run(t, nil, args...)
		if code != 2 || !strings.Contains(errOut, "usage:") {
			t.Errorf("%q = exit %d, %q; want exit 2 with usage", args, code, errOut)
		}
	}
}
