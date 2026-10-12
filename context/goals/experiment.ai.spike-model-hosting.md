# goal · experiment.ai.spike-model-hosting

- **State:** building
- **Task:** consumers
- **Branch:** consumers

## Tasks

1. [x] align
2. [x] profile
3. [ ] consumers
4. [ ] main-model
5. [ ] rocm
6. [ ] engines-strix
7. [ ] gateway
8. [ ] admin (needs: go-cli-sdk v0.1.0)
9. [ ] cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
10. [ ] engines-cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
11. [ ] validate

## Task brief · consumers

```
Problem       Evidence 4 and 5 need Pi, Claude Code and go-ai's model client to run against
              the Framework's Vulkan router at once, with no response crossing between
              requests. Evidence 5 needs that run measured, as the baseline every later
              platform is measured against. Tool-call reliability has five-run rates for Pi
              and OpenCode, but no pass^k against OpenAI's bar (0 invalid, >=90%). It has no
              Claude Code row, because clutch runs Claude Code only on Anthropic, and no
              reasoning round-trip check per consumer. The router runs b11565, and upstream
              was at b11569 at PLAN. The architect moved every consumer and instrument to the
              laptop: the Framework only hosts and serves inference. Mistral Small 4's probe
              moved to `main-model`.
Behaviors     1. The Framework moves to the newest b-tag at BUILD. The spike records its review
                 of the release notes after b11565: chat parser, /v1/messages and thinking,
                 Vulkan MoE, Gemma 4 input and embeddings, flags and preset keys, sampling
                 defaults. Presets change only where the notes require it. The probe passes,
                 and every measurement below names the build from /props build_info.
              2. The repository pins Pi 1.1.0, Claude Code 2.1.296, Node, Python and GuideLLM
                 0.8.1 exactly, and runs them from the laptop. `mise run currency` reports each
                 one when it trails. The check stays hermetic, and the architect's global
                 installs and ~/.claude stay untouched.
              3. Claude Code runs from a dedicated config directory in the spike's untracked
                 state, under the architect's subscription login. Its base URL is the router,
                 and opus, sonnet and haiku all map to gpt-oss-120b. A turn with tool use
                 succeeds over streamed /v1/messages, with thinking intact: thinking blocks
                 stream back, and the next request after a tool call carries them back to the
                 model, as seen on the wire. The run records the exact thinking request
                 Claude Code sends.
              4. One command runs four streams at once against the router: Pi over OpenAI chat,
                 Claude Code over /v1/messages, and the model client over chat and over
                 embeddings. It keeps every gpt-oss-120b slot busy, for a fixed number of
                 rounds. Each chat request carries a unique nonce. The run passes only when
                 every answer echoes its own nonce and no other. Each embedding must equal the
                 same input's embedding requested alone, within a stated cosine tolerance. An
                 injected swap makes the run fail and name the two requests.
              5. For each consumer, the record says whether it sends reasoning back on a turn
                 that called tools: Pi as reasoning_content, Claude Code as thinking blocks,
                 OpenCode with `interleaved` set. Each answer is seen on the wire.
              6. pass^5 per consumer on gpt-oss-120b (Vulkan): conform's 9 capabilities run 5
                 times for Pi, Claude Code over the router, and OpenCode with `interleaved`.
                 Each consumer is judged against 9 of 9 at 5 of 5, and every cell below is
                 named. The engine row is OpenAI's gpt-oss compatibility-test (30 cases, -k 5,
                 Chat Completions): invalid requests, pass@5 and pass^5. Every row names the
                 consumer version, the build, and that it ran from the laptop. Comparisons with
                 align's rates note that they cross a Pi major.
              7. GuideLLM measures gpt-oss-120b, 26B-A4B and E4B. The profiles are short
                 (512/256) and agentic (8192/512), each at concurrency 1 and 4. Each row gives
                 client TTFT, ITL and throughput beside the engine's own figures (timings,
                 /metrics), and names the network path (DERP relay, its RTT).
              8. Prefix reuse per model: a shared 8192-token prefix with varied suffixes. The
                 record shows cache_n and TTFT for the first request and for each later one.
              9. Cold load, timed from the laptop by polling /models until all four models are
                 loaded: after a warm restart, and after a restart with the page cache dropped.
              10. Footprint is sampled during behavior 4's run, beside the memory budget's
                 synthetic 4-in-flight figure, and at least 3 GiB of the pool stays free.
              11. The spike's instructions, README hosts and boundaries, and standards say:
                 consumers and instruments run from the laptop, and the Framework only hosts
                 and serves. The README's main-model pool points the probe to `main-model`.
Test seams    The concurrent-run command's API, tested hermetically against fake endpoints:
              nonce routing, swap detection, the embedding comparison. Building the conform
              cell for Claude Code over the router (its environment and model map). Integration
              runs at SPIKE_ROUTER_URL (3, 4, 6). Measured evidence (7-10).
Slices        1. Build move (1): the architect runs the root commands; release-note review;
                 the probe passes on the new build.
              2. Laptop toolchain (2): the exact pins, currency lines, the check unchanged.
              3. At once, walking skeleton (4 for Pi and the model client): nonces, the
                 embedding comparison, the fake-endpoint tests, one live run.
              4. Claude Code over the router (3, 5, and Claude Code joins 4): the architect
                 logs in once in the dedicated directory. The first request shows whether the
                 subscription works with the base-URL override; if it doesn't, BUILD
                 escalates.
              5. Reliability (5, 6): the conform cells, 5 runs, the compatibility-test row.
              6. Baseline (7-10): the GuideLLM grid, prefix reuse, cold load (the architect
                 runs the root command that drops the page cache), footprint during the run.
              7. Records and boundaries (11), and the pending coordinator edits.
Out of scope  Mistral Small 4 (main-model). Forcing tool_choice for OpenCode or Claude Code,
              and resample or fallback on a parse 5xx (gateway). Any change to
              spike-harness-driver, personal-agents or clutch itself. ROCm, vLLM, SGLang,
              llama-swap, gateways. Installing consumers on the Framework.
Door          two-way. The repository reverts by PR. On the Framework, the earlier b-tag stays
              in its versioned directory, and `current` points back to it. On the laptop,
              removing the pinned tools and the dedicated Claude config directory reverses
              everything; the subscription session ends at logout. One effect isn't undone by
              revert: with the base URL overridden, Claude Code sends its subscription bearer
              token to the router over the tailnet (plain HTTP inside WireGuard).
```

