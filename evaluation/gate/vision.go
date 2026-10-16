package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// Vision runs one vision tier on a router's vision model and leaves Inspect AI's JSON logs, one a
// task, in out (ReadVision). input is the tier list for a benchmark, or the probe's image for the
// probe. A bar, a floor, stops a benchmark early once its pass rate can't reach it
// (vision_quality.py's PassStop); nil runs the whole tier.
type Vision interface {
	Vision(ctx context.Context, router, model, tierName, input, out string, bar *float64) error
}

// Vision runs the tier through vision_quality.py eval, and fails when uv does.
func (u UV) Vision(ctx context.Context, router, model, tierName, input, out string, bar *float64) error {
	args := []string{"eval", "--router", router, "--model", model, "--tier", tierName, "--output", out}
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

// sampleID is an Inspect sample's ID, a string or a number in the log.
type sampleID string

func (s *sampleID) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v := v.(type) {
	case string:
		*s = sampleID(v)
	case float64:
		*s = sampleID(strconv.FormatFloat(v, 'f', -1, 64))
	default:
		return fmt.Errorf("a sample ID %s is neither a string nor a number", b)
	}
	return nil
}

// visionPassAt is the score at which a sample passes: a correct choice (1), or an ANLS of 0.5 or
// more, which ANLS's own threshold makes any ANLS above 0. vision_quality.py's PASS_AT.
const visionPassAt = 0.5

// VisionResult is one vision tier's run, read from its Inspect AI JSON logs, one a task.
type VisionResult struct {
	// Tier and Dataset are the run's metadata: the tier, and the benchmark's repository,
	// revision and split. Inspect and InspectEvals are the releases that wrote the logs (no
	// inspect_evals for the probe). Tasks are the logs' tasks, each with its inspect_evals
	// version, task@version.
	Tier, Dataset, Inspect, InspectEvals string
	Tasks                                []string
	// IDs are the samples scored, task by task in the logs' order, and Scores each one's score:
	// 1 or 0 for a choice, its ANLS for DocVQA.
	IDs    []string
	Scores []float64
	// RequestS is the time of the scored samples' requests, summed: Inspect's ModelOutput.time.
	RequestS float64
	// Stopped are the samples early stopping skipped, and StopReason why; Upper is the 95% upper
	// bound on the pass rate when the run ended. All three come with a bar only.
	Stopped    []string
	StopReason string
	Upper      *float64
}

// Score is the mean of the samples' scores: the accuracy, or the mean ANLS.
func (v VisionResult) Score() float64 {
	sum := 0.0
	for _, s := range v.Scores {
		sum += s
	}
	return sum / float64(len(v.Scores))
}

// Passes counts the samples that passed, at a score of 0.5 or more.
func (v VisionResult) Passes() int {
	n := 0
	for _, s := range v.Scores {
		if s >= visionPassAt {
			n++
		}
	}
	return n
}

// earlyStopFile is the account of a tier's early stop vision_quality.py writes beside its logs
// when it runs with a bar: the samples stopped, why, and the bound when the tier ended. Inspect
// logs a task whose every sample stopped with neither samples nor early stops, so the logs alone
// can't account for them.
const earlyStopFile = "early_stop.json"

