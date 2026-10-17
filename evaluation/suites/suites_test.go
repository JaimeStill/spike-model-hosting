package suites_test

import (
	"os/exec"
	"regexp"
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

// TestClaudeCodeProvider runs the claude-code Inspect provider's own test, test_claude_code.py,
// against its fake claude executable, through the consumers mise environment's uv and Python,
// offline: the project's locked environment, no network and no login.
func TestClaudeCodeProvider(t *testing.T) {
	uv, err := consumers.Executable(t.Context(), "", "uv")
	if err != nil {
		t.Skipf("the consumers environment's pinned uv isn't installed: %v", err)
	}
	python, err := consumers.Executable(t.Context(), "", "python")
	if err != nil {
		t.Skipf("the consumers environment's pinned Python isn't installed: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), uv, "run", "--project", ".", "--locked", "--offline", "--no-python-downloads",
		"--python", python, "python", "-W", "ignore", "-m", "unittest", "test_claude_code")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("test_claude_code: %v\n%s", err, out)
	}
}
