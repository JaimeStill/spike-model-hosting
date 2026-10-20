package profile_test

import (
	"os"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// TestSetSetting shows SetSetting rewrites one model's slots, c and their reasons in the
// profile's TOML and nothing else: every line outside the four keys stays, comments and the
// other models among them, the reasons wrap at 100 columns, and the result loads with the new
// values. It edits the first model and the last, whose table a subtable follows.
func TestSetSetting(t *testing.T) {
	src, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	reason := strings.Repeat("A sweep chose this setting for the reasons it records. ", 6)
	for _, id := range []string{"ggml-org/gpt-oss-120b-GGUF:MXFP4", "ggml-org/embeddinggemma-2-GGUF:Q8_0"} {
		t.Run(id, func(t *testing.T) {
			out, err := profile.SetSetting(src, id, profile.Setting{Slots: 1, C: 8192, SlotsReason: "one: " + reason, CReason: "c: " + reason})
			if err != nil {
				t.Fatalf("SetSetting: %v", err)
			}
			p, err := profile.Parse(out, "edited")
			if err != nil {
				t.Fatal(err)
			}
			m := p.Model(id)
			if m.Slots != 1 || m.C != 8192 || !strings.HasPrefix(m.SlotsReason, "one: A sweep") || !strings.HasPrefix(m.CReason, "c: A sweep") {
				t.Errorf("%s after the edit: %d slots, c = %d, reasons %q, %q", id, m.Slots, m.C, m.SlotsReason, m.CReason)
			}
			for line := range strings.SplitSeq(string(out), "\n") {
				if len(line) > 100 {
					t.Errorf("a line over 100 columns: %q", line)
				}
			}
			// The source's lines the edit kept, in order, are all but the old c and slots lines and
			// the old reasons' text lines.
			kept := strings.Split(string(src), "\n")
			old, _ := profile.Parse(src, "source")
			om := old.Model(id)
			dropped := strings.Count(om.SlotsReason, "\n") + strings.Count(om.CReason, "\n") + 1
			if om.C != 8192 {
				dropped++ // the old c line, unless it is the new one
			}
			if got := common(kept, strings.Split(string(out), "\n")); got != len(kept)-dropped {
				t.Errorf("the edit kept %d of the source's %d lines; want all but %d, the old slots line, the old c line where it changed, and the old reasons' lines", got, len(kept), dropped)
			}
		})
	}
}

// TestSetSettingRefuses shows SetSetting refuses an edit it can't make safely: a model the
// profile doesn't hold, a reason a multi-line string can't carry as written, a c over the card's
// context (the result doesn't validate), and a key the model's table holds twice.
func TestSetSettingRefuses(t *testing.T) {
	src, err := os.ReadFile(strixHalo)
	if err != nil {
		t.Fatal(err)
	}
	const id = "ggml-org/embeddinggemma-2-GGUF:Q8_0"
	ok := profile.Setting{Slots: 1, C: 8192, SlotsReason: "a reason", CReason: "a reason"}
	twice := strings.Replace(string(src), "dims = 768\n", "dims = 768\nslots = 2\n", 1)
	cases := map[string]struct {
		src  string
		id   string
		s    profile.Setting
		want string
	}{
		"no model":      {string(src), "example/none-GGUF:Q4_0", ok, "no model"},
		"backslash":     {string(src), id, profile.Setting{Slots: 1, C: 8192, SlotsReason: `a \n reason`, CReason: "x"}, "backslash"},
		"triple quote":  {string(src), id, profile.Setting{Slots: 1, C: 8192, SlotsReason: "x", CReason: `a """ reason`}, "triple quote"},
		"empty reason":  {string(src), id, profile.Setting{Slots: 1, C: 8192, SlotsReason: " ", CReason: "x"}, "reason"},
		"over the card": {string(src), id, profile.Setting{Slots: 1, C: 16384, SlotsReason: "x", CReason: "x"}, "over the card's context"},
		"twice":         {twice, id, ok, "slots"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := profile.SetSetting([]byte(tc.src), tc.id, tc.s)
			if err == nil || out != nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("SetSetting = %d bytes, %v; want refused, naming %q", len(out), err, tc.want)
			}
		})
	}
}

// common is the length of a and b's longest common subsequence of lines.
func common(a, b []string) int {
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for i := range a {
		for j := range b {
			if a[i] == b[j] {
				cur[j+1] = prev[j] + 1
			} else {
				cur[j+1] = max(cur[j], prev[j+1])
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
