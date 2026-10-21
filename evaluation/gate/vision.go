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

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// VisionTier is one tier of the vision role's quality stage: a part the suites project's
// vision_quality.py runs, by its --tier name.
type VisionTier struct {
	// Name is vision_quality.py's tier: probe, or a benchmark, vstar, docvqa or mmmu.
	Name string
	// Tier is the tier's place in the order, cheapest first; the probe and V*Bench share the
	// first.
	Tier int
	// Metric is what the tier's score is: the share of answers correct, or DocVQA's mean ANLS.
	Metric string
}

// VisionTiers are the vision role's tiers, in the order they run, cheapest first: the probe's
// image five times and V*Bench, then DocVQA, then MMMU. A tier that fails ends the stage before
// the next.
var VisionTiers = []VisionTier{
	{"probe", 1, "accuracy"},
	{"vstar", 1, "accuracy"},
	{"docvqa", 2, "ANLS"},
	{"mmmu", 3, "accuracy"},
}

// VisionCheck is a vision tier's check name, its list's name in TiersDir and its bar's in the
// bars: vision-<tier>.
func VisionCheck(tierName string) string { return "vision-" + tierName }

// probeRepeats is how many times the probe tier asks its question; it passes on all of them.
const probeRepeats = 5

// InspectEvalsVersion is the inspect_evals release the suites project pins, whose stock tasks
// the vision tiers run; a log written under another fails its check.
var InspectEvalsVersion = mustPin("inspect-evals")

// Vision runs one vision tier on a router's vision model, slots requests in flight (the model's
// profile slots, Runner.slotsOf), and leaves Inspect AI's JSON logs, one a task, in out (ReadTier).
// input is the tier list for a benchmark, or the probe's image for the probe. A bar, a floor, stops
// a benchmark early once its pass rate can't reach it (vision_quality.py's PassStop); nil runs the
// whole tier.
type Vision interface {
	Vision(ctx context.Context, router, model, tierName, input, out string, slots int, bar *float64) error
}

// Vision runs the tier through vision_quality.py eval, slots as its --slots, and fails when uv
// does.
func (u UV) Vision(ctx context.Context, router, model, tierName, input, out string, slots int, bar *float64) error {
	args := []string{"eval", "--router", router, "--model", model, "--tier", tierName, "--output", out,
		"--slots", strconv.Itoa(slots)}
	if tierName == "probe" {
		args = append(args, "--image", input)
	} else {
		args = append(args, "--list", input)
	}
	if bar != nil {
		args = append(args, "--bar", strconv.FormatFloat(*bar, 'g', -1, 64))
	}
	if err := u.run(ctx, "vision_quality.py", args...); err != nil {
		return fmt.Errorf("uv run vision_quality.py eval --tier %s: %w", tierName, err)
	}
	return nil
}

// cardScore is a figure a model's card reports beside a vision tier: recorded with the tier's
// score, labelled, and never judged.
type cardScore struct {
	value float64
	label string
}

// cardScores are the card figures, by the card's URL (the profile's card_url) and tier.
var cardScores = map[string]map[string]cardScore{
	"https://huggingface.co/google/gemma-4-26B-A4B-it": {
		"mmmu": {0.738, `the card's "MMMU Pro" 73.8% (google/gemma-4-26B-A4B-it), a harder variant (ten options, ` +
			`filtered questions), not MMMU validation: ours should land near or above it, not far below`},
	},
}

// visionTiers runs the vision role's tiers on t's model in order, each reused from the index when
// the run may reuse its passed record (unit), and stops at the first that fails.
func (r *Runner) visionTiers(ctx context.Context, t target) []Check {
	var checks []Check
	for _, vt := range VisionTiers {
		tierChecks := r.unit(t, RoleVision, VisionCheck(vt.Name), func() []Check { return []Check{r.visionTier(ctx, t, vt)} })
		checks = append(checks, tierChecks...)
		if !passed(tierChecks) || ctx.Err() != nil {
			break
		}
	}
	return checks
}

