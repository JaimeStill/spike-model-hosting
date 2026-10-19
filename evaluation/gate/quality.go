package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/suites"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
)

// EmbeddingTasks are the MTEB tasks the quality stage runs on the embedding role, each in full:
// one a task type the role serves (similarity, classification, retrieval twice).
var EmbeddingTasks = []string{"STSBenchmark", "Banking77Classification", "SciFact", "NFCorpus"}

// MTEBVersion and InspectVersion are the MTEB and Inspect AI releases the suites project
// (evaluation/suites) pins, read from its pyproject.toml; a result written by another fails its
// check.
var (
	MTEBVersion    = mustPin("mteb")
	InspectVersion = mustPin("inspect-ai")
)

// mustPin returns the suites project's pin of name; the pyproject.toml it reads is embedded, so
// a pin missing from it is a fault of the build, and panics.
func mustPin(name string) string {
	v, err := suites.Pin(name)
	if err != nil {
		panic(err)
	}
	return v
}

// SuitesDir is the uv project, under the module root, that holds the quality suites and the
// encoder the embedding tasks run through.
const SuitesDir = "evaluation/suites"

// MTEB runs one MTEB task on a router's model, its encoder adding prompts, the model's task
// prompts by MTEB prompt name (profile.Prompts.Embedding; nil for a model whose card gives none),
// and leaves, in out, MTEB's result, <task>.json, and the encoder's account of it, encoder.json
// (ReadMTEB).
type MTEB interface {
	Task(ctx context.Context, router, model, task string, prompts map[string]string, out string) error
}

// ASR runs one tier of an ASR set on a router's audio model, the tier's list at tierPath, each
// clip sent after prompt, the model's ASR prompt (profile.Prompts.ASR), and leaves Inspect AI's
// JSON log in out (ReadASR). A bar, a WER ceiling, stops the set early once
// its WER can't come under the bar (asr_quality.py's WERStop); nil runs the whole tier.
type ASR interface {
	ASR(ctx context.Context, router, model, set, tierPath, prompt, out string, bar *float64) error
}

// UV runs the suites project's scripts with uv: `uv run --project <Project> --locked
// --no-python-downloads --python <Python> python <Project>/<script> ...`, which syncs the
// project's .venv from its lock on first use. The datasets download from Hugging Face under
// Cache, which the gated run keeps under the state directory: HF_HOME is its huggingface/,
// MTEB_CACHE its mteb/, ASR_CACHE, where asr_quality.py keeps each tier's clips as WAV, its
// asr/, and INSPECT_EVALS_CACHE_DIR, where inspect_evals' tasks keep their images, its
// inspect_evals/.
type UV struct {
	// Exe is uv, and Python the interpreter it runs: the consumers mise environment's.
	Exe, Python string
	// Project is the suites project's directory; Cache, the root of the caches.
	Project, Cache string
	// Log takes the run's output, MTEB's and Inspect's progress; nil discards it.
	Log io.Writer
	// Claude is the Claude Code that simulates τ2's user (Tau2), the consumers mise environment's,
	// and ClaudeConfig its configuration directory, which holds the subscription login.
	Claude, ClaudeConfig string
}

// Task runs the MTEB task through mteb_quality.py and fails when uv does. The prompts go to it as
// a file, out's prompts.json, a JSON object kept beside the result: what the encoder was given.
func (u UV) Task(ctx context.Context, router, model, task string, prompts map[string]string, out string) error {
	if prompts == nil {
		prompts = map[string]string{}
	}
	// encoding/json writes a map's keys sorted, so the file is the same on every run.
	js, err := json.MarshalIndent(prompts, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(out, "prompts.json")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(js, '\n'), 0o644); err != nil {
		return err
	}
	if err := u.run(ctx, "mteb_quality.py", "--router", router, "--model", model, "--task", task,
		"--prompts", path, "--output", out); err != nil {
		return fmt.Errorf("uv run mteb_quality.py --task %s: %w", task, err)
	}
	return nil
}

