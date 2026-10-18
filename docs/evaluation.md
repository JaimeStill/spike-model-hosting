# How the evaluation works

`go run ./evaluation/cmd/evaluate` takes one model, or one role of a set, through six stages
against the router, from the architect's laptop: admission, fitness, capacity, quality, set, and
deep. Each stage is cheaper than the next, and the run stops at the first one that fails, so a
model that can't do the job fails early and cheaply. A model is judged by what it does in each
role the set gives it: agent, vision, audio, or embedding.

The first five stages are gates. The deep stage records and judges nothing; it runs only once
every gate has passed. Each run writes one report, which names the build, set, model, roles, the
stage reached, the stages not run and why, and each stage's wall time.

The code is the `gate` package (the stages), the `tier` package (seeded lists and bars), the
uv project `evaluation/suites` (the Inspect AI and MTEB runs, with their early stops), and
`evaluation/cmd/evaluate` (the command); each package's doc lists its API. Set A's measured results are in [`findings/baseline.md`](findings/baseline.md), "Set A's baseline on
b11715".

## The stages

| Stage | Gates on | Fails when |
|---|---|---|
| Admission | Paper: the profile and the router's build. Licence and origin recorded, the set's roles declared, the profile's context within the card's, the set's memory budget leaving the pool floor free, the router's build at or above the profile's minimum. | Any check fails. Licence and origin fail only when missing; origin is judged by `STANDARDS.md`. |
| Fitness | Under ~3 minutes: a smoke per declared role (the probe's tool call, image, audio clip, or embedding), the role's single-stream floors, and the idle pool. | A smoke fails, a single-stream floor is missed, or the idle pool is under the floor. |
| Capacity | The profile's slots (4) of requests in flight on the model per role, through the `concurrent` package's run. | A nonce crosses, an embedding leaves its tolerance, the pool at the peak is under the floor, or a 4-stream floor is missed. |
| Quality | Each role's tiers, cheapest first, each against its bar (below). | A tier misses its bar; the stage stops at that tier. |
| Set | Each role's tier 1 alone, then every role's at once, all of the set's models loaded. | The pool at the concurrent peak is under the floor, or an answer carries another request's nonce. |
| Deep | τ2 airline and retail for the agent role; the GuideLLM grid and prefix reuse for a generative model; then cold load. | Never on a score: only when a measurement can't be taken. |

In a model's run, the set stage takes the alone figure from the model's own quality tier 1, and
runs only that model's tier 1s at once while the set's other role models carry load for exactly
that window. A set run (`-set` with no model) runs every role's tier 1 alone, role after role,
then all at once. An answer that errored, came back empty, or hit its `max_tokens` with no nonce
is counted unanswered, not crossed; only an answer carrying a nonce not its own is crossed.

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
with its reason. It replaced judging gpt-oss-120b against its own comparison row, which it
failed on b11669 ([`findings/tool-reliability.md`](findings/tool-reliability.md), "Harness fit on
b11715").

The time it takes, from set A's two baseline runs on b11715: a vision run took 82.1 and 70.9
minutes from admission through the set stage, against a budget of ~85 minutes for a
chat-and-vision model that passes every gate; an agent run 37.2 and 35.9, then about 48 for the
deep stage; an audio run 17.9 to 18.6, an embedding run about 5, and a set run about 38.

## The tools

Each tool, its kind, the stage that uses it, and why:

| Tool | Kind | Stage | Why |
|---|---|---|---|
| [Inspect AI](https://inspect.aisi.org.uk/) 0.3.281 | harness | quality, set, deep | Runs every generative benchmark over the router's OpenAI API from the laptop, with sample-ID filtering for the seeded lists and an early-stop hook. Chosen over native tau2-bench (needs Python under 3.14), bfcl-eval (no gpt-oss or Gemma 4 handler), lmms-eval (torch), VLMEvalKit, and own fixtures alone. |
| inspect_evals 0.24.0: BFCL | benchmark | quality (agent tiers 1 and 2), set | Tool calls for every model and engine, through the API, single- and multi-turn. It replaced OpenAI's gpt-oss compatibility test. |
| inspect_evals: V\*Bench, DocVQA, MMMU | benchmark | quality (vision tiers 1 to 3), set (V\*Bench) | Fine detail in large images, document reading, and multi-image reasoning across subjects, in cost order. |
| The ASR task (`asr_quality.py`, an Inspect task) | benchmark | quality (audio), set (LibriSpeech test-clean) | The Open ASR Leaderboard's English sets and FLEURS English, scored by corpus WER after the leaderboard's English normalizer, with RTFx. |
| inspect_evals: τ2 airline and retail | benchmark | deep (agent) | Multi-turn tool use against a simulated user and a policy. Telecom is out: its simulated user needs tools. |
| The claude-code Inspect provider (`claude_code.py`), Claude Code 2.1.301 as Sonnet | harness | deep (τ2) | τ2's simulated user, with no cloud spend: the laptop's pinned Claude Code in print mode on the architect's subscription, tools off, its own system prompt, no session persistence, the reported model ID recorded. inspect_evals' airline scorer has no model grader, so the simulator is its only use. |
| [MTEB](https://github.com/embeddings-benchmark/mteb) 2.26.4 | benchmark | quality (embedding), set (STSBenchmark) | Similarity, classification, and retrieval, through an encoder that adds EmbeddingGemma 2's task prompts and calls the router's embeddings API; the stock OpenAI encoder adds no prompts. |
| clutch's conform, through the `passk` package | conformance test | quality (agent tier 3, harness fit) | Whether each coding agent can drive the model through clutch's nine capabilities, k = 5 times. |
| Pi 1.3.0, Claude Code 2.1.301, OpenCode 1.19.3 | harness | quality (harness fit); the `concurrent` step | The coding agents the model serves, each judged by harness fit, and run at once by `evaluate concurrent`. |
| [GuideLLM](https://github.com/vllm-project/guidellm) 0.8.1 | load generator | deep (generative models) | The grid: short 512/256 and agentic 8192/512 at 1 and 4 streams, client figures beside the engine's `/metrics`. The grid `evaluation/scripts/baseline.sh` runs is ported to Go as `gate.GuideLLM`, the deep stage's runner of the same cells. |
| The `concurrent` package | load generator | capacity | Requests in flight on every slot, each carrying a nonce, so a crossed answer shows; a chat role's run carries an embedding stream beside it. The set stage sends rounds of the same nonce requests while its tiers run. |
| Prefix reuse | benchmark | deep (generative models) | An 8192-token prefix, then four warm requests: the cache's effect on TTFT. Also `evaluate prefix`. |
| The `coldload` package | benchmark | deep | A warm and a cold router restart, timed until every model is loaded again. Needs the architect's root restarts (`-restart`); otherwise recorded not run. Also `evaluate coldload`. |
| `amdgpu_top` over ssh | footprint reader | fitness, capacity, set | The pool's free memory, the gate every stage that loads the host shares. |

The Python suites are one uv project, [`evaluation/suites`](../evaluation/suites), at Python
3.14.8 with `uv.lock` committed; the coding agents, GuideLLM, uv, and Python are pinned in
[`evaluation/mise.consumers.toml`](../evaluation/mise.consumers.toml). Every benchmark is the
stock task, unforked.

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
with seed 20261016 from its dataset's pinned revision, the same for every model. The list's
first fifth, its screening fifth, always runs whole. After it:

- A pass-rate tier (BFCL, the vision benchmarks) stops, failed, once the one-sided 95% Wilson
  upper bound on its pass rate falls below the bar.
- An ASR set stops, failed, once the one-sided 95% lower bound on its WER rises above the
  ceiling.
- MTEB tasks run whole; early stop doesn't apply to a continuous score.
- In harness fit, a harness drops at its first failed trial.

The record names the tier and the bound that stopped it.

## Before each run

- **Reload the set.** Every model run, and the set run, starts from freshly loaded models. Each
  model keeps the largest working memory it has needed until it is reloaded, so without a reload
  one run's growth carries into the next, and a model's verdict would depend on where it ran in
  the day ([`findings/memory-budget.md`](findings/memory-budget.md), "The pool over a day of
  runs"). Unload and load the set's models over the router's API, wait 30 s, and record the
  idle pool:

  ```bash
  ids=$(go run ./hosting/cmd/profile set -set a strix-halo | awk '$1 == "model" {print $2}')
  status() { curl -fsS "$SPIKE_ROUTER_URL/models" | jq -r --arg id "$1" '.data[] | select(.id == $id) | .status.value'; }
  for verb in unload load; do
    want=${verb}ed
    for id in $ids; do
      curl -fsS -X POST "$SPIKE_ROUTER_URL/models/$verb" -H 'Content-Type: application/json' \
        -d "$(jq -nc --arg m "$id" '{model: $m}')" && echo
      until [ "$(status "$id")" = "$want" ]; do sleep 1; done
    done
  done
  sleep 30
  mise run //evaluation:footprint -- -set a <ssh destination> 1
  ```

  A run resumed with `-from` on the same run directory doesn't reload.
- **Point `TMPDIR` into `.state`.** inspect_evals' τ2 task writes one copy of the domain's
  database to `TMPDIR` per sample (airline 6.8 MB, retail 2.7 MB) and never removes it, about
  230 MB an agent run, which filled the laptop's `/tmp` quota in the baseline. Run with
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
# One role's model of a set, admission through deep.
go run ./evaluation/cmd/evaluate -set a -role agent

# A model by router ID, in the set that assigns it a role.
go run ./evaluation/cmd/evaluate -set a -model ggml-org/gemma-4-E4B-it-GGUF:Q8_0

# A set run: every role's tier 1 alone, then all at once.
go run ./evaluation/cmd/evaluate -set a

# Resume at a stage, reusing the passed stages before it from the model's newest run,
# or from the run directory -run names.
go run ./evaluation/cmd/evaluate -set a -role agent -from deep -run .state/evaluate/<run>

# The architect is at hand for the deep stage's two cold-load restarts.
go run ./evaluation/cmd/evaluate -set a -role agent -restart
```

`-role` takes agent, vision, audio, or embedding. The set is `-set`, `SPIKE_SET`, or the
profile's startup set. The run refuses a model the router doesn't list as loaded, and never loads
one. Each report is `.state/evaluate/<UTC time>-<model>/report.json`, a set run's
`.state/evaluate/<UTC time>-set-<name>/report.json`, with a summary on stdout. The raw output stays
untracked under `.state`; once a run's findings are committed to `docs/findings/`, the run
outputs are removed, and only the logins and caches stay. `go run ./evaluation/cmd/evaluate -h`
lists every flag and the instruments' steps (`conform`, `concurrent`, `prefix`, `coldload`), each
of which also runs alone.

The tier lists are drawn again from their seed with the consumers environment's tasks, for
example `MISE_ENV=consumers mise run //evaluation:vision-tiers`; each rewrites the committed lists.

## Planned for candidates, not built

The goal record's Decisions plan these changes for `candidates`:

- **A screen, not a full run.** A candidate gets a screen; the full run is for finalists. A tier
  stops early on a pass as well as a fail, once the one-sided 95% lower bound clears the bar.
  Every candidate runs each tier's screening fifth by default; only finalists, or models whose
  bound still straddles the bar, run the full tier and the deep stage. The roles' quality tiers
  run concurrently, while the speed floors are still measured alone. Results are kept by model,
  build, and preset, so a changed profile reruns only its changed roles plus the set stage. The
  estimate, 15 to 25 minutes a candidate, is extrapolated, not measured. Set A's baseline stays
  two full runs, since its bars and spreads are what make a screen trustworthy.
- **The concurrency count.** Every evaluated set reports the most agent sessions at once that
  still meet the per-stream floors with the pool floor held, even when it is one. Capacity
  today checks a fixed 4 in flight.
- **A lighter grouping.** Candidates also looks for a grouping that scores near set A's bars
  with less of the pool and bandwidth, so the host keeps headroom and concurrency.
- **Only passing models.** A host keeps installed only the models that passed evaluation on it,
  and its sets group only models that passed together.
