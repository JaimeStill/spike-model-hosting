package gate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// The modes a run takes. A screen is a model's or a set's first run: every tier its screening
// fifth and on until its bound decides it, the roles' quality tiers at once, ending at the quality
// stage. A full run is a finalist's: the screen's records, the rest of each tier, the set stage
// and the deep stage.
const (
	ModeScreen = "screen"
	ModeFull   = "full"
)

// The sides a sampled tier's early stop ends it on: failed, its bar out of reach at 95%, or
// passed, its bar cleared at 95%.
const (
	StopFail = "fail"
	StopPass = "pass"
)

// Stop is how a screen's early stop ended a sampled tier (a BFCL or vision benchmark's, an ASR
// set's): the side it stopped on, the bound that decided it and its value when the tier ended,
// the samples run of the list's, and the suite's reason, which names the bound at the stop.
type Stop struct {
	Side string `json:"side"`
	// Bound names the bound: the pass rate's upper or the mean score's lower for a pass-rate
	// tier, the WER's lower or upper for an ASR set.
	Bound string   `json:"bound"`
	Value *float64 `json:"value,omitempty"`
	Ran   int      `json:"ran"`
	Size  int      `json:"size"`
	// Reason is the suite's: the bound when it stopped the tier, against the bar.
	Reason string `json:"reason"`
}

// stopSide checks an early stop account: a run that stopped samples names the side it stopped
// on, StopFail or StopPass, and one that stopped none names none.
func stopSide(stopped int, side string) error {
	switch {
	case stopped > 0 && side != StopFail && side != StopPass:
		return fmt.Errorf("%d samples stopped early on side %q; want %q or %q", stopped, side, StopFail, StopPass)
	case stopped == 0 && side != "":
		return fmt.Errorf("no sample stopped early, yet the account names the side %q", side)
	}
	return nil
}

// tierStop returns a pass-rate tier's early stop, or nil when it ran its whole list.
func tierStop(res TierResult, size int) *Stop {
	if len(res.Stopped) == 0 {
		return nil
	}
	s := &Stop{Side: res.StopSide, Bound: "pass rate upper bound", Value: res.Upper, Ran: len(res.IDs), Size: size, Reason: res.StopReason}
	if s.Side == StopPass {
		s.Bound, s.Value = "mean score lower bound", res.Lower
	}
	return s
}

// asrStop returns an ASR set's early stop, or nil when it ran its whole list.
func asrStop(res ASRResult, size int) *Stop {
	if len(res.Stopped) == 0 {
		return nil
	}
	s := &Stop{Side: res.StopSide, Bound: "WER lower bound", Value: res.LowerBound, Ran: len(res.IDs), Size: size, Reason: res.StopReason}
	if s.Side == StopPass {
		s.Bound, s.Value = "WER upper bound", res.UpperBound
	}
	return s
}

// restDir is where a full run keeps the rest of a sampled tier, under the tier's own directory:
// its list, list.txt, and its output, beside the screen's.
const restDir = "rest"

// restList returns the rest of list a full run adds: the IDs the screen stopped, in the list's
// order, under the list's header.
func restList(list tier.List, stopped []string) tier.List {
	rest := list
	rest.IDs = nil
	for _, id := range list.IDs {
		if slices.Contains(stopped, id) {
			rest.IDs = append(rest.IDs, id)
		}
	}
	return rest
}

// writeRest writes the rest of list, the IDs the screen stopped, as dir/list.txt, after clearing
// dir of an earlier full run's rest, and returns the list and its path.
func writeRest(dir string, list tier.List, stopped []string) (tier.List, string, error) {
	rest := restList(list, stopped)
	if len(rest.IDs) != len(stopped) {
		return rest, "", fmt.Errorf("the screen stopped %d samples, %d of them on the tier's list", len(stopped), len(rest.IDs))
	}
	if err := os.RemoveAll(dir); err != nil {
		return rest, "", err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return rest, "", err
	}
	path := filepath.Join(dir, "list.txt")
	f, err := os.Create(path)
	if err != nil {
		return rest, "", err
	}
	err = rest.Write(f)
	return rest, path, errors.Join(err, f.Close())
}

// screenCheck returns the screen's record of role's unit named name, which a full run reuses or
// continues, and false when the screen holds none.
func (t target) screenCheck(role, name string) (Check, bool) {
	if t.screen == nil {
		return Check{}, false
	}
	for _, c := range t.screen.Checks {
		if c.Role == role && c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

// reused returns the screen's record c as a full run's: a unit the screen ran whole (an MTEB task,
// the vision probe, harness fit, a tier whose bounds straddled its bar to its list's end, or one
// with no bar) is its own full figure.
func reused(c Check) Check {
	c.Detail += "; full run: the screen's record, its whole list run there"
	return c
}

// errNoScreen is a full run's error for a unit the screen it continues holds no record of.
func errNoScreen(name string) error {
	return fmt.Errorf("the screen this full run continues holds no passed record of %s", name)
}

// continued records, in a full run's check of a tier it continued, the screen's samples and wall
// time beside the whole list's, and returns the note naming how the screen stopped it.
func continued(values map[string]float64, screen Check) string {
	values["screen_samples"], values["screen_wall_s"] = float64(screen.Stop.Ran), screen.Values["wall_s"]
	return fmt.Sprintf("the screen's %d of %d reused, early stop %s there: %s", screen.Stop.Ran, screen.Stop.Size, screen.Stop.Side, screen.Stop.Reason)
}

// boundValues records a pass-rate tier's bounds when it ended, those its early stop judged by.
func boundValues(values map[string]float64, res TierResult) {
	if res.Upper != nil {
		values["pass_rate_upper_bound"] = *res.Upper
	}
	if res.Lower != nil {
		values["mean_score_lower_bound"] = *res.Lower
	}
}

// restTier runs the rest of a pass-rate tier a full run continues: it reads the screen's run of
// list from out, writes the samples the screen stopped into dir as a list of their own, runs
// that list whole, without a bar (run, into dir's logs), and returns the two runs together,
// checked to be list's whole.
func (r *Runner) restTier(tierName string, list tier.List, out, dir string, run func(listPath, out string) error) (TierResult, error) {
	screen, err := ReadTier(out)
	if err == nil {
		err = screen.matches(tierName, &list)
	}
	if err != nil {
		return screen, fmt.Errorf("the screen's run: %w", err)
	}
	rest, path, err := writeRest(dir, list, screen.Stopped)
	if err != nil {
		return screen, err
	}
	restOut := filepath.Join(dir, "logs")
	if err := run(path, restOut); err != nil {
		return screen, err
	}
	more, err := ReadTier(restOut)
	if err == nil {
		err = more.matches(tierName, &rest)
	}
	if err != nil {
		return screen, fmt.Errorf("the rest: %w", err)
	}
	if len(more.Stopped) > 0 {
		return screen, fmt.Errorf("the rest stopped %d samples early; it runs without a bar", len(more.Stopped))
	}
	return screen.plus(more), nil
}
