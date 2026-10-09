# goal · experiment.ai.spike-model-hosting

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [x] align
2. [ ] profile
3. [ ] consumers
4. [ ] rocm
5. [ ] engines-strix
6. [ ] gateway
7. [ ] admin (needs: go-cli-sdk v0.1.0)
8. [ ] cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
9. [ ] engines-cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
10. [ ] validate

## Task brief · align

```
Problem       The spike's starting pool is settled (decisions 1–3), but nothing runs or
              documents it. The Framework's router runs Arch's llama.cpp b10809, which predates
              EmbeddingGemma 2, with Qwen models. personal-agents' tracked profile, tier
              recipes, and Pi docs name Qwen. spike-harness-driver's defaults and tests name Qwen.
              The "clutch driving Pi" artifact shows runs on those models. profile renders from
              an aligned starting point, so align runs first.
Behaviors     1. The Framework's router runs llama.cpp's upstream Vulkan x64 release: the newest
                 b-number build at BUILD time, b11529 or newer, in a versioned directory. Arch's
                 llama-cpp and ggml-vulkan packages and the pacman restart hook are gone. The
                 build the server reports matches the one personal-agents' setup names.
              2. personal-agents' tracked 96 GiB profile holds exactly four model sections, each
                 with its recipe comment: gpt-oss-120b (ggml-org MXFP4, c 131072), Gemma 4
                 26B-A4B (ggml-org Q4_0 with its mmproj, c 65536), gemma-4-E4B (ggml-org Q8_0
                 with its mmproj, c 32768), and EmbeddingGemma 2 (ggml-org Q8_0, embeddings, no
                 mmproj). Each runs 4 slots on a unified KV cache, and no Qwen section remains.
              3. In personal-agents:
                 - Every tier recipe names a model of non-Chinese origin.
                 - Pi's setup and the host's preparation name gpt-oss-120b.
                 - The setup doc, unit template, and hooks describe the upstream install.
                 - The worked examples use set A.
                 - The slot-persistence task targets a Pi session on gpt-oss-120b.
                 - No committed file names the Framework's tailnet address.
                 - Every cross-reference resolves to the file and section it cites.
              4. Installed with outpost (preset install, service restart), the router passes these
                 checks from the laptop over HTTP:
                 - /health is ok, and /models lists the four.
                 - gpt-oss-120b answers a chat and a /v1/messages tool call.
                 - Gemma 4 26B-A4B describes an image.
                 - gemma-4-E4B transcribes an audio clip.
                 - EmbeddingGemma 2 returns 768-dimension embeddings.
              5. With all four loaded and four concurrent requests per model, `outpost amd
                 usage` shows at least 3 GiB of the 96 GiB pool free. If it shows less,
                 26B-A4B's c drops to 32768 and the check runs again. The presets hold the final
                 c values. The footprint doesn't grow with the slot count, which confirms
                 --kv-unified for each model. personal-agents' usable-memory rule names this
                 measured test as the binding one.
              6. personal-agents and the spike document the same memory-budget table:
                 - each model's weights, buffers, and KV at its c
                 - the estimated and measured totals
                 - the llama.cpp build measured
                 - the Mistral Small 4 swap (gpt-oss-120b and 26B-A4B out)
              7. spike-harness-driver's defaults are gpt-oss-120b ggml-org MXFP4 (the Pi and
                 OpenCode harness model), Gemma 4 26B-A4B (vision), EmbeddingGemma 2 (embed),
                 and gemma-4-E4B (audio). Its embed scenario uses EmbeddingGemma 2's query form
                 and its document form. No live default, test, or setup document names a Qwen
                 model; historical findings and fixtures stay. `mise run check` passes, and
                 `clutch conform` runs against the router.
              8. clutch, run on the Framework against the aligned router, completes the
                 artifact's ten router scenarios. The artifact shows those runs with their model
                 IDs and llama.cpp build; the Azure runs stay as they were.
Test seams    The router's HTTP API, probed from the laptop (1, 4), with `outpost amd usage` on
              the Framework (5). In spike-harness-driver, clutch's default-resolution tests
              behind `mise run check` (7).
Slices        1. Build (1): the upstream Vulkan build serves the current presets unchanged, so
                 the build move lands separately from the model change. personal-agents'
                 setup, unit template, and hook removal land here.
              2. Set A served (2, 3, 4, 5): personal-agents' profile and docs, installed with
                 outpost, probed, and measured; contexts adjusted if the margin falls short.
              3. Memory-budget docs (6): the measured table in both repositories.
              4. spike-harness-driver (7): defaults, tests, the embed prompt forms, and the setup
                 doc.
              5. Artifact (8): clutch and Pi at clutch's pins on the Framework; the ten router
                 runs re-captured and republished.
Out of scope  The profile schema and renderer; the consumer suite and measurements; Mistral
              Small 4's promotion probes (consumers); the CUDA candidate list; the laptop's Pi
              configuration; personal-agents' research history apart from the tailnet address;
              outpost's code apart from what the build move needs; any roadmap entry apart from
              slot-persistence.
Door          two-way for the repositories (each reverts by PR). On the Framework, decision 8
              accepts the change; the earlier configuration survives in personal-agents' git
              history.
```

