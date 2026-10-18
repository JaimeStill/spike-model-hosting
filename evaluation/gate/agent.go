package gate

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/passk"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/router"
)

// AgentTier is one tier of the agent role's quality stage: a BFCL tier agent_quality.py runs, by
// its --tier name, or harness fit, conform's pass^k through the harnesses.
type AgentTier struct {
	// Name is agent_quality.py's tier, bfcl-single or bfcl-multi, or conform.
	Name string
	// Tier is the tier's place in the order, cheapest first.
	Tier int
}

// AgentTiers are the agent role's tiers, in the order they run, cheapest first: BFCL's
// single-turn questions, then its multi-turn conversations, then harness fit, conform's pass^5
// through every pinned harness (passk.Harnesses). A tier that fails ends the stage before the
// next.
var AgentTiers = []AgentTier{{"bfcl-single", 1}, {"bfcl-multi", 2}, {ConformTier, 3}}

// ConformTier is the agent tier that runs conform's pass^k through the harnesses: harness fit.
const ConformTier = "conform"

// ConformK is the trials the conform tier asks of each harness: pass^5.
const ConformK = 5

// AgentCheck is an agent tier's check name, its list's name in TiersDir and its bar's in the
// bars: agent-<tier>. The conform tier records one check a harness, agent-conform-<harness>,
// then its verdict, agent-conform.
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

// Conform runs a plan of conform trials through the harnesses: passk.Runner.
type Conform interface {
	Run(ctx context.Context, plan passk.Plan) (passk.Result, error)
}

// agentTiers runs the agent role's tiers on t's model in order, and stops at the first that
// fails.
func (r *Runner) agentTiers(ctx context.Context, rep *Report, t target) []Check {
	var checks []Check
	for _, at := range AgentTiers {
		var tierChecks []Check
		if at.Name == ConformTier {
			tierChecks = r.conformTier(ctx, rep, at)
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
	ch, judged := judgeTier(tg, name, RoleAgent, score, "accuracy", bar, hasBar, len(res.Stopped) > 0, n, res.StopReason)
	notes = append(notes, judged)
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

// ConformReport is the conform tier's table in its run directory's conform/: the model, the
// build, each harness's cells and fit, and every trial that didn't pass.
const ConformReport = "report.md"

// conformTier runs harness fit: conform's pass^5 through every pinned harness (passk.Harnesses)
// on the set's roles, the chat model at its consumer reasoning setting, each trial's recording
// kept in the run directory's conform/wire and the table in conform/report.md. Every harness is
// held to one absolute bar (passk.Judge): each applicable capability 5/5 and 0 invalid
// requests, a capability it reports n/a excluded and noted. A harness drops at its first failed
// trial, and the tier ends once every harness has dropped or run all five. Each harness's fit is
// recorded, never failing on its own; the tier's verdict, agent-conform, fails only when no
// harness fits. Every model of the set must be loaded, since the capabilities send images and
// audio to the set's other models.
func (r *Runner) conformTier(ctx context.Context, rep *Report, t AgentTier) []Check {
	name := AgentCheck(ConformTier)
	if r.Conform == nil || r.Dir == "" {
		return []Check{broken(name, RoleAgent, errors.New("the runner has no conform runner or run directory"))}
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
	plan := passk.Plan{
		Router: r.Router, Roles: r.Set.Roles, K: ConformK, Harnesses: passk.Harnesses,
		Wire: filepath.Join(dir, "wire"), Drop: true,
	}
	if m := r.model(); m != nil {
		plan.Reasoning = m.Consumers
	}
	began := time.Now()
	res, err := r.Conform.Run(ctx, plan)
	wall := time.Since(began).Seconds()
	if err == nil {
		err = writeConform(filepath.Join(dir, ConformReport), res, rep)
	}
	if err != nil {
		ch := broken(name, RoleAgent, err)
		ch.Values = map[string]float64{"wall_s": wall}
		return []Check{ch}
	}

	var checks []Check
	var fit, unfit []string
	for _, f := range res.Fits {
		checks = append(checks, harnessFit(f, res.Wall[f.Row.Consumer], t.Tier))
		if f.Fits {
			fit = append(fit, f.Row.Consumer)
		} else {
			unfit = append(unfit, f.Row.Consumer)
		}
	}
	verdict := measure(name, RoleAgent, float64(len(fit)), "harnesses fit", AtLeast, 1)
	verdict.Values = map[string]float64{"wall_s": wall, "tier": float64(t.Tier), "harnesses": float64(len(res.Fits))}
	verdict.Detail = fmt.Sprintf("tier %d; harness fit at pass^%d, model %s on %s; fit: %s; does not fit: %s",
		t.Tier, ConformK, rep.Model, rep.Build, orNone(fit), orNone(unfit))
	return append(checks, verdict)
}

// harnessFit is a harness's record in the conform tier: whether it fits, the trials it ran, each
// cell's passes over them, its invalid requests, the capabilities it excluded, why its first
// failed trial failed, and its wall time. It is recorded, passed, whether or not the harness
// fits: the tier's verdict judges the harnesses together.
func harnessFit(f passk.Fit, wall time.Duration, tierN int) Check {
	applicable := float64(f.Applicable())
	ch := Check{
		Name: AgentCheck(ConformTier + "-" + f.Row.Consumer), Role: RoleAgent,
		Measured: &applicable, Unit: fmt.Sprintf("applicable capabilities at pass^%d", f.K), Passed: true,
	}
	fits := 0.0
	if f.Fits {
		fits = 1
	}
	ch.Values = map[string]float64{
		"fit": fits, "trials": float64(f.Row.K), "invalid": float64(f.Row.Invalid),
		"wall_s": wall.Seconds(), "tier": float64(tierN),
	}
	var cells []string
	for _, c := range f.Row.Cells {
		ch.Values["cell_"+c.Capability] = float64(c.Pass)
		cell := fmt.Sprintf("%s %d/%d", c.Capability, c.Pass, f.Row.K)
		if slices.Contains(f.Excluded, c.Capability) {
			cell += " n/a"
		}
		cells = append(cells, cell)
	}
	ch.Detail = strings.Join([]string{
		fmt.Sprintf("tier %d", tierN), f.Verdict(),
		fmt.Sprintf("%d trials of %d, %d invalid", f.Row.K, f.K, f.Row.Invalid),
		strings.Join(cells, ", "), "version " + cmp.Or(f.Row.Version, "-"),
		"recorded: the tier fails only when no harness fits",
	}, "; ")
	return ch
}

// writeConform writes the conform tier's table to path: the model and build it ran on, each
// harness's cells and fit, each harness's verdict and wall time, and why each trial that didn't
// pass didn't.
func writeConform(path string, res passk.Result, rep *Report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	var walls []string
	for _, fit := range res.Fits {
		walls = append(walls, fmt.Sprintf("%s %s", fit.Row.Consumer, res.Wall[fit.Row.Consumer].Round(time.Second)))
	}
	_, err = fmt.Fprintf(f, "harness fit, conform pass^%d: model %s, build %s, set %s; %s (%s)\n\n",
		ConformK, rep.Model, rep.Build, rep.Set, res.Elapsed.Round(time.Second), strings.Join(walls, ", "))
	if err == nil {
		err = passk.WriteTable(f, res.Fits)
	}
	if err == nil {
		_, err = fmt.Fprintln(f)
	}
	if err == nil {
		err = passk.WriteFits(f, res.Fits)
	}
	if err == nil {
		err = passk.WriteReasons(f, res.Rows, res.Trials)
	}
	return errors.Join(err, f.Close())
}

// orNone joins names, or says none.
func orNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
