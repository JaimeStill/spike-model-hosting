package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// AudioSets are the ASR sets the quality stage runs on the audio role, in order: the Open ASR
// Leaderboard's English test sets LibriSpeech test-clean and test-other, VoxPopuli, AMI and
// Earnings-22, from hf-audio/esb-datasets-test-only-sorted, and FLEURS English (google/fleurs,
// en_us test). asr_quality.py's SETS names each one's repository and revision.
var AudioSets = []string{"librispeech-clean", "librispeech-other", "voxpopuli", "ami", "earnings22", "fleurs-en"}

// TiersDir holds the committed tier lists, under the module root; an ASR set's is
// asr-<set>.txt, drawn by evaluation/cmd/tierlist.
const TiersDir = "evaluation/tiers"

// ASRTier is an ASR set's tier: the name of its list in TiersDir, and of its bar in the bars.
func ASRTier(set string) string { return "asr-" + set }

// cardWER is a figure a model's card reports for an ASR set: recorded beside the WER measured on
// the set, labelled, and never judged.
type cardWER struct {
	value float64
	label string
}

// cardWERs are the card figures, by the card's URL (the profile's card_url) and set.
var cardWERs = map[string]map[string]cardWER{
	"https://huggingface.co/google/gemma-4-E4B-it": {
		"fleurs-en": {0.08, `the card's "FLEURS (lower is better)" 0.08 (google/gemma-4-E4B-it@ee0ef60), a WER over languages it doesn't state, likely FLEURS's multilingual mean, not en_us alone`},
	},
}

// ASRResult is one ASR set's run, read from Inspect AI's JSON log.
type ASRResult struct {
	// Set, Dataset and Normalizer are the task's metadata: the set, its repository, revision and
	// files, and the normalizer's source. Inspect is the Inspect AI release that wrote the log.
	Set, Dataset, Normalizer, Inspect string
	// IDs are the utterances scored, in the log's order; Errors and Words, their word errors and
	// reference words, summed.
	IDs           []string
	Errors, Words int
	// AudioS is the scored utterances' audio, in seconds; RequestS, the time of their requests,
	// each sent alone: Inspect's ModelOutput.time, the generate call's.
	AudioS, RequestS float64
	// Stopped are the utterances early stopping skipped, StopReason why, and StopSide the side it
	// stopped the set on, StopFail or StopPass; LowerBound and UpperBound are the 95% bounds on the
	// WER when the run ended (UpperBound nil while fewer than two utterances bound it), recorded
	// with a bar only.
	Stopped                []string
	StopReason, StopSide   string
	LowerBound, UpperBound *float64
}

// plus returns the run of a's utterances and then o's, the rest of a's tier a full run adds: the
// utterances, counts and times of both, and no early stop, since together they run the tier whole.
func (a ASRResult) plus(o ASRResult) ASRResult {
	out := a
	out.IDs = slices.Concat(a.IDs, o.IDs)
	out.Errors, out.Words = a.Errors+o.Errors, a.Words+o.Words
	out.AudioS, out.RequestS = a.AudioS+o.AudioS, a.RequestS+o.RequestS
	out.Stopped, out.StopReason, out.StopSide, out.LowerBound, out.UpperBound = nil, "", "", nil, nil
	return out
}

// WER is the corpus WER: every word error over every reference word.
func (a ASRResult) WER() float64 { return float64(a.Errors) / float64(a.Words) }

// RTFx is the audio's seconds over the requests' seconds.
func (a ASRResult) RTFx() float64 { return a.AudioS / a.RequestS }

