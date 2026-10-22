# spike-model-hosting

A spike for the Standards Lab reference architecture. It tests which serving platform,
configuration, and specification shape hold for locally hosted models across two host classes
and three consumers at once, and records the evidence. It decides nothing itself: its answer is
evidence for standards-lab's experiment.ai intake, which decides what the `ai-hosting`
specification holds.

## The question

What does llama.cpp's Vulkan router on the Framework establish for the self-hosted stack: a
host-class profile, Pi, Claude Code, and go-ai's model client at once, tool-call reliability, a
memory budget on unified memory, and a measurement method?

The spike first asked which serving platform, configuration, and specification shape hold across
both host classes and all three consumers at once, with llama.cpp, vLLM, and SGLang behind no
gateway, agentgateway, or LiteLLM. The architect narrowed it on 2026-10-22 to what the
Framework's llama.cpp baseline proved, with no new measurement; the rest of the original evidence
moved to other spikes, each listed under [The answer](#the-answer).

## The areas

One Go module, a mise monorepo of five areas. Each area's tasks run from the root as
`mise run //<area>:<task>`; `mise run check` runs every area's check and stays hermetic.

| Area | Holds |
|---|---|
| [`hosting/`](hosting) | The Go tooling for profiles and models: the `profile` package and command (load, validate, and render a host-class profile to the router's presets and systemd unit), the `router` client, the `swap` package and command (swap the router between a profile's sets), and the `probe` (the HTTP probes, `mise run integration`). |
| [`evaluation/`](evaluation) | The instruments and the gated pipelines, all run from the architect's laptop: the `evaluate` command, its stages and steps, the floors and bars, the seeded tier lists, the Python quality suites, and the consumer pins. |
| [`devices/`](devices) | One area per host class: its profile, runbook, and build log. [`devices/strix-halo/`](devices/strix-halo) is the Framework desktop's. |
| [`docs/`](docs) | The methods, the model pool, and the findings. |
| [`context/`](context) | The marathon workflow's files: the vision and capability map, and the goal record. |

## The docs

- [How the evaluation works](docs/evaluation.md): the stages and what each gates, each tool by
  kind and the stage that uses it, the floors and bars, the screen and full run, the results
  index, the sweep, and how to run it.
- [Reading a model card](docs/model-cards.md): what each card value means, the values each model
  runs at, and the context catalog.
- [The model pool](docs/pool.md): on paper, the open-weight models that could serve each role on
  Strix Halo under the origin and licence rules, each admitted or excluded and why, and the
  profiles they could make, from set A's near-full pool to small models with large headroom.
- [The Strix Halo runbook](devices/strix-halo/README.md): the Framework's OS, Tailscale, the
  versioned llama.cpp install, the rendered unit and presets, and the root commands of a build
  move. Its build log is [`llama-cpp-builds.md`](devices/strix-halo/llama-cpp-builds.md).

The findings:

- [The baseline](docs/findings/baseline.md): llama.cpp on Vulkan, measured from the laptop, and
  set A's baseline on b11715, every stage run twice, from which the bars come.
- [Tool-call reliability](docs/findings/tool-reliability.md): the layered convention, pass^5 by
  consumer, and harness fit.
- [The memory budget](docs/findings/memory-budget.md): set A's budget against the Framework's
  96 GiB pool, the candidate sets' footprints, set swaps, and the pool's growth under use.
- [Per-model slots](docs/findings/slots.md): the sweep's tables for set A's models on b11734,
  each model's chosen slots and context, and the 2 agent sessions the sweep found set A supports,
  not confirmed by a set stage.

## What the Framework serves

The Framework runs llama.cpp b11734 (`b11734-f3fb2fcdb`, upstream Vulkan x64) behind the
profile's rendered unit and presets, serving set A: gpt-oss-120b (agent), Gemma 4 26B-A4B
(vision), gemma-4-E4B (audio), and EmbeddingGemma 2 (embedding). Set A's scores from two full
evaluation runs set the bars every candidate is judged by
([`evaluation/bars.toml`](evaluation/bars.toml)).

## The remaining tasks

The tasks below are planned to be split into separate spikes, with benchmarking last. Benchmarking
is planned as small elements composed per scenario: a model's fit on a host, a model as an agent,
a concurrent role set, concurrent sessions, and load balancing across hosts. evaluate's screen,
full run and results index are built and tested against a fake router, not yet proven on a
live screen. Each task, and what it settles:

- **`candidates`:** a candidate for every role, judged by the evaluation's floors and set A's
  bars; the verdict, and the startup set. Expected to become part of the benchmarking spike.
- **`rocm`** and **`engines-strix`:** llama.cpp's ROCm backend against Vulkan, then vLLM and,
  time-boxed, SGLang on Strix Halo.
- **`gateway`:** no gateway, agentgateway, and LiteLLM in front of the Framework.
- **`admin`:** one admin tool over the profile.
- **`cuda`** and **`engines-cuda`:** the Dell NVIDIA workstation's platforms. The workstation
  is two GB10 units, not a discrete-VRAM box, so these tasks await a re-plan.
- **`validate`:** the evidence on both hosts, the platform comparison, and the answer.

## The answer

**Answer:** llama.cpp's Vulkan router on the Framework serves set A from one rendered host-class
profile to Pi, Claude Code, and go-ai's model client at once, with no answer crossed and the
pool's floor held; with gpt-oss-120b only Pi meets the tool-call bar, and the measurement method
stands on set A's two baseline runs, its screen proven by tests only.

1. **A host-class profile.** One TOML profile, `devices/strix-halo/profile.toml`, validates and
   renders the router's presets and systemd unit for set A, each model at its card's sampling
   and reasoning settings, naming no host: the unit resolves the tailnet address when it starts,
   and the service user, `HF_HOME`, and the models directory are given at render time. Proven by
   the `profile` package's tests (`hosting/profile`, among them `TestStrixHaloMatchesPinnedPreset`
   and `TestRenderUnitRequiresHost`), and by a running demo: the committed render's SHA-256,
   `a65fd306…`, equals the installed presets', `/props` reports every model's slots and `n_ctx`,
   and the probe passes, on b11734 (`b11734-f3fb2fcdb`), 2026-10-21
   ([slots](docs/findings/slots.md#the-startup-set-restored-2026-10-21)).
2. **Three consumers at once.** Pi, Claude Code, and the model client streamed at once on
   gpt-oss-120b's 4 slots, beside the model client's embedding stream on EmbeddingGemma 2: 40
   chat requests and 1032 embeddings in 51.7 s, every answer carrying its own nonce and no
   other, every embedding matching its input's lone embedding. Pi 1.1.0, Claude Code 2.1.296
   (`/v1/messages`, `thinking` blocks before `tool_use`), and OpenCode 1.19.0 each send
   gpt-oss's reasoning back on the turn after a tool call. Proven by a running demo, `evaluate
   concurrent -rounds 10`, and the `wire` package's round-trip integration tests, on b11572
   (`b11572-efe3626b1`), 2026-10-12
   ([memory budget](docs/findings/memory-budget.md#during-the-concurrent-run-b11572),
   [tool-call reliability](docs/findings/tool-reliability.md#reasoning-round-trip-by-consumer)).
   Set A's role models, each loaded by the others, crossed nothing either: 18 and 22 crossing
   rounds in the set runs on b11715, 2026-10-17 and 2026-10-18
   ([baseline](docs/findings/baseline.md#the-set-stage)).
3. **Tool-call reliability.** With gpt-oss-120b, Pi, constrained to `tool_choice` `"required"`,
   meets OpenAI's bar (pass^5 9 of 9, 0 invalid); Claude Code and OpenCode don't. Claude Code
   answers in text where the `respond` call is due and makes up the `skill` motto without loading
   the skill; OpenCode, on `tool_choice` `auto`, gets the parser's "does not match the expected
   peg-native format" error in its stream. The layered convention (constrain at the server, card
   settings in the profile, reasoning round-trip at the client, bounded resample at the gateway)
   is recorded with its sources. Proven by running demos: `evaluate conform -k 5` on b11572,
   2026-10-12 (Pi 1.1.0, Claude Code 2.1.296, OpenCode 1.19.0), and harness fit, the agent
   role's tier 3, in both of set A's baseline agent runs on b11715 (`b11715-9d1878a43`),
   2026-10-17 and 2026-10-18 (Pi 1.3.0, Claude Code 2.1.301, OpenCode 1.19.3)
   ([tool-call reliability](docs/findings/tool-reliability.md)).
4. **Speed, prefix reuse, and cold load.** gpt-oss-120b generates 48.8 tok/s alone and 21.2 per
   stream at four (68.2 together), on 512-token prompts; the two Gemmas can't hold four 8k
   agentic requests at `c = 32768` ("Context size has been exceeded"); a shared 8192-token
   prefix cuts TTFT from 4-11 s to under 0.55 s on every generative model; a router restart
   loads all four models in about 21 s, warm or cold. Proven by running demos (GuideLLM 0.8.1
   through `//evaluation:baseline`, `evaluate prefix`, `evaluate coldload`) on b11572,
   2026-10-12; the deep stages reran the grid and prefix reuse on b11715, 2026-10-17 and
   2026-10-18, gpt-oss-120b's short cells within 3%, and the move to b11715 read 20.8 and 21.0 s
   cold, 2026-10-17 ([baseline](docs/findings/baseline.md)).
5. **A memory budget on unified memory.** Set A leaves 3378 MiB (3.30 GiB) of the 96 GiB GTT pool
   free with 4 requests in flight on every model (b11565, 2026-10-12), and 3286 MiB (3.21 GiB)
   during the concurrent run (b11572, 2026-10-12), against the 2816 MiB (2.75 GiB) floor. Each
   model keeps its largest working memory until it is reloaded, a high-water mark and not a leak:
   the pool fell from 3509 to 2778 MiB free over a day of runs and a reload restored 3509 (b11715,
   2026-10-17 and 2026-10-18). Reloaded and driven to each model's peak, set A read 3068 MiB
   free, 252 over the floor (b11734, 2026-10-19). A swap between cached sets takes about half a
   minute: set A back in 23.0 s (b11669, 2026-10-15). Proven by running demos:
   `evaluation/scripts/footprint.sh` (`amdgpu_top -p` over ssh), evaluate's reload and
   warmed-peak reading, and `hosting/cmd/swap`
   ([memory budget](docs/findings/memory-budget.md)).
6. **A measurement method.** `evaluate` gates a model through admission, fitness, capacity, and
   quality on absolute floors (`evaluation/floors.toml`); set A's two full runs, every stage
   twice, every rate clearing its floor by at least 2x, set the bars (`evaluation/bars.toml`) on
   b11715, 2026-10-17 and 2026-10-18. The sweep chose each model's slots and context on b11734,
   2026-10-20, and found set A supports 2 concurrent agent sessions, not confirmed by a set
   stage; the startup set runs at its pre-sweep 4 slots. Proven by running demos (the baseline
   runs, `evaluate sweep`); the screen, the full run, and the results index are proven by tests
   only, against a fake router. On paper, the pool survey counts 47 models, 34 admissible
   (b11734, 2026-10-19), and Mistral Small 4 is disqualified at fitness on Vulkan, 3.36 tok/s
   against that day's 24 tok/s floor, its expert down-projections falling back to the CPU until
   llama.cpp's open PR #30105 lands (b11669, 2026-10-15 and 2026-10-16)
   ([evaluation](docs/evaluation.md), [baseline](docs/findings/baseline.md),
   [slots](docs/findings/slots.md), [pool](docs/pool.md)).

The original evidence's other items, each with the spike that now owns it:

- **Item 2**, one schema rendering every platform on both host classes: spike-model-engines,
  which decides the engine and hosting method per host class; the profile format is settled at
  experiment.ai's spike-host-admin's setup.
- **Item 3**, how each platform is set up and managed: spike-model-engines (install and setup
  scripted as named, idempotent steps) and experiment.ai's spike-host-admin.
- **Item 6**, a capability matrix per platform: spike-model-engines, raw capability by model
  shape (embedding and ASR among them), quantization, and concurrency.
- **Item 7**, tier presets, context sizing, and the footprint method beyond the Framework:
  spike-model-layout (context × sessions per placed model) and spike-model-engines (vLLM).
- **Item 8**, each gateway approach: spike-model-gateway, with budgets, rate limits, and
  OpenTelemetry traces added by this task's pending edit.
- **Item 9**, one admin tool over every platform and host: experiment.ai's spike-host-admin.
- **Item 10**, IL6 acceptability on paper: spike-model-gateway, and spike-model-engines for the
  engines by this task's pending edit.
- The model candidates and the benchmark method: experiment.ai's spike-model-benchmarks.
