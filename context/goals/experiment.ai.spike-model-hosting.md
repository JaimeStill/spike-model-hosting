# goal · experiment.ai.spike-model-hosting

- **State:** building
- **Task:** profile
- **Branch:** profile

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

## Task brief · profile

```
Problem       Every later task renders its serving setup from a host-class profile, and none
              exists. The Framework's router runs personal-agents' presets and a unit file
              written from its docs, on llama.cpp b11529, which trails upstream (b11565 at
              PLAN). The schema must carry each model's sampling and reasoning settings from
              its card. The spike also needs a note on reading a model card. There is no
              restore point. Evidence 1.
Behaviors     1. A profile missing a required field fails validation and names the field.
                 Required: host class, engine with backend and minimum build, bind mode,
                 slots, KV mode, the memory-budget record, models. Each model records ID,
                 publisher, origin, card URL, c, and its reason for c. Validation doesn't judge
                 origin; STANDARDS.md's convention governs it at review.
              2. A key the schema doesn't define fails validation and names the key, so host
                 state has nowhere to go. The service user, HF_HOME, and models directory are
                 inputs given at render time, and rendering fails without them.
              3. Each generative model has typed fields for temperature, top-p, top-k, min-p
                 and reasoning, each traced to its card URL. A value out of range fails
                 validation. The embedding model has none. Keys that only llama.cpp has
                 (pooling, b/ub, no-mmproj, embeddings) go in a raw per-model args map.
              4. Rendering the Strix Halo profile yields presets with shared defaults and four
                 sections: gpt-oss-120b, Gemma 4 26B-A4B, gemma-4-E4B, EmbeddingGemma 2. Their
                 sections, keys and values equal a pinned copy of personal-agents' tracked
                 96 GiB preset, apart from load-on-startup on each of the four. Each model's
                 reasons render as a comment above its section.
              5. Rendering yields the router's systemd unit:
                 - it runs the engine through the build's `current` link, in router mode, with
                   the rendered presets;
                 - it binds to the tailnet IPv4 the host resolves when the unit starts;
                 - it takes the render-time host values;
                 - it starts after tailscaled and restarts on failure.
              6. The profile tool lists each engine with its upstream repository and minimum
                 build. The profile pins no llama.cpp build, and `mise run currency` reports
                 no llama.cpp line.
              7. Installed on the Framework from exact commands the architect runs, the
                 rendered unit and presets serve set A. These checks run from the laptop over
                 HTTP:
                 - /health is ok;
                 - after a restart, /models lists the four as loaded with no manual load;
                 - gpt-oss-120b answers a chat and a /v1/messages tool call;
                 - 26B-A4B describes an image, and E4B transcribes an audio clip;
                 - EmbeddingGemma 2 returns 768-dimension embeddings;
                 - /props build_info is at or above the profile's minimum build, and the
                   probe reports it;
                 - each model's default sampling in /props equals the profile's.
              8. The Framework moves to the newest b-tag at BUILD time. The spike records its
                 review of the release notes after b11529: chat parser and /v1/messages,
                 Vulkan MoE, Gemma 4 input and embeddings, flags and preset keys, sampling
                 defaults. Presets change only where the notes require it, and the checks in
                 7 pass on that build.
              9. On that build, with all four loaded and 4 requests in flight on each, at
                 least 3 GiB of the 96 GiB pool is free. The spike's memory-budget table is
                 re-measured and names the build it ran on.
              10. `clutch conform --provider llama.cpp`, run 5 times on the Framework, has its
                 per-cell rates in the spike's tool-reliability note, beside align's b11529
                 rates, each set naming its build and where it ran. Any cell that falls is
                 named.
              11. A spike note explains how to read a model card and what each value means:
                 temperature, top-p, top-k, min-p, reasoning effort, chat template, context
                 length. It says why a model runs at its card's values, and lists the values
                 set A takes from each card, with sources.
              12. The spike's standards and README say: the profile and renderer carry no host
                 name or address (the unit resolves its tailnet address at start; host-local
                 values come at render time); llama.cpp is held to a minimum build, and every
                 document and measurement names the build it ran on.
Test seams    The profile package's load, validate and render API: golden renders, plus the
              pinned preset copy as test data. The profile tool's engine listing. One
              integration-tagged HTTP probe at SPIKE_ROUTER_URL (7, 8). Measured evidence on
              the Framework (9, 10).
Slices        1. Walking skeleton (1, 4 for one model): load, validate, render one section,
                 golden test.
              2. Strix Halo profile (2, 3, 4): set A, typed sampling, the closed schema,
                 equality with the pinned copy.
              3. Unit and engines (5, 6, 12): the rendered unit; the engine listing;
                 currency without a llama.cpp pin scan; the standards and README lines.
              4. Rendered setup live on the running b11529 (7): the architect installs it,
                 and the probe passes. The render change lands apart from the build change.
              5. Build move (8, 9, 10): release-note review, newest build installed, probe,
                 footprint, conform rates, memory-budget table.
              6. Model-card note (11), and the pending coordinator edits in the goal record.
Out of scope  Changes to personal-agents, spike-harness-driver or outpost. An install command
              (admin). The consumer suite and pass^k (consumers). ROCm, vLLM, SGLang,
              llama-swap, gateways. Rendering a CUDA profile (the schema allows the class).
              Computing a memory estimate. Mistral Small 4. Writing the architecture-layer
              page itself.
Door          two-way. The repository reverts by PR. On the Framework, b11529 stays in its
              versioned directory and `current` points back to it. personal-agents' tracked
              preset and documented unit restore the earlier setup through outpost. Decision 8
              accepts that there is no restore point.
```

