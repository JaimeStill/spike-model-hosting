# spike-model-hosting

spike-model-hosting tests which serving platform, configuration, and specification shape hold
across the Strix Halo and Dell NVIDIA host classes for three consumers at once: Pi, Claude Code,
and go-ai's model client. It compares llama.cpp, vLLM, and SGLang, recording how each is set up
and managed and what it makes possible, and compares agentgateway and LiteLLM in front of the
serving with no gateway at all. Its answer is evidence for standards-lab's experiment.ai intake,
which decides what the `ai-hosting` specification holds; the spike decides nothing itself.

The spike is the sub-goal `experiment.ai.spike-model-hosting` in the coordinator's roadmap,
`standards-lab/context/roadmap.toml`, where its tasks live. Its answer lands in
`standards-lab/context/ai-strategy.md`, "Answers · experiment.ai"; its question is framed in
`standards-lab/context/ai-hosting.md`, "Experiment: spike-model-hosting".

## The question

Which serving platform, configuration, and specification shape hold across both host classes
(Strix Halo with Vulkan or ROCm, Dell NVIDIA with CUDA) and all three consumers at once?

## The decision it changes

The `ai-hosting` specification: the serving platform on each host class, the schema of a
host-class profile, whether a gateway sits in front of the serving and which one, and the form
of the admin tooling.

## The evidence

1. One host-class profile schema renders the Framework's llama.cpp router (presets, slots, KV,
   bind, systemd unit) serving the shared model set, holding no host state.
2. The same schema renders a running setup for every serving platform in scope, on Strix Halo
   (Vulkan and ROCm) and on the Dell NVIDIA workstation (CUDA).
3. For each platform and host class, a record of how it is set up and managed: install, update,
   service unit, model format, adding and swapping models, admin verbs.
4. For each platform, Pi (OpenAI-compatible), Claude Code (Anthropic `/v1/messages`, with tool
   use, streaming, and thinking intact), and the go-ai model client (chat, embeddings) run at
   once, with no response crossing between requests.
5. For each platform and host, measured: memory footprint, prompt and generation speed for one
   request and for four at once, prefix-cache reuse, and cold-load time.
6. A capability matrix per platform: multi-model swapping, prefix caching, structured output,
   batching, embeddings, rerank, vision, audio, and the Anthropic API; each cell proven by a
   probe or marked paper.
7. Tier presets, context sizing, and the memory-footprint method carry over to discrete VRAM and
   to each platform, with exceptions recorded.
8. Each gateway approach in front of the serving: fidelity for the three consumers, per-consumer
   identity, budgets and rate limits, OpenTelemetry traces, memory and latency, and one
   configuration on both host classes. The answer may be "no gateway".
9. One admin tool manages every platform kept, on both host classes.
10. Every platform and gateway kept is acceptable air-gapped at IL6, checked on paper only.

## Scope

- **Serving platforms, live:** llama.cpp (Vulkan and ROCm on Strix Halo, CUDA on the
  workstation) behind its own router; vLLM (ROCm, CUDA) and SGLang (CUDA, and time-boxed on
  Strix Halo from its nightly gfx1151 images) behind llama-swap. Lemonade serves only as an
  install path for vLLM on Strix Halo. Ollama, LM Studio, and TGI are recorded on paper.
- **Gateways, live:** no gateway, agentgateway, and LiteLLM. Envoy AI Gateway (Agent Router) and
  vLLM's production-stack are Kubernetes-first and recorded on paper.
- **Main-model pool:** gpt-oss-120b leads, and Gemma 4 26B-A4B completes the pool. Mistral
  Small 4 is disqualified on Vulkan at b11669 (`main-model`). Nemotron 3.5 Super and other
  candidates go to the `candidates` task, through the `evaluation` task's pipelines.
- **Shared model set (set A):** gpt-oss-120b (ggml-org MXFP4, the main model), Gemma 4 26B-A4B
  (vision), gemma-4-E4B (audio and the small model), and EmbeddingGemma 2 (embeddings), each in
  its platform's native format: GGUF for llama.cpp, safetensors for vLLM and SGLang. On the
  Framework all four stay loaded together as the startup set; a candidate set swaps its main
  model in for gpt-oss-120b and Gemma 4 26B-A4B. The workstation's set is sized to its VRAM in
  the `cuda` task, whose candidate list adds the dense models Mistral Medium 3.5, Gemma 4 31B,
  and Muse Glimmer 30B.