// inspectLogName matches the name Inspect gives a log: its start time, then the task.
var inspectLogName = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T.*\.json$`)

// ReadASR reads the Inspect JSON log asr_quality.py left in out, the only one there. A log of a
// run that didn't succeed, or with a sample that errored or went unscored, fails.
func ReadASR(out string) (ASRResult, error) {
	var res ASRResult
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
	if len(logs) != 1 {
		return res, fmt.Errorf("%s holds %d Inspect logs; want one", out, len(logs))
	}
	var raw struct {
		Status string `json:"status"`
		Eval   struct {
			Packages map[string]string `json:"packages"`
			Metadata struct {
				Set, Dataset, Normalizer string
			} `json:"metadata"`
		} `json:"eval"`
		Results *struct {
			EarlyStopping *struct {
				EarlyStops []struct {
					ID     string `json:"id"`
					Reason string `json:"reason"`
				} `json:"early_stops"`
				Metadata struct {
					Side       string   `json:"side"`
					LowerBound *float64 `json:"lower_bound"`
					UpperBound *float64 `json:"upper_bound"`
				} `json:"metadata"`
			} `json:"early_stopping"`
		} `json:"results"`
		Samples []struct {
			ID       string `json:"id"`
			Metadata struct {
				AudioS float64 `json:"audio_s"`
			} `json:"metadata"`
			Output struct {
				Time *float64 `json:"time"`
			} `json:"output"`
			Scores map[string]struct {
				Metadata struct {
					Errors *int `json:"errors"`
					Words  *int `json:"words"`
				} `json:"metadata"`
			} `json:"scores"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"samples"`
	}
	path := filepath.Join(out, logs[0])
	if err := readJSON(path, &raw, true); err != nil {
		return res, err
	}
	if raw.Status != "success" {
		return res, fmt.Errorf("%s: the run's status is %q", path, raw.Status)
	}
	m := raw.Eval.Metadata
	res.Set, res.Dataset, res.Normalizer, res.Inspect = m.Set, m.Dataset, m.Normalizer, raw.Eval.Packages["inspect_ai"]
	for _, s := range raw.Samples {
		score, ok := s.Scores["wer"]
		switch {
		case s.Error != nil:
			return res, fmt.Errorf("%s: utterance %s errored: %s", path, s.ID, s.Error.Message)
		case !ok || score.Metadata.Errors == nil || score.Metadata.Words == nil:
			return res, fmt.Errorf("%s: utterance %s has no wer score", path, s.ID)
		case s.Output.Time == nil || s.Metadata.AudioS <= 0:
			return res, fmt.Errorf("%s: utterance %s has no request time or audio length", path, s.ID)
		}
		res.IDs = append(res.IDs, s.ID)
		res.Errors += *score.Metadata.Errors
		res.Words += *score.Metadata.Words
		res.AudioS += s.Metadata.AudioS
		res.RequestS += *s.Output.Time
	}
	if res.Words == 0 || res.RequestS <= 0 {
		return res, fmt.Errorf("%s: no utterance scored", path)
	}
	if raw.Results != nil && raw.Results.EarlyStopping != nil {
		es := raw.Results.EarlyStopping
		res.LowerBound, res.UpperBound, res.StopSide = es.Metadata.LowerBound, es.Metadata.UpperBound, es.Metadata.Side
		for _, s := range es.EarlyStops {
			res.Stopped = append(res.Stopped, s.ID)
			res.StopReason = s.Reason
		}
	}
	if err := stopSide(len(res.Stopped), res.StopSide); err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	return res, nil
}

// asrSet runs one ASR set's tier on t's model into t's asr/<set>, and, when t is judged, judges its
// WER against the tier's bar, which must be a ceiling, [bars_at_most]. In a screen a bar also stops
// the set early on either side (the screening fifth, then a 95% lower bound on the WER above it,
// failed, or the upper bound at or under it, passed, asr_quality.py's WERStop); without one the
// tier runs whole and its WER is recorded. A full run reuses the screen's record of a set it ran
// whole, and runs the rest of one it stopped, from asr/<set>/rest, judging the whole list. The
// check records the RTFx, the counts, the wall time, the dataset, normalizer and Inspect release,
// and the card's figure for the set beside the WER where the model's card reports one.
func (r *Runner) asrSet(ctx context.Context, t target, set string) Check {
	name := ASRTier(set)
	if r.ASR == nil || t.dir == "" || r.Tiers == "" {
		return broken(name, RoleAudio, errors.New("the runner has no ASR runner, run directory or tiers directory"))
	}
	bar, hasBar := r.bar(t, name)
	var ceiling *float64
	if hasBar {
		if !bar.AtMost {
			return broken(name, RoleAudio, errors.New("a WER is lower-is-better, so its bar belongs under [bars_at_most], not [bars]"))
		}
		ceiling = &bar.Value
	}
	var screen Check
	if t.full {
		c, ok := t.screenCheck(RoleAudio, name)
		switch {
		case !ok || !c.Passed:
			return broken(name, RoleAudio, errNoScreen(name))
		case c.Stop == nil:
			return reused(c)
		}
		screen = c
	}
	listPath := filepath.Join(r.Tiers, name+".txt")
	list, err := tier.ReadListFile(listPath)
	if err != nil {
		return broken(name, RoleAudio, err)
	}
	out := filepath.Join(t.dir, "asr", set)
	// A rerun into the same run directory (a resumed run) must not read the last run's log; a
	// full run reads the screen's.
	if !t.full {
		if err := os.RemoveAll(out); err != nil {
			return broken(name, RoleAudio, err)
		}
	}
	began := time.Now()
	var res ASRResult
	if t.full {
		var screenOut string
		if screenOut, err = t.screenDir(screen, filepath.Join("asr", set)); err == nil {
			res, err = r.restASR(ctx, t, set, list, screenOut, out)
		}
	} else {
		err = r.ASR.ASR(ctx, r.Router, t.model, set, listPath, r.asrPrompt(t.model), out, ceiling)
		if err == nil {
			res, err = ReadASR(out)
		}
		if err == nil {
			err = res.matches(set, list)
		}
	}
	wall := time.Since(began).Seconds() + screen.Values["wall_s"]
	if err != nil {
		ch := broken(name, RoleAudio, err)
		ch.Values = map[string]float64{"wall_s": wall}
		return ch
	}

	wer := res.WER()
	notes := []string{
		fmt.Sprintf("%d of %d utterances", len(res.IDs), len(list.IDs)),
		fmt.Sprintf("RTFx %.1f: %.0f s of audio over %.1f s of requests, sent one at a time", res.RTFx(), res.AudioS, res.RequestS),
	}
	ch, judged := judgeTier(t, name, RoleAudio, wer, "WER", bar, hasBar, asrStop(res, len(list.IDs)))
	notes = append(notes, judged)
	ch.Values = map[string]float64{
		"wall_s": wall, "rtfx": res.RTFx(), "audio_s": res.AudioS, "request_s": res.RequestS,
		"utterances": float64(len(res.IDs)), "errors": float64(res.Errors), "words": float64(res.Words),
	}
	if res.LowerBound != nil {
		ch.Values["wer_lower_bound"] = *res.LowerBound
	}
	if res.UpperBound != nil {
		ch.Values["wer_upper_bound"] = *res.UpperBound
	}
	if t.full {
		notes = append(notes, continued(ch.Values, screen))
	}
	if m := r.Profile.Model(t.model); m != nil {
		if card, ok := cardWERs[m.CardURL][set]; ok {
			ch.Values["card_wer"] = card.value
			notes = append(notes, fmt.Sprintf("card: %s; ours %.3f on en_us", card.label, wer))
		}
	}
	notes = append(notes, "dataset "+res.Dataset, "normalizer "+res.Normalizer, "inspect "+res.Inspect)
	ch.Detail = strings.Join(notes, "; ")
	return ch
}

// restASR runs the rest of an ASR set's tier a full run continues: it reads the screen's run of
// list from screenOut, writes the utterances the screen stopped into out's rest/ as a list of
// their own, runs that list whole, without a bar, into rest/, and returns the two runs together,
// checked to be list's whole.
func (r *Runner) restASR(ctx context.Context, t target, set string, list tier.List, screenOut, out string) (ASRResult, error) {
	screen, err := ReadASR(screenOut)
	if err == nil {
		err = screen.matches(set, list)
	}
	if err != nil {
		return screen, fmt.Errorf("the screen's run: %w", err)
	}
	dir := filepath.Join(out, restDir)
	rest, path, err := writeRest(dir, list, screen.Stopped)
	if err != nil {
		return screen, err
	}
	if err := r.ASR.ASR(ctx, r.Router, t.model, set, path, r.asrPrompt(t.model), dir, nil); err != nil {
		return screen, err
	}
	more, err := ReadASR(dir)
	if err == nil {
		err = more.matches(set, rest)
	}
	if err != nil {
		return screen, fmt.Errorf("the rest: %w", err)
	}
	if len(more.Stopped) > 0 {
		return screen, fmt.Errorf("the rest stopped %d utterances early; it runs without a bar", len(more.Stopped))
	}
	return screen.plus(more), nil
}

// matches checks the run is the set's tier as the list holds it: the pinned Inspect's, the
// set's, drawn from the list's dataset, and each of the list's utterances either scored or
// stopped early, none else.
func (a ASRResult) matches(set string, list tier.List) error {
	switch {
	case a.Inspect != InspectVersion:
		return fmt.Errorf("the log is Inspect %s's, not the pinned %s's", a.Inspect, InspectVersion)
	case a.Set != set:
		return fmt.Errorf("the log is set %q's, not %q's", a.Set, set)
	case a.Dataset != list.Dataset:
		return fmt.Errorf("the log's dataset %q isn't the tier list's %q", a.Dataset, list.Dataset)
	}
	ran := slices.Concat(a.IDs, a.Stopped)
	slices.Sort(ran)
	want := slices.Sorted(slices.Values(list.IDs))
	if !slices.Equal(ran, want) {
		return fmt.Errorf("the log's %d scored and %d stopped utterances aren't the tier list's %d", len(a.IDs), len(a.Stopped), len(list.IDs))
	}
	return nil
}