## Progress

slices 0/6 committed · standards — · spec — · editor —

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
- profile (r1): the Framework moves to the newest b-tag at BUILD time (b11565 at PLAN); BUILD
  reviews release notes for chat parser, Vulkan MoE, Gemma 4 input/embeddings, flags; rejected
  holding b11529.
- profile (r3): llama.cpp is held to a minimum build (b11452, EmbeddingGemma 2's floor), and
  each measurement records /props build_info; rejected an exact b-number pin (never rendered,
  stale within hours at ~10 builds a day). Supersedes setup's exact-pin and b-tag currency
  decisions for llama.cpp.
- profile (r3): origin is recorded per model and judged by STANDARDS.md's convention at review;
  rejected a validator rule, which can only echo the declared origin.
- profile (r3): the schema is closed (unknown keys fail) and host values are render-time
  inputs; rejected a pattern scan for tailnet names and addresses.
- profile (r1): rendered presets equal personal-agents' unified-96gb.ini by sections, keys and
  values against a pinned test-data copy; rejected byte-identical text.
- profile (r1/r2): bind resolves `$(tailscale ip -4)` at unit start; rejected a render-time
  tailnet name (`framework` may resolve to LAN addresses on its own host, and no host name is
  committed). Clients still use the MagicDNS name.
- profile (r1): service user, HF_HOME, models dir are render-time inputs, not profile fields.
- profile (r1): presets render load-on-startup for set A.
- profile (r1): install by exact commands the architect runs; install verbs belong to admin.
- profile (r1): memory budget recorded, not validated; rejected an estimate check.
- profile (r1): typed sampling/reasoning fields + card URL, plus a raw llama.cpp args map.
- profile (r1): text/template; BurntSushi/toml the only new dependency; rejected gopkg.in/ini.v1
  and go-systemd's unit package.
- profile (r2): re-verify after the bump: align's probes, fresh footprint, 5-run conform rates
  in tool-reliability.md.
- profile (r2): personal-agents stays as align left it; the workspace's home for hosting
  configuration is ai-hosting (v1.ai.hosting) plus architecture-layer methods.
- profile (r2): the model-card note is written in the spike in profile; a pending edit carries
  it to intake.

## Pending edits

- coordinator · ai-hosting.md, "Where personal-agents' parts go": add "how to read a
  model card (spike-model-hosting's model-card note)" to the methods proposed for the
  architecture layer, and say that ai-hosting's per-model presets cite the card values that
  method reads.
- coordinator · roadmap, the spike's profile summary: "pinned exactly in the
  profile" and "Currency compares the profile's pins against the newest `b<N>` tag" become a
  minimum build, with each measurement naming the build it ran on.
