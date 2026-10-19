package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JaimeStill/spike-model-hosting/evaluation/coldload"
	"github.com/JaimeStill/spike-model-hosting/evaluation/consumers"
	"github.com/JaimeStill/spike-model-hosting/evaluation/tier"
	"github.com/JaimeStill/spike-model-hosting/hosting/profile"
)

// Tau2Domains are the deep stage's τ2 domains, in the order they run: airline and retail;
// telecom's simulated user needs tools, so it is left out.
var Tau2Domains = []string{"airline", "retail"}

// Tau2Trials is the trials each τ2 task runs, from which pass^1 and pass^2 are taken.
const Tau2Trials = 2

// Tau2Check is a τ2 domain's check name, and its task list's name in TiersDir: agent-tau2-<domain>.
func Tau2Check(domain string) string { return "agent-tau2-" + domain }

// Tau2Simulator is the τ2 user simulator, as an Inspect model (tau2_quality.py's SIMULATOR).
const Tau2Simulator = "claude-code/sonnet"

// Tau2 runs one τ2 domain on a router's agent model, its task list at list, each task trials
// times, slots samples at once (the model's profile slots, Runner.slotsOf), and leaves Inspect
// AI's log and its summary, Tau2Summary, in out (ReadTau2).
type Tau2 interface {
	Tau2(ctx context.Context, router, model, domain, list, out string, trials, slots int) error
}

// Tau2 runs the domain through tau2_quality.py eval, its user simulated through Claude Code at
// Claude with the login in ClaudeConfig, and fails when uv does.
func (u UV) Tau2(ctx context.Context, router, model, domain, list, out string, trials, slots int) error {
	if u.Claude == "" || u.ClaudeConfig == "" {
		return errors.New("the suites runner has no Claude Code or login directory for τ2's simulated user")
	}
	if err := consumers.CheckClaudeLogin(u.ClaudeConfig); err != nil {
		return err
	}
	if err := u.run(ctx, "tau2_quality.py", "eval", "--router", router, "--model", model, "--domain", domain,
		"--list", list, "--trials", strconv.Itoa(trials), "--slots", strconv.Itoa(slots), "--claude", u.Claude, "--claude-config", u.ClaudeConfig,
		"--output", out); err != nil {
		return fmt.Errorf("uv run tau2_quality.py eval --domain %s: %w", domain, err)
	}
	return nil
}

// Tau2Result is one τ2 domain's run, read from its summary.
type Tau2Result struct {
	// Domain, Dataset and Trials are the run's metadata; Inspect and InspectEvals the releases
	// that wrote the log, Task the task with its inspect_evals version; Simulator the simulated
	// user's Inspect model and ClaudeCode the Claude Code release that ran it.
	Domain, Dataset, Inspect, InspectEvals, Task, Simulator, ClaudeCode string
	Trials                                                              int
	// Passed holds each task's trials, by task ID, in epoch order: whether each passed.
	Passed map[string][]bool
	// WorkingS is the samples' working time, summed.
	WorkingS float64
	// SimulatorCalls, SimulatorRetries and SimulatorS are the simulated user's Claude Code calls,
	// their transient retries and the seconds they took, summed over the samples' stores;
	// SimulatorModels counts the calls by the model Claude Code reported serving them.
	SimulatorCalls, SimulatorRetries int
	SimulatorS                       float64
	SimulatorModels                  map[string]int
	// SimulatorErrors and AgentErrors count the model calls that failed, after their retries,
	// the simulator's (the "user" role's) and the model's under test: the stock task carries on
	// past either, so they show only in the log's model events, which the summary counts.
	SimulatorErrors, AgentErrors int
}

// Tau2Summary is the account of a τ2 run tau2_quality.py writes beside its Inspect log: the log
// is Inspect's compressed .eval, since the stock task keeps the domain's database in every
// sample's store (some 28 MB of JSON a sample), and the summary holds what the deep stage reads
// of it.
const Tau2Summary = "tau2.json"

