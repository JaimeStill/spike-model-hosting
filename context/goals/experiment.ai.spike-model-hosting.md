# goal · experiment.ai.spike-model-hosting

- **State:** building
- **Task:** main-model
- **Branch:** main-model

## Tasks

1. [x] align
2. [x] profile
3. [x] consumers
4. [ ] main-model
5. [ ] rocm
6. [ ] engines-strix
7. [ ] gateway
8. [ ] admin (needs: go-cli-sdk v0.1.0)
9. [ ] cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
10. [ ] engines-cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
11. [ ] validate

## Task brief · main-model

```
Problem       gpt-oss-120b leads the Framework's main-model pool, and it can't see images.
              Two vision MoEs may replace it and Gemma 4 26B-A4B: Mistral Small 4 (Mistral
              AI, France, Apache 2.0) and Nemotron 3.5 Super VL (NVIDIA, US, OpenMDW-1.1).
              The plan promotes one only if it matches gpt-oss-120b per consumer on Pi tool
              calls and Claude Code over /v1/messages, and its vision works on Vulkan. The
              profile describes one model set, all loaded at startup; the instruments are
              fixed on gpt-oss-120b; the router runs b11572; four consumer pins trail. Vulkan
              runs Mistral Small 4's expert down-projections on the CPU on every current
              build (llama.cpp #30105, open). Sessions may only observe the Framework.
Behaviors     1. The repository pins Pi 1.2.0, Claude Code 2.1.300, node 26.12.0 and uv
                 0.13.1 exactly; Python stays held at 3.14.8 (no torch 3.15 wheels).
                 Currency reports only that hold; the check stays hermetic.
              2. The Framework moves to the newest b-tag at BUILD (b11669 at PLAN). The
                 spike records its release-note review since b11572: chat output rewrite,
                 template number formatting, embedding path, Vulkan KV on AMD, SWA reuse,
                 the Ministral 3 and Qwen3-Coder handlers, Nemotron support, and whether
                 Vulkan still refuses F32 MUL_MAT_ID. Presets change only where the notes
                 require it. The probe passes on set A; every measurement names the build
                 from /props build_info.
              3. A profile holds named sets: each lists its models (at most 4) and its own
                 memory budget; exactly one is the startup set. Each model declares roles
                 (chat, vision, audio, embedding) and a licence beside its origin.
                 Validation refuses no or several startup sets, a set over 4 models, a set
                 naming an unlisted model, a set with no chat model, an unknown role, a
                 missing licence. Only the startup set renders load-on-startup. Set A as
                 the startup set renders the same sections, keys and values as before.
              4. The Strix Halo profile holds three sets, each value traced to its card;
                 every set includes gemma-4-E4B and EmbeddingGemma 2. Set A (startup)
                 unchanged. Mistral Small 4: unsloth UD-Q4_K_XL, the projector the router
                 picks, c 131072, reasoning high, temp 0.7, top_p 1.0, top_k 0, min_p 0.
                 Nemotron 3.5 Super: ggml-org NVFP4, c 131072, the template's reasoning
                 (thinking on), temp 1.0, top_p 0.95, top_k 0, min_p 0, 8 context
                 checkpoints per slot.
              5. One command swaps the router between named sets over its HTTP API:
                 unloads the outgoing set's models the incoming set doesn't share, then
                 loads the rest, timing each step, ending with exactly the incoming set
                 loaded. It refuses an unknown set and never loads before the unloads
                 finish. Models download through the router's own Hugging Face fetch.
              6. The probe and every instrument take a set name (conform, concurrent,
                 wire, GuideLLM grid, prefix, cold load, footprint), find their chat,
                 vision, audio and embedding models from its roles, and refuse a set that
                 isn't loaded. Set A is the default, so consumers' runs reproduce.
              7. Each consumer runs each candidate with a reasoning setting its card
                 accepts (e.g. Pi "high" for Mistral Small 4); Claude Code unchanged. The
                 wire record shows, per candidate and consumer, the reasoning request sent
                 and the reasoning returned on a tool turn.
              8. gpt-oss-120b's pass^5 runs again on the new build and pins (Pi, Claude
                 Code over the router; OpenCode measured beside): the comparison row,
                 every cell named.
              9. Each candidate is judged against that row: Pi 9/9 at 5/5, 0 invalid;
                 Claude Code over streamed /v1/messages, every cell at or above
                 gpt-oss-120b's same-build count, no more invalid, thinking streamed and
                 returned on tool turns; vision cells 5/5 for Pi and Claude Code and the
                 probe's image answer correct; engine generation ≥ 24 tok/s one stream,
                 short profile. OpenCode measured, not judged. Every candidate gets the
                 full bar and measurement set whatever its speed.
              10. A candidate that misses only the speed floor re-runs once at
                 the next-smaller native-kernel quant: Nemotron at ggml-org Q4_0; none for
                 Mistral Small 4 (no quant avoids its CPU fallback). Nothing below ~4 bits;
                 gpt-oss-120b stays MXFP4. Judged against the full bar; the record gives
                 each candidate's estimated decode ceiling.
              11. Each candidate set is measured: the concurrent run; footprint at 4 in
                 flight with ≥ 3 GiB of the pool free, plus a system-RAM column for
                 Nemotron; the GuideLLM grid (short and agentic, 1 and 4 streams); prefix
                 reuse; the set's cold load. For Mistral Small 4 the record shows the CPU
                 fallback from the router log. No engine row.
              12. Verdict: a failing candidate is "not promoted on Vulkan, Strix Halo,
                 b<N>", with its failing cells and re-probe triggers (#30105, #30260 for
                 Mistral Small 4). If both pass, more Claude Code cells at 5/5 leads,
                 single-stream speed breaks a tie. The winner's set becomes the startup
                 set (router restarted on it; README names it the lead). With no pass,
                 set A stays. The task closes either way.
              13. The model-card note catalogs, for set A and both candidates: trained or
                 maximum context, KV bytes per token, recurrent state per sequence, and the
                 c the profile sets, with its reason.
              14. The spike's instructions, README and standards say sessions may do on
                 the Framework whatever needs no sudo (router HTTP incl. load, unload and
                 downloads; user-level SSH reads; logs). Root stays the architect's, by
                 exact commands.
              15. Decisions records that the workstations stay on DGX OS 7. Pending edits
                 add the roadmap's main-model entry and consumers summary, and a note for
                 the next plan session with the workstation facts (aarch64 GB10, sm_121,
                 273 GB/s unified; DGX OS 7.5, driver 580, CUDA 13.0; two units over
                 200GbE; nvidia-smi can't report memory; dense candidates' decode ceiling;
                 start dates and needs markers; admin's go-cli-sdk need met).
Test seams    The profile package's load, validate and render (sets, roles, licence,
              startup flag, per-set budgets). The swap command against a fake router. Set
              resolution in the instruments against fake endpoints. Integration probes per
              set at SPIKE_ROUTER_URL (2, 4, 5). Measured evidence (8-12).
Slices        1. Upgrade (1): consumer pins; currency shows only the Python hold; check.
              2. Build move (2): the architect's root commands; release-note review; probe
                 on set A.
              3. Sets and roles (3), set A only: validation tests; set A's render equals
                 the installed presets.
              4. Swap and set-aware instruments, walking skeleton (5, 6): swap set A out
                 and back, timed; every instrument by set name against set A.
              5. Candidates in the profile (4): both sets render and download; each loads
                 by swap; probe and footprint pass per set.
              6. Comparison row (8): gpt-oss-120b pass^5 on the new build.
              7. Mistral Small 4 (7, 9, 11, 10's record): settings, bar, measurements,
                 CPU fallback.
              8. Nemotron 3.5 Super (7, 9, 11), and 10's Q4_0 re-run if it applies.
              9. Verdict (12): tie-break; startup set changes (architect's restart) or
                 stays set A.
              10. Records (13, 14, 15): context catalog, boundary text, Decisions, pending
                 edits and note.
Out of scope  The workstations (cuda and engines-cuda re-planned after this task). ROCm,
              vLLM, SGLang, llama-swap, gateways. Forcing tool_choice; parse-5xx resample
              or fallback. Building llama.cpp from source or patching #30105. Speculative
              decoding with an MTP head. Quants below ~4 bits; Mistral Small 4 NVFP4.
              Dense models. A template-arguments field (Nemotron's medium effort);
              separate sampling for vision. Any change to spike-harness-driver,
              personal-agents or clutch.
Door          two-way. The repository reverts by PR. On the Framework the earlier b-tag
              stays in its versioned directory and `current` points back; the startup set
              reverts by rendering set A's presets and restarting; the candidates (~150 GB
              of 657 GB free) move to the retired directory beside the cache. Swaps and
              loads undo on restart. On the laptop, removing the new pins restores the old.
```