Amendment (round 6): behavior 9 — the spike note context/tool-reliability.md holds the
convention with sources; personal-agents' presets use each model's official embedded template
and its card's sampling, with no patched template and no non-default reasoning effort; clutch
sets tool_choice "required" on Pi requests that offer `respond`, and the skill fixture no longer
contradicts it; `clutch conform --provider llama.cpp`, run 5 times, has its per-cell rates
recorded in spike-harness-driver's findings. Behavior 8's bar is a clean capture of each of the
ten scenarios (a re-run is allowed and recorded). Slice 4b (9) runs before slice 5.

## Decisions

- setup: the evidence is the ten items in `context/README.md`, "The evidence".
- setup: kind code, Go; `mise run check` is hermetic, modeled on spike-cli-architecture's, plus
  shellcheck over `scripts/`; `mise run integration` probes a router over HTTP at
  `SPIKE_ROUTER_URL`.
- setup: `mise run currency` covers the module, the Go directive, mise tools, and every engine
  and gateway pin the profile tool lists, against each upstream's newest release tag,
  pre-releases included.
- setup: merge is plain `gh pr merge --merge --delete-branch`; no CI.
- setup: references are keys in the coordinator's catalog: personal-agents, spike-harness-driver,
  tau-examples, tau-protocol. The spike keeps no references files.
- setup: platforms live are llama.cpp (Vulkan, ROCm, CUDA), vLLM, and SGLang (time-boxed on
  Strix Halo); Lemonade only as an install path; Ollama, LM Studio, and TGI on paper.
- setup: llama.cpp keeps its native router; vLLM and SGLang run behind llama-swap.
- setup: gateways live are agentgateway and LiteLLM, against no gateway; Envoy AI Gateway
  (Agent Router) and vLLM production-stack on paper.
- setup: the shared model set is gpt-oss-120b, EmbeddingGemma 2 (270M text model first), and
  gemma-4-E4B, each in its platform's native format. Qwen presets survive only in the restore
  point.
- setup: models of Chinese origin are avoided where an alternative serves (`ai-strategy.md`,
  "Constraint: model origin").
- setup: every engine, gateway, and model tracks its latest upstream release; profiles pin the
  exact versions, and documents name the exact versions in use when written. The Framework moves
  from Arch's lagging llama-cpp package to llama.cpp's upstream Vulkan x64 release.
- setup: consumers run on the Framework. The laptop runs only the repository, its check,
  read-only probes, and HTTP. The spike may change anything on the Framework and the
  workstation.
- setup: Tailscale SSH in accept mode for the Framework and the workstation, set by the architect
  before `profile`; `validate` reverts it to check mode.
- setup: go-ai's model client is spike-harness-driver's `model` package at a pseudo-version.
- setup: profiles are TOML read with BurntSushi/toml; rejected encoding/json, which can't hold
  the presets' reasoning as comments.
- setup: the admin tool builds on go-cli-sdk; rejected the standard library's flag alone, since
  go-cli-sdk is the workspace's CLI standard. It stays sixth, before the workstation tasks: the
  architect expects go-cli-sdk before the workstation arrives.
- setup: a task marked "needs" hands off at LOCATE until its prerequisite exists.
- plan: the main-model pool is gpt-oss-120b (leads), Mistral Small 4, and Gemma 4 26B-A4B;
  Mistral Small 4 is promoted only if it matches gpt-oss-120b on /v1/messages and Pi tool calls
  and its vision works on Vulkan (`consumers` probes it); rejected the dense models (Mistral
  Medium 3.5, Gemma 4 31B, Muse Glimmer 30B) on Strix Halo, at 7-19 tok/s against ~55; they go
  to the CUDA candidate list.
- plan: set A is loaded while gpt-oss-120b leads: gpt-oss-120b MXFP4 c 131072, Gemma 4 26B-A4B
  Q4_0 with mmproj (vision) c 65536, gemma-4-E4B Q8_0 with mmproj (audio) c 32768, EmbeddingGemma
  2; 4 slots on --kv-unified, ~92 of 96 GiB; rejected gemma-4-E4B for vision too (~75 GiB),
  since gpt-oss-120b's pool already reaches its trained 131072. Mistral Small 4 swaps in for
  gpt-oss-120b and 26B-A4B. Supersedes setup's three-model set.
- plan: gpt-oss-120b is ggml-org MXFP4 everywhere; rejected unsloth Q4_K_M.
- plan: `align` runs before `profile` and writes personal-agents and spike-harness-driver, the
  one exception to read-only references; the goal's `repos` lists both for align only, an
  exception to a spike's repos being its own repository.