// visionTier runs one vision tier on t's model into t's vision/<tier>, and judges it when t is
// judged; otherwise it runs whole and is recorded, passed. The probe passes on all of its repeats,
// pass^5, whatever the bars hold. A benchmark's score is judged against its floor, by check name
// (vision-<tier>), under [bars]; in a screen the floor also stops it early on either side (its
// screening fifth, then the 95% Wilson upper bound on the pass rate under the floor, failed, or
// the lower bound on the mean score at or over it, passed); without one it runs whole and its
// score is recorded. A full run reuses the screen's record of a tier it ran whole, and runs the
// rest of one it stopped, from vision/<tier>/rest, judging the whole list. The check records the
// samples run and passed, the request time, the wall time, the dataset's revision, the tasks'
// inspect_evals versions and Inspect's release, and the card's figure beside a tier the card
// reports.
func (r *Runner) visionTier(ctx context.Context, tg target, t VisionTier) Check {
	name := VisionCheck(t.Name)
	if r.Vision == nil || tg.dir == "" || r.Tiers == "" {
		return broken(name, RoleVision, errors.New("the runner has no vision runner, run directory or tiers directory"))
	}
	probe := t.Name == "probe"
	var floor *float64
	bar, hasBar := r.bar(tg, name)
	switch {
	case probe:
		hasBar = false
	case hasBar && bar.AtMost:
		return broken(name, RoleVision, fmt.Errorf("a %s is higher-is-better, so its bar belongs under [bars], not [bars_at_most]", t.Metric))
	case hasBar:
		floor = &bar.Value
	}
	var screen Check
	if tg.full {
		c, ok := tg.screenCheck(RoleVision, name)
		switch {
		case !ok || !c.Passed:
			return broken(name, RoleVision, errNoScreen(name))
		case probe || c.Stop == nil:
			return reused(c)
		}
		screen = c
	}
	dir := filepath.Join(tg.dir, "vision", t.Name)
	out := filepath.Join(dir, "logs")
	// A rerun into the same run directory (a resumed run) must not read the last run's logs; a
	// full run reads the screen's.
	if !tg.full {
		if err := os.RemoveAll(dir); err != nil {
			return broken(name, RoleVision, err)
		}
	}
	var list *tier.List
	var input string
	if probe {
		input = filepath.Join(dir, "shapes.png")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return broken(name, RoleVision, err)
		}
		if err := os.WriteFile(input, r.Media.PNG, 0o600); err != nil {
			return broken(name, RoleVision, err)
		}
	} else {
		input = filepath.Join(r.Tiers, name+".txt")
		l, err := tier.ReadListFile(input)
		if err != nil {
			return broken(name, RoleVision, err)
		}
		list = &l
	}
	began := time.Now()
	var res TierResult
	var err error
	if tg.full {
		var screenOut string
		if screenOut, err = tg.screenDir(screen, filepath.Join("vision", t.Name, "logs")); err == nil {
			res, err = r.restTier(t.Name, *list, screenOut, filepath.Join(dir, restDir), func(listPath, restOut string) error {
				return r.Vision.Vision(ctx, r.Router, tg.model, t.Name, listPath, restOut, r.slotsOf(tg.model), nil)
			})
		}
	} else {
		err = r.Vision.Vision(ctx, r.Router, tg.model, t.Name, input, out, r.slotsOf(tg.model), floor)
		if err == nil {
			res, err = ReadTier(out)
		}
		if err == nil {
			err = res.matches(t.Name, list)
		}
	}
	wall := time.Since(began).Seconds() + screen.Values["wall_s"]
	if err != nil {
		ch := broken(name, RoleVision, err)
		ch.Values = map[string]float64{"wall_s": wall}
		return ch
	}

	score, passes, n := res.Score(), res.Passes(), len(res.IDs)
	size := probeRepeats
	if list != nil {
		size = len(list.IDs)
	}
	notes := []string{fmt.Sprintf("tier %d", t.Tier), fmt.Sprintf("%d of %d samples, %d passed", n, size, passes)}
	if t.Metric == "ANLS" {
		notes = append(notes, fmt.Sprintf("mean ANLS %.3f; a sample passes at ANLS >= 0.5, pass rate %.3f, the rate the fail side's bound bounds", score, float64(passes)/float64(n)))
	}
	var ch Check
	switch {
	case probe && !tg.judged:
		ch = Check{Name: name, Role: RoleVision, Measured: &score, Unit: t.Metric, Passed: true}
		notes = append(notes, tg.unjudged(""))
	case probe:
		ch = fact(name, RoleVision, "", passes == probeRepeats)
		ch.Measured, ch.Unit = &score, t.Metric
		notes = append(notes, fmt.Sprintf("pass^%d: every repeat must name the three shapes and colours, at the router's defaults", probeRepeats))
	default:
		var judged string
		ch, judged = judgeTier(tg, name, RoleVision, score, t.Metric, bar, hasBar, tierStop(res, size))
		notes = append(notes, judged)
	}
	ch.Values = map[string]float64{
		"wall_s": wall, "request_s": res.RequestS, "samples": float64(n), "passes": float64(passes), "tier": float64(t.Tier),
	}
	boundValues(ch.Values, res)
	if tg.full {
		notes = append(notes, continued(ch.Values, screen))
	}
	if m := r.Profile.Model(tg.model); m != nil {
		if card, ok := cardScores[m.CardURL][t.Name]; ok {
			ch.Values["card_score"] = card.value
			notes = append(notes, fmt.Sprintf("card: %s; ours %.3f", card.label, score))
		}
	}
	if !probe {
		notes = append(notes, "temperature 0, reasoning off", "dataset "+res.Dataset, "inspect_evals "+res.InspectEvals+" "+strings.Join(res.Tasks, ", "))
	}
	notes = append(notes, "inspect "+res.Inspect)
	ch.Detail = strings.Join(notes, "; ")
	return ch
}
