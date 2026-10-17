package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// AgentTier is one tier of the agent role's quality stage: a BFCL tier agent_quality.py runs, by
// its --tier name, or conform's pass^k through the consumers.
type AgentTier struct {
	// Name is agent_quality.py's tier, bfcl-single or bfcl-multi, or conform.
	Name string
	// Tier is the tier's place in the order, cheapest first.
	Tier int
}

// AgentTiers are the agent role's tiers, in the order they run, cheapest first: BFCL's
// single-turn questions, then its multi-turn conversations, then conform's pass^5 through Pi,
// Claude Code and OpenCode. A tier that fails ends the stage before the next.
var AgentTiers = []AgentTier{{"bfcl-single", 1}, {"bfcl-multi", 2}, {ConformTier, 3}}

// ConformTier is the agent tier that runs conform's pass^k through the consumers.
const ConformTier = "conform"

// AgentCheck is an agent tier's check name, its list's name in TiersDir and its bar's in the
// bars: agent-<tier>. The conform tier's checks are one a consumer, agent-conform-<consumer>.
func AgentCheck(tierName string) string { return "agent-" + tierName }

// Agent runs one BFCL tier on a router's agent model, the tier list at list, and leaves Inspect
// AI's JSON log in out (ReadTier). A bar, a floor, stops the tier early once its pass rate
// can't reach it (pass_stop.py's PassStop); nil runs the whole tier.
type Agent interface {
	Agent(ctx context.Context, router, model, tierName, list, out string, bar *float64) error
}

// Agent runs the tier through agent_quality.py eval, and fails when uv does.
func (u UV) Agent(ctx context.Context, router, model, tierName, list, out string, bar *float64) error {
	args := []string{"eval", "--router", router, "--model", model, "--tier", tierName, "--list", list, "--output", out}
	if bar != nil {
		args = append(args, "--bar", strconv.FormatFloat(*bar, 'g', -1, 64))
	}
	if err := u.run(ctx, "agent_quality.py", args...); err != nil {
		return fmt.Errorf("uv run agent_quality.py eval --tier %s: %w", tierName, err)
	}
	return nil
}

// Conform runs a plan of conform trials through the consumers: passk.Runner.
type Conform interface {
	Run(ctx context.Context, plan passk.Plan) (passk.Result, error)
}

// agentTiers runs the agent role's tiers on t's model in order, and stops at the first that
// fails.
func (r *Runner) agentTiers(ctx context.Context, t target) []Check {
	var checks []Check
	for _, at := range AgentTiers {
		var tierChecks []Check
		if at.Name == ConformTier {
			tierChecks = r.conformTier(ctx, at)
		} else {
			tierChecks = []Check{r.bfclTier(ctx, t, at)}
		}
		checks = append(checks, tierChecks...)
		for _, c := range tierChecks {
			if !c.Passed {
				return checks
			}
		}
		if ctx.Err() != nil {
			return checks
		}
	}
	return checks
}

