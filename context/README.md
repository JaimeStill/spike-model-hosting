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
- **Main-model pool:** gpt-oss-120b leads. Mistral Small 4 replaces it only if it matches
  gpt-oss-120b on `/v1/messages` and Pi tool calls and its vision works on Vulkan; the
  `consumers` task runs that probe. Gemma 4 26B-A4B completes the pool.
- **Shared model set (set A):** gpt-oss-120b (ggml-org MXFP4, the main model), Gemma 4 26B-A4B
  (vision), gemma-4-E4B (audio and the small model), and EmbeddingGemma 2 (embeddings), each in
  its platform's native format: GGUF for llama.cpp, safetensors for vLLM and SGLang. On the
  Framework all four stay loaded together; Mistral Small 4 swaps in for gpt-oss-120b and Gemma 4
  26B-A4B. The workstation's set is sized to its VRAM in the `cuda` task, whose candidate list
  adds the dense models Mistral Medium 3.5, Gemma 4 31B, and Muse Glimmer 30B.
- **Model origin:** models of Chinese origin are avoided where an alternative serves
  (`standards-lab/context/ai-strategy.md`, "Constraint: model origin").
- **Versions:** every engine, gateway, and model runs its latest upstream release. Profiles pin
  the exact versions, `mise run currency` reports the pins that trail, and every document names
  the exact versions in use when it is written. llama.cpp is pinned as a b-number build, because
  its semver releases ship no binaries and lag its model support.

## Hosts and boundaries

- **Framework desktop** (Strix Halo, 96 GB GTT pool): the first host class, available now. The
  spike may change anything on it, including the runtime, its configuration, and the models.
  Only the OS install and the Tailscale registration stay fixed, and there is no restore point.
  Set A's memory budget in its pool, estimated against measured, is in
  [`memory-budget.md`](memory-budget.md).
- **Dell NVIDIA workstation** (CUDA): available from 2026-10-14. The tasks that need it say so.
- **Remote access:** sessions reach both hosts by Tailscale SSH in accept mode, set by the
  architect before the first task and reverted to check mode when the spike completes.
- **The architect's laptop:** it runs only this repository, its check, read-only probes, and
  HTTP. Nothing is installed, served, or run as a consumer there.
- Nothing committed here names a host: no tailnet names or addresses, no hostnames.

## Capabilities

- **Host-class profile:** the TOML schema, its validator, and its renderer to each platform's
  presets, configuration, and systemd units.
- **Serving platforms:** llama.cpp (Vulkan, ROCm, CUDA), vLLM, SGLang, and llama-swap, each with
  its setup and management record.
- **Consumers:** Pi, Claude Code, and spike-harness-driver's `model` package as go-ai's
  stand-in, run on the hosts.
- **Measurement:** footprint, speed, prefix cache, cold load, and the capability matrix.
- **Tool-call reliability:** the layered convention every platform, profile, consumer, and
  gateway is held to, with pass^k as its measure, in [`tool-reliability.md`](tool-reliability.md).
- **Gateways:** none, agentgateway, and LiteLLM.
- **Admin tool:** a CLI on go-cli-sdk over the profile, with personal-agents' `outpost` as its
  baseline.
- **Host runbooks:** what each host needs to run every platform kept.

## Path

The tasks, in the order they run; each cites the evidence it proves.

1. `align`: the Framework's router, personal-agents, spike-harness-driver, and the "clutch
   driving Pi" artifact brought in line with set A on an upstream b-number build (evidence 1).
2. `profile`: the profile schema and the Strix Halo llama.cpp profile (evidence 1).
3. `consumers`: the three consumers at once on the Vulkan router, the baseline every platform is
   measured against, and Mistral Small 4's promotion probe (evidence 4, 5).
4. `rocm`: llama.cpp's ROCm backend against Vulkan (evidence 2–7).
5. `engines-strix`: vLLM, and SGLang time-boxed, on Strix Halo (evidence 2–7).
6. `gateway`: the gateway approaches in front of the Framework (evidence 8, 10).
7. `admin`: needs go-cli-sdk v0.1.0. The admin tool over the profile (evidence 9).
8. `cuda`: needs the Dell NVIDIA workstation (from 2026-10-14). The CUDA profile and llama.cpp's
   CUDA router (evidence 2, 5, 7).
9. `engines-cuda`: needs the Dell NVIDIA workstation. vLLM and SGLang on CUDA; the gateway
   configurations and the admin tool rerun unchanged (evidence 2–9).
10. `validate`: evidence 1–10 on both hosts, the platform comparison, and the answer.

## References

The spike reads these repositories. It writes only to personal-agents and spike-harness-driver,
and only in its `align` task. Each is a key in the coordinator's
`references.toml`, with its local checkout in `references.local.toml`: `personal-agents` (the
running llama.cpp setup, the tier, context-sizing, and memory-footprint methods, and `outpost`),
`spike-harness-driver` (its `model` client and router findings), `tau-examples` (local-model
configurations), and `tau-protocol` (per-model capability options).

## The answer

Pending: the `validate` task writes it.
