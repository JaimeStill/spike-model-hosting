package coldload_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JaimeStill/spike-model-hosting/coldload"
)

// TestTracker follows a restart poll by poll: a stopping router that still lists a model loaded
// counts for nothing, the router's return is its first answer after refusing, and each model's
// time is its first loaded listing after it was gone, each from the restart's start.
func TestTracker(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	all := func(s string) map[string]string { return map[string]string{"a": s, "b": s} }
	down := errors.New("connection refused")
	var tr coldload.Tracker
	if tr.Ready() {
		t.Fatal("ready before the first poll")
	}
	steps := []struct {
		at     float64
		status map[string]string
		err    error
	}{
		{0, all("loaded"), nil},
		{1, all("loaded"), nil},
		// Stopping: a still listed loaded, b unloading. The restart starts here.
		{2, map[string]string{"a": "loaded", "b": "unloading"}, nil},
		{3, nil, down},
		{4, nil, down},
		{5, all("loading"), nil},
		{7, map[string]string{"a": "loading", "b": "loaded"}, nil},
		{9, map[string]string{"a": "loaded", "b": "unloading"}, nil},
	}
	for _, s := range steps {
		if tr.Done() {
			t.Fatalf("done before the poll at %vs", s.at)
		}
		if err := tr.Observe(at(s.at), s.status, s.err); err != nil {
			t.Fatalf("poll at %vs: %v", s.at, err)
		}
		if !tr.Ready() {
			t.Fatalf("not ready after the poll at %vs", s.at)
		}
	}
	if !tr.Done() {
		t.Fatalf("not done: %s", tr.Progress())
	}
	var out strings.Builder
	if err := tr.Write(&out); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(strings.Fields(out.String()), " ")
	for _, want := range []string{
		"restart seen 12:00:02.000 UTC (last steady poll 1.0s before)",
		"router up 3.0s",
		"b 5.0s (2.0s after the router)",
		"a 7.0s (4.0s after the router)",
		"all loaded 7.0s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Index(got, "b 5.0s") > strings.Index(got, "a 7.0s") {
		t.Errorf("want b before a, in the order they loaded:\n%s", out.String())
	}
}

// TestTrackerNeverDown shows a restart in which the router kept answering counts the router as
// up from the restart's start.
func TestTrackerNeverDown(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var tr coldload.Tracker
	for i, status := range []string{"loaded", "unloaded", "loading", "loaded"} {
		if err := tr.Observe(t0.Add(time.Duration(i)*time.Second), map[string]string{"a": status}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var out strings.Builder
	if err := tr.Write(&out); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(strings.Fields(out.String()), " ")
	for _, want := range []string{"router up never seen down", "a 2.0s (2.0s after the router)"} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// TestTrackerStart shows the tracker refuses to start unless the first poll lists every model
// loaded, so it never times a restart already under way.
func TestTrackerStart(t *testing.T) {
	for name, tc := range map[string]struct {
		status map[string]string
		err    error
		want   string
	}{
		"down":      {nil, errors.New("connection refused"), "must be up"},
		"loading":   {map[string]string{"a": "loaded", "b": "loading"}, nil, `"loading"`},
		"no models": {map[string]string{}, nil, "no models"},
	} {
		t.Run(name, func(t *testing.T) {
			var tr coldload.Tracker
			err := tr.Observe(time.Now(), tc.status, tc.err)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want one naming %q", err, tc.want)
			}
		})
	}
}

// TestTrackerModels shows a tracker given its models waits for those alone: a model outside them,
// unloaded throughout, neither stops it starting nor keeps it from finishing, and one of them not
// loaded at the first poll refuses the start.
func TestTrackerModels(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	tr := coldload.Tracker{Models: []string{"a"}}
	for i, status := range []string{"loaded", "unloaded", "loaded"} {
		if err := tr.Observe(t0.Add(time.Duration(i)*time.Second), map[string]string{"a": status, "other": "unloaded"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !tr.Done() {
		t.Errorf("not done: %s", tr.Progress())
	}

	tr = coldload.Tracker{Models: []string{"a", "b"}}
	err := tr.Observe(t0, map[string]string{"a": "loaded"}, nil)
	if err == nil || !strings.Contains(err.Error(), `b as ""`) {
		t.Errorf("err %v, want one naming b, not listed", err)
	}
}

// TestTrackerProgress shows how far an unfinished restart got, for the timeout's message.
func TestTrackerProgress(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var tr coldload.Tracker
	if got := tr.Progress(); !strings.Contains(got, "never listed every model loaded") {
		t.Errorf("before any poll: %q", got)
	}
	steps := []struct {
		status map[string]string
		err    error
		want   string
	}{
		{map[string]string{"a": "loaded", "b": "loaded"}, nil, "no restart seen"},
		{nil, errors.New("refused"), "never came back"},
		{map[string]string{"a": "loaded", "b": "loading"}, nil, "1 of 2 models loaded; not b"},
	}
	for i, s := range steps {
		if err := tr.Observe(t0.Add(time.Duration(i)*time.Second), s.status, s.err); err != nil {
			t.Fatal(err)
		}
		if got := tr.Progress(); !strings.Contains(got, s.want) {
			t.Errorf("after poll %d: Progress = %q, want %q", i, got, s.want)
		}
	}
}
