package tier_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// TestGenerateDeterministic shows the same IDs and seed give the same list, in whatever order
// the dataset lists them, a different seed another, and the list round-trips its format.
func TestGenerateDeterministic(t *testing.T) {
	var ids []string
	for i := range 50 {
		ids = append(ids, fmt.Sprintf("q%02d", i))
	}
	a := tier.Generate("t", "d", ids, 10, 42)
	reversed := slices.Clone(ids)
	slices.Reverse(reversed)
	b := tier.Generate("t", "d", append(reversed, "q01"), 10, 42)
	c := tier.Generate("t", "d", ids, 10, 43)
	if !slices.Equal(a.IDs, b.IDs) || len(a.IDs) != 10 {
		t.Errorf("same seed: %q and %q; want equal lists of 10", a.IDs, b.IDs)
	}
	if slices.Equal(a.IDs, c.IDs) {
		t.Errorf("seeds 42 and 43 drew the same list %q", a.IDs)
	}
	// A fixed point: the generator's output for this input must never drift, or committed lists
	// stop matching their seed.
	if want := []string{"q48", "q02", "q13"}; !slices.Equal(a.IDs[:3], want) {
		t.Errorf("seed 42 starts %q; want %q", a.IDs[:3], want)
	}
	if all := tier.Generate("t", "d", ids, 0, 42); len(all.IDs) != 50 {
		t.Errorf("n = 0 drew %d; want all 50", len(all.IDs))
	}

	var buf bytes.Buffer
	if err := a.Write(&buf); err != nil {
		t.Fatal(err)
	}
	back, err := tier.ReadList(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if back.Tier != "t" || back.Dataset != "d" || back.Seed != 42 || !slices.Equal(back.IDs, a.IDs) {
		t.Errorf("round trip = %+v; want %+v", back, a)
	}
}

// TestReadListRefuses shows a list without its header, or with an ID twice, fails.
func TestReadListRefuses(t *testing.T) {
	for name, text := range map[string]string{
		"no seed":    "# tier t\n# dataset d\na\n",
		"twice":      "# tier t\n# dataset d\n# seed 1\na\na\n",
		"bad header": "# tier t\n# dataset d\n# seed 1\n# colour red\n",
	} {
		if _, err := tier.ReadList(strings.NewReader(text)); err == nil {
			t.Errorf("%s: read without error", name)
		}
	}
}

// TestLoadBars reads a floor and a ceiling, refuses a bar outside [0, 1], a name with both and
// an undefined key, and reads the committed bars file, which may be empty until the baseline
// fills it.
func TestLoadBars(t *testing.T) {
	dir := t.TempDir()
	write := func(text string) string {
		p := filepath.Join(dir, "bars.toml")
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	bars, err := tier.LoadBars(write("[bars]\ncode = 0.62\n\n[bars_at_most]\nasr-ami = 0.2\n"))
	if b, ok := bars.Bar("code"); err != nil || !ok || b != (tier.Bar{Value: 0.62}) {
		t.Errorf("LoadBars = %v, %v; want code at least 0.62", bars, err)
	}
	if b, ok := bars.Bar("asr-ami"); !ok || b != (tier.Bar{Value: 0.2, AtMost: true}) {
		t.Errorf("LoadBars = %v; want asr-ami at most 0.2", bars)
	}
	if _, ok := bars.Bar("missing"); ok {
		t.Error("a missing tier has a bar")
	}
	if _, err := tier.LoadBars(write("[bars]\ncode = 1.5\n")); err == nil {
		t.Error("a bar of 1.5 loaded")
	}
	if _, err := tier.LoadBars(write("[bars]\nx = 0.5\n[bars_at_most]\nx = 0.5\n")); err == nil {
		t.Error("a name under both tables loaded")
	}
	if _, err := tier.LoadBars(write("[bars_at_most]\nx = -0.1\n")); err == nil {
		t.Error("a ceiling of -0.1 loaded")
	}
	if _, err := tier.LoadBars(write("colour = \"red\"\n")); err == nil {
		t.Error("an undefined key loaded")
	}
	if _, err := tier.LoadBars("../bars.toml"); err != nil {
		t.Errorf("the committed bars: %v", err)
	}
}