// tau2Sample is one sample of a τ2 summary: a task's trial (its epoch), its score, the simulator's
// calls from its store, and its failed model calls by role.
type tau2Sample struct {
	ID          sampleID        `json:"id"`
	Epoch       int             `json:"epoch"`
	Score       json.RawMessage `json:"score"`
	WorkingTime float64         `json:"working_time"`
	Error       *string         `json:"error"`
	Store       struct {
		Calls   int            `json:"claude_code:calls"`
		Models  map[string]int `json:"claude_code:models"`
		Retries int            `json:"claude_code:retries"`
		Seconds float64        `json:"claude_code:seconds"`
	} `json:"store"`
	SimulatorErrors int `json:"simulator_errors"`
	AgentErrors     int `json:"agent_errors"`
}

// ReadTau2 reads the summary tau2_quality.py left in out (Tau2Summary). A run that didn't
// succeed, or with a sample that errored or went unscored, fails.
func ReadTau2(out string) (Tau2Result, error) {
	res := Tau2Result{Passed: map[string][]bool{}, SimulatorModels: map[string]int{}}
	var raw struct {
		Status   string            `json:"status"`
		Task     string            `json:"task"`
		Packages map[string]string `json:"packages"`
		Metadata struct {
			Domain, Dataset, Simulator string
			Trials                     int
			ClaudeCode                 string `json:"claude_code_version"`
			FullTaskVersion            string `json:"full_task_version"`
		} `json:"metadata"`
		Samples []tau2Sample `json:"samples"`
	}
	path := filepath.Join(out, Tau2Summary)
	if err := readJSON(path, &raw, true); err != nil {
		return res, err
	}
	if raw.Status != "success" {
		return res, fmt.Errorf("%s: the run's status is %q", path, raw.Status)
	}
	m := raw.Metadata
	res.Domain, res.Dataset, res.Trials, res.Simulator, res.ClaudeCode = m.Domain, m.Dataset, m.Trials, m.Simulator, m.ClaudeCode
	res.Inspect, res.InspectEvals = raw.Packages["inspect_ai"], raw.Packages["inspect_evals"]
	res.Task = raw.Task
	if m.FullTaskVersion != "" {
		res.Task += "@" + m.FullTaskVersion
	}
	samples := slices.Clone(raw.Samples)
	slices.SortStableFunc(samples, func(a, b tau2Sample) int { return a.Epoch - b.Epoch })
	for _, s := range samples {
		if s.Error != nil {
			return res, fmt.Errorf("%s: sample %s, trial %d, errored: %s", path, s.ID, s.Epoch, *s.Error)
		}
		if len(s.Score) == 0 || string(s.Score) == "null" {
			return res, fmt.Errorf("%s: sample %s, trial %d, has no score", path, s.ID, s.Epoch)
		}
		score, err := scoreValue(s.Score)
		if err != nil {
			return res, fmt.Errorf("%s: sample %s: %w", path, s.ID, err)
		}
		res.Passed[string(s.ID)] = append(res.Passed[string(s.ID)], score >= passAt)
		res.WorkingS += s.WorkingTime
		res.SimulatorCalls += s.Store.Calls
		res.SimulatorRetries += s.Store.Retries
		res.SimulatorS += s.Store.Seconds
		for model, n := range s.Store.Models {
			res.SimulatorModels[model] += n
		}
		res.SimulatorErrors += s.SimulatorErrors
		res.AgentErrors += s.AgentErrors
	}
	if len(res.Passed) == 0 {
		return res, fmt.Errorf("%s: no sample scored", path)
	}
	return res, nil
}

// PassK is τ-bench's pass^k: over the tasks, the chance that k trials drawn from a task's n all
// pass, C(c, k) / C(n, k) for its c passes, averaged.
func (v Tau2Result) PassK(k int) float64 {
	sum := 0.0
	for _, trials := range v.Passed {
		c := 0
		for _, p := range trials {
			if p {
				c++
			}
		}
		sum += choose(c, k) / choose(len(trials), k)
	}
	return sum / float64(len(v.Passed))
}

// choose is the binomial coefficient C(n, k), 0 when k > n.
func choose(n, k int) float64 {
	if k > n || k < 0 {
		return 0
	}
	r := 1.0
	for i := range k {
		r = r * float64(n-i) / float64(i+1)
	}
	return r
}

