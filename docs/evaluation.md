# How the evaluation works

`go run ./evaluation/cmd/evaluate` takes one model, or one role of a set, through six stages
against the router, from the architect's laptop: admission, fitness, capacity, quality, set, and
deep. Each stage is cheaper than the next, and the run stops at the first one that fails, so a
model that can't do the job fails early and cheaply. A model is judged by what it does in each
role the set gives it: agent, vision, audio, or embedding.

The first five stages are gates. The deep stage records and judges nothing; it runs only once
every gate has passed. Each run writes one report, which names the build, set, model, roles, the
run's mode, screen or full, whether it made a finalist, the stage reached, the stages not run and
why, and each stage's wall time.

A run is a screen by default, and a full run is for finalists ("Screen and full run", below).

The code is the `gate` package (the stages), the `tier` package (seeded lists and bars), the
uv project `evaluation/suites` (the Inspect AI and MTEB runs, with their early stops), and
`evaluation/cmd/evaluate` (the command); each package's doc lists its API. Set A's measured
results are in [`findings/baseline.md`](findings/baseline.md), "Set A's baseline on b11715".

## The stages

| Stage | Gates on | Fails when |
|---|---|---|
| Admission | Paper: the profile and the router's build. Licence and origin recorded, origin judged by `STANDARDS.md`, the set's roles declared, the profile's context within the card's, the set's memory budget leaving the pool floor free, the router's build at or above the profile's minimum. | Any check fails. Licence fails only when missing. Origin fails when missing, and for a model of Chinese origin (`origin = "China"`) whose profile entry records no `origin_reason`, why no alternative serves; `STANDARDS.md` chooses such a model only then. Validation refuses an `origin_reason` on any other origin. |
| Fitness | Under ~3 minutes: a smoke per declared role (the probe's tool call, image, audio clip, or embedding), the role's single-stream floors, and the pool at the warmed peak (below). | A smoke fails, a single-stream floor is missed, or the pool at the warmed peak is under the floor. |
| Capacity | N requests in flight on the model per role, N its slots in the profile (4 for set A's), through the `concurrent` package's run: the agent's in the agentic shape (8192-token prompt, 512 out), the audio role's timed transcriptions; then N of fitness's image questions or 32-token queries at once. Each of the N is judged by the role's floor at N (`floors.toml`). | A nonce crosses, an embedding leaves its tolerance, a request runs out of context, the pool at capacity's peak is under the floor, or a request misses its floor at N. |
| Quality | Each role's tiers, cheapest first, each against its bar (below); every role of the run at once, each on its own model. In a screen, also the pool's least free memory and the crossing rounds over that pass. | A tier misses its bar; that role's tiers stop there. In a screen, also the set stage's two concurrent gates, below. |
| Set | A full run's: each role's tier 1 alone, then every role's at once, all of the set's models loaded. | The pool at the concurrent peak is under the floor, or an answer carries another request's nonce. |
| Deep | A full run's: τ2 airline and retail for the agent role; the GuideLLM grid and prefix reuse for a generative model; then cold load. | Never on a score: only when a measurement can't be taken. |

The quality stage runs every role of the run at once, each role's tiers in order on its own
model: a quality score doesn't depend on speed, and fitness and capacity have measured the speed
floors alone. In a screen, that pass runs inside the set stage's concurrent window, which it
stands in for: the pool's least free memory over it, against the pool's floor, and rounds of
nonce requests to every role's model of the set while the tiers run, no answer crossed. A
model run's pass is its own roles' tiers, with the set's other models loaded and answering only
the crossing rounds; a set run's (`-set` with no model) is every role's.

In a model's full run, the set stage takes the alone figure from the model's own quality tier 1,
and runs only that model's tier 1s at once while the set's other role models carry load for
exactly that window. A set run's full run runs every role's tier 1 alone, role after role, then
all at once. An answer that errored, came back empty, or hit its `max_tokens` with no nonce is
counted unanswered, not crossed; only an answer carrying a nonce not its own is crossed.

A set run takes every model of the set through admission, fitness and capacity in turn, alone,
each check naming its model, and, in a full run, each model's part of the deep stage, then cold
load once.

The quality tiers, by role:

| Role | Tier 1 | Tier 2 | Tier 3 |
|---|---|---|---|
| agent | BFCL single-turn, 100 | BFCL multi-turn, 20 | harness fit: conform pass^5 through Pi, Claude Code, and OpenCode |
| vision | the probe image ×5 (pass^5), and V\*Bench, 100 | DocVQA, 150 | MMMU, 200 |
| audio | LibriSpeech test-clean, 150 utterances | the other five ASR sets, 150 each: LibriSpeech test-other, VoxPopuli, AMI, Earnings-22, FLEURS English | |
| embedding | STSBenchmark, in full | Banking77Classification, SciFact, NFCorpus, in full | |

Harness fit judges each pinned harness by one absolute bar, not by set A's score: every
applicable capability 5/5 and 0 invalid requests, a capability the harness can't exercise
excluded and noted. A harness drops at its first failed trial. The role fails only when no
harness fits, and the report gives the fit profile: which harnesses fit, and each failed cell
with its reason. A comparison with one model's own row can't serve as the bar: gpt-oss-120b
failed its own row on b11669 ([`findings/tool-reliability.md`](findings/tool-reliability.md),
"Harness fit on b11715").

A chat-and-vision model that passes every gate is budgeted ~85 minutes from admission through
the set stage. Set A's two baseline runs on b11715 put such a model at about 95-105 minutes, over
the budget: a vision run took 82.1 and 70.9 minutes, and an agent run 37.2 and 35.9, then about
48 for the deep stage. An audio run took 17.9 to 18.6 minutes, an embedding run about 5, and a
set run about 38. `candidates` times its first chat-and-vision model.

## The tools

Each tool, its kind, the stage that uses it, and why:

| Tool | Kind | Stage | Why |
|---|---|---|---|
| [Inspect AI](https://inspect.aisi.org.uk/) 0.3.281 | harness | quality, set, deep | Runs every generative benchmark over the router's OpenAI API from the laptop, with sample-ID filtering for the seeded lists and an early-stop hook. Chosen over native tau2-bench (needs Python under 3.14), bfcl-eval (no gpt-oss or Gemma 4 handler), lmms-eval (torch), VLMEvalKit, and own fixtures alone. |
| inspect_evals 0.24.0: BFCL | benchmark | quality (agent tiers 1 and 2), set | Tool calls for every model and engine, through the API, single- and multi-turn. |
| inspect_evals: V\*Bench, DocVQA, MMMU | benchmark | quality (vision tiers 1 to 3), set (V\*Bench) | Fine detail in large images, document reading, and multi-image reasoning across subjects, in cost order. |
| The ASR task (`asr_quality.py`, an Inspect task) | benchmark | quality (audio), set (LibriSpeech test-clean) | The Open ASR Leaderboard's English sets and FLEURS English, scored by corpus WER after the leaderboard's English normalizer, with RTFx. Each clip goes with the card's ASR prompt, which the task takes as `--prompt` from the audio model's `prompts.asr` in the profile, the prompt's one source. |
| inspect_evals: τ2 airline and retail | benchmark | deep (agent) | Multi-turn tool use against a simulated user and a policy. Telecom is out: its simulated user needs tools. |
| The claude-code Inspect provider (`claude_code.py`), Claude Code 2.1.301 as Sonnet | harness | deep (τ2) | τ2's simulated user, with no cloud spend: the laptop's pinned Claude Code in print mode on the architect's subscription, tools off, its own system prompt, no session persistence, the reported model ID recorded. inspect_evals' airline scorer has no model grader, so the simulator is its only use. |
| [MTEB](https://github.com/embeddings-benchmark/mteb) 2.26.4 | benchmark | quality (embedding), set (STSBenchmark) | Similarity, classification, and retrieval, through an encoder that adds EmbeddingGemma 2's task prompts, the profile's `prompts.embedding`, and calls the router's embeddings API; the stock OpenAI encoder adds no prompts. |
| clutch's conform, through the `passk` package | conformance test | quality (agent tier 3, harness fit) | Whether each coding agent can drive the model through clutch's nine capabilities, k = 5 times. |
| Pi 1.3.0, Claude Code 2.1.301, OpenCode 1.19.3 | harness | quality (harness fit); the `concurrent` step | The coding agents the model serves, each judged by harness fit, and run at once by `evaluate concurrent`. |
| [GuideLLM](https://github.com/vllm-project/guidellm) 0.8.1 | load generator | deep (generative models) | The grid: short 512/256 and agentic 8192/512 at 1 and 4 streams, client figures beside the engine's `/metrics`. The deep stage runs the same cells as `evaluation/scripts/baseline.sh`, through `gate.GuideLLM`. |
| The `concurrent` package | load generator | capacity | Requests in flight on every slot, each carrying a nonce, so a crossed answer shows; a chat role's run carries an embedding stream beside it. The set stage sends rounds of the same nonce requests while its tiers run. |
| Prefix reuse | benchmark | deep (generative models) | An 8192-token prefix, then four warm requests: the cache's effect on TTFT. Also `evaluate prefix`. |
| The `coldload` package | benchmark | deep | A warm and a cold router restart, timed until every model is loaded again. Needs the architect's root restarts (`-restart`); otherwise recorded not run. Also `evaluate coldload`. |
| `amdgpu_top` over ssh | footprint reader | fitness, capacity, set | The pool's free memory, the gate every stage that loads the host shares. |

The Python suites are one uv project, [`evaluation/suites`](../evaluation/suites), at Python
3.14.8 with `uv.lock` committed; the coding agents, GuideLLM, uv, and Python are pinned in
[`evaluation/mise.consumers.toml`](../evaluation/mise.consumers.toml). Every benchmark is the
stock task, unforked. `mise run check` runs the suites' own Python tests through that uv and
Python, offline, and fails on a machine without them, so the environment is set up once, from the
repository root, before the check: `MISE_ENV=consumers mise -C evaluation install`, then
`MISE_ENV=consumers mise -C evaluation exec -- uv sync --project suites --locked`.

## Floors

Speed floors are absolute and task-based: what the role's work needs, not a fraction of any
model's score. They are data, in [`evaluation/floors.toml`](../evaluation/floors.toml), and
these are initial values a later decision may change:

| Role | Floor |
|---|---|
| pool | at least 2816 MiB (2.75 GiB) free, idle and at the peak |
| agent | decode ≥ 20 tok/s; 8k-prompt prefill ≥ 500 tok/s; 8k-prompt TTFT ≤ 20 s; each of 4 streams ≥ 10 tok/s |
| vision | one image question ≤ 10 s |
| audio | RTFx ≥ 5; each of 4 streams' RTFx ≥ 1 |
| embedding | ≥ 4,300 tok/s; a 32-token query ≤ 250 ms |

Ceiling efficiency, decode over 256 GB/s ÷ the bytes a token reads, is recorded and never
gated.

## Bars

A quality tier's bar is in [`evaluation/bars.toml`](../evaluation/bars.toml): set A's score less
set A's two-run spread. Set A's baseline ran every stage twice, each role on its assigned model,
with no bars set, so every tier ran whole. Each bar is the mean of the two quality-stage scores
less their difference, rounded down to three places. A lower-is-better measure, an ASR set's
WER, takes a ceiling under `[bars_at_most]` instead: the mean plus the spread, rounded up. A score
both runs measured the same has no spread, so its bar is the score itself. A tier, task, or set
with no bar runs whole, records its score, and never fails.

## Early stop

Each tier runs a committed list of sample IDs ([`evaluation/tiers`](../evaluation/tiers)), drawn
with seed 20261016 from its dataset's pinned revision, the same for every model. In a screen, the
list's first fifth, its screening fifth, always runs whole. After it, before each further sample,
the tier stops once its one-sided 95% bound falls on either side of its bar:

- A pass-rate tier (BFCL, the vision benchmarks) stops, failed, once the Wilson upper bound on
  its pass rate falls below the bar, and, passed, once the Wilson lower bound on its mean score
  is at or over the bar. The pass side bounds the mean score because the bar judges the mean:
  for a 0-or-1 score it is the pass rate, and for DocVQA's ANLS, a score in [0, 1] whose spread
  is at most the pass rate's, the bound is conservative.
- An ASR set stops, failed, once the lower bound on its WER rises above the ceiling, and,
  passed, once the upper bound is at or under it: the corpus WER, a ratio, give or take 1.645
  utterance-clustered standard errors.
- A tier whose bounds still straddle the bar runs on, until one side decides it or its list ends;
  a list that ends undecided is judged by its score.
- MTEB tasks run whole; early stop doesn't apply to a continuous score.
- In harness fit, a harness drops at its first failed trial.

The record names the tier, its stop's side, the bound that decided it and its value, and the
samples run of the list's (the check's `stop`). A full run sends the samples its screen stopped
as a list of their own, without a bar, and judges the whole list.

## Screen and full run

A run is a screen unless `-full` is given. A screen takes admission, fitness, capacity and the
quality stage, early stopped on either side and every role at once; it reports the set and deep
stages not run, since its quality pass stands in for the set stage's concurrent phase and the
deep stage is a finalist's. A screen that passes every stage makes its model, or a set run's
models, a finalist, and the report says so.

A full run continues a finalist's screen, given as the run directory `-run` names, and writes its
report there in place of the screen's, naming when the screen started. It reuses the screen's
admission, fitness and capacity, its warm-up (it reloads nothing), and each unit the screen ran
whole: an MTEB task, the vision probe, harness fit, and a tier its bounds straddled to the end.
It runs the rest of each tier the screen stopped (from the tier's `rest/`), judges each whole
list, and carries the screen's pool and crossing gates. Then it runs the set stage, its alone
figures, concurrent phase and slowdowns, and the deep stage. A full run given no screen screens
first, in a new run directory, and continues only a finalist; a screen that fails is its report.

## The results index

evaluate keeps an index of its passed results, `.state/evaluate/index.json`, beside the run
directories, and reuses them: a rerun measures only what changed.

- **Keys.** A model's results are keyed by the model, the router's build (`/props` build_info),
  and the SHA-256 of the preset it runs at: the rendered `[*]` defaults and its own section, key
  lines only (`profile.Profile.PresetSection`), so a changed `c`, slots or sampling changes it and
  a changed reason or startup set doesn't. Its card's prompts and consumer reasoning, which render
  into no preset, are hashed beside it. The set's key is a SHA-256 over its role assignments and
  every model's key. Every entry is also kept under the method's key: the floors, the bars, the
  suites' pins, the tier lists and the probe's media; a change to any of them reruns everything.
- **Units.** Each role's fitness and capacity records of a model are one entry, and each quality
  unit (an MTEB task, an ASR set, a vision or BFCL tier) another, under the model's key. Harness
  fit sends to every role's model, so it is kept under the set's key, and so is the set stage. A
  changed profile reruns the roles of the models whose sections changed, harness fit, and the set
  stage; a new build reruns everything.
- **Mode.** A screen reuses a full run's entry or else a screen's; a full run reuses only a full
  run's, which judged each tier's whole list. A full run continuing a screen that reused a stopped
  tier reads the screen's samples from the run that measured them; once that run is removed,
  screen again with `-fresh`.
- **Never kept.** A failed unit is never kept, so it reruns. Admission (on paper), the reload and
  warm-up, and the deep stage run every time, and so do the gates of the whole set's pool, which
  depend on every model loaded: fitness's warmed peak, and a screen's quality-pass pool and
  crossing gates, over the units it runs or, when every unit is reused, one crossing round.
  Capacity's peak, the model's own slots in flight, stays with its role's record: the warmed peak
  holds every model's KV, fixed when the model loads.
- **The report** names, for each reused check, the run it was measured in (`reused_from`), and
  holds the run's keys; the summary marks each reused check and stage. Each entry holds a copy of
  its checks, so the index outlives the run directories removed once findings are committed.
  `-fresh` reuses nothing and still records.

The suites' own caches stay off, so a unit that runs is a real rerun: the Inspect suites call
`eval`, never `eval_set` or `eval_retry`, with no model-output cache, and MTEB runs with
`cache=None`. The suites' tests hold them so.

## The sweep

`evaluate sweep` chooses one model's slots and context, `c`, on the router, with the rest of its
set loaded and idle at their own settings. It is a step, not a stage: it writes the router's
preset file, which no stage touches, and records nothing in the results index.

- **Steps.** N runs 1, 2, 4, 8 and 16 until a step fails, then one bisection between the last
  step that held and that failure (their integer midpoint; none when they are adjacent).
- **Context.** Each step's `c` is N times the model's per-session target, rounded up to a
  multiple of 256 and capped at the card's context. The target is the largest of the model's
  roles' in [`evaluation/targets.toml`](../evaluation/targets.toml): each role's largest tier
  request, its prompt and the room its answer is given, measured once on b11734 by the router's
  own token count. The agent's is the agentic shape, 8192 in and 512 out; the vision role's the
  tier lists' largest prompt on Gemma 4 26B-A4B (MMMU, 1076 tokens) and the tiers' 4096 out; the
  audio role's the longest clip on gemma-4-E4B (Earnings-22, 36.4 s, 932 tokens) and the ASR
  task's 1024 out; the embedding role's one 8192-token input. A step the cap leaves a session
  short of its target fails on context, with no request sent.
- **Each step** renders the profile with that one model's override
  (`profile.Profile.Override`), installs it as the router's preset file, has the router read it
  again (`GET /models?reload=1`, which unloads the model whose section changed), loads the model
  again and checks its server reports the step's slots and context (`/props`), then reloads and
  warms the whole set as a run does ("Before each run", below; the warm-up's 8192-token prompt is
  cut to a smaller `c`) and runs the capacity stage on the model at N, each role the set assigns
  it.
- **Each step records** every stream's rate against the floors at N; the median and 95th
  percentile time to first token (llama.cpp's `prompt_ms`; an embedding query's wall time) and
  the total throughput of the N requests (the streams' decode summed for the agent and vision
  roles, their RTFx summed for audio, the queries' tokens a second for embedding); the pool at the
  warmed peak with system RAM, and at capacity's peak; and any request that ran out of context.
- **The choice.** A step holds when every floor at N, the pool's floor and every session's
  context hold; a step whose requests ran out of context names the context alone. The chosen N
  is the smaller of the largest N that held and the knee, the last N before total throughput
  gains under 10% a step (over the steps whose requests all answered). The report gives both,
  and which bound: the floor, the pool, the context, the knee, or the sweep's last step.
- **Restore.** The sweep refuses to start unless the set is loaded and the installed preset file
  is the committed render, by SHA-256. Once started, it ends on every exit, an error, SIGINT and
  SIGTERM among them, by installing the committed render, checking the installed file's SHA-256
  on the host over ssh, having the router read it again, and waiting until every model of the
  set serves at its committed setting. A restore that fails says `RESTORE FAILED` and exits 1;
  `evaluate sweep -restore` runs the restore alone. The preset file is written as the Strix Halo
  runbook's "The preset file's write access" allows: a temporary file in `/etc/llama-router`,
  renamed over `models.ini`, with no sudo and no other file touched.

The report is `.state/evaluate/<UTC time>-sweep-<model>/sweep.json`, with a summary on stdout.
A sweep's chosen N is a finding, recorded in [`findings/slots.md`](findings/slots.md); the
profile's slots change only by a commit.

## Before each run

- **The run reloads the set.** Every model run, and the set run, starts from freshly loaded
  models. Each model keeps the largest working memory it has needed until it is reloaded, so
  without a reload one run's growth carries into the next, and a model's verdict would depend on
  where it ran in the day ([`findings/memory-budget.md`](findings/memory-budget.md), "The pool
  over a day of runs"). evaluate does it itself, once admission has passed: it unloads and loads the set's models over the router's API, waits 30 s, and
  records the idle pool. It then drives each of the set's models to its high-water mark: an
  8192-token prompt to each generative model (prefilled in full ubatches, as the deep stage's
  8192-token prompts are), the vision role's largest image and the audio role's longest clip in
  the committed tier lists (found by `vision_quality.py peak` and `asr_quality.py peak` into the
  run directory's `warm/`), and the embedding role's largest input, one that fills its physical
  batch. It reads the pool again at that warmed peak, and every pool floor is judged there:
  fitness judges the warmed reading, and capacity's, the quality pass's and the set stage's
  readings come after it. Each reading carries the host's system RAM beside it: used
  and available (`free -b`), and each model process's RSS (`ps`, by its `--alias`). The report's
  `warm` records the reload's steps, both readings and the warm-up's requests. A run resumed
  with `-from` on the same run directory, or a full run continuing its screen, doesn't reload,
  and keeps that run's warm-up. The run
  needs every model of the set loaded, and never loads one it didn't find loaded.
- **Point `TMPDIR` into `.state`.** inspect_evals' τ2 task writes one copy of the domain's
  database to `TMPDIR` per sample (airline 6.8 MB, retail 2.7 MB) and never removes it, about
  230 MB an agent run, enough to fill the laptop's `/tmp` quota. Run with
  `mkdir -p .state/tmp && export TMPDIR="$PWD/.state/tmp"`.
- **Keep the laptop awake and the pool readable.** A long run holds `systemd-inhibit` on sleep,
  idle, and the lid switch. The pool is read over Tailscale SSH, so the SSH rule must not lapse
  mid-run ([the Strix Halo runbook](../devices/strix-halo/README.md), "Tailscale").
- **Sign Claude Code in once.** Harness fit and τ2's simulator use the pinned Claude Code on the
  subscription login in `.state/claude`: `CLAUDE_CONFIG_DIR=.state/claude MISE_ENV=consumers
  mise -C evaluation exec -- claude`, then `/login`.

## How to run it

From the repository root on the laptop, with `SPIKE_ROUTER_URL` set to the router:

```bash
# Screen one role's model of a set, admission through quality.
go run ./evaluation/cmd/evaluate -set a -role agent

# Screen a model by router ID, in the set that assigns it a role.
go run ./evaluation/cmd/evaluate -set a -model ggml-org/gemma-4-E4B-it-GGUF:Q8_0

# A set run's screen: every model's admission, fitness and capacity, then every role's
# quality tiers at once.
go run ./evaluation/cmd/evaluate -set a

# A finalist's full run, continuing its screen in the run directory -run names: the rest of
# each tier, the set stage, then deep. Without -run it screens first.
go run ./evaluation/cmd/evaluate -set a -role agent -full -run .state/evaluate/<run>

# Resume at a stage, reusing the passed stages before it from the model's newest run, or from
# the run directory -run names: a screen from admission to quality, a full run (-full) at set
# or deep.
go run ./evaluation/cmd/evaluate -set a -role agent -full -from deep -run .state/evaluate/<run>

# Screen again, reusing nothing from the results index.
go run ./evaluation/cmd/evaluate -set a -role agent -fresh

# The architect is at hand for the deep stage's two cold-load restarts.
go run ./evaluation/cmd/evaluate -set a -role agent -full -restart

# Sweep one model's slots and c, the rest of its set loaded; restore the committed presets alone.
go run ./evaluation/cmd/evaluate sweep -set a -role audio
go run ./evaluation/cmd/evaluate sweep -restore -set a
```

`-role` takes agent, vision, audio, or embedding. The set is `-set`, `SPIKE_SET`, or the
profile's startup set. The run refuses a model the router doesn't list as loaded (for a run that
reloads, every model of the set), and loads only the set it reloads. Each report is `.state/evaluate/<UTC time>-<model>/report.json`, a set run's
`.state/evaluate/<UTC time>-set-<name>/report.json`, with a summary on stdout. The raw output stays
untracked under `.state`; once a run's findings are committed to `docs/findings/`, the run
outputs are removed, and only the logins and caches stay. `go run ./evaluation/cmd/evaluate -h`
lists every flag and the instruments' steps (`conform`, `concurrent`, `prefix`, `coldload`), each
of which also runs alone, and the sweep (`sweep`, above).

The tier lists are drawn again from their seed with the consumers environment's tasks, for
example `MISE_ENV=consumers mise run //evaluation:vision-tiers`; each rewrites the committed lists.

## Planned for candidates

The goal record's Decisions plan these changes for `candidates`:

- **A screen, not a full run.** Built in `screen` ("Screen and full run" and "The results index",
  above), results kept by model, build, and preset, so a changed profile reruns only its changed
  roles plus the set stage. The estimate, 15 to 25 minutes a candidate, is extrapolated, not measured.
  Set A's baseline stays two full runs, since its bars and spreads are what make a screen
  trustworthy.
- **The concurrency count.** Every evaluated set reports the most agent sessions at once that
  still meet the per-stream floors with the pool floor held, even when it is one. The capacity
  stage checks each model's own slots in flight, and the sweep ("The sweep", above) chooses them.
- **A lighter grouping.** Candidates also looks for a grouping that scores near set A's bars
  with less of the pool and bandwidth, so the host keeps headroom and concurrency.
- **Only passing models.** A host keeps installed only the models that passed evaluation on it,
  and its sets group only models that passed together.
