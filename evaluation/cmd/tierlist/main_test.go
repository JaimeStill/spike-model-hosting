package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// TestDraw draws a list from IDs on stdin: the dataset line becomes its header, and the IDs are
// tier's Generate's draw, whatever order they come in.
func TestDraw(t *testing.T) {
	in := "# dataset repo@abc set/test-*.parquet\nc\na\n\nd\nb\ne\n"
	var out, errs bytes.Buffer
	if code := run([]string{"-tier", "asr-x", "-n", "3", "-seed", "7"}, strings.NewReader(in), &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	got, err := tier.ReadList(&out)
	if err != nil {
		t.Fatal(err)
	}
	want := tier.Generate("asr-x", "repo@abc set/test-*.parquet", []string{"e", "d", "c", "b", "a"}, 3, 7)
	if got.Tier != want.Tier || got.Dataset != want.Dataset || got.Seed != 7 || strings.Join(got.IDs, ",") != strings.Join(want.IDs, ",") {
		t.Errorf("list %+v; want %+v", got, want)
	}
}

// TestDrawRefuses exits 2 on a usage error, and 1 on IDs without a dataset line or fewer than
// the draw.
func TestDrawRefuses(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		in   string
		code int
	}{
		"no tier":    {[]string{"-n", "1"}, "# dataset d\na\n", 2},
		"no count":   {[]string{"-tier", "t"}, "# dataset d\na\n", 2},
		"no dataset": {[]string{"-tier", "t", "-n", "1"}, "a\n", 1},
		"too few":    {[]string{"-tier", "t", "-n", "3"}, "# dataset d\na\nb\na\n", 1},
		"stray line": {[]string{"-tier", "t", "-n", "1"}, "# dataset d\n# seed 3\na\n", 1},
	} {
		var out, errs bytes.Buffer
		if code := run(tc.args, strings.NewReader(tc.in), &out, &errs); code != tc.code {
			t.Errorf("%s: exit %d; want %d (%s)", name, code, tc.code, errs.String())
		}
	}
}