## Progress

- PLAN approved 2026-10-15.
- Slice 1 committed (ff8238d): Pi 1.3.0, Claude Code 2.1.301, OpenCode 1.19.1, node 26.12.0,
  uv 0.13.1 (newer than the brief's PLAN versions; nothing to adapt); currency shows only the
  Python hold.
- Slice 2: root commands for b11669 handed to the architect; awaiting the install.
- Slice 3 committed (bed1dac): named sets with per-set budgets, roles, licences; non-startup
  models render no load-on-startup key; a model in no set is refused.
- Slice 4 committed (f4077c9, a4158b2): sets name a model per role ([sets.roles]); `swap`
  command and package; every instrument takes -set (SPIKE_SET fallback, default the startup
  set); embedding models record dims. Live swap of set A not yet run (router still b11572).
- Slice 5: profile half in progress (hermetic); live half waits on b11669.

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
- consumers (r1): Mistral Small 4's promotion probe becomes its own task, `main-model`, after
  `consumers` and before `rocm`.
- consumers (r1, architect): every consumer and instrument runs from the laptop. The Framework
  only hosts and serves inference: the architect runs the root commands that configure its
  hosting layer, and sessions observe that layer read-only (amdgpu_top over ssh). Supersedes
  setup's "consumers run on the Framework" and CLAUDE.md's laptop boundary.
- consumers (r1): pass^k imports clutch's conform package and the claude driver at
  spike-harness-driver's pseudo-version (v0.0.0-20261009211659-9981a3e37466 or newer at BUILD);
  the Claude-Code-over-router cell is built through the driver's Env; k = 5 over the 9
  capabilities; the bar is 0 invalid and pass^5 >= 90% (9 of 9).
- consumers (r1): OpenAI's gpt-oss compatibility-test (30 cases, -k 5) is the engine row and
  gives the count of invalid requests.
- consumers (r1): a Go command in the spike drives Pi and Claude Code through
  spike-harness-driver's drivers, and the `model` client, at once, with per-request nonces.
- consumers (r1): the latest consumers (Pi 1.1.0, Claude Code 2.1.296), versions recorded;
  comparisons with align cross a Pi major.
- consumers (r1): OpenCode's `interleaved` setting is tried here; forcing tool_choice stays in
  `gateway`.
- consumers (r2): GuideLLM 0.8.1 on the laptop at concurrency 1 and 4, with server-side numbers
  as the engine figures; the network path (DERP relay, 58 ms) is recorded.
- consumers (r2): pi, claude, node, python and guidellm are pinned exactly and covered by
  currency; global installs stay untouched.
- consumers (r2, architect): Claude Code uses the architect's subscription login with
  ANTHROPIC_BASE_URL at the router, and every tier maps to gpt-oss-120b; rejected a placeholder
  token; BUILD escalates if the override fails.
- consumers (r2): the router moves to the newest b-tag at BUILD start (b11572), with
  release-note review and probe.
- consumers (r2): cold load is measured warm and with the page cache dropped, polled from the
  laptop.
- consumers (r2): footprint is sampled during the real concurrent run, beside the synthetic
  figure.
- consumers (r3): Claude Code runs from a dedicated CLAUDE_CONFIG_DIR in untracked spike state,
  logged in once with the subscription.
- consumers (r3): the GuideLLM grid is short 512/256 and agentic 8192/512 at concurrency 1 and 4
  on three models, plus a shared-prefix run per model.
- consumers (r3): "thinking intact" means thinking blocks stream back and return on tool turns;
  the exact form is observed at BUILD.
- consumers: the consumer and instrument pins live in `mise.consumers.toml`, loaded only with
  `MISE_ENV=consumers` and run through `mise exec`, and mise.toml excludes their shims, so the
  architect's own `claude`, `pi`, and system Python still resolve in the repository root.
- consumers: Python is held at 3.14.8, because GuideLLM needs torch, which ships no Python 3.15
  wheels; currency keeps reporting the hold. OpenCode is pinned at 1.19.0, the latest.
- consumers: a shared `consumers` package owns the consumer pins, the harness drivers, the
  Claude Code login check, and the pinned-tool lookup (standards review).
- consumers: usage tests run binaries in an empty environment, so the check stays hermetic with
  SPIKE_ROUTER_URL set (spec review gap).
- consumers: the concurrent run opens one harness session per request, so no nonce reaches a
  later answer through history.
- consumers: the concurrent run refuses to start unless gpt-oss-120b and the embedding model are
  loaded, since Pi's provider can't send `autoload=false`.
- consumers: Pi uses its built-in llama.cpp provider, with its agent directory under the
  untracked `.state/`.
- consumers: the run's prompt frames the nonce as a request ID to repeat, after gpt-oss-120b
  refused "reply with exactly this token".
- consumers: the embedding tolerance is cosine 1e-3 (batching moved EmbeddingGemma 2 by at most
  1.4e-4; distinct inputs sit below 0.67).
- consumers: the live tools run with the session's inherited CLAUDE_* variables unset.
- consumers: the wire recorder strips credentials before forwarding and redacts them in its
  record, because Claude Code sends its subscription bearer token to the base URL.
- consumers: the wire round-trip tests are opt-in, each run only when SPIKE_PI, SPIKE_CLAUDE, or
  SPIKE_OPENCODE names its consumer's executable.
- consumers: Claude Code's dropped-image cell is n/a, since clutch's profile marks every model as
  image-capable; making it run would change clutch, which this task can't write.
- consumers: n/a and skip count as not passing; invalid counts error statuses and error events in
  a 200 stream, not consumer-cancel 502s.
- consumers: OpenCode's `interleaved` setting goes in a file named by OPENCODE_CONFIG, merged
  beneath the driver's config; on 1.19.0 it changes nothing on the wire, since OpenCode sends
  reasoning_content back either way.
- consumers: the engine row's 0/30 (150/150 invalid) comes from the suite requiring a `reasoning`
  field where llama.cpp sends `reasoning_content`; the script also reports pass on the tool call
  alone (29/30 pass@5, 27-28/30 pass^5) from the suite's own records. Renaming the field is a
  gateway or validate matter.
- consumers: the baseline (context/baseline.md) is GuideLLM's grid with fixed output
  (`ignore_eos`), each model's upstream tokenizer fetched into `.state/hf`, and engine rates
  from /metrics deltas.
- consumers: the baseline names the network path once per section, since every row shares it.
- consumers: both Gemmas exceed their 32768-token unified KV at 4 agentic streams (about 35k
  needed); recorded, not retuned.
- consumers: prefix reuse needs no `id_slot` pinning.
- consumers (architect): experiment.ai gains a fourth spike, a native Go harness behind
  spike-harness-driver's driver interface, measured beside Pi, Claude Code, and OpenCode with
  this spike's conform, concurrent, and wire packages; it answers whether go-ai carries its own
  agent loop and what replacing the process-backed drivers costs. The intake waits for it. It is
  set up by `marathon plan experiment.ai` in its own session, not by this task.
- consumers (accepted): the boundary wording names the Framework only; the workstation's is set
  when its tasks start.

- main-model (r1): the candidate is judged per consumer against gpt-oss-120b, not OpenAI's
  absolute bar: Pi 9/9 at 5/5 with 0 invalid; Claude Code over streamed /v1/messages each cell at
  or above gpt-oss-120b's count, no more invalid, thinking streamed and returned on tool turns;
  vision cells 5/5 and the probe's image answer correct; OpenCode measured, not judged.
- main-model (r1): promotion also needs engine generation of at least 24 tok/s, one stream, short
  profile (half of gpt-oss-120b's 48.8).
- main-model (r1): probe now, not held for llama.cpp #30105; Vulkan runs Mistral Small 4's expert
  down-projections on the CPU on every build through b11669, recorded from the router log.
- main-model (r1): the Framework moves to the newest b-tag at BUILD start (b11669 at PLAN).
- main-model (r1): Mistral Small 4 is unsloth UD-Q4_K_XL; rejected bartowski Q4_0 and unsloth
  MXFP4_MOE; no ggml-org or mistralai GGUF exists.
- main-model (r1): Mistral Small 4's preset is reasoning_effort high at temperature 0.7, top_p
  1.0, top_k 0, min_p 0; its template accepts only none or high.
- main-model (r1): candidates run at c 131072; rejected Mistral's 262144 (under the 3 GiB bar).
- main-model (r1): one Strix Halo profile holds named sets swapped through the router's unload
  and load; rejected a second profile with a restart.
- main-model (r1): gpt-oss-120b's pass^5 runs again on the candidates' build and pins.
- main-model (r1): every candidate gets the full measurement set (concurrent run, footprint,
  GuideLLM grid, prefix, cold load) whatever its verdict; no engine row.
- main-model (r1): a failing candidate closes as "not promoted on Vulkan, Strix Halo, b<N>"; a
  re-probe is a new task.
- main-model (r1): a passing candidate's set becomes the startup set in this task.
- main-model (r1): Nemotron 3.5 Super VL 120B-A12B (NVIDIA, US) is probed beside Mistral Small 4.
- main-model (r1): the workstation re-plan runs as a `plan` session after main-model ships and
  before cuda.
- main-model (architect): the Dell workstations are two Dell Pro Max FCM1253 (NVIDIA GB10,
  ConnectX-7, 128 GB LPDDR5x unified, joined by QSFP) and stay on DGX OS 7; rejected
  reinstalling them with Omarchy.
- main-model (r2, architect): sessions may do on the Framework whatever needs no sudo (router
  HTTP including load, unload and downloads, user-level SSH reads, logs); root stays the
  architect's, by exact commands. Supersedes consumers (r1)'s read-only observation.
- main-model (r2): a profile holds named sets (at most 4 models, one startup set, a budget per
  set); rejected one candidate set whose model is replaced between probes.
- main-model (r2): models declare typed roles (chat, vision, audio, embedding) and the tools take
  a set name; rejected model IDs as flags.
- main-model (r2): Nemotron is ggml-org NVFP4, the publisher's evaluated precision; rejected Q4_0
  as the first run.
- main-model (r2): Nemotron uses its template's reasoning default (thinking on); rejected a
  template-arguments field for medium effort.
- main-model (r2): Nemotron samples at the card's text values (1.0, 0.95, top_k 0, min_p 0) for
  every request; rejected the vision values.
- main-model (r2): Nemotron runs at c 131072; rejected 262144 and 1M for the comparison.
- main-model (r2): Nemotron's context checkpoints are capped at 8 per slot (default 32 would need
  ~20.6 GiB of system RAM outside the pool, est.); footprint gains a system-RAM column.
- main-model (r2): when both pass, more Claude Code cells at 5/5 leads, and single-stream speed
  breaks a tie.
- main-model (r2): OpenMDW-1.1 is acceptable beside Apache 2.0; each model records its licence
  beside its origin.
- main-model (r2): each consumer runs each candidate with a reasoning setting its card accepts,
  recorded on the wire; Claude Code is left unchanged.
- main-model (r2, architect): model-cards.md catalogs each model's trained or maximum context and
  its KV and recurrent-state cost, in case a larger context becomes a desired capability.
- main-model (r3): a candidate missing only the speed floor re-runs once at the next-smaller
  native-kernel quant (Nemotron at ggml-org Q4_0); Mistral Small 4 gets none, since no weight
  type avoids its CPU fallback; nothing below ~4 bits; gpt-oss-120b stays MXFP4 (its smaller
  GGUFs are at most 1.3% smaller).
- main-model (r3): Nemotron gets the full bar and measurement set even if its estimated decode
  ceiling (~18-20 tok/s) misses the floor, since the workstation re-plan needs its cells.

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
- coordinator · roadmap, consumers summary: the consumers run from the laptop against the
  Framework's router; drop the Mistral sentence; add pass^5 over conform's 9 capabilities, OpenAI's
  gpt-oss compatibility test as the engine row, and the baseline note (`context/baseline.md`)
  that every later platform is measured against.
- coordinator · roadmap: add task `main-model` (the Mistral Small 4 probe) after `consumers`,
  before `rocm`.
- coordinator · roadmap, the spike's goal summary: the new boundary: every consumer and
  instrument runs from the laptop, and the Framework only hosts and serves inference.
- coordinator · ai-hosting.md, "Experiment: spike-model-hosting": the Hosts boundary change;
  point the main-model probe to `main-model`.
- coordinator · roadmap: the spike's workstation tasks need re-planning (a `plan` session after
  main-model): the hosts are two GB10 units (aarch64, sm_121, 128 GB unified at 273 GB/s, DGX OS
  7.5 with driver 580 and CUDA 13.0, joined by 200GbE QSFP), not a discrete-VRAM box; evidence 7,
  the cuda summary ("sized to its VRAM", nvidia-smi footprint), ai-hosting.md's "discrete VRAM"
  passages, the "from 2026-10-14" dates and needs markers, two-unit serving scope, the dense
  candidates (decode ceiling ~15 tok/s at 30B, ~4 at 128B), and admin's go-cli-sdk need (met).