## Progress

slices 0/7 committed · standards — · spec — · editor —

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
  tailnet name (a host's own MagicDNS name may resolve to LAN addresses on that host, and no
  host name is committed). Clients still use the MagicDNS name.
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
- profile: the `[defaults]` table is required, with `c`, `gpu_layers`, and
  `checkpoint_min_step`.
- profile: the presets always render `jinja` and `metrics` on; the profile has no field for them.
- profile: `host_class`, `bind`, `kv`, and `engine.name` are closed sets, and validation refuses
  any other value.
- profile: each model declares its kind, generative or embedding; sampling is required on a
  generative model and refused on an embedding model.
- profile: `sampling.source` names the page the values come from, beside `card_url`, since
  gpt-oss's sampling comes from its GitHub README, not its card.
- profile: a non-empty `reasoning` renders as `chat-template-kwargs` `reasoning_effort`; set A
  sets none.
- profile: raw `args` refuse the keys the profile renders and a fixed list of keys the unit or
  host owns (`host`, `port`, `models-*`, path-valued flags): a list of keys, not a scan of
  values.
- profile: Host.Validate refuses shell and systemd metacharacters in host values, and a relative
  HF_HOME or models directory.
- profile: the unit keeps `Requires=tailscaled.service`, and keeps `--no-models-autoload`
  whether or not the presets set `load-on-startup`.
- profile: `cmd/profile` takes host values from `-user`, `-hf-home`, and `-models-dir`, or else
  from SPIKE_SERVICE_USER, SPIKE_HF_HOME, and SPIKE_MODELS_DIR.
- profile: the probe never loads a model (`?autoload=false`) and checks the content of the chat,
  image, and audio answers.