// bfclTier runs one BFCL tier on t's model into t's agent/<tier>, and, when t is judged, judges
// it; otherwise it runs whole and is recorded, passed. Its accuracy is judged against its floor, by check name (agent-<tier>), under [bars]; the floor also stops
// it early (its screening fifth, then a 95% Wilson upper bound on the pass rate under the
// floor); without one it runs whole and its accuracy is recorded. The check records the samples
// run and passed, their model requests, their working time, the wall time, the data's commit,
// the task's inspect_evals version and Inspect's release.
func (r *Runner) bfclTier(ctx context.Context, tg target, t AgentTier) Check {
	name := AgentCheck(t.Name)
	if r.Agent == nil || tg.dir == "" || r.Tiers == "" {
		return broken(name, RoleAgent, errors.New("the runner has no agent runner, run directory or tiers directory"))
	}
	var floor *float64
	bar, hasBar := r.bar(tg, name)
	switch {
	case hasBar && bar.AtMost:
		return broken(name, RoleAgent, errors.New("an accuracy is higher-is-better, so its bar belongs under [bars], not [bars_at_most]"))
	case hasBar:
		floor = &bar.Value
	}
	dir := filepath.Join(tg.dir, "agent", t.Name)
	out := filepath.Join(dir, "logs")
	// A rerun into the same run directory (a resumed run) must not read the last run's logs.
	if err := os.RemoveAll(dir); err != nil {
		return broken(name, RoleAgent, err)
	}
	listPath := filepath.Join(r.Tiers, name+".txt")
	list, err := tier.ReadListFile(listPath)
	if err != nil {
		return broken(name, RoleAgent, err)
	}
	began := time.Now()
	err = r.Agent.Agent(ctx, r.Router, tg.model, t.Name, listPath, out, floor)
	wall := time.Since(began).Seconds()
	var res TierResult
	if err == nil {
		res, err = ReadTier(out)
	}
	if err == nil {
		err = res.matches(t.Name, &list)
	}
	if err != nil {
		ch := broken(name, RoleAgent, err)
		ch.Values = map[string]float64{"wall_s": wall}
		return ch
	}

	score, passes, n := res.Score(), res.Passes(), len(res.IDs)
	notes := []string{fmt.Sprintf("tier %d", t.Tier), fmt.Sprintf("%d of %d samples, %d passed, %d model requests", n, len(list.IDs), passes, res.Requests)}
	var ch Check
	switch {
	case !hasBar:
		ch = Check{Name: name, Role: RoleAgent, Measured: &score, Unit: "accuracy", Passed: true}
		notes = append(notes, tg.unjudged("no bar: recorded, never fails; early stop needs a bar"))
	case len(res.Stopped) > 0:
		ch = measure(name, RoleAgent, score, "accuracy", AtLeast, bar.Value)
		ch.Passed = false
		notes = append(notes, fmt.Sprintf("early stop after %d: %s", n, res.StopReason))
	default:
		ch = measure(name, RoleAgent, score, "accuracy", AtLeast, bar.Value)
		notes = append(notes, "early stop: ran in full")
	}
	ch.Values = map[string]float64{
		"wall_s": wall, "working_s": res.WorkingS, "requests": float64(res.Requests),
		"samples": float64(n), "passes": float64(passes), "tier": float64(t.Tier),
	}
	if res.Upper != nil {
		ch.Values["pass_rate_upper_bound"] = *res.Upper
	}
	notes = append(notes, "the router's defaults, parallel tool calls allowed", "dataset "+res.Dataset,
		"inspect_evals "+res.InspectEvals+" "+strings.Join(res.Tasks, ", "), "inspect "+res.Inspect)
	ch.Detail = strings.Join(notes, "; ")
	return ch
}

// ConformReport is the conform tier's table in its run directory's conform/: the build, the
// comparison row, the consumers' cells and every trial that didn't pass.
const ConformReport = "report.md"

// conformTier runs conform's pass^k through Pi, Claude Code and OpenCode on the set's roles, the
// chat model at its consumer reasoning setting, each trial's recording kept in the run
// directory's conform/wire and the table in conform/report.md, and judges each consumer against
// the comparison row (r.Comparison): a gated consumer must reach each of the row's cells and stay
// at or under its invalid requests; one not gated is recorded. Every consumer runs its first
// trial, the screening fifth at k = 5; after that the tier stops once a gated consumer can no
// longer meet its row (passk.Comparison.Stop). Every model of the set must be loaded, since the
// capabilities send images and audio to the set's other models.
func (r *Runner) conformTier(ctx context.Context, t AgentTier) []Check {
	name := AgentCheck(ConformTier)
	if r.Conform == nil || r.Comparison == nil || r.Dir == "" {
		return []Check{broken(name, RoleAgent, errors.New("the runner has no conform runner, comparison row or run directory"))}
	}
	if err := router.CheckLoaded(ctx, r.HTTP, r.Router, r.Set.Models...); err != nil {
		return []Check{broken(name, RoleAgent, err)}
	}
	dir := filepath.Join(r.Dir, "conform")
	// A rerun into the same run directory (a resumed run) must not keep the last run's
	// recordings.
	if err := os.RemoveAll(dir); err != nil {
		return []Check{broken(name, RoleAgent, err)}
	}
	cmp := r.Comparison
	plan := passk.Plan{
		Router: r.Router, Roles: r.Set.Roles, K: cmp.K, Consumers: passk.ConsumerNames,
		Wire: filepath.Join(dir, "wire"), Stop: cmp.Stop,
	}
	if m := r.model(); m != nil {
		plan.Reasoning = m.Consumers
	}
	began := time.Now()
	res, err := r.Conform.Run(ctx, plan)
	wall := time.Since(began).Seconds()
	if err == nil {
		err = writeConform(filepath.Join(dir, ConformReport), res, cmp)
	}
	if err != nil {
		ch := broken(name, RoleAgent, err)
		ch.Values = map[string]float64{"wall_s": wall}
		return []Check{ch}
	}

	var checks []Check
	for _, row := range res.Rows {
		checks = append(checks, judgeConsumer(row, res, cmp, t.Tier, wall))
	}
	return checks
}