// matches checks the run is the domain as its list holds it: the pinned Inspect's and
// inspect_evals', the domain's, drawn from the list's dataset, the pinned Claude Code simulating,
// and each of the list's tasks run trials times, none else.
func (v Tau2Result) matches(domain string, list tier.List, trials int) error {
	switch {
	case v.Inspect != InspectVersion:
		return fmt.Errorf("the log is Inspect %s's, not the pinned %s's", v.Inspect, InspectVersion)
	case v.InspectEvals != InspectEvalsVersion:
		return fmt.Errorf("the log is inspect_evals %s's, not the pinned %s's", v.InspectEvals, InspectEvalsVersion)
	case v.Domain != domain:
		return fmt.Errorf("the log is domain %q's, not %q's", v.Domain, domain)
	case v.Dataset != list.Dataset:
		return fmt.Errorf("the log's dataset %q isn't the task list's %q", v.Dataset, list.Dataset)
	case v.Simulator != Tau2Simulator:
		return fmt.Errorf("the log's simulator is %q, not %s", v.Simulator, Tau2Simulator)
	case v.ClaudeCode != consumers.ClaudeVersion:
		return fmt.Errorf("the simulator ran on Claude Code %s, not the pinned %s", v.ClaudeCode, consumers.ClaudeVersion)
	case v.Trials != trials:
		return fmt.Errorf("the log ran %d trials, not %d", v.Trials, trials)
	}
	ran := slices.Sorted(maps.Keys(v.Passed))
	if want := slices.Sorted(slices.Values(list.IDs)); !slices.Equal(ran, want) {
		return fmt.Errorf("the log's %d tasks aren't the list's %d", len(ran), len(want))
	}
	for id, t := range v.Passed {
		if len(t) != trials {
			return fmt.Errorf("task %s ran %d trials, not %d", id, len(t), trials)
		}
	}
	return nil
}

// tau2Domain runs one τ2 domain on the model into the run's deep/tau2/<domain>, and records its
// pass^1 and pass^2, never judged. A simulator call that failed past its retries makes the run
// a measurement that couldn't be taken: the stock task carries on past it, with a user who said
// nothing. The model's own failed calls are its own, recorded.
func (r *Runner) tau2Domain(ctx context.Context, domain string) (Check, Tau2Result) {
	name := Tau2Check(domain)
	if r.Tau2 == nil || r.Dir == "" || r.Tiers == "" {
		return broken(name, RoleAgent, errors.New("the runner has no τ2 runner, run directory or tiers directory")), Tau2Result{}
	}
	dir := filepath.Join(r.Dir, "deep", "tau2", domain)
	// A rerun into the same run directory (a resumed run) must not read the last run's log.
	if err := os.RemoveAll(dir); err != nil {
		return broken(name, RoleAgent, err), Tau2Result{}
	}
	listPath := filepath.Join(r.Tiers, name+".txt")
	list, err := tier.ReadListFile(listPath)
	if err != nil {
		return broken(name, RoleAgent, err), Tau2Result{}
	}
	began := time.Now()
	err = r.Tau2.Tau2(ctx, r.Router, r.Model, domain, listPath, dir, Tau2Trials, r.slotsOf(r.Model))
	wall := time.Since(began).Seconds()
	var res Tau2Result
	if err == nil {
		res, err = ReadTau2(dir)
	}
	if err == nil {
		err = res.matches(domain, list, Tau2Trials)
	}
	if err != nil {
		ch := broken(name, RoleAgent, err)
		ch.Values = map[string]float64{"wall_s": wall}
		return ch, Tau2Result{}
	}
	pass1, pass2 := res.PassK(1), res.PassK(2)
	samples, passed := 0, 0
	for _, t := range res.Passed {
		for _, p := range t {
			samples++
			if p {
				passed++
			}
		}
	}
	ch := Check{Name: name, Role: RoleAgent, Measured: &pass1, Unit: "pass^1", Passed: true, Values: map[string]float64{
		"pass_1": pass1, "pass_2": pass2, "tasks": float64(len(res.Passed)), "trials": float64(res.Trials),
		"samples": float64(samples), "samples_passed": float64(passed), "wall_s": wall, "working_s": res.WorkingS,
		"simulator_calls": float64(res.SimulatorCalls), "simulator_retries": float64(res.SimulatorRetries),
		"simulator_s": res.SimulatorS, "simulator_errors": float64(res.SimulatorErrors), "agent_errors": float64(res.AgentErrors),
	}}
	ch.Detail = fmt.Sprintf("pass^1 %.3f, pass^2 %.3f over %d tasks x %d trials (%d of %d samples passed) in %.0fs; %d model calls failed; "+
		"the user simulated by %s through Claude Code %s, reported %s, %d calls; %s, Inspect %s, %s; deep stage: recorded, never judged",
		pass1, pass2, len(res.Passed), res.Trials, passed, samples, wall, res.AgentErrors,
		res.Simulator, res.ClaudeCode, models(res.SimulatorModels), res.SimulatorCalls, res.Task, res.Inspect, list.Dataset)
	if res.SimulatorErrors > 0 {
		ch = broken(name, RoleAgent, fmt.Errorf("%d of the simulated user's calls failed past their retries, so its conversations aren't τ2's; %s",
			res.SimulatorErrors, ch.Detail))
		ch.Measured, ch.Unit, ch.Values = &pass1, "pass^1", map[string]float64{"pass_1": pass1, "pass_2": pass2, "wall_s": wall,
			"simulator_errors": float64(res.SimulatorErrors)}
	}
	return ch, res
}

