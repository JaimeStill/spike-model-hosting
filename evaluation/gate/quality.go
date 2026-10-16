package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// EmbeddingTasks are the MTEB tasks the quality stage runs on the embedding role, each in full:
// one a task type the role serves (similarity, classification, retrieval twice).
var EmbeddingTasks = []string{"STSBenchmark", "Banking77Classification", "SciFact", "NFCorpus"}

// MTEBVersion is the MTEB release the suites project (evaluation/suites) pins; a result written
// by another fails its check.
const MTEBVersion = "2.26.3"

// SuitesDir is the uv project, under the module root, that holds the quality suites and the
// encoder the embedding tasks run through.
const SuitesDir = "evaluation/suites"

// MTEB runs one MTEB task on a router's model and leaves, in out, MTEB's result, <task>.json,
// and the encoder's account of it, encoder.json (ReadMTEB).
type MTEB interface {
	Task(ctx context.Context, router, model, task, out string) error
}

// UV runs the suites project's mteb_quality.py with uv: `uv run --project <Project> --locked
// --no-python-downloads --python <Python> python <Project>/mteb_quality.py ...`, which syncs the
// project's .venv from its lock on first use. The datasets download from Hugging Face under
// Cache, which the gated run keeps under the state directory: HF_HOME is its huggingface/, and
// MTEB_CACHE its mteb/.
type UV struct {
	// Exe is uv, and Python the interpreter it runs: the consumers mise environment's.
	Exe, Python string
	// Project is the suites project's directory; Cache, the root of the caches.
	Project, Cache string
	// Log takes the run's output, MTEB's progress; nil discards it.
	Log io.Writer
}

// Task runs task through uv and fails when uv does.
func (u UV) Task(ctx context.Context, router, model, task, out string) error {
	cmd := exec.CommandContext(ctx, u.Exe, "run", "--project", u.Project, "--locked", "--no-python-downloads",
		"--python", u.Python, "python", filepath.Join(u.Project, "mteb_quality.py"),
		"--router", router, "--model", model, "--task", task, "--output", out)
	cmd.Env = append(os.Environ(),
		"HF_HOME="+filepath.Join(u.Cache, "huggingface"), "MTEB_CACHE="+filepath.Join(u.Cache, "mteb"))
	cmd.Stdout, cmd.Stderr = u.Log, u.Log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("uv run mteb_quality.py --task %s: %w", task, err)
	}
	return nil
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

// qualityBuilt reports whether the quality stage has a suite for any of the run's roles: so far,
// the embedding role's MTEB tasks.
func qualityBuilt(rep *Report) bool { return slices.Contains(rep.Roles, RoleEmbedding) }

// quality runs each role's quality suite that is built. The embedding role runs EmbeddingTasks
// through r.MTEB, one check a task: its main score judged against the task's bar, by the task's
// name, in r.Bars, and recorded, passed, when there is none.
func (r *Runner) quality(ctx context.Context, rep *Report) []Check {
	var checks []Check
	if slices.Contains(rep.Roles, RoleEmbedding) {
		for _, task := range EmbeddingTasks {
			checks = append(checks, r.mtebTask(ctx, task))
			if ctx.Err() != nil {
				break
			}
		}
	}
	return checks
}

// mtebTask runs one MTEB task into the run directory's mteb/<task>, and judges its result.
func (r *Runner) mtebTask(ctx context.Context, task string) Check {
	if r.MTEB == nil || r.Dir == "" {
		return broken(task, RoleEmbedding, errors.New("the runner has no MTEB runner or no run directory"))
	}
	out := filepath.Join(r.Dir, "mteb", task)
	// A rerun into the same run directory (a resumed run) must not read the last run's result.
	if err := os.RemoveAll(out); err != nil {
		return broken(task, RoleEmbedding, err)
	}
	began := time.Now()
	err := r.MTEB.Task(ctx, r.Router, r.Model, task, out)
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
	if bar, ok := r.Bars.Bar(task); ok {
		ch = measure(task, RoleEmbedding, res.MainScore, res.Metric, AtLeast, bar)
	} else {
		score := res.MainScore
		ch = Check{Name: task, Role: RoleEmbedding, Measured: &score, Unit: res.Metric, Passed: true}
		notes = append(notes, "no bar: recorded, never fails")
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
	keys := make([]string, 0, len(prompts))
	for k := range prompts {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, prompts[k])
	}
	return strings.Join(parts, ", ")
}