// ASR runs the set's tier through asr_quality.py eval, with prompt as its --prompt, and fails
// when uv does.
func (u UV) ASR(ctx context.Context, router, model, set, tierPath, prompt, out string, bar *float64) error {
	args := []string{"eval", "--router", router, "--model", model, "--set", set, "--tier", tierPath, "--output", out,
		"--prompt", prompt}
	if bar != nil {
		args = append(args, "--bar", strconv.FormatFloat(*bar, 'g', -1, 64))
	}
	if err := u.run(ctx, "asr_quality.py", args...); err != nil {
		return fmt.Errorf("uv run asr_quality.py eval --set %s: %w", set, err)
	}
	return nil
}

// run runs the project's script with args through uv.
func (u UV) run(ctx context.Context, script string, args ...string) error {
	cmd := exec.CommandContext(ctx, u.Exe, append([]string{"run", "--project", u.Project, "--locked", "--no-python-downloads",
		"--python", u.Python, "python", filepath.Join(u.Project, script)}, args...)...)
	cmd.Env = append(os.Environ(), "HF_HOME="+filepath.Join(u.Cache, "huggingface"),
		"MTEB_CACHE="+filepath.Join(u.Cache, "mteb"), "ASR_CACHE="+filepath.Join(u.Cache, "asr"),
		"INSPECT_EVALS_CACHE_DIR="+filepath.Join(u.Cache, "inspect_evals"))
	cmd.Stdout, cmd.Stderr = u.Log, u.Log
	return cmd.Run()
}

// MTEBResult is one task's result, read from MTEB's result JSON and the encoder's account.
type MTEBResult struct {
	Task, Version string
	// Metric names the main score, and MainScore is its mean over the result's splits and
	// subsets, as MTEB's own TaskResult.get_score takes it; Entries counts them.
	Metric    string
	MainScore float64
	Entries   int
	// Prompts are the prompts the encoder applied, by MTEB prompt type: query and document for
	// a retrieval task, all for a symmetric one.
	Prompts map[string]string
}

// ReadMTEB reads task's result from out: MTEB's <task>.json, and encoder.json.
func ReadMTEB(out, task string) (MTEBResult, error) {
	res := MTEBResult{Task: task}
	var raw struct {
		TaskName string `json:"task_name"`
		Version  string `json:"mteb_version"`
		Scores   map[string][]struct {
			MainScore *float64 `json:"main_score"`
		} `json:"scores"`
	}
	if err := readJSON(filepath.Join(out, task+".json"), &raw, true); err != nil {
		return res, err
	}
	var enc struct {
		MainScore string            `json:"main_score"`
		Prompts   map[string]string `json:"prompts"`
	}
	if err := readJSON(filepath.Join(out, "encoder.json"), &enc, false); err != nil {
		return res, err
	}
	if raw.TaskName != task {
		return res, fmt.Errorf("%s.json is task %q's result", task, raw.TaskName)
	}
	res.Version, res.Metric, res.Prompts = raw.Version, enc.MainScore, enc.Prompts
	sum := 0.0
	for split, entries := range raw.Scores {
		for _, e := range entries {
			if e.MainScore == nil {
				return res, fmt.Errorf("%s.json: a %s entry has no main_score", task, split)
			}
			sum += *e.MainScore
			res.Entries++
		}
	}
	if res.Entries == 0 {
		return res, fmt.Errorf("%s.json holds no scores", task)
	}
	res.MainScore = sum / float64(res.Entries)
	return res, nil
}

// pythonNonFinite matches the NaN and ±Infinity values Python's json module writes as bare
// tokens, which JSON lacks: MTEB writes a metric that is undefined for a task, such as
// Banking77Classification's ap, as NaN.
var pythonNonFinite = regexp.MustCompile(`([:\[,]\s*)(?:NaN|-?Infinity)\b`)