- **Model origin:** models of Chinese origin are avoided where an alternative serves
  (`standards-lab/context/ai-strategy.md`, "Constraint: model origin").
- **Versions:** every engine, gateway, and model runs its latest upstream release. Every document
  names the exact versions in use when it is written, and every measurement the versions it ran
  on. A profile holds llama.cpp to a minimum build, a b-number, and a host runs any build at or
  above it; the build in use is the one the router's `/props` reports in `build_info`. llama.cpp
  is tracked by b-number because its semver releases ship no binaries and lag its model support.
  Other versions are pinned exactly, and `mise run currency` reports the pins that trail.

## Hosts and boundaries

- **Framework desktop** (Strix Halo, 96 GB GTT pool): the first host class, available now. It
  hosts and serves inference only. The spike may change anything in its hosting layer,
  including the runtime, its configuration, and the models. Sessions may do on it whatever
  needs no sudo: the router's HTTP API (including load, unload, and downloads), user-level SSH
  reads such as `amdgpu_top`, and logs. Root stays the architect's, run from exact commands.
  Only the OS install and the Tailscale registration stay fixed, and there is no restore point.
- **Dell NVIDIA workstation** (CUDA): available from 2026-10-14. The tasks that need it say so.
- **Remote access:** sessions reach both hosts by Tailscale SSH in accept mode, set by the
  architect before the first task and reverted to check mode when the spike completes.
- **The architect's laptop:** every consumer (Pi, Claude Code, OpenCode, and the `model`
  client) and every instrument (GuideLLM, OpenAI's gpt-oss compatibility test, the concurrent
  run, pass^k, prefix, and cold load) runs from it, pinned in `mise.consumers.toml` and run as
  `MISE_ENV=consumers mise exec -- <tool>`, off the root PATH so the architect's own Claude Code
  and Pi stay untouched. Their untracked state is under `.state/`; Claude Code runs from its own
  `CLAUDE_CONFIG_DIR` there, on the architect's subscription login, with `ANTHROPIC_BASE_URL` at
  the router. Nothing is served on the laptop.
- Nothing committed here names a host: no tailnet names or addresses, no hostnames. The profile
  and its renderer carry no host's name or address; the rendered unit resolves the host's tailnet
  address when it starts, and host-local values (the service user, HF_HOME, and the models
  directory) are given at render time.

## Capabilities

- **Host-class profile:** the `profile` package loads, validates, and renders a TOML profile to
  llama.cpp's router presets and systemd unit, and the `profile` command (`cmd/profile`) lists,
  validates, and renders the profiles in `profiles/`, and describes a set for the scripts. Each
  set assigns the model each role's requests go to (`[sets.roles]`), and `Profile.Set` is the one
  lookup the probe and every instrument share: each takes `-set` (or `SPIKE_SET`), the startup
  set by default. `profiles/strix-halo.toml` holds the Framework's sets, with set A the startup
  set. Renderers for the other platforms' configuration are planned.
- **Model cards:** how to read a model card, what each value means, the values set A takes
  from each card, and a catalog of each profiled model's maximum context and its computed KV
  and recurrent-state cost, in [`model-cards.md`](model-cards.md).
- **Serving platforms:** llama.cpp (Vulkan, ROCm, CUDA), vLLM, SGLang, and llama-swap, each with
  its setup and management record.
- **Consumers:** Pi, Claude Code, OpenCode, and spike-harness-driver's `model` package as
  go-ai's stand-in, run from the laptop. The `consumers` package sets up Pi, Claude Code, and
  OpenCode against the router for every tool and test: the pinned version, its harness driver,
  the Claude Code login check, and state under the run's own directory.
- **Router state:** the `router` package reads a router's models, their status, and its build
  over HTTP, and loads and unloads a model. Each tool reads the state to refuse to start against
  an unloaded model and to name the build it ran on.
- **Set swap:** the `swap` package and the `swap` command (`cmd/swap`) swap the router from the
  set it has loaded to another of the profile's sets over its API. They unload before they load,
  and time each model's step and the total.
- **Concurrent run:** the `concurrent` package and the `concurrent` command (`cmd/concurrent`)
  run Pi, Claude Code, and the model client's chat and embedding streams against the router at
  once. The run fails when an answer carries another request's nonce, or an embedding differs
  from its input's lone embedding by more than the cosine tolerance.
