package passk_test

import (
	"os"
	"strings"
	"testing"

	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// TestRunStops runs consumers whose executables don't exist, so every trial is skipped and sends
// nothing: every consumer runs its first trial, the screening fifth, whole, and then the
// comparison row's Stop ends the run, Pi out of reach, before a second trial; each trial's
// recording is saved. Without a Stop the run makes every trial.
func TestRunStops(t *testing.T) {
	c := committed(t)
	state, wireDir := t.TempDir(), t.TempDir()
	r := passk.Runner{
		Commands: map[string]string{"pi": "/nonexistent/pi", "claude": "/nonexistent/claude", "opencode": "/nonexistent/opencode"},
		State:    state, ClaudeConfig: t.TempDir(),
	}
	plan := passk.Plan{
		Router: "http://127.0.0.1:1", Roles: profile.SetRoles{Chat: "chat", Vision: "vision", Audio: "audio", Embedding: "embed"},
		K: 5, Consumers: passk.ConsumerNames, Wire: wireDir, Stop: c.Stop,
	}
	res, err := r.Run(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stopped, "pi's exchange can reach at most 4/5 after 1 trials") {
		t.Errorf("Stopped = %q; want Pi out of reach after its first trial", res.Stopped)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("rows %+v; want one a consumer", res.Rows)
	}
	for _, row := range res.Rows {
		if row.K != 1 || len(row.Skipped) != 1 {
			t.Errorf("%s: %d trials, skipped %q; want its first trial alone, skipped", row.Consumer, row.K, row.Skipped)
		}
	}
	if entries, err := os.ReadDir(wireDir); err != nil || len(entries) != 3 {
		t.Errorf("recordings %v, %v; want one a trial, 3", entries, err)
	}

	plan.Stop, plan.K, plan.Consumers = nil, 2, []string{"pi"}
	res, err = r.Run(t.Context(), plan)
	if err != nil || res.Stopped != "" || len(res.Rows) != 1 || res.Rows[0].K != 2 {
		t.Errorf("no Stop: %+v, %v; want Pi's 2 trials, not stopped", res, err)
	}
}