// models lists a count of calls by model, most used first: "claude-sonnet-5-5 (312)".
func models(counts map[string]int) string {
	if len(counts) == 0 {
		return "no model"
	}
	ids := slices.Sorted(maps.Keys(counts))
	slices.SortStableFunc(ids, func(a, b string) int { return counts[b] - counts[a] })
	var parts []string
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s (%d)", id, counts[id]))
	}
	return strings.Join(parts, ", ")
}

// simulator rolls the τ2 domains' simulator calls up into one record: every call, its retries
// and seconds, and the model Claude Code reported serving each, recorded, never judged.
func simulator(results []Tau2Result) Check {
	calls, retries, errs, secs, byModel := 0, 0, 0, 0.0, map[string]int{}
	version := ""
	for _, res := range results {
		calls, retries, errs, secs = calls+res.SimulatorCalls, retries+res.SimulatorRetries, errs+res.SimulatorErrors, secs+res.SimulatorS
		for m, n := range res.SimulatorModels {
			byModel[m] += n
		}
		version = res.ClaudeCode
	}
	n := float64(calls)
	ch := Check{Name: "tau2 simulator", Role: RoleAgent, Measured: &n, Unit: "calls", Passed: true, Values: map[string]float64{
		"calls": n, "retries": float64(retries), "errors": float64(errs), "seconds": secs, "models": float64(len(byModel)),
	}}
	ch.Detail = fmt.Sprintf("%s through Claude Code %s on the subscription login (claude -p, tools off, its own system prompt, no session persistence): "+
		"%d calls, %d retried, %d failed, %.0fs; reported model: %s", Tau2Simulator, version, calls, retries, errs, secs, models(byModel))
	return ch
}

// GridCell is one cell of the GuideLLM grid: a profile's prompt and output tokens, the streams
// in flight, and the requests sent, a fixed number per stream (evaluation/scripts/baseline.sh's).
type GridCell struct {
	Profile                    string
	PromptTokens, OutputTokens int
	Streams, Requests          int
}

// GridCells are the deep stage's GuideLLM grid on the model: short 512/256 and agentic 8192/512,
// each at 1 and 4 streams, 8 requests a stream on the short profile and 4 on the agentic one.
var GridCells = []GridCell{
	{"short", 512, 256, 1, 8}, {"short", 512, 256, 4, 32},
	{"agentic", 8192, 512, 1, 4}, {"agentic", 8192, 512, 4, 16},
}

