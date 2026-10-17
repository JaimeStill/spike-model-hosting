package passk

import (
	"fmt"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/JaimeStill/spike-harness-driver/clutch/conform"
)

// Comparison is the row a model's consumers are judged against at pass^k: a model's own
// pass^k, on a build, each consumer's cells and invalid requests. The agent role's quality stage
// reads gpt-oss-120b's b11669 row from evaluation/comparison.toml (LoadComparison).
type Comparison struct {
	// Model, Build, Date and Source say whose row it is, where and when it ran, and where it is
	// recorded.
	Model  string `toml:"model"`
	Build  string `toml:"build"`
	Date   string `toml:"date"`
	Source string `toml:"source"`
	// K is the trials each cell ran.
	K int `toml:"k"`
	// Consumers are the row's consumers, by name (ConsumerNames).
	Consumers map[string]ComparisonRow `toml:"consumers"`
}

// ComparisonRow is one consumer's row: its passes of K in each capability and its invalid
// requests. A gated consumer must reach each cell and stay at or under the invalid count; one
// not gated is measured and recorded, never judged.
type ComparisonRow struct {
	Gated   bool           `toml:"gated"`
	Invalid int            `toml:"invalid"`
	Cells   map[string]int `toml:"cells"`
}

// LoadComparison reads the comparison file at path. A key the file doesn't define, a k under 1,
// a consumer missing or unknown, or a row that lacks a capability, names another, or holds a
// count outside [0, k] fails.
func LoadComparison(path string) (Comparison, error) {
	var c Comparison
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		return c, fmt.Errorf("comparison: %w", err)
	}
	var errs []string
	for _, k := range md.Undecoded() {
		errs = append(errs, fmt.Sprintf("%s: not a key the file defines", k))
	}
	if c.K < 1 {
		errs = append(errs, fmt.Sprintf("k = %d: at least 1", c.K))
	}
	for name := range c.Consumers {
		if !slices.Contains(ConsumerNames, name) {
			errs = append(errs, fmt.Sprintf("consumers.%s: not one of %q", name, ConsumerNames))
		}
	}
	for _, name := range ConsumerNames {
		row, ok := c.Consumers[name]
		if !ok {
			errs = append(errs, fmt.Sprintf("consumers.%s: missing", name))
			continue
		}
		if row.Invalid < 0 {
			errs = append(errs, fmt.Sprintf("consumers.%s.invalid = %d: under 0", name, row.Invalid))
		}
		for capability := range row.Cells {
			if !slices.ContainsFunc(conform.Capabilities, func(c conform.Capability) bool { return c.Name == capability }) {
				errs = append(errs, fmt.Sprintf("consumers.%s.cells.%s: not one of conform's capabilities", name, capability))
			}
		}
		for _, capability := range conform.Capabilities {
			n, ok := row.Cells[capability.Name]
			switch {
			case !ok:
				errs = append(errs, fmt.Sprintf("consumers.%s.cells.%s: missing", name, capability.Name))
			case n < 0 || n > c.K:
				errs = append(errs, fmt.Sprintf("consumers.%s.cells.%s = %d: outside [0, %d]", name, capability.Name, n, c.K))
			}
		}
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("comparison %s: %s", path, strings.Join(errs, "; "))
	}
	return c, nil
}

// Judge returns where r falls short of the consumer's row: each capability whose passes are
// under the row's, and invalid requests over the row's. It returns none for a consumer the row
// doesn't gate; a trial count other than K is the one shortfall it names, since counts out of
// another k don't compare.
func (c Comparison) Judge(r Row) []string {
	row, ok := c.Consumers[r.Consumer]
	if !ok || !row.Gated {
		return nil
	}
	if r.K != c.K {
		return []string{fmt.Sprintf("ran %d trials, not the row's %d", r.K, c.K)}
	}
	var short []string
	for _, cell := range r.Cells {
		if want := row.Cells[cell.Capability]; cell.Pass < want {
			short = append(short, fmt.Sprintf("%s %d/%d under the row's %d/%d", cell.Capability, cell.Pass, r.K, want, c.K))
		}
	}
	if r.Invalid > row.Invalid {
		short = append(short, fmt.Sprintf("%d invalid over the row's %d", r.Invalid, row.Invalid))
	}
	return short
}

// Stop is a Plan's Stop: once a gated consumer can no longer meet its row, whatever its trials
// still to run do, it returns why. A cell is out of reach when its passes and the trials left
// together are under the row's, and invalid requests, which only grow, are out of reach once
// over the row's. The bound is exact, not a confidence bound: no trial still to run can bring
// the consumer back.
func (c Comparison) Stop(trials map[string][]Trial) string {
	for _, name := range ConsumerNames {
		row, ok := c.Consumers[name]
		ts, ran := trials[name]
		if !ok || !row.Gated || !ran {
			continue
		}
		r := Tally(name, ts)
		left := c.K - len(ts)
		for _, cell := range r.Cells {
			if want := row.Cells[cell.Capability]; cell.Pass+left < want {
				return fmt.Sprintf("%s's %s can reach at most %d/%d after %d trials, under the row's %d/%d",
					name, cell.Capability, cell.Pass+left, c.K, len(ts), want, c.K)
			}
		}
		if r.Invalid > row.Invalid {
			return fmt.Sprintf("%s has %d invalid requests after %d trials, over the row's %d", name, r.Invalid, len(ts), row.Invalid)
		}
	}
	return ""
}
