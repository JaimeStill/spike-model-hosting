package suites_test

import (
	"regexp"
	"testing"

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
