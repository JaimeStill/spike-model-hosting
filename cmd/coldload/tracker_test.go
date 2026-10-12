package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTracker follows a restart poll by poll: a stopping router that still lists a model loaded
// counts for nothing, the router's return is its first answer after refusing, and each model's
// time is its first loaded listing after it was gone.
func TestTracker(t *testing.T) {
	t0 := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	at := func(s float64) time.Time { return t0.Add(time.Duration(s * float64(time.Second))) }
	all := func(s string) map[string]string { return map[string]string{"a": s, "b": s} }
	down := errors.New("connection refused")
	var tr tracker
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
		if tr.done() {
			t.Fatalf("done before the poll at %vs", s.at)
		}
		if err := tr.observe(at(s.at), s.status, s.err); err != nil {
			t.Fatalf("poll at %vs: %v", s.at, err)
		}
	}
	if !tr.done() {
		t.Fatalf("not done: %s", tr.progress())
	}
	for name, got := range map[string]struct{ got, want time.Time }{
		"last steady": {tr.lastSteady, at(1)},
		"start":       {tr.start, at(2)},
		"up":          {tr.up, at(5)},
		"a":           {tr.loaded["a"], at(9)},
		"b":           {tr.loaded["b"], at(7)},
	} {
		if !got.got.Equal(got.want) {
			t.Errorf("%s at %s, want %s", name, got.got.Sub(t0), got.want.Sub(t0))
		}
	}
	var out strings.Builder
	if err := tr.write(&out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"router up  3.0s", "b  5.0s  (2.0s after the router)", "a  7.0s  (4.0s after the router)", "all loaded  7.0s"} {
		if !strings.Contains(strings.Join(strings.Fields(out.String()), "  "), strings.Join(strings.Fields(want), "  ")) {
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
			var tr tracker
			err := tr.observe(time.Now(), tc.status, tc.err)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err %v, want one naming %q", err, tc.want)
			}
		})
	}
}
