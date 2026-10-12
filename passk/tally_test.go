package passk_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"

	"github.com/JaimeStill/spike-model-hosting/consumers"
	"github.com/JaimeStill/spike-model-hosting/passk"
	"github.com/JaimeStill/spike-model-hosting/wire"
)

// report is a trial's report in which every capability passed except those in statuses.
func report(statuses map[string]conform.Status) conform.Report {
	rep := conform.Report{Harness: "pi", Provider: consumers.Provider, Version: "1.1.0"}
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

// TestTallyBar shows pass^k over the nine tasks and the bar at its boundary: nine of nine at 5
// of 5 meets it, and one trial lost in one task leaves eight of nine, 88.9%, which fails it, as
// does a single invalid request with nine of nine.
func TestTallyBar(t *testing.T) {
	if n := len(conform.Capabilities); n != 9 {
		t.Fatalf("the suite has %d capabilities; this test's arithmetic is for 9", n)
	}
	all := passk.Tally("pi", trials(5, nil))
	if all.K != 5 || all.Passed() != 9 || all.PassK() != 1 || !all.MeetsBar() {
		t.Errorf("all passing: K %d, passed %d, pass^k %v, meets %v; want 5, 9, 1, true", all.K, all.Passed(), all.PassK(), all.MeetsBar())
	}

	oneLost := passk.Tally("opencode", trials(5, map[int]conform.Report{3: report(map[string]conform.Status{"skill": conform.Fail})}))
	if oneLost.Passed() != 8 || math.Abs(oneLost.PassK()-8.0/9) > 1e-9 || oneLost.MeetsBar() {
		t.Errorf("one trial lost: passed %d, pass^k %v, meets %v; want 8, 0.889, false", oneLost.Passed(), oneLost.PassK(), oneLost.MeetsBar())
	}
	skill := oneLost.Cells[4]
	if skill.Capability != "skill" || skill.Pass != 4 || skill.Fail != 1 || skill.Passed(5) || len(skill.Reasons) != 1 {
		t.Errorf("skill = %+v; want 4 passes, 1 failure with its reason", skill)
	}

	ts := trials(5, nil)
	ts[2].Invalid = []wire.Exchange{{Path: "/v1/chat/completions", Status: http.StatusInternalServerError}}
	invalid := passk.Tally("pi", ts)
	if invalid.Invalid != 1 || invalid.PassK() != 1 || invalid.MeetsBar() {
		t.Errorf("one invalid request: invalid %d, pass^k %v, meets %v; want 1, 1, false", invalid.Invalid, invalid.PassK(), invalid.MeetsBar())
	}
}

// TestTallyNotPasses shows n/a and a skipped trial count against a task: a capability a consumer
// can't run is not a pass.
func TestTallyNotPasses(t *testing.T) {
	na := report(map[string]conform.Status{"dropped-image": conform.NA})
	r := passk.Tally("claude", trials(5, map[int]conform.Report{0: na, 1: na, 2: na, 3: na, 4: na}))
	if c := r.Cells[7]; c.Capability != "dropped-image" || c.NA != 5 || c.Pass != 0 {
		t.Errorf("dropped-image = %+v; want 5 n/a", c)
	}
	if r.Passed() != 8 || r.MeetsBar() {
		t.Errorf("n/a in every trial: passed %d, meets %v; want 8, false", r.Passed(), r.MeetsBar())
	}

	skipped := passk.Tally("pi", trials(5, map[int]conform.Report{2: {Harness: "pi", Version: "-", Skipped: "need the pi executable: not found"}}))
	if skipped.Passed() != 0 || len(skipped.Skipped) != 1 {
		t.Errorf("one skipped trial: passed %d, skipped %q; want 0 and the reason", skipped.Passed(), skipped.Skipped)
	}
	for _, c := range skipped.Cells {
		if c.Pass != 4 || c.Skip != 1 || len(c.Reasons) != 1 || c.Reasons[0] != skipped.Skipped[0] {
			t.Errorf("%s = %+v; want 4 passes and 1 skip with the trial's reason", c.Capability, c)
		}
	}
	if skipped.Version != "1.1.0" {
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