// judgeConsumer is a consumer's conform check: its capabilities passed at k against the row's,
// judged cell by cell for a gated consumer, recorded for one not gated. A gated consumer whose
// trials the tier stopped short of k fails, whether it was the one out of reach or not: its
// evidence is incomplete.
func judgeConsumer(row passk.Row, res passk.Result, cmp *passk.Comparison, tierN int, wall float64) Check {
	name := AgentCheck(ConformTier + "-" + row.Consumer)
	want := cmp.Consumers[row.Consumer]
	rowPassed := 0
	for _, n := range want.Cells {
		if n == cmp.K {
			rowPassed++
		}
	}
	unit := fmt.Sprintf("capabilities at pass^%d", cmp.K)
	notes := []string{fmt.Sprintf("tier %d", tierN), fmt.Sprintf("%d trials of %d, %d invalid", row.K, cmp.K, row.Invalid)}
	var ch Check
	if want.Gated {
		ch = measure(name, RoleAgent, float64(row.Passed()), unit, AtLeast, float64(rowPassed))
		short := cmp.Judge(row)
		ch.Passed = len(short) == 0
		switch {
		case res.Stopped != "" && row.K < cmp.K:
			ch.Passed = false
			notes = append(notes, "stopped early: "+res.Stopped)
		case len(short) > 0:
			notes = append(notes, "under the row: "+strings.Join(short, ", "))
		default:
			notes = append(notes, "each cell at or above the row's")
		}
	} else {
		passed := float64(row.Passed())
		ch = Check{Name: name, Role: RoleAgent, Measured: &passed, Unit: unit, Passed: true}
		notes = append(notes, fmt.Sprintf("measured, not gated: the row's %d at pass^%d with %d invalid beside it", rowPassed, cmp.K, want.Invalid))
	}
	var cells []string
	ch.Values = map[string]float64{"wall_s": wall, "trials": float64(row.K), "invalid": float64(row.Invalid), "tier": float64(tierN)}
	for _, c := range row.Cells {
		ch.Values["cell_"+c.Capability] = float64(c.Pass)
		cells = append(cells, fmt.Sprintf("%s %d/%d (row %d/%d)", c.Capability, c.Pass, row.K, want.Cells[c.Capability], cmp.K))
	}
	notes = append(notes, strings.Join(cells, ", "),
		fmt.Sprintf("comparison row %s on %s, %s", cmp.Model, cmp.Build, cmp.Date), "version "+row.Version)
	ch.Detail = strings.Join(notes, "; ")
	return ch
}

// writeConform writes the conform tier's table to path: the comparison row it was judged by,
// whether it stopped early, the consumers' cells, and why each trial that didn't pass didn't.
func writeConform(path string, res passk.Result, cmp *passk.Comparison) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "conform pass^%d, %s, judged against %s's row on %s (%s)\n", cmp.K, res.Elapsed.Round(time.Second), cmp.Model, cmp.Build, cmp.Source)
	if err == nil && res.Stopped != "" {
		_, err = fmt.Fprintf(f, "stopped early: %s\n", res.Stopped)
	}
	if err == nil {
		_, err = fmt.Fprintln(f)
	}
	if err == nil {
		err = passk.WriteTable(f, res.Rows)
	}
	if err == nil {
		err = passk.WriteReasons(f, res.Rows, res.Trials)
	}
	return errors.Join(err, f.Close())
}
