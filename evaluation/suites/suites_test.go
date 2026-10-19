package suites_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/consumers"
	"github.com/JaimeStill/spike-model-hosting/evaluation/suites"
)

// TestPin reads the exact pins the quality stage checks results against, and refuses a package
// the project doesn't pin.
func TestPin(t *testing.T) {
	version := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	for _, name := range []string{"mteb", "inspect-ai"} {
		if v, err := suites.Pin(name); err != nil || !version.MatchString(v) {
			t.Errorf("Pin(%q) = %q, %v; want an exact version", name, v, err)
		}
	}
	if v, err := suites.Pin("torch"); err == nil {
		t.Errorf("Pin(torch) = %q; want an error, the project pins no torch", v)
	}
}

// TestPython runs the project's Python tests, test_*.py: the claude-code Inspect provider against
// its fake claude executable, and the tiers' early stops. They run through the consumers mise
// environment's uv and Python, offline: the project's locked environment, no network and no
// login. A machine without that environment fails rather than skips, so a green check always
// ran them; install names the commands that set it up, run once from the repository root.
func TestPython(t *testing.T) {
	const install = "install the consumers environment (MISE_ENV=consumers mise -C evaluation install) " +
		"and sync the suites project from its lock (MISE_ENV=consumers mise -C evaluation exec -- uv sync --project suites --locked)"
	uv, err := consumers.Executable(t.Context(), "", "uv")
	if err != nil {
		t.Fatalf("the consumers environment's pinned uv isn't installed: %v; %s", err, install)
	}
	python, err := consumers.Executable(t.Context(), "", "python")
	if err != nil {
		t.Fatalf("the consumers environment's pinned Python isn't installed: %v; %s", err, install)
	}
	cmd := exec.CommandContext(t.Context(), uv, "run", "--project", ".", "--locked", "--offline", "--no-python-downloads",
		"--python", python, "python", "-W", "ignore", "-m", "unittest", "discover", "-p", "test_*.py")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("python tests: %v\n%s\nif uv can't resolve offline, %s", err, out, install)
	}
}

// TestCachesOff shows the suites' own result caches stay off, so a unit evaluate runs is a real
// rerun and evaluate's results index (package gate) is the only reuse: no script reuses Inspect
// logs (eval_set, eval_retry) or caches model output (a generate cache, CachePolicy), and every
// MTEB evaluation runs without its result cache (cache=None).
func TestCachesOff(t *testing.T) {
	scripts, err := filepath.Glob("*.py")
	if err != nil || len(scripts) == 0 {
		t.Fatalf("the suites' scripts: %q, %v", scripts, err)
	}
	reuse := regexp.MustCompile(`\beval_set\b|\beval_retry\b|\bCachePolicy\b|\bcache\s*=\s*True\b`)
	mtebCalls := 0
	for _, path := range scripts {
		if strings.HasPrefix(path, "test_") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if m := reuse.FindString(src); m != "" {
			t.Errorf("%s uses %q: a cache or log reuse of the suite's own", path, m)
		}
		for _, call := range regexp.MustCompile(`mteb\.evaluate\([^)]*\)`).FindAllString(src, -1) {
			mtebCalls++
			if !regexp.MustCompile(`\bcache\s*=\s*None\b`).MatchString(call) {
				t.Errorf("%s: %s runs with MTEB's result cache; want cache=None", path, call)
			}
		}
	}
	if mtebCalls == 0 {
		t.Error("no script calls mteb.evaluate; the check of its cache checks nothing")
	}
}