// Name is the cell's check name: grid <profile> <prompt>/<output> c<streams>.
func (c GridCell) Name() string {
	return fmt.Sprintf("grid %s %d/%d c%d", c.Profile, c.PromptTokens, c.OutputTokens, c.Streams)
}

// stem is the cell's files' name: <profile>-c<streams>.
func (c GridCell) stem() string { return fmt.Sprintf("%s-c%d", c.Profile, c.Streams) }

// GridResult is one GuideLLM cell's client figures: its requests completed and errored (or left
// incomplete), the median and 95th-percentile time to first token, the median inter-token
// latency and the mean output tokens a second, as GuideLLM reports them.
type GridResult struct {
	Completed, Errored                   int
	TTFTMedianMS, TTFTP95MS, ITLMedianMS float64
	OutputTokS                           float64
}

// Grid runs one GuideLLM cell against a router's model, the prompt cut by tokenizer, a Hugging
// Face repository, and leaves GuideLLM's files under out.
type Grid interface {
	Cell(ctx context.Context, router, model, tokenizer string, c GridCell, out string) (GridResult, error)
}

// GuideLLM runs a grid cell with GuideLLM at Exe, the consumers mise environment's pin, as
// evaluation/scripts/baseline.sh does: a scenario of GuideLLM's synthetic text at the cell's
// token counts, the prompt cut by the model's own upstream tokenizer (fetched under HFHome), the
// output held to its count, the cell's streams in flight until its requests are sent.
type GuideLLM struct {
	Exe, HFHome string
	// Log takes GuideLLM's output; nil discards it.
	Log io.Writer
}

// Cell writes the cell's scenario to out/<stem>.scenario.json, runs it, and reads GuideLLM's
// out/<stem>.json.
func (g GuideLLM) Cell(ctx context.Context, router, model, tokenizer string, c GridCell, out string) (GridResult, error) {
	if err := os.MkdirAll(out, 0o750); err != nil {
		return GridResult{}, err
	}
	result := filepath.Join(out, c.stem()+".json")
	scenario := filepath.Join(out, c.stem()+".scenario.json")
	spec := map[string]any{"spec": map[string]any{
		"backend":     map[string]any{"kind": "openai_http", "target": router, "model": model, "request_format": "/v1/chat/completions"},
		"tokenizer":   map[string]any{"kind": "huggingface_auto", "model": tokenizer},
		"profile":     map[string]any{"kind": "concurrent", "streams": c.Streams},
		"data":        []map[string]any{{"kind": "synthetic_text", "prompt_tokens": c.PromptTokens, "output_tokens": c.OutputTokens}},
		"constraints": []map[string]any{{"kind": "max_requests", "count": c.Requests}},
		"outputs":     []map[string]any{{"kind": "json", "path": result}},
	}}
	b, err := json.MarshalIndent(spec, "", " ")
	if err != nil {
		return GridResult{}, err
	}
	if err := os.WriteFile(scenario, b, 0o600); err != nil {
		return GridResult{}, err
	}
	cmd := exec.CommandContext(ctx, g.Exe, "run", "-c", scenario, "--disable-console")
	cmd.Env = append(os.Environ(), "HF_HOME="+g.HFHome)
	cmd.Stdout, cmd.Stderr = g.Log, g.Log
	if err := cmd.Run(); err != nil {
		return GridResult{}, fmt.Errorf("guidellm run %s: %w", c.Name(), err)
	}
	return ReadGrid(result)
}

