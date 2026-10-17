package passk

import (
	"fmt"
	"slices"
	"strings"

	"github.com/JaimeStill/spike-harness-driver/clutch/conform"
)

// Harness is one harness a run can take: its name, which names its executable's pin and flag,
// and how its cell is built from a Config.
type Harness struct {
	Name string
	Cell func(Config) (conform.Cell, error)
}

// Harnesses are the pinned harnesses, in the order a run runs and reports them. Each is judged
// the same way, by Judge; a harness joins the fit tier with one entry here and a pin in the
// consumers mise environment.
var Harnesses = []Harness{
	{Name: "pi", Cell: Pi},
	{Name: "claude", Cell: func(cfg Config) (conform.Cell, error) { return Claude(cfg), nil }},
	{Name: "opencode", Cell: OpenCode},
}

// ConsumerNames are the harnesses' names, in Harnesses' order.
var ConsumerNames = names(Harnesses)

// LookupHarness returns the harness named name, or false when Harnesses has none.
func LookupHarness(name string) (Harness, bool) {
	i := slices.IndexFunc(Harnesses, func(h Harness) bool { return h.Name == name })
	if i < 0 {
		return Harness{}, false
	}
	return Harnesses[i], true
}

func names(hs []Harness) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name
	}
	return out
}

// Fit is a harness judged against the fit bar at k: every applicable capability passed in each
// of k trials, with no invalid request. A capability the harness reported n/a in every trial it
// ran is one it can't exercise: it is excluded from the bar and noted.
type Fit struct {
	// Row is the harness's trials, counted; Row.K is the trials it ran.
	Row Row
	// K is the trials the bar asks for.
	K int
	// Fits reports whether the harness ran all K trials and met the bar in every one.
	Fits bool
	// Excluded are the capabilities n/a in every trial, in conform.Capabilities' order.
	Excluded []string
	// Trial is the first trial that failed the bar, from 1; zero when none did.
	Trial int
	// Failures are why that trial failed, a line each: a skipped trial, each failed cell with
	// conform's reason, and each invalid request.
	Failures []string
}

// Judge judges a harness's trials against the fit bar at k, trial by trial: the first trial
// that was skipped, failed a capability, made an invalid request, or reported n/a for a
// capability another of its trials exercised (or the reverse) fails it, and the harness
// can't fit whatever trials still to come do. A harness that met the bar in every trial it
// ran but ran fewer than k doesn't fit either: its evidence is incomplete.
func Judge(name string, trials []Trial, k int) Fit {
	f := Fit{Row: Tally(name, trials), K: k}
	for i, c := range f.Row.Cells {
		if c.NA > 0 && c.NA == f.Row.K {
			f.Excluded = append(f.Excluded, conform.Capabilities[i].Name)
		}
	}
	// na marks the capabilities a trial has reported n/a, and ran those it has exercised.
	na, ran := map[string]bool{}, map[string]bool{}
	for n, t := range trials {
		if fs := trialFailures(t, na, ran); len(fs) > 0 {
			f.Trial, f.Failures = n+1, fs
			return f
		}
	}
	f.Fits = len(trials) == k
	return f
}

// trialFailures returns why trial t fails the bar, given the capabilities earlier trials
// reported n/a (na) and exercised (ran), which it updates.
func trialFailures(t Trial, na, ran map[string]bool) []string {
	if t.Report.Skipped != "" {
		return []string{"skipped: " + oneLine(t.Report.Skipped)}
	}
	var out []string
	for i, c := range conform.Capabilities {
		status, reason := conform.Skip, "no result"
		if i < len(t.Report.Results) {
			status, reason = t.Report.Results[i].Status, t.Report.Results[i].Reason
		}
		switch status {
		case conform.Pass:
			if na[c.Name] {
				out = append(out, fmt.Sprintf("%s: passed after n/a in an earlier trial", c.Name))
			}
			ran[c.Name] = true
		case conform.NA:
			if ran[c.Name] {
				out = append(out, fmt.Sprintf("%s: n/a after it ran in an earlier trial: %s", c.Name, oneLine(reason)))
			}
			na[c.Name] = true
		default:
			out = append(out, fmt.Sprintf("%s %s: %s", c.Name, status, oneLine(reason)))
		}
	}
	for _, x := range t.Invalid {
		out = append(out, fmt.Sprintf("invalid: %s %s -> %d %s", x.Method, x.Path, x.Status, truncate(oneLine(x.Response), 200)))
	}
	return out
}

// Failed reports whether the harness has failed the bar in a trial it ran, so that no further
// trial can bring it back.
func (f Fit) Failed() bool { return f.Trial > 0 }

// Applicable is the number of capabilities the bar holds the harness to: every one but those
// it excluded.
func (f Fit) Applicable() int { return len(conform.Capabilities) - len(f.Excluded) }

// Verdict is a line saying whether the harness fits and, when it doesn't, why.
func (f Fit) Verdict() string {
	note := ""
	if len(f.Excluded) > 0 {
		note = "; excluded, n/a: " + strings.Join(f.Excluded, ", ")
	}
	switch {
	case f.Fits:
		return fmt.Sprintf("fits: %d applicable capabilities at %d/%d, 0 invalid%s", f.Applicable(), f.K, f.K, note)
	case f.Failed():
		return fmt.Sprintf("does not fit: failed trial %d of %d: %s%s", f.Trial, f.K, strings.Join(f.Failures, "; "), note)
	default:
		return fmt.Sprintf("does not fit: ran %d of %d trials%s", f.Row.K, f.K, note)
	}
}