- **Wire recording:** the `wire` package's `Recorder` is a reverse proxy that records what a
  consumer sends the router and what comes back, with credentials dropped. Its `RoundTrip`
  integration tests record each consumer's reasoning round-trip; each runs only when
  `SPIKE_PI`, `SPIKE_CLAUDE`, or `SPIKE_OPENCODE` names its executable.
- **Tool-call reliability:** the layered convention every platform, profile, consumer, and
  gateway is held to, with pass^k as its measure, in [`tool-reliability.md`](tool-reliability.md).
  The `passk` package and the `conform` command (`cmd/conform`) run clutch's conformance suite k
  times per consumer and judge each by pass^k; `mise run gpt-oss-compat` runs OpenAI's gpt-oss
  compatibility test as the engine row.
- **Measurement:** footprint, speed, prefix cache, cold load, and the capability matrix. The
  llama.cpp Vulkan baseline every platform is measured against is in
  [`baseline.md`](baseline.md): the GuideLLM grid (`mise run baseline`), prefix reuse (the
  `prefix` command, `cmd/prefix`), and cold load (the `coldload` package and command,
  `cmd/coldload`). Set A's memory budget on the Framework, estimated against measured, the
  candidate sets' idle footprints, and the swap timings between sets (evidence 6's
  model-swapping cell) are in [`memory-budget.md`](memory-budget.md).
- **llama.cpp builds:** each build move on the Framework, with its release-note review and
  what was re-verified on the new build, and what the main-model candidates loaded (files,
  handlers, the CPU fallback), in [`llama-cpp-builds.md`](llama-cpp-builds.md). The Framework
  runs b11669.
- **Gateways:** none, agentgateway, and LiteLLM.
- **Admin tool:** a CLI on go-cli-sdk over the profile, with personal-agents' `outpost` as its
  baseline.
- **Host runbooks:** what each host needs to run every platform kept.

## Path

The tasks, in the order they run; each cites the evidence it proves.

1. `align`: the Framework's router, personal-agents, spike-harness-driver, and the "clutch
   driving Pi" artifact brought in line with set A on an upstream b-number build (evidence 1).
2. `profile`: the profile schema and the Strix Halo llama.cpp profile, which holds llama.cpp to
   a minimum build (evidence 1).
3. `consumers`: the three consumers at once on the Vulkan router, and the baseline every
   platform is measured against, all run from the laptop (evidence 4, 5).
4. `main-model`: named sets in the profile, swapped over the router's API, with the probe and
   every instrument taking a set; gpt-oss-120b's comparison row on b11669; Mistral Small 4
   disqualified on Vulkan (evidence 4, 6).
5. `evaluation`: role-specific evaluation pipelines (agent, vision, audio, embedding) with
   staged gates (admission, fitness under ~3 minutes, capacity, quality, characterization);
   graded fixtures of rising complexity; a set evaluation for co-loaded models covering all four
   core roles; speed judged beside accuracy, with floors set by the hardware (evidence 4, 5).
6. `candidates`: candidates for every role, including Nemotron 3.5 Super and new vision, audio,
   and embedding candidates, run through the pipelines; the verdict and the startup set
   (evidence 4, 6).
7. `rocm`: llama.cpp's ROCm backend against Vulkan (evidence 2–7).
8. `engines-strix`: vLLM, and SGLang time-boxed, on Strix Halo (evidence 2–7).
9. `gateway`: the gateway approaches in front of the Framework (evidence 8, 10).
10. `admin`: needs go-cli-sdk v0.1.0. The admin tool over the profile (evidence 9).
11. `cuda`: needs the Dell NVIDIA workstation (from 2026-10-14). The CUDA profile and
   llama.cpp's CUDA router (evidence 2, 5, 7).
12. `engines-cuda`: needs the Dell NVIDIA workstation. vLLM and SGLang on CUDA; the gateway
   configurations and the admin tool rerun unchanged (evidence 2–9).
13. `validate`: evidence 1–10 on both hosts, the platform comparison, and the answer.

## References

The spike reads these repositories. It writes only to personal-agents and spike-harness-driver,
and only in its `align` task. Each is a key in the coordinator's
`references.toml`, with its local checkout in `references.local.toml`: `personal-agents` (the
running llama.cpp setup, the tier, context-sizing, and memory-footprint methods, and `outpost`),
`spike-harness-driver` (its `model` client and router findings), `tau-examples` (local-model
configurations), and `tau-protocol` (per-model capability options).

## The answer

Pending: the `validate` task writes it.