// ReadGrid reads a GuideLLM run's JSON: its first benchmark's client figures.
func ReadGrid(path string) (GridResult, error) {
	type dist struct {
		Mean        float64            `json:"mean"`
		Median      float64            `json:"median"`
		Percentiles map[string]float64 `json:"percentiles"`
	}
	var raw struct {
		Benchmarks []struct {
			Requests struct {
				Successful, Errored, Incomplete []json.RawMessage
			} `json:"requests"`
			Metrics struct {
				TTFT struct {
					Successful dist `json:"successful"`
				} `json:"time_to_first_token_ms"`
				ITL struct {
					Successful dist `json:"successful"`
				} `json:"inter_token_latency_ms"`
				Output struct {
					Successful dist `json:"successful"`
				} `json:"output_tokens_per_second"`
			} `json:"metrics"`
		} `json:"benchmarks"`
	}
	if err := readJSON(path, &raw, false); err != nil {
		return GridResult{}, err
	}
	if len(raw.Benchmarks) == 0 {
		return GridResult{}, fmt.Errorf("%s holds no benchmark", path)
	}
	bm := raw.Benchmarks[0]
	m := bm.Metrics
	return GridResult{
		Completed: len(bm.Requests.Successful), Errored: len(bm.Requests.Errored) + len(bm.Requests.Incomplete),
		TTFTMedianMS: m.TTFT.Successful.Median, TTFTP95MS: m.TTFT.Successful.Percentiles["p95"],
		ITLMedianMS: m.ITL.Successful.Median, OutputTokS: m.Output.Successful.Mean,
	}, nil
}

// gridCell runs one cell on the model into the run's deep/grid, with the engine's own figures
// from the model's /metrics differenced over the cell: prompt and generation tokens over their
// seconds (the server sums those per slot, so at 4 streams they are each request's own rate),
// and the prompt tokens served from the cache. Recorded, never judged.
func (r *Runner) gridCell(ctx context.Context, c *client, tokenizer string, cell GridCell) Check {
	name := cell.Name()
	before, err := c.metrics(ctx, r.Model)
	if err != nil {
		return broken(name, "", err)
	}
	began := time.Now()
	res, err := r.Grid.Cell(ctx, r.Router, r.Model, tokenizer, cell, filepath.Join(r.Dir, "deep", "grid"))
	wall := time.Since(began).Seconds()
	if err != nil {
		ch := broken(name, "", err)
		ch.Values = map[string]float64{"wall_s": wall}
		return ch
	}
	after, err := c.metrics(ctx, r.Model)
	if err != nil {
		return broken(name, "", err)
	}
	rate := func(tokens, seconds string) float64 {
		if ds := after[seconds] - before[seconds]; ds > 0 {
			return (after[tokens] - before[tokens]) / ds
		}
		return 0
	}
	out := res.OutputTokS
	ch := Check{Name: name, Measured: &out, Unit: "tok/s", Passed: true, Values: map[string]float64{
		"completed": float64(res.Completed), "errored": float64(res.Errored),
		"ttft_median_ms": res.TTFTMedianMS, "ttft_p95_ms": res.TTFTP95MS, "itl_median_ms": res.ITLMedianMS,
		"output_tok_s": res.OutputTokS, "wall_s": wall,
		"engine_prompt_tok_s":  rate("prompt_tokens_total", "prompt_seconds_total"),
		"engine_gen_tok_s":     rate("tokens_predicted_total", "tokens_predicted_seconds_total"),
		"engine_cached_tokens": after["prompt_tokens_cached_total"] - before["prompt_tokens_cached_total"],
	}}
	ch.Detail = fmt.Sprintf("GuideLLM, %d requests at %d streams, the prompt cut by %s: %d completed, %d errored; TTFT median %.0f ms, ITL median %.1f ms, %.1f output tok/s; deep stage: recorded, never judged",
		cell.Requests, cell.Streams, tokenizer, res.Completed, res.Errored, res.TTFTMedianMS, res.ITLMedianMS, res.OutputTokS)
	return ch
}

// The prefix-reuse measurement: requests that share one prefix, PrefixTokens long in the model's
// tokens, each ending in a different question, as evaluate prefix sends them by default.
const (
	PrefixTokens   = 8192
	prefixRequests = 5
	prefixMax      = 32
)

// prefixQuestions end the prefix, one a request.
var prefixQuestions = []string{
	"In one sentence, what is the passage above about?",
	"Name one place the passage above mentions.",
	"Which ledger number above is the highest? Answer with the number.",
	"Give a title for the passage above in five words or fewer.",
	"Does the passage above mention a river? Answer yes or no.",
}

