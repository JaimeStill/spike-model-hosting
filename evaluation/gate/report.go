package gate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// The comparisons a Check judges its measured value by.
const (
	AtLeast = ">="
	AtMost  = "<="
)

// Check is one thing a stage judged: a measured value against its floor or bar, or a fact the
// profile or the router states.
type Check struct {
	Name string `json:"name"`
	// Role is the role the check judges, empty for one that judges the model or the set.
	Role string `json:"role,omitempty"`
	// Measured is the value judged, in Unit; nil for a fact, which Detail states.
	Measured *float64 `json:"measured,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	// Op is AtLeast or AtMost, and Floor the floor or bar Measured is judged against; both are
	// empty for a measurement that is recorded only, such as a quality task with no bar yet.
	Op    string   `json:"op,omitempty"`
	Floor *float64 `json:"floor,omitempty"`
	// Values are further measurements the check recorded, such as a request's token counts or
	// each stream's speed; decode_tok_s is the one the ceiling comparison reads.
	Values map[string]float64 `json:"values,omitempty"`
	Detail string             `json:"detail,omitempty"`
	Passed bool               `json:"passed"`
}

// measure returns a check of measured against floor by op.
func measure(name, role string, measured float64, unit, op string, floor float64) Check {
	pass := measured >= floor
	if op == AtMost {
		pass = measured <= floor
	}
	return Check{Name: name, Role: role, Measured: &measured, Unit: unit, Op: op, Floor: &floor, Passed: pass}
}

// fact returns a check of a stated fact, passed when ok.
func fact(name, role, detail string, ok bool) Check {
	return Check{Name: name, Role: role, Detail: detail, Passed: ok}
}

// broken returns a failed check for a measurement that couldn't be taken.
func broken(name, role string, err error) Check {
	return Check{Name: name, Role: role, Detail: "couldn't measure: " + err.Error()}
}

// The statuses a stage ends in.
const (
	StatusPass   = "pass"
	StatusFail   = "fail"
	StatusNotRun = "not run"
)

// StageResult is one stage of a run.
type StageResult struct {
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Checks []Check `json:"checks,omitempty"`
	// Reason says why a stage didn't run, or wasn't built.
	Reason string `json:"reason,omitempty"`
	// WallS is the stage's wall time in seconds.
	WallS float64 `json:"wall_s"`
	// Reused marks a stage whose result a resumed run took from the run it resumed.
	Reused bool `json:"reused,omitempty"`
}

// Ceiling compares a model's single-stream decode with the speed memory bandwidth allows it:
// bandwidth over the bytes one token reads, estimated as active parameters times bits per
// weight. It is recorded, never gated.
type Ceiling struct {
	// DecodeTokS is the measured decode, from the fitness check Source names.
	DecodeTokS    float64 `json:"decode_tok_s"`
	Source        string  `json:"source"`
	ActiveParamsB float64 `json:"active_params_b"`
	BitsPerWeight float64 `json:"bits_per_weight"`
	BytesPerToken float64 `json:"bytes_per_token"`
	BandwidthGBs  float64 `json:"bandwidth_gb_s"`
	// CeilingTokS is BandwidthGBs over BytesPerToken; Efficiency, DecodeTokS over it.
	CeilingTokS float64 `json:"ceiling_tok_s"`
	Efficiency  float64 `json:"efficiency"`
}

// Report is one run of evaluate's stages.
type Report struct {
	Router    string `json:"router"`
	Build     string `json:"build"`
	HostClass string `json:"host_class"`
	Set       string `json:"set"`
	// Model is the model evaluated, empty for a set run; Roles, the roles it takes, or for a set
	// run every role the set assigns.
	Model   string    `json:"model"`
	Roles   []string  `json:"roles"`
	Started time.Time `json:"started"`
	// From is the stage a resumed run started at, empty for a run from the start.
	From string `json:"from,omitempty"`
	// Reached is the last stage that ran; Passed, whether every stage that ran passed.
	Reached string        `json:"reached"`
	Passed  bool          `json:"passed"`
	Stages  []StageResult `json:"stages"`
	// Ceiling is nil when the run measured no decode, or the profile lacks the figures.
	Ceiling *Ceiling `json:"ceiling,omitempty"`
	// Warm is the run's reload and warm-up, nil for a run that stopped before it.
	Warm *Warm `json:"warm,omitempty"`
}

// Stage returns the report's result for the stage named name, or nil.
func (r *Report) Stage(name string) *StageResult {
	for i := range r.Stages {
		if r.Stages[i].Name == name {
			return &r.Stages[i]
		}
	}
	return nil
}

// ReportFile is the report's file name in its run directory.
const ReportFile = "report.json"

// Slug returns a model's router ID as a directory name: each '/' and ':' turned to '_'.
func Slug(model string) string {
	return strings.NewReplacer("/", "_", ":", "_").Replace(model)
}

// RunDir returns the run directory of a run of model started at t, under the state root:
// <state>/evaluate/<UTC time>-<slug>. A set run's model is SetSubject(set).
func RunDir(state, model string, t time.Time) string {
	return filepath.Join(state, "evaluate", t.UTC().Format("20060102T150405Z")+"-"+Slug(model))
}

// LatestRun returns the newest run directory of model under the state root that holds a report,
// or "" when there is none.
func LatestRun(state, model string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(state, "evaluate"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), "-"+Slug(model)) {
			dirs = append(dirs, e.Name())
		}
	}
	slices.Sort(dirs)
	for _, d := range slices.Backward(dirs) {
		dir := filepath.Join(state, "evaluate", d)
		if _, err := os.Stat(filepath.Join(dir, ReportFile)); err == nil {
			return dir, nil
		}
	}
	return "", nil
}

// WriteReport writes r as dir/ReportFile, making dir.
func WriteReport(dir string, r *Report) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ReportFile), append(b, '\n'), 0o600)
}

// ReadReport reads dir/ReportFile.
func ReadReport(dir string) (*Report, error) {
	b, err := os.ReadFile(filepath.Join(dir, ReportFile))
	if err != nil {
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(dir, ReportFile), err)
	}
	return &r, nil
}

// WriteSummary writes r for a reader: the run's header, then each stage and its checks, one a
// line.
func WriteSummary(w io.Writer, r *Report) error {
	var b strings.Builder
	verdict := "passed"
	if !r.Passed {
		verdict = "FAILED"
	}
	subject := r.Model
	if subject == "" {
		subject = "a set run"
	}
	fmt.Fprintf(&b, "%s on %s, build %s, set %s, roles %s\n", subject, r.Router, r.Build, r.Set, strings.Join(r.Roles, ", "))
	fmt.Fprintf(&b, "reached %s: %s\n", r.Reached, verdict)
	if w := r.Warm; w != nil {
		line := fmt.Sprintf("reload %.0fs", w.ReloadS)
		if w.Reused {
			line = "the resumed run's reload"
		}
		for _, p := range []struct {
			name string
			p    *Pool
		}{{"idle", w.Idle}, {"warmed peak", w.Peak}} {
			if p.p != nil {
				line += fmt.Sprintf(", %s %d MiB free", p.name, p.p.FreeMiB())
				if ram := p.p.RAM; ram != nil {
					line += fmt.Sprintf(" (system RAM %d MiB used, %d available)", ram.UsedMiB, ram.AvailableMiB)
				}
			}
		}
		fmt.Fprintln(&b, line)
	}
	for _, s := range r.Stages {
		line := fmt.Sprintf("%-9s %-9s", s.Name, s.Status)
		if s.Status == StatusPass || s.Status == StatusFail {
			line += fmt.Sprintf(" %6.1fs", s.WallS)
		}
		if s.Reused {
			line += " (reused)"
		}
		if s.Reason != "" {
			line += "  " + s.Reason
		}
		fmt.Fprintln(&b, strings.TrimRight(line, " "))
		for _, c := range s.Checks {
			mark := "ok  "
			if !c.Passed {
				mark = "FAIL"
			}
			name := c.Name
			if c.Role != "" {
				name = c.Role + " " + name
			}
			fmt.Fprintf(&b, "  %s %s", mark, name)
			switch {
			case c.Measured != nil && c.Floor != nil:
				fmt.Fprintf(&b, ": %s, floor %s %s", strings.TrimSpace(num(*c.Measured)+" "+c.Unit), c.Op, num(*c.Floor))
			case c.Measured != nil:
				fmt.Fprintf(&b, ": %s, no floor", strings.TrimSpace(num(*c.Measured)+" "+c.Unit))
			}
			if c.Detail != "" {
				fmt.Fprintf(&b, " (%s)", c.Detail)
			}
			b.WriteString("\n")
		}
	}
	if c := r.Ceiling; c != nil {
		fmt.Fprintf(&b, "ceiling: decode %.1f tok/s of %.1f (%.0f GB/s over %.2f GB a token): %.0f%%\n",
			c.DecodeTokS, c.CeilingTokS, c.BandwidthGBs, c.BytesPerToken/1e9, 100*c.Efficiency)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// num formats v with as many decimals as it needs, up to three.
func num(v float64) string {
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}