- plan: personal-agents drops Qwen from every tier recipe, profile, Pi doc, and worked example;
  slot-persistence targets gpt-oss-120b; its tailnet address is removed.
- plan: the artifact's ten Framework runs are re-captured on the Framework; its Azure runs stay.
- plan: on unified memory the binding test is a measured footprint with every model loaded and
  at least 3 GiB of the pool free; 85-90% stays the planning estimate. Under 3 GiB, 26B-A4B's c
  drops to 32768 first.
- plan: no restore point; on the Framework only the OS install and the Tailscale registration
  stay fixed. Upstream's Vulkan x64 release replaces Arch's llama-cpp and ggml-vulkan and the
  pacman hook; the prior configuration survives in personal-agents' history.
- plan: spike-harness-driver declares a hermetic `mise run check`; it and personal-agents merge
  with plain `gh pr merge --merge --delete-branch`.
- plan: llama.cpp pins are b-number builds; rejected semver releases, which ship no binaries and
  lag model support (v0.6.0 = b11430 predates EmbeddingGemma 2, #30054). Currency compares
  against the newest `b<N>` tag.
- plan: profile's approved brief is revised as standards-lab #73 and this repository's #1 state:
  no restore point, set A, b-number pin, semver tags filtered from currency.

- plan (align, round 6): tool-call reliability follows the field's layered convention, not
  per-model patches. Mandatory shapes are constrained at the server; presets use official
  templates and each card's sampling; clients send reasoning back on tool turns; the harness
  returns validation errors to the model; the gateway does a bounded resample and then falls
  back on a parse 5xx; reliability is measured as pass^k. Rejected the patched gpt-oss template
  and reasoning-effort high (personal-agents 75107bd, 628056a; reverted), and rejected an
  upstream comment on llama.cpp #25321 (architect).
- plan (align, round 6): clutch sets tool_choice "required" on Pi requests that offer `respond`,
  and the skill fixture stops contradicting `respond`; OpenCode stays on `auto`, measured.
- plan (align, round 6): align's bar is clean captures of the artifact's ten scenarios, plus
  5-run conformance rates recorded as evidence; pass^k against OpenAI's bar (0 invalid, >=90%)
  belongs to consumers; parse-5xx resample and fallback belong to gateway.
- align: llama.cpp lives in /opt/llama.cpp/<tag>, with a `current` symlink the unit runs, so a
  build move never edits the unit; Arch's ggml was removed along with llama-cpp and
  ggml-vulkan.
- align: the retired models moved out of the router's HF cache into a retired directory beside
  it on the same drive, not deleted, because the router lists every cached model.
- align: EmbeddingGemma 2 runs with no-mmproj, mean pooling, c 8192, and b/ub 8192.
- align: card sampling is set explicitly, with top-k 0 and min-p 0 where the card names none,
  because b11529 applies a GGUF's general.sampling.* keys and gpt-oss's GGUF carries none.
- align: tool_choice "required" is forced only on OpenAI-shaped requests; Anthropic's forced
  form is refused with extended thinking on.
- align: personal-agents' research follow-ups note records the hook's retirement, and
  install.sh drops dangling outpost links; both are accepted as part of the build move (spec
  review gaps 1 and 2).
- align: behavior 5's "footprint doesn't grow with slots" is shown by 0, 1, and 4 requests in
  flight at parallel 4, with --kv-unified in each model's args; accepted as meeting the brief
  (spec review question 3).
- align: the architect ran the root steps on the Framework from exact commands (slice 1's
  escalation).
- align (accepted): the artifact page widens to 1680px with a 560px left column, and its
  setup uses outpost (version 5).
- align (accepted): personal-agents' Pi setup stays unpinned, so Pi tracks its releases; the
  spike-harness-driver pins stay for conformance.
- align (accepted): the coordinator edits land at align's closeout through a plan pull request,
  so `profile` starts next session.

## Pending edits

- coordinator: `context/roadmap.toml`: gateway evaluates bounded resample and fallback on a
  parse 5xx; consumers measures pass^k per model, engine, and consumer against 0 invalid and
  >=90%, and tests each consumer's reasoning round-trip; profile's schema carries per-model
  sampling and reasoning settings; validate answers the tool-call reliability convention per
  platform.
- coordinator: `context/roadmap.toml`: drop personal-agents and spike-harness-driver from the
  goal's `repos` and its comment, once align merges (a `plan` run, before `profile`).
- coordinator: `context/ai-hosting.md`: personal-agents' served models (line ~23, Qwen3-Coder-Next
  at 131k and gpt-oss-120b at 32k) become set A as align measured it.
- coordinator: `context/ai-hosting.md`, "Where personal-agents' parts go": drop "the pacman
  restart hook" from what goes into `ai-hosting` (align retired it; the build runs from
  /opt/llama.cpp/current), and add the tool-call reliability convention (this spike's
  `context/tool-reliability.md`) to the serving conventions promoted to the architecture layer.