// prefix measures prefix-cache reuse on the model: one prefix, unique to the run, then
// prefixRequests streamed requests, each the prefix and a different question, unpinned, as a
// consumer sends them. The first is cold; the check is the warm requests' least share of the
// prompt served from the cache (llama.cpp's cache_n over cache_n and prompt_n), beside the cold
// and warm times to first token. Recorded, never judged.
func (r *Runner) prefix(ctx context.Context, c *client) Check {
	const name = "prefix reuse"
	text, err := c.fitText(ctx, r.Model, PrefixTokens)
	if err != nil {
		return broken(name, "", err)
	}
	var cold completion
	var warmTTFT []float64
	least, warmCache, warmPrompt := 1.0, math.MaxInt, 0
	for i := range prefixRequests {
		got, err := c.stream(ctx, r.Model, text+"\n\n"+prefixQuestions[i%len(prefixQuestions)], prefixMax)
		if err != nil {
			return broken(name, "", fmt.Errorf("request %d: %w", i+1, err))
		}
		if i == 0 {
			cold = got
			continue
		}
		t := got.Timings
		share := 0.0
		if total := t.CacheN + t.PromptN; total > 0 {
			share = float64(t.CacheN) / float64(total)
		}
		least, warmCache, warmPrompt = min(least, share), min(warmCache, t.CacheN), max(warmPrompt, t.PromptN)
		warmTTFT = append(warmTTFT, float64(got.TTFT.Milliseconds()))
	}
	slices.Sort(warmTTFT)
	median := warmTTFT[len(warmTTFT)/2]
	ch := Check{Name: name, Measured: &least, Unit: "share cached", Passed: true, Values: map[string]float64{
		"prefix_tokens": PrefixTokens, "requests": prefixRequests,
		"cold_prompt_n": float64(cold.Timings.PromptN), "cold_ttft_ms": float64(cold.TTFT.Milliseconds()),
		"warm_cache_n_min": float64(warmCache), "warm_prompt_n_max": float64(warmPrompt), "warm_ttft_median_ms": median,
	}}
	ch.Detail = fmt.Sprintf("a %d-token prefix, then %d requests unpinned: cold TTFT %d ms on %d prompt tokens; warm, at least %d cached and at most %d evaluated, TTFT median %.0f ms; deep stage: recorded, never judged",
		PrefixTokens, prefixRequests, cold.TTFT.Milliseconds(), cold.Timings.PromptN, warmCache, warmPrompt, median)
	return ch
}

// The root commands a cold load needs, run by the architect on the router's host: a warm restart,
// then one with the page cache dropped first.
const (
	ColdLoadWarm = "sudo systemctl restart llama-router"
	ColdLoadCold = "sync && echo 3 | sudo tee /proc/sys/vm/drop_caches && sudo systemctl restart llama-router"
)

// coldLoadTimeout bounds the wait for one restart, as evaluate coldload's -timeout does by
// default.
const coldLoadTimeout = 30 * time.Minute

// coldLoadNotRun is a cold load's record when the architect isn't at hand to restart the router.
func coldLoadNotRun(reason string) Check {
	return fact("cold load", "", fmt.Sprintf("not run: %s; run the deep stage with -restart, and run on the router's host, when asked, `%s` (warm), then `%s` (cold)",
		reason, ColdLoadWarm, ColdLoadCold), true)
}