// ReadVision reads the Inspect JSON logs vision_quality.py left in out, one a task, in their
// names' order (their start times'), and its early stop account, when there is one. A log of a
// run that didn't succeed, with a sample that errored or went unscored, or of another tier than
// the others', fails.
func ReadVision(out string) (VisionResult, error) {
	var res VisionResult
	entries, err := os.ReadDir(out)
	if err != nil {
		return res, err
	}
	var logs []string
	for _, e := range entries {
		if inspectLogName.MatchString(e.Name()) {
			logs = append(logs, e.Name())
		}
	}
	if len(logs) == 0 {
		return res, fmt.Errorf("%s holds no Inspect log", out)
	}
	for i, name := range logs {
		var raw struct {
			Status string `json:"status"`
			Eval   struct {
				Task     string            `json:"task"`
				Packages map[string]string `json:"packages"`
				Metadata struct {
					Tier, Dataset   string
					FullTaskVersion string `json:"full_task_version"`
				} `json:"metadata"`
			} `json:"eval"`
			Samples []struct {
				ID     sampleID `json:"id"`
				Output struct {
					Time *float64 `json:"time"`
				} `json:"output"`
				Scores map[string]struct {
					Value json.RawMessage `json:"value"`
				} `json:"scores"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"samples"`
		}
		path := filepath.Join(out, name)
		if err := readJSON(path, &raw, true); err != nil {
			return res, err
		}
		if raw.Status != "success" {
			return res, fmt.Errorf("%s: the run's status is %q", path, raw.Status)
		}
		m := raw.Eval.Metadata
		if i == 0 {
			res.Tier, res.Dataset = m.Tier, m.Dataset
			res.Inspect, res.InspectEvals = raw.Eval.Packages["inspect_ai"], raw.Eval.Packages["inspect_evals"]
		} else if m.Tier != res.Tier || m.Dataset != res.Dataset {
			return res, fmt.Errorf("%s: tier %q of %q beside tier %q of %q", path, m.Tier, m.Dataset, res.Tier, res.Dataset)
		}
		task := raw.Eval.Task
		if m.FullTaskVersion != "" {
			task += "@" + m.FullTaskVersion
		}
		res.Tasks = append(res.Tasks, task)
		for _, s := range raw.Samples {
			if s.Error != nil {
				return res, fmt.Errorf("%s: sample %s errored: %s", path, s.ID, s.Error.Message)
			}
			if len(s.Scores) != 1 {
				return res, fmt.Errorf("%s: sample %s has %d scores; want one", path, s.ID, len(s.Scores))
			}
			var score float64
			for _, sc := range s.Scores {
				if score, err = scoreValue(sc.Value); err != nil {
					return res, fmt.Errorf("%s: sample %s: %w", path, s.ID, err)
				}
			}
			if s.Output.Time == nil {
				return res, fmt.Errorf("%s: sample %s has no request time", path, s.ID)
			}
			res.IDs = append(res.IDs, string(s.ID))
			res.Scores = append(res.Scores, score)
			res.RequestS += *s.Output.Time
		}
	}
	var stop struct {
		Stopped    []string `json:"stopped"`
		Reason     string   `json:"reason"`
		UpperBound *float64 `json:"upper_bound"`
	}
	switch err := readJSON(filepath.Join(out, earlyStopFile), &stop, true); {
	case err == nil:
		res.Stopped, res.StopReason, res.Upper = stop.Stopped, stop.Reason, stop.UpperBound
	case !errors.Is(err, os.ErrNotExist):
		return res, err
	}
	if len(res.Scores) == 0 {
		return res, fmt.Errorf("%s: no sample scored", out)
	}
	return res, nil
}

// scoreValue reads an Inspect score's value: choice()'s "C" and "I" as 1 and 0, a number as it
// is.
func scoreValue(raw json.RawMessage) (float64, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	switch v := v.(type) {
	case float64:
		return v, nil
	case string:
		switch v {
		case "C":
			return 1, nil
		case "I":
			return 0, nil
		}
	}
	return 0, fmt.Errorf("the score %s is neither C, I nor a number", raw)
}

// matches checks the run is the tier as its list holds it: the pinned Inspect's and
// inspect_evals', the tier's, drawn from the list's dataset, and each of the list's samples
// either scored or stopped early, none else. The probe has no list: its samples are its
// repeats.
func (v VisionResult) matches(tierName string, list *tier.List) error {
	switch {
	case v.Inspect != InspectVersion:
		return fmt.Errorf("the logs are Inspect %s's, not the pinned %s's", v.Inspect, InspectVersion)
	case v.Tier != tierName:
		return fmt.Errorf("the logs are tier %q's, not %q's", v.Tier, tierName)
	}
	var want []string
	if list == nil {
		for i := range probeRepeats {
			want = append(want, fmt.Sprintf("probe-%d", i+1))
		}
	} else {
		switch {
		case v.InspectEvals != InspectEvalsVersion:
			return fmt.Errorf("the logs are inspect_evals %s's, not the pinned %s's", v.InspectEvals, InspectEvalsVersion)
		case v.Dataset != list.Dataset:
			return fmt.Errorf("the logs' dataset %q isn't the tier list's %q", v.Dataset, list.Dataset)
		}
		want = list.IDs
	}
	ran := slices.Concat(v.IDs, v.Stopped)
	slices.Sort(ran)
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(ran, want) {
		return fmt.Errorf("the logs' %d scored and %d stopped samples aren't the tier's %d", len(v.IDs), len(v.Stopped), len(want))
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

// visionTiers runs the vision role's tiers in order, and stops at the first that fails.
func (r *Runner) visionTiers(ctx context.Context) []Check {
	var checks []Check
	for _, t := range VisionTiers {
		ch := r.visionTier(ctx, t)
		checks = append(checks, ch)
		if !ch.Passed || ctx.Err() != nil {
			break
		}
	}
	return checks
}

// visionTier runs one vision tier into the run directory's vision/<tier>, and judges it. The
// probe passes on all of its repeats, pass^5, whatever the bars hold. A benchmark's score is
// judged against its floor, by check name (vision-<tier>), under [bars]; the floor also stops it
// early (its screening fifth, then a 95% Wilson upper bound on the pass rate under the floor);
// without one it runs whole and its score is recorded. The check records the samples run and
// passed, the request time, the wall time, the dataset's revision, the tasks' inspect_evals
// versions and Inspect's release, and the card's figure beside a tier the card reports.
func (r *Runner) visionTier(ctx context.Context, t VisionTier) Check {
	name := VisionCheck(t.Name)
	if r.Vision == nil || r.Dir == "" || r.Tiers == "" {
		return broken(name, RoleVision, errors.New("the runner has no vision runner, run directory or tiers directory"))
	}
	probe := t.Name == "probe"
	var floor *float64
	bar, hasBar := r.Bars.Bar(name)
	switch {
	case probe:
		hasBar = false
	case hasBar && bar.AtMost:
		return broken(name, RoleVision, fmt.Errorf("a %s is higher-is-better, so its bar belongs under [bars], not [bars_at_most]", t.Metric))
	case hasBar:
		floor = &bar.Value
	}
	dir := filepath.Join(r.Dir, "vision", t.Name)
	out := filepath.Join(dir, "logs")
	// A rerun into the same run directory (a resumed run) must not read the last run's logs.
	if err := os.RemoveAll(dir); err != nil {
		return broken(name, RoleVision, err)
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
	err := r.Vision.Vision(ctx, r.Router, r.Model, t.Name, input, out, floor)
	wall := time.Since(began).Seconds()
	var res VisionResult
	if err == nil {
		res, err = ReadVision(out)
	}
	if err == nil {
		err = res.matches(t.Name, list)
	}
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
		notes = append(notes, fmt.Sprintf("mean ANLS %.3f; a sample passes at ANLS >= 0.5, pass rate %.3f, the rate the early stop bounds", score, float64(passes)/float64(n)))
	}
	var ch Check
	switch {
	case probe:
		ch = fact(name, RoleVision, "", passes == probeRepeats)
		ch.Measured, ch.Unit = &score, t.Metric
		notes = append(notes, fmt.Sprintf("pass^%d: every repeat must name the three shapes and colours, at the router's defaults", probeRepeats))
	case !hasBar:
		ch = Check{Name: name, Role: RoleVision, Measured: &score, Unit: t.Metric, Passed: true}
		notes = append(notes, "no bar: recorded, never fails; early stop needs a bar")
	case len(res.Stopped) > 0:
		ch = measure(name, RoleVision, score, t.Metric, AtLeast, bar.Value)
		ch.Passed = false
		notes = append(notes, fmt.Sprintf("early stop after %d: %s", n, res.StopReason))
	default:
		ch = measure(name, RoleVision, score, t.Metric, AtLeast, bar.Value)
		notes = append(notes, "early stop: ran in full")
	}
	ch.Values = map[string]float64{
		"wall_s": wall, "request_s": res.RequestS, "samples": float64(n), "passes": float64(passes), "tier": float64(t.Tier),
	}
	if res.Upper != nil {
		ch.Values["pass_rate_upper_bound"] = *res.Upper
	}
	if m := r.model(); m != nil {
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
