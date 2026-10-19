package passk_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
)

// TestJudgeFits shows the fit bar: every capability passing in each of k trials with no invalid
// request fits; the same run one trial short of k doesn't, its evidence incomplete.
func TestJudgeFits(t *testing.T) {
	f := passk.Judge("pi", trials(5, nil), 5)
	if !f.Fits || f.Failed() || f.Applicable() != len(conform.Capabilities) || len(f.Excluded) != 0 {
		t.Errorf("all passing: %+v; want fits, every capability applicable", f)
	}
	if v := f.Verdict(); !strings.HasPrefix(v, "fits: 9 applicable capabilities at 5/5, 0 invalid") {
		t.Errorf("verdict %q", v)
	}
	short := passk.Judge("pi", trials(4, nil), 5)
	if short.Fits || short.Failed() || !strings.Contains(short.Verdict(), "ran 4 of 5 trials") {
		t.Errorf("4 of 5 trials: %+v, %q; want not fit, incomplete", short, short.Verdict())
	}
}

// TestJudgeFailures shows the first trial that fails the bar decides, naming each failed cell
// with conform's reason: a failed capability, a skipped trial, and an invalid request with every
// capability passing.
func TestJudgeFailures(t *testing.T) {
	failed := passk.Judge("opencode", trials(3, map[int]conform.Report{
		1: report(map[string]conform.Status{"skill": conform.Fail, "audio-tool": conform.Fail}),
		2: report(map[string]conform.Status{"tool": conform.Fail}),
	}), 5)
	want := []string{"skill FAIL: skill FAIL", "audio-tool FAIL: audio-tool FAIL"}
	if failed.Fits || failed.Trial != 2 || !slices.Equal(failed.Failures, want) {
		t.Errorf("failed at trial 2: trial %d, failures %q; want 2, %q", failed.Trial, failed.Failures, want)
	}
	if v := failed.Verdict(); !strings.Contains(v, "does not fit: failed trial 2 of 5: skill FAIL: skill FAIL; audio-tool") {
		t.Errorf("verdict %q", v)
	}

	skipped := passk.Judge("pi", []passk.Trial{{Report: conform.Report{Harness: "pi", Skipped: "need the pi executable: not found"}}}, 5)
	if skipped.Trial != 1 || len(skipped.Failures) != 1 || skipped.Failures[0] != "skipped: need the pi executable: not found" {
		t.Errorf("skipped: %+v; want trial 1, the skip's reason", skipped)
	}

	ts := trials(5, nil)
	ts[0].Invalid = []wire.Exchange{{Method: "POST", Path: "/v1/chat/completions", Status: http.StatusInternalServerError, Response: "does not match"}}
	invalid := passk.Judge("opencode", ts, 5)
	if invalid.Fits || invalid.Trial != 1 || len(invalid.Failures) != 1 || invalid.Failures[0] != "invalid: POST /v1/chat/completions -> 500 does not match" {
		t.Errorf("invalid request: %+v; want dropped at trial 1, naming it", invalid)
	}
}

// TestJudgeNA shows a capability n/a in every trial excluded from the bar and noted, so Claude
// Code passing the rest fits; a capability n/a in one trial and run in another fails.
func TestJudgeNA(t *testing.T) {
	na := map[string]conform.Status{"dropped-image": conform.NA}
	all := map[int]conform.Report{}
	for i := range 5 {
		all[i] = report(na)
	}
	f := passk.Judge("claude", trials(5, all), 5)
	if !f.Fits || !slices.Equal(f.Excluded, []string{"dropped-image"}) || f.Applicable() != len(conform.Capabilities)-1 {
		t.Errorf("n/a in every trial: %+v; want fits, dropped-image excluded", f)
	}
	if v := f.Verdict(); !strings.Contains(v, "8 applicable capabilities at 5/5") || !strings.Contains(v, "excluded, n/a: dropped-image") {
		t.Errorf("verdict %q; want the exclusion noted", v)
	}
	mixed := passk.Judge("claude", trials(2, map[int]conform.Report{0: report(na)}), 5)
	if mixed.Trial != 2 || len(mixed.Excluded) != 0 || !strings.Contains(mixed.Failures[0], "dropped-image: passed after n/a") {
		t.Errorf("n/a then passed: %+v; want failed at trial 2", mixed)
	}
}