// coldLoad times two router restarts the architect runs, through the coldload package's Tracker:
// warm, then with the page cache dropped, each until every model of the set is loaded again,
// each time from the restart's start. The router restarts into its startup set, so the run's set
// must be it. Without Runner.Restarts, it is recorded not run, naming the commands. Recorded,
// never judged.
func (r *Runner) coldLoad(ctx context.Context) []Check {
	if !r.Restarts {
		return []Check{coldLoadNotRun("needs the architect's restart")}
	}
	if s := r.Profile.StartupSet(); s == nil || s.Name != r.Set.Name {
		return []Check{coldLoadNotRun(fmt.Sprintf("the router restarts into its startup set, not set %s", r.Set.Name))}
	}
	every := r.PollEvery
	if every == 0 {
		every = 250 * time.Millisecond
	}
	var checks []Check
	for _, restart := range []struct{ kind, command string }{{"warm", ColdLoadWarm}, {"cold", ColdLoadCold}} {
		name := "cold load " + restart.kind
		t, err := coldload.Follow(ctx, r.Router, r.Set.Models, every, coldLoadTimeout, func(n int) {
			r.logf("evaluate: deep: cold load %s: %d models loaded; restart the router now, on its host: %s\n", restart.kind, n, restart.command)
		})
		if err != nil {
			checks = append(checks, broken(name, "", err))
			break
		}
		times := t.Times()
		all := times.All.Seconds()
		ch := Check{Name: name, Measured: &all, Unit: "s", Passed: true, Values: map[string]float64{"router_up_s": times.Up.Seconds()}}
		var loaded []string
		for _, a := range r.assignments() {
			if d, ok := times.Loaded[a.model]; ok {
				ch.Values[a.role+"_loaded_s"] = d.Seconds()
				loaded = append(loaded, fmt.Sprintf("%s %.1fs", a.role, d.Seconds()))
			}
		}
		ch.Detail = fmt.Sprintf("after `%s`: router up %.1fs, %s, all %.1fs, each from the restart's start and late by up to a %s poll; deep stage: recorded, never judged",
			restart.command, times.Up.Seconds(), strings.Join(loaded, ", "), all, every)
		checks = append(checks, ch)
		if ctx.Err() != nil {
			break
		}
	}
	return checks
}

// deep runs the deep stage, reached only once every gate passed: for the agent role, τ2 airline
// and retail (Tau2Domains, Tau2Trials each), its user simulated by Sonnet through Claude Code,
// and the simulator's calls rolled up; for a generative model, the GuideLLM grid (GridCells) and
// prefix reuse; then cold load, which needs the architect's restart (coldLoad). Its scores are
// recorded, never judged; a measurement that couldn't be taken fails it, as in every stage.
// Its output is in the run's deep/.
//
// A set run takes each of the set's models' τ2, grid and prefix reuse in turn (perModel), each
// model's output under the run directory's <Slug(model)>/deep, then cold load once, for the set.
func (r *Runner) deep(ctx context.Context, rep *Report) []Check {
	if r.Dir == "" {
		return []Check{broken("deep", "", errors.New("the runner has no run directory"))}
	}
	checks := r.perModel(ctx, rep, (*Runner).deepModel)
	if ctx.Err() != nil {
		return checks
	}
	return append(checks, r.coldLoad(ctx)...)
}

// deepModel is the deep stage's part for the model: τ2 for the agent role, and the grid and prefix
// reuse for a generative model.
func (r *Runner) deepModel(ctx context.Context, rep *Report) []Check {
	var checks []Check
	if slices.Contains(rep.Roles, RoleAgent) {
		var results []Tau2Result
		for _, d := range Tau2Domains {
			r.logf("evaluate: deep: τ2 %s, %d trials\n", d, Tau2Trials)
			ch, res := r.tau2Domain(ctx, d)
			checks = append(checks, ch)
			if res.Domain != "" {
				results = append(results, res)
			}
			if ctx.Err() != nil {
				return checks
			}
		}
		if len(results) > 0 {
			checks = append(checks, simulator(results))
		}
	}
	m := r.model()
	if m != nil && m.Kind == profile.KindGenerative {
		c := &client{base: r.Router, hc: r.HTTP}
		tokenizer, ok := strings.CutPrefix(m.CardURL, "https://huggingface.co/")
		for _, cell := range GridCells {
			switch {
			case r.Grid == nil:
				checks = append(checks, broken(cell.Name(), "", errors.New("the runner has no GuideLLM")))
				continue
			case !ok:
				checks = append(checks, broken(cell.Name(), "", fmt.Errorf("the card %s is no Hugging Face repository to take a tokenizer from", m.CardURL)))
				continue
			}
			r.logf("evaluate: deep: %s\n", cell.Name())
			checks = append(checks, r.gridCell(ctx, c, tokenizer, cell))
			if ctx.Err() != nil {
				return checks
			}
		}
		r.logf("evaluate: deep: prefix reuse\n")
		checks = append(checks, r.prefix(ctx, c))
	}
	return checks
}