- profile: clutch conform on b11565 ran the Framework's existing clutch build from 5afeac3, whose
  code is 37fa153's, with OpenCode 1.18.34 installed with mise as the Framework's unprivileged account.
- profile: EmbeddingGemma 2's `card_url` is Google's card; its ID stays the ggml-org GGUF
  repository.
- profile (accepted): the presets installed on the Framework equal the committed render;
  tool-reliability.md records three untested levers for OpenCode (force the shape, send
  reasoning back, retry the parse 5xx) for consumers and gateway.
- consumers (r1): Mistral Small 4's promotion probe becomes its own task, `main-model`, after `consumers` and before `rocm`.
- consumers (r1): pass^k imports clutch's conform package and the claude driver at spike-harness-driver's pseudo-version (v0.0.0-20261009211659-9981a3e37466 or newer at BUILD). The Claude-Code-over-router cell is built through the driver's Env. k = 5 over the 9 capabilities. The bar is 0 invalid and pass^5 >= 90% (9 of 9).
- consumers (r1): OpenAI's gpt-oss compatibility-test (30 cases, -k 5) is the engine row, and gives the count of invalid requests.
- consumers (r1): a Go command in the spike drives Pi and Claude Code through spike-harness-driver's drivers, and the `model` client, at once, with per-request nonces.
- consumers (r1, architect): every consumer and instrument runs from the laptop, and the Framework only hosts and serves inference. This supersedes setup's "consumers run on the Framework" and the laptop boundary in CLAUDE.md.
- consumers (r1): the latest consumers (Pi 1.1.0, Claude Code 2.1.296), with versions recorded. Comparisons with align cross a Pi major.
- consumers (r1): OpenCode's `interleaved` setting is tried here. Forcing tool_choice stays in `gateway`.
- consumers (r2): GuideLLM 0.8.1 on the laptop at concurrency 1 and 4, with server-side numbers as the engine figures. The network path (DERP relay, 58 ms) is recorded.
- consumers (r2): pi, claude, node, python and guidellm are pinned in the repository's mise.toml and covered by currency. Global installs stay untouched.
- consumers (r2, architect): Claude Code uses the architect's subscription login with ANTHROPIC_BASE_URL at the router, and every tier maps to gpt-oss-120b. Rejected: a placeholder token. BUILD escalates if the override fails.
- consumers (r2): the router moves to the newest b-tag at BUILD start, with release-note review and probe.
- consumers (r2): cold load is measured warm and with the page cache dropped, polled from the laptop.
- consumers (r2): footprint is sampled during the real concurrent run, beside the synthetic figure.
- consumers (r3): a dedicated CLAUDE_CONFIG_DIR in untracked spike state, logged in once with the subscription.
- consumers (r3): the GuideLLM grid is short 512/256 and agentic 8192/512 at concurrency 1 and 4 on three models, plus a shared-prefix run per model.
- consumers (r3): "thinking intact" means thinking blocks stream back and return on tool turns. The exact form is observed at BUILD.

## Pending edits

- coordinator · ai-hosting.md, "Where personal-agents' parts go": add "how to read a
  model card (spike-model-hosting's model-card note)" to the methods proposed for the
  architecture layer, and say that ai-hosting's per-model presets cite the card values that
  method reads.
- coordinator · roadmap, the spike's profile summary: replace "pinned exactly in the profile"
  and "Currency compares the profile's pins against the newest `b<N>` tag" with a minimum
  build (b11452), with each measurement naming the build it ran on.
- coordinator · ai-hosting.md, "The existing setup: personal-agents", the Serving line: it
  names release build b11529; the Framework's router now runs b11565 from the spike's rendered
  presets and unit, which hold llama.cpp to a minimum build (b11452).
- coordinator · roadmap, consumers summary: the consumers run from the laptop against the Framework's router; drop the Mistral sentence; add pass^5 over conform's 9 capabilities plus the compatibility-test engine row.
- coordinator · roadmap: add task `main-model` (the Mistral Small 4 probe) after `consumers`, before `rocm`.
- coordinator · roadmap, the spike's goal summary: the new laptop boundary.
- coordinator · ai-hosting.md, "Experiment: spike-model-hosting": the Hosts boundary change; point the main-model probe to `main-model`.