// readJSON decodes the file at path into v, reading Python's non-finite values as null when
// python is set.
func readJSON(path string, v any, python bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if python {
		b = pythonNonFinite.ReplaceAll(b, []byte("${1}null"))
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// quality runs each role's quality suite, one check a task, set or tier, each judged against its
// bar, by name, in r.Bars, and recorded, passed, when there is none. The agent role runs
// AgentTiers, BFCL through r.Agent and conform through r.Conform; the embedding role,
// EmbeddingTasks through r.MTEB; the audio role, AudioSets through r.ASR; the vision role,
// VisionTiers through r.Vision. The agent's and vision's tiers run cheapest first, stopping at
// the first tier that fails.
//
// Every role of the run runs its tiers at once, each on its own model (roleQuality): a quality
// score doesn't depend on speed, which fitness and capacity measured alone. In a screen the pass
// runs inside the concurrent window the set stage's concurrent phase takes in a full run: the
// pool's least free memory over it, against the pool's floor, and rounds of nonce requests to
// every role's model of the set, no answer crossed. It stands in for that phase, so a screen ends
// here. A model run's pass is its own roles' tiers, the set's other models loaded beside them and
// answering only the crossing rounds; a set run's is every role's. A full run continues the
// screen's pass (rep.Screen's quality): it reuses each unit the screen ran whole and runs the rest
// of each one the screen stopped, the roles again at once, and carries the screen's two gates.
func (r *Runner) quality(ctx context.Context, rep *Report) []Check {
	full := rep.Mode == ModeFull
	var screen *StageResult
	if full {
		if r.screen != nil {
			screen = r.screen.Stage(StageQuality)
		}
		if screen == nil {
			return []Check{broken("screen", "", errors.New("a full run continues a screen's quality stage, and has none"))}
		}
	}
	var runs []assignment
	for _, a := range r.assignments() {
		if slices.Contains(rep.Roles, a.role) && (r.Model == "" || a.model == r.Model) {
			runs = append(runs, a)
		}
	}
	results := make([][]Check, len(runs))
	pass := func() {
		var wg sync.WaitGroup
		for i, a := range runs {
			t := target{model: a.model, dir: r.Dir, judged: true, full: full, screen: screen}
			wg.Go(func() { results[i] = r.roleQuality(ctx, rep, t, a.role) })
		}
		wg.Wait()
	}
	if full {
		pass()
		checks := slices.Concat(results...)
		for _, c := range screen.Checks {
			if c.Role == "" {
				checks = append(checks, reused(c))
			}
		}
		return checks
	}
	what := "every role's quality tiers ran at once"
	if r.Model != "" {
		what = "the model's quality tiers ran, the set's other models loaded"
	}
	r.logf("evaluate: quality: %s\n", what)
	w := r.window(ctx, r.assignments(), pass)
	return append(slices.Concat(results...), w.gates(r.Floors.Pool.FreeMiB, what)...)
}

// roleQuality runs role's quality suite on t's model, each unit reused from the index when the
// run may reuse its passed record (unit).
func (r *Runner) roleQuality(ctx context.Context, rep *Report, t target, role string) []Check {
	var checks []Check
	switch role {
	case RoleAgent:
		return r.agentTiers(ctx, rep, t)
	case RoleEmbedding:
		for _, task := range EmbeddingTasks {
			checks = append(checks, r.unit(t, role, task, func() []Check { return []Check{r.mtebTask(ctx, t, task)} })...)
			if ctx.Err() != nil {
				break
			}
		}
	case RoleAudio:
		for _, set := range AudioSets {
			checks = append(checks, r.unit(t, role, ASRTier(set), func() []Check { return []Check{r.asrSet(ctx, t, set)} })...)
			if ctx.Err() != nil {
				break
			}
		}
	case RoleVision:
		return r.visionTiers(ctx, t)
	}
	return checks
}

// mtebTask runs one MTEB task on t's model into t's mteb/<task>, and judges its result when t
// is judged.
func (r *Runner) mtebTask(ctx context.Context, t target, task string) Check {
	if r.MTEB == nil || t.dir == "" {
		return broken(task, RoleEmbedding, errors.New("the runner has no MTEB runner or no run directory"))
	}
	bar, hasBar := r.bar(t, task)
	if hasBar && bar.AtMost {
		return broken(task, RoleEmbedding, errors.New("a main score is higher-is-better, so its bar belongs under [bars], not [bars_at_most]"))
	}
	if t.full {
		// An MTEB task runs whole in a screen, so its record is its full figure.
		if c, ok := t.screenCheck(RoleEmbedding, task); ok && c.Passed {
			return reused(c)
		}
		return broken(task, RoleEmbedding, errNoScreen(task))
	}
	out := filepath.Join(t.dir, "mteb", task)
	// A rerun into the same run directory (a resumed run) must not read the last run's result.
	if err := os.RemoveAll(out); err != nil {
		return broken(task, RoleEmbedding, err)
	}
	began := time.Now()
	var prompts map[string]string
	if m := r.Profile.Model(t.model); m != nil && m.Prompts != nil {
		prompts = m.Prompts.Embedding
	}
	err := r.MTEB.Task(ctx, r.Router, t.model, task, prompts, out)
	wall := time.Since(began).Seconds()
	var res MTEBResult
	if err == nil {
		res, err = ReadMTEB(out, task)
	}
	if err == nil && res.Version != MTEBVersion {
		err = fmt.Errorf("the result is MTEB %s's, not the pinned %s's", res.Version, MTEBVersion)
	}
	if err != nil {
		ch := broken(task, RoleEmbedding, err)
		ch.Values = map[string]float64{"wall_s": wall}
		return ch
	}

	// Early stop judges a pass rate over samples; an MTEB main score is a continuous metric
	// over the whole task, so every task runs in full.
	notes := []string{"mteb " + res.Version, "prompts " + promptSet(res.Prompts), "early stop n/a: a continuous score, run in full"}
	if res.Entries > 1 {
		notes = append(notes, fmt.Sprintf("mean of %d split/subset scores", res.Entries))
	}
	var ch Check
	if hasBar {
		ch = measure(task, RoleEmbedding, res.MainScore, res.Metric, AtLeast, bar.Value)
	} else {
		score := res.MainScore
		ch = Check{Name: task, Role: RoleEmbedding, Measured: &score, Unit: res.Metric, Passed: true}
		notes = append(notes, t.unjudged("no bar: recorded, never fails"))
	}
	ch.Values = map[string]float64{"wall_s": wall}
	ch.Detail = strings.Join(notes, "; ")
	return ch
}

// promptSet formats the encoder's prompts, by prompt type, sorted: all="..." or
// document="...", query="...".
func promptSet(prompts map[string]string) string {
	if len(prompts) == 0 {
		return "none"
	}
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(prompts)) {
		parts = append(parts, fmt.Sprintf("%s=%q", k, prompts[k]))
	}
	return strings.Join(parts, ", ")
}

// judgeTier judges a sampled unit's score (a BFCL or vision tier's, an ASR set's) against its bar
// and returns the check with a note saying how it was judged: without a bar, recorded and passed;
// in a screen, ended by its bar's early stop (stop) on either side, failed or passed, the stop
// recorded; in a screen whose bounds straddled the bar to its list's end, or a full run, which
// runs the list whole, held to the bar, a floor or, AtMost, a ceiling.
func judgeTier(t target, name, role string, score float64, unit string, bar tier.Bar, hasBar bool, stop *Stop) (Check, string) {
	op := AtLeast
	if bar.AtMost {
		op = AtMost
	}
	switch {
	case !hasBar:
		return Check{Name: name, Role: role, Measured: &score, Unit: unit, Passed: true},
			t.unjudged("no bar: recorded, never fails; early stop needs a bar")
	case stop != nil:
		ch := measure(name, role, score, unit, op, bar.Value)
		// The bound decides the tier, whatever the point estimate: a passed stop's bound clears
		// the bar, so its score does too, and a failed stop's is out of reach.
		ch.Passed, ch.Stop = stop.Side == StopPass, stop
		verdict := "failed"
		if ch.Passed {
			verdict = "passed"
		}
		return ch, fmt.Sprintf("early stop, %s, after %d of %d: %s", verdict, stop.Ran, stop.Size, stop.Reason)
	case t.full:
		return measure(name, role, score, unit, op, bar.Value), "full run: the screen's samples and the rest, the whole list judged"
	default:
		return measure(name, role, score, unit, op, bar.Value), "early stop: undecided, its bounds straddling the bar through its whole list"
	}
}
