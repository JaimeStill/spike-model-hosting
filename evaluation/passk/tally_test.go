package passk_test

import (
	"net/http"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/evaluation/consumers"
	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/wire"
)

// report is a trial's report in which every capability passed except those in statuses.
func report(statuses map[string]conform.Status) conform.Report {
	rep := conform.Report{Harness: "pi", Provider: consumers.Provider, Version: "1.3.0"}
	for _, c := range conform.Capabilities {
		s, ok := statuses[c.Name]
		if !ok {
			s = conform.Pass
		}
		rep.Results = append(rep.Results, conform.Result{Capability: c.Name, Status: s, Reason: c.Name + " " + s.String()})
	}
	return rep
}

// trials returns k trials that all pass, with the i-th trial replaced by override when it is
// set.
func trials(k int, override map[int]conform.Report) []passk.Trial {
	ts := make([]passk.Trial, k)
	for i := range ts {
		ts[i].Report = report(nil)
		if r, ok := override[i]; ok {
			ts[i].Report = r
		}
	}
	return ts
}

// TestTally counts each capability's passes and failures over k trials, with the reasons of
// those that didn't pass, and the invalid requests over every trial.
func TestTally(t *testing.T) {
	ts := trials(5, map[int]conform.Report{3: report(map[string]conform.Status{"skill": conform.Fail})})
	ts[2].Invalid = []wire.Exchange{{Path: "/v1/chat/completions", Status: http.StatusInternalServerError}}
	r := passk.Tally("opencode", ts)
	if r.K != 5 || r.Invalid != 1 || r.Version != "1.3.0" {
		t.Errorf("row %+v; want 5 trials, 1 invalid, version 1.3.0", r)
	}
	skill := r.Cells[4]
	if skill.Capability != "skill" || skill.Pass != 4 || skill.Fail != 1 || len(skill.Reasons) != 1 {
		t.Errorf("skill = %+v; want 4 passes, 1 failure with its reason", skill)
	}
	if tool := r.Cells[3]; tool.Capability != "tool" || tool.Pass != 5 || len(tool.Reasons) != 0 {
		t.Errorf("tool = %+v; want 5 passes", tool)
	}
}

// TestTallyNotPasses shows n/a and a skipped trial counted apart from passes: a capability a
// consumer can't run is not a pass, and a skipped trial is a skip in every capability.
func TestTallyNotPasses(t *testing.T) {
	na := report(map[string]conform.Status{"dropped-image": conform.NA})
	r := passk.Tally("claude", trials(5, map[int]conform.Report{0: na, 1: na, 2: na, 3: na, 4: na}))
	if c := r.Cells[7]; c.Capability != "dropped-image" || c.NA != 5 || c.Pass != 0 {
		t.Errorf("dropped-image = %+v; want 5 n/a", c)
	}

	skipped := passk.Tally("pi", trials(5, map[int]conform.Report{2: {Harness: "pi", Version: "-", Skipped: "need the pi executable: not found"}}))
	if len(skipped.Skipped) != 1 {
		t.Errorf("one skipped trial: skipped %q; want the reason", skipped.Skipped)
	}
	for _, c := range skipped.Cells {
		if c.Pass != 4 || c.Skip != 1 || len(c.Reasons) != 1 || c.Reasons[0] != skipped.Skipped[0] {
			t.Errorf("%s = %+v; want 4 passes and 1 skip with the trial's reason", c.Capability, c)
		}
	}
	if skipped.Version != "1.3.0" {
		t.Errorf("Version = %q, want the first trial that ran's", skipped.Version)
	}
}

// TestInvalid shows which recorded exchanges count as invalid: a model request the router
// answered with an error, on any chat route, as a status or as an error event in a stream that
// began with 200, but not a request the consumer cancelled, a route that generates nothing, or a
// success.
func TestInvalid(t *testing.T) {
	xs := []wire.Exchange{
		{Method: "POST", Path: "/v1/chat/completions", Status: 500, Response: `{"error":"does not match the expected peg-native format"}`},
		{Method: "POST", Path: "/v1/messages?beta=true", Status: 400},
		{Method: "POST", Path: "/v1/responses", Status: 503},
		{Method: "POST", Path: "/v1/chat/completions", Status: 502, Response: "dial tcp: connection refused"},
		{Method: "POST", Path: "/v1/chat/completions", Status: 200, Response: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"x\"}}]}\n\n" +
			"data: {\"error\":{\"code\":500,\"message\":\"The model produced output that does not match the expected peg-native format\"}}\n\n"},
		{Method: "POST", Path: "/v1/messages?beta=true", Status: 200, Response: "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\"}}\n\n"},
		{Method: "POST", Path: "/v1/chat/completions", Status: 502, Response: "context canceled"},
		{Method: "POST", Path: "/v1/messages/count_tokens?beta=true", Status: 500},
		{Method: "HEAD", Path: "/api/hello", Status: 404},
		{Method: "GET", Path: "/v1/models", Status: 500},
		{Method: "POST", Path: "/v1/chat/completions", Status: 200, Response: "data: {\"choices\":[{\"delta\":{\"content\":\"error\"}}],\"error\":null}\n\ndata: [DONE]\n\n"},
		{Method: "POST", Path: "/v1/chat/completions", Status: 200, Response: `{"choices":[{"message":{"content":"ok"}}]}`},
	}
	got := passk.Invalid(xs)
	if len(got) != 6 {
		t.Fatalf("Invalid found %d, want the first 6: %+v", len(got), got)
	}
	for i, x := range got {
		if x.Path != xs[i].Path || x.Status != xs[i].Status {
			t.Errorf("Invalid[%d] = %s %d, want %s %d", i, x.Path, x.Status, xs[i].Path, xs[i].Status)
		}
	}
}
