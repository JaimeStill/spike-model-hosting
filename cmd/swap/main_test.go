package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// bin is the swap tool, built once for the tests, which drive it as a user would.
var bin string

// root is the repository root, the working directory the tool runs in.
const root = "../.."

// The Strix Halo profile's models, by router ID.
const (
	gptOSS    = "ggml-org/gpt-oss-120b-GGUF:MXFP4"
	gemma26B  = "ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0"
	gemmaE4B  = "ggml-org/gemma-4-E4B-it-GGUF:Q8_0"
	embedding = "ggml-org/embeddinggemma-2-GGUF:Q8_0"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "swap-tool")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "swap")
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
	// A nil Env would inherit this process's environment, SPIKE_ROUTER_URL with it; a non-nil
	// slice holds the tool to exactly env.
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
		"no set":        {[]string{router}, nil, "name one set"},
		"two sets":      {[]string{router}, []string{"a", "b"}, "name one set"},
		"no router":     {nil, []string{"a"}, "no router"},
		"zero interval": {[]string{router}, []string{"-interval", "0s", "a"}, "-interval"},
		"zero timeout":  {[]string{router}, []string{"-load-timeout", "0s", "a"}, "-load-timeout"},
		"unknown flag":  {[]string{router}, []string{"-nope", "a"}, "-nope"},
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

// fakeRouter is a router whose loads and unloads take effect at the next poll of GET /models.
type fakeRouter struct {
	mu       sync.Mutex
	order    []string
	status   map[string]string
	next     map[string]string
	requests int
}

func newFake(loaded ...string) *fakeRouter {
	f := &fakeRouter{order: []string{gptOSS, gemma26B, gemmaE4B, embedding}, status: map[string]string{}, next: map[string]string{}}
	for _, id := range f.order {
		f.status[id] = "unloaded"
	}
	for _, id := range loaded {
		f.status[id] = "loaded"
	}
	return f
}

func (f *fakeRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	switch r.URL.Path {
	case "/props":
		_, _ = w.Write([]byte(`{"build_info":"b2-test"}`))
	case "/models":
		maps.Copy(f.status, f.next)
		clear(f.next)
		var data []map[string]any
		for _, id := range f.order {
			data = append(data, map[string]any{"id": id, "status": map[string]string{"value": f.status[id]}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	case "/models/load", "/models/unload":
		var in struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.next[in.Model] = map[string]string{"/models/load": "loaded", "/models/unload": "unloaded"}[r.URL.Path]
		_, _ = w.Write([]byte(`{"success":true}`))
	default:
		http.NotFound(w, r)
	}
}

// twoSets writes the Strix Halo profile with a second set, b, of Gemma 4 26B-A4B, taking chat
// and images both as a candidate main model does, and the embedding model, and returns its path.
func twoSets(t *testing.T) string {
	t.Helper()
	base, err := os.ReadFile(filepath.Join(root, "profiles", "strix-halo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(base) + `
[[sets]]
name = "b"
models = ["` + gemma26B + `", "` + embedding + `"]

[sets.roles]
chat = "` + gemma26B + `"
vision = "` + gemma26B + `"
embedding = "` + embedding + `"

[sets.memory_budget]
pool_mib = 98304
measured_mib = 30000
build = "b11565"
`
	path := filepath.Join(t.TempDir(), "two-sets.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSwap shows the tool swaps from set b to set a: it names the build and the set it swapped
// from, keeps the shared models, loads the others, each with its time, and the total.
func TestSwap(t *testing.T) {
	f := newFake(gemma26B, embedding)
	srv := httptest.NewServer(f)
	defer srv.Close()
	out, errOut, code := run(t, nil, "-router", srv.URL, "-profile", twoSets(t), "-interval", "1ms", "a")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q\n%s", code, errOut, out)
	}
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`build b2-test\nswap to set a from set b\n`),
		regexp.MustCompile(`keep +` + regexp.QuoteMeta(gemma26B) + ` +- +shared`),
		regexp.MustCompile(`load +` + regexp.QuoteMeta(gptOSS) + ` +\d+\.\ds +ok`),
		regexp.MustCompile(`load +` + regexp.QuoteMeta(gemmaE4B) + ` +\d+\.\ds +ok`),
		regexp.MustCompile(`total +\d+\.\ds`),
		regexp.MustCompile(`swapped to set a in \d+\.\ds, build b2-test`),
	} {
		if !want.MatchString(out) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
	for id, s := range f.status {
		if s != "loaded" {
			t.Errorf("%s ends %q, want loaded", id, s)
		}
	}
}

// TestRefusesUnknownSet shows the tool refuses a set the profile doesn't name, naming the sets
// it does, before it sends the router anything.
func TestRefusesUnknownSet(t *testing.T) {
	f := newFake(gptOSS)
	srv := httptest.NewServer(f)
	defer srv.Close()
	_, errOut, code := run(t, nil, "-router", srv.URL, "-profile", twoSets(t), "c")
	if code != 2 || !strings.Contains(errOut, `no set "c"`) || !strings.Contains(errOut, `["a" "b"]`) {
		t.Errorf("exit %d, stderr %q; want 2, naming c and the sets a and b", code, errOut)
	}
	if f.requests != 0 {
		t.Errorf("the router got %d requests, want none", f.requests)
	}
}
