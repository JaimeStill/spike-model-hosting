# spike-model-hosting

A spike for the Standards Lab reference architecture. It tests which serving platform,
configuration, and specification shape hold for locally hosted models across two host classes
and three consumers at once, and records the evidence. It decides nothing itself: its answer is
evidence for standards-lab's experiment.ai intake, which decides what the `ai-hosting`
specification holds.

## The question

Which serving platform, configuration, and specification shape hold across both host classes
(Strix Halo with Vulkan or ROCm, Dell NVIDIA with CUDA) and all three consumers at once (Pi,
Claude Code, and go-ai's model client)?

The platforms are llama.cpp, vLLM, and SGLang; in front of them, no gateway, agentgateway, or
LiteLLM. The ten items of evidence the answer rests on, the scope, and the task path are in
[`context/README.md`](context/README.md).

## The areas

One Go module, a mise monorepo of five areas. Each area's tasks run from the root as
`mise run //<area>:<task>`; `mise run check` runs every area's check and stays hermetic.

| Area | Holds |
|---|---|
| [`hosting/`](hosting) | The Go tooling for profiles and models: the `profile` package and command (load, validate, and render a host-class profile to the router's presets and systemd unit), the `router` client, the `swap` package and command (swap the router between a profile's sets), and the `probe` (the HTTP probes, `mise run integration`). |
| [`evaluation/`](evaluation) | The instruments and the gated pipelines, all run from the architect's laptop: the `evaluate` command, its stages and steps, the floors and bars, the seeded tier lists, the Python quality suites, and the consumer pins. |
| [`devices/`](devices) | One area per host class: its profile, runbook, and build log. [`devices/strix-halo/`](devices/strix-halo) is the Framework desktop's. |
| [`docs/`](docs) | The methods and the findings. |
| [`context/`](context) | The marathon workflow's files: the vision and capability map, and the goal record. |

## The docs

- [How the evaluation works](docs/evaluation.md): the stages and what each gates, each tool by
  kind and the stage that uses it, the floors and bars, and how to run it.
- [Reading a model card](docs/model-cards.md): what each card value means, the values each model
  runs at, and the context catalog.
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

## What the Framework serves

The Framework runs llama.cpp b11715 (`b11715-9d1878a43`, upstream Vulkan x64) behind the
profile's rendered unit and presets, serving set A: gpt-oss-120b (agent), Gemma 4 26B-A4B
(vision), gemma-4-E4B (audio), and EmbeddingGemma 2 (embedding). Set A's scores from two full
evaluation runs set the bars every candidate is judged by
([`evaluation/bars.toml`](evaluation/bars.toml)).

## The planned answer

The tasks still to run, in order, and what each settles:

- **`candidates`:** a candidate for every role, Nemotron 3.5 Super among them, judged by the
  evaluation's floors and set A's bars; the verdict, and the startup set. Planned changes to the
  evaluation, recorded in the goal record's Decisions: a candidate gets a
  screen, not a full run (a tier stops early on a pass as well as a fail, the screening fifth
  runs by default, the roles' quality tiers run concurrently, and results are kept by model,
  build, and preset), with the full run kept for finalists; each set states the concurrent agent
  sessions it supports on its host; a lighter grouping than set A is sought; and a host keeps
  only the models that passed on it, in sets that passed together.
- **`rocm`** and **`engines-strix`:** llama.cpp's ROCm backend against Vulkan, then vLLM and,
  time-boxed, SGLang on Strix Halo.
- **`gateway`:** no gateway, agentgateway, and LiteLLM in front of the Framework.
- **`admin`:** one admin tool over the profile.
- **`cuda`** and **`engines-cuda`:** the Dell NVIDIA workstation's platforms. The workstation
  is two GB10 units, not a discrete-VRAM box, so these tasks await a re-plan.
- **`validate`:** the evidence on both hosts, the platform comparison, and the answer.

## The answer

Pending: the `validate` task writes it.
