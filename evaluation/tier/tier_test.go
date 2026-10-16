package tier_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// TestUpperBound checks the Wilson bound at known points, computed independently.
func TestUpperBound(t *testing.T) {
	for _, tc := range []struct {
		passes, n int
		want      float64
	}{
		{0, 4, 0.4034786},
		{5, 10, 0.7307282},
		{8, 10, 0.9314420},
		{0, 1, 0.7301341},
		{0, 0, 1},
	} {
		if got := tier.UpperBound(tc.passes, tc.n); math.Abs(got-tc.want) > 1e-6 {
			t.Errorf("UpperBound(%d, %d) = %.7f, want %.7f", tc.passes, tc.n, got, tc.want)
		}
	}
}

// list returns a list of n IDs, s0 to s<n-1>.
func list(n int) tier.List {
	l := tier.List{Tier: "t", Dataset: "d", Seed: 1}
	for i := range n {
		l.IDs = append(l.IDs, fmt.Sprintf("s%d", i))
	}
	return l
}

// alternate passes the samples at even positions and fails the rest, counting the samples sent.
func alternate(sent *int) tier.Judge {
	return func(_ context.Context, id string) (bool, error) {
		*sent++
		var i int
		_, _ = fmt.Sscanf(id, "s%d", &i)
		return i%2 == 0, nil
	}
}

// TestRunStopsAtKnownPoint runs alternating passes and fails, a 50% rate, against bars the
// rate can't reach: the bound falls below 0.8 at the 6th sample and below 0.7 at the 16th.
func TestRunStopsAtKnownPoint(t *testing.T) {
	for _, tc := range []struct {
		bar      float64
		stopAt   int
		boundFmt string
	}{
		{0.8, 6, "0.779"},
		{0.7, 16, "0.690"},
	} {
		t.Run(fmt.Sprint(tc.bar), func(t *testing.T) {
			sent := 0
			res, err := tier.Run(t.Context(), list(20), &tc.bar, alternate(&sent))
			if err != nil {
				t.Fatal(err)
			}
			if !res.Stopped || res.Passed || res.Run != tc.stopAt || sent != tc.stopAt {
				t.Errorf("stopped %v passed %v after %d (sent %d); want a failed stop after %d", res.Stopped, res.Passed, res.Run, sent, tc.stopAt)
			}
			if res.Tier != "t" || !strings.Contains(res.Reason, "upper bound "+tc.boundFmt) {
				t.Errorf("result %+v doesn't name tier t and the bound %s", res, tc.boundFmt)
			}
		})
	}
}

// TestRunScreensFirst shows the screening fifth runs whole: every sample failing, the bound is
// under the bar after one, but the tier stops only after its screening fifth, 4 of 20.
func TestRunScreensFirst(t *testing.T) {
	bar := 0.8
	sent := 0
	res, err := tier.Run(t.Context(), list(20), &bar, func(context.Context, string) (bool, error) { sent++; return false, nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.Screening != 4 || res.Run != 4 || sent != 4 || !res.Stopped {
		t.Errorf("screening %d, run %d, sent %d, stopped %v; want a stop after the screening 4", res.Screening, res.Run, sent, res.Stopped)
	}
}

// TestRunWithoutBar shows a tier with no bar runs to its end and passes, recording its rate.
func TestRunWithoutBar(t *testing.T) {
	sent := 0
	res, err := tier.Run(t.Context(), list(10), nil, alternate(&sent))
	if err != nil {
		t.Fatal(err)
	}
	if res.Run != 10 || !res.Passed || res.Stopped || res.Rate != 0.5 || !strings.HasPrefix(res.Reason, "no bar") {
		t.Errorf("result %+v; want all 10 run, passed, rate 0.5, no bar", res)
	}
}

// TestRunToEnd shows a tier whose bound stays over the bar runs to its end, and is judged by its
// rate against the bar.
func TestRunToEnd(t *testing.T) {
	sent := 0
	for _, tc := range []struct {
		bar  float64
		pass bool
	}{{0.5, true}, {0.55, false}} {
		res, err := tier.Run(t.Context(), list(10), &tc.bar, alternate(&sent))
		if err != nil {
			t.Fatal(err)
		}
		if res.Run != 10 || res.Stopped || res.Passed != tc.pass {
			t.Errorf("bar %v: result %+v; want all run, passed %v", tc.bar, res, tc.pass)
		}
	}
}

// TestRunJudgeError shows a sample that can't be judged ends the run with an error naming it.
func TestRunJudgeError(t *testing.T) {
	_, err := tier.Run(t.Context(), list(3), nil, func(_ context.Context, id string) (bool, error) {
		if id == "s1" {
			return false, errors.New("boom")
		}
		return true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "sample s1") {
		t.Errorf("err = %v; want one naming sample s1", err)
	}
}

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

// TestLoadBars reads a bar, refuses one outside [0, 1] and an undefined key, and reads the
// committed bars file, which may be empty until the baseline fills it.
func TestLoadBars(t *testing.T) {
	dir := t.TempDir()
	write := func(text string) string {
		p := filepath.Join(dir, "bars.toml")
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	bars, err := tier.LoadBars(write("[bars]\ncode = 0.62\n"))
	if b, ok := bars.Bar("code"); err != nil || !ok || b != 0.62 {
		t.Errorf("LoadBars = %v, %v; want code 0.62", bars, err)
	}
	if _, ok := bars.Bar("missing"); ok {
		t.Error("a missing tier has a bar")
	}
	if _, err := tier.LoadBars(write("[bars]\ncode = 1.5\n")); err == nil {
		t.Error("a bar of 1.5 loaded")
	}
	if _, err := tier.LoadBars(write("colour = \"red\"\n")); err == nil {
		t.Error("an undefined key loaded")
	}
	if _, err := tier.LoadBars("../bars.toml"); err != nil {
		t.Errorf("the committed bars: %v", err)
	}
}
