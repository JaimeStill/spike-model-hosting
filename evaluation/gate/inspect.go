package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

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

// passAt is the score at which a sample passes: a correct choice or BFCL call (1), or an ANLS of
// 0.5 or more, which ANLS's own threshold makes any ANLS above 0. pass_stop.py's PASS_AT.
const passAt = 0.5

// TierResult is one pass-rate tier's run, a vision or an agent tier's, read from its Inspect AI
// JSON logs, one a task.
type TierResult struct {
	// Tier and Dataset are the run's metadata: the tier, and the benchmark's repository,
	// revision and split. Inspect and InspectEvals are the releases that wrote the logs (no
	// inspect_evals for the probe). Tasks are the logs' tasks, each with its inspect_evals
	// version, task@version.
	Tier, Dataset, Inspect, InspectEvals string
	Tasks                                []string
	// IDs are the samples scored, task by task in the logs' order, and Scores each one's score:
	// 1 or 0 for a choice or a BFCL sample, its ANLS for DocVQA.
	IDs    []string
	Scores []float64
	// RequestS is the time of the scored samples' last requests, summed: Inspect's
	// ModelOutput.time, the whole of a sample that makes one request.
	RequestS float64
	// WorkingS is the scored samples' working time, summed, every request and tool call of a
	// sample that makes many; Requests counts their model requests (Inspect's turn_count).
	WorkingS float64
	Requests int
	// Stopped are the samples early stopping skipped, and StopReason why; Upper is the 95% upper
	// bound on the pass rate when the run ended. All three come with a bar only.
	Stopped    []string
	StopReason string
	Upper      *float64
}

// Score is the mean of the samples' scores: the accuracy, or the mean ANLS.
func (v TierResult) Score() float64 {
	sum := 0.0
	for _, s := range v.Scores {
		sum += s
	}
	return sum / float64(len(v.Scores))
}

// Passes counts the samples that passed, at a score of 0.5 or more.
func (v TierResult) Passes() int {
	n := 0
	for _, s := range v.Scores {
		if s >= passAt {
			n++
		}
	}
	return n
}

// earlyStopFile is the account of a tier's early stop pass_stop.py writes beside its logs
// when it runs with a bar: the samples stopped, why, and the bound when the tier ended. Inspect
// logs a task whose every sample stopped with neither samples nor early stops, so the logs alone
// can't account for them.
const earlyStopFile = "early_stop.json"

// ReadTier reads the Inspect JSON logs vision_quality.py or agent_quality.py left in out, one a
// task, in their
// names' order (their start times'), and its early stop account, when there is one. A log of a
// run that didn't succeed, with a sample that errored or went unscored, or of another tier than
// the others', fails.
func ReadTier(out string) (TierResult, error) {
	var res TierResult
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
				WorkingTime float64 `json:"working_time"`
				TurnCount   int     `json:"turn_count"`
				Scores      map[string]struct {
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
			res.WorkingS += s.WorkingTime
			res.Requests += s.TurnCount
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
func (v TierResult) matches(tierName string, list *tier.List) error {
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
