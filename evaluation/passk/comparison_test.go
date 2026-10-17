package passk_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
)

// committed loads the committed comparison row, gpt-oss-120b's on b11669.
func committed(t *testing.T) passk.Comparison {
	t.Helper()
	c, err := passk.LoadComparison("../comparison.toml")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestLoadComparison reads the committed row as the findings record it, Pi and Claude Code gated
// and OpenCode measured, and refuses a file that misses a consumer or a capability, names one it
// doesn't know, or holds a count past k.
func TestLoadComparison(t *testing.T) {
	c := committed(t)
	if c.K != 5 || c.Build != "b11669-620d2225c" || c.Model != "ggml-org/gpt-oss-120b-GGUF:MXFP4" {
		t.Errorf("row %+v; want gpt-oss-120b's pass^5 on b11669", c)
	}
	pi, claude, opencode := c.Consumers["pi"], c.Consumers["claude"], c.Consumers["opencode"]
	if !pi.Gated || pi.Invalid != 0 || !claude.Gated || claude.Invalid != 0 || opencode.Gated || opencode.Invalid != 18 {
		t.Errorf("consumers %+v; want Pi and Claude Code gated at 0 invalid, OpenCode measured at 18", c.Consumers)
	}
	for _, capability := range conform.Capabilities {
		if pi.Cells[capability.Name] != 5 {
			t.Errorf("pi %s = %d; want 5/5", capability.Name, pi.Cells[capability.Name])
		}
	}
	if claude.Cells["tool"] != 0 || claude.Cells["skill"] != 0 || claude.Cells["dropped-image"] != 0 || claude.Cells["audio-tool"] != 5 {
		t.Errorf("claude cells %v; want tool, skill and dropped-image 0, audio-tool 5", claude.Cells)
	}

	base, err := os.ReadFile("../comparison.toml")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ old, new, want string }{
		"consumer missing": {"[consumers.opencode]", "[consumers.codex]", "consumers.opencode: missing"},
		"cell missing":     {"audio-tool = 3 }", "}", "consumers.opencode.cells.audio-tool: missing"},
		"unknown cell":     {"audio-tool = 3 }", "audio-tool = 3, hearing = 1 }", "hearing: not one of conform's capabilities"},
		"count past k":     {"tool = 4,", "tool = 6,", "cells.tool = 6: outside [0, 5]"},
		"no k":             {"k = 5\n", "", "k = 0"},
		"unknown key":      {"k = 5\n", "k = 5\nbar = 0.9\n", "bar: not a key"},
	} {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(string(base), tc.old) {
				t.Fatalf("the committed file lacks %q", tc.old)
			}
			path := filepath.Join(t.TempDir(), "comparison.toml")
			if err := os.WriteFile(path, []byte(strings.Replace(string(base), tc.old, tc.new, 1)), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := passk.LoadComparison(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("LoadComparison = %v; want an error naming %q", err, tc.want)
			}
		})
	}
}

// TestJudge judges a consumer cell by cell against its row: Claude Code at its row's own counts
// meets it, one trial lost in a cell the row holds at 5/5 falls short naming the cell, an invalid
// request over the row's falls short, and OpenCode, measured, is never short.
func TestJudge(t *testing.T) {
	c := committed(t)
	failing := map[string]conform.Status{"tool": conform.Fail, "skill": conform.Fail, "dropped-image": conform.NA}
	asRow := func(k int, override map[int]conform.Report) []passk.Trial {
		ts := trials(k, override)
		for i := range ts {
			if _, ok := override[i]; !ok {
				ts[i].Report = report(failing)
			}
		}
		return ts
	}
	claude := passk.Tally("claude", asRow(5, nil))
	if short := c.Judge(claude); len(short) != 0 {
		t.Errorf("Claude Code at its row: short %q; want none", short)
	}
	lost := map[string]conform.Status{"tool": conform.Fail, "skill": conform.Fail, "dropped-image": conform.NA, "audio-tool": conform.Fail}
	worse := passk.Tally("claude", asRow(5, map[int]conform.Report{2: report(lost)}))
	if short := c.Judge(worse); len(short) != 1 || !strings.Contains(short[0], "audio-tool 4/5 under the row's 5/5") {
		t.Errorf("audio-tool lost once: short %q; want audio-tool named", short)
	}
	ts := asRow(5, nil)
	ts[1].Invalid = []wire.Exchange{{Path: "/v1/messages", Status: 500}}
	if short := c.Judge(passk.Tally("claude", ts)); len(short) != 1 || !strings.Contains(short[0], "1 invalid over the row's 0") {
		t.Errorf("one invalid request: short %q; want it named", short)
	}
	if short := c.Judge(passk.Tally("opencode", trials(5, map[int]conform.Report{0: report(lost)}))); len(short) != 0 {
		t.Errorf("OpenCode: short %q; want none, it is measured", short)
	}
	if short := c.Judge(passk.Tally("pi", trials(3, nil))); len(short) != 1 || !strings.Contains(short[0], "ran 3 trials") {
		t.Errorf("Pi on 3 trials: short %q; want the trial count named", short)
	}
}

// TestStop stops a run once a gated consumer can't meet its row whatever its trials to come do,
// and not before: Pi losing one trial of a cell is out of reach at once, Claude Code's 0/5 cells
// never are, an invalid request over the row is, and OpenCode never stops a run.
func TestStop(t *testing.T) {
	c := committed(t)
	claudeRow := report(map[string]conform.Status{"tool": conform.Fail, "skill": conform.Fail, "dropped-image": conform.NA})
	allPass := map[string][]passk.Trial{
		"pi": trials(2, nil), "claude": {{Report: claudeRow}, {Report: claudeRow}},
		"opencode": trials(2, map[int]conform.Report{1: report(map[string]conform.Status{"tool": conform.Fail})}),
	}
	if reason := c.Stop(allPass); reason != "" {
		t.Errorf("every gated consumer on its row: stopped, %q", reason)
	}
	piLost := map[string][]passk.Trial{"pi": trials(2, map[int]conform.Report{1: report(map[string]conform.Status{"vision": conform.Fail})})}
	if reason := c.Stop(piLost); !strings.Contains(reason, "pi's vision can reach at most 4/5 after 2 trials") {
		t.Errorf("Pi lost vision once: %q; want it out of reach", reason)
	}
	invalid := map[string][]passk.Trial{"claude": {{Report: claudeRow, Invalid: []wire.Exchange{{Path: "/v1/messages", Status: 500}}}}}
	if reason := c.Stop(invalid); !strings.Contains(reason, "claude has 1 invalid requests after 1 trials") {
		t.Errorf("Claude Code's invalid request: %q; want it out of reach", reason)
	}
}
