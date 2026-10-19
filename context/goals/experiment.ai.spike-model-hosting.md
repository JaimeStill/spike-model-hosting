# goal · experiment.ai.spike-model-hosting

- **State:** building
- **Task:** screen
- **Branch:** screen

## Tasks

1. [x] align
2. [x] profile
3. [x] consumers
4. [x] main-model
5. [x] evaluation
6. [ ] screen
7. [ ] candidates
8. [ ] rocm
9. [ ] engines-strix
10. [ ] gateway
11. [ ] admin (needs: go-cli-sdk v0.1.0)
12. [ ] cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
13. [ ] engines-cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
14. [ ] validate

## Task brief · screen

```
Problem       The candidates task can't start yet. evaluate gives every model
              the full run that set A's baseline took most of a day for. It
              holds every model to a fixed 4 slots and a hand-chosen c, and
              reads the pool only after a fresh load, where set A's day of
              runs fell to 2778 MiB, under its own floor. The embedding and
              ASR prompts are set A's, hard-coded. No pool of admissible
              models, and no rule for their licences or for fine-tunes of a
              Chinese-origin base, says what candidates should screen.
              screen builds the screen, measures each model's slots and c,
              and surveys the pool on paper. It proves all of this on set
              A's own models, on the Framework's newest build.
Behaviors     1. The Framework runs the newest b-tag at the start of BUILD.
                 Its release-note review since b11715 is recorded, and set A
                 passes the probe and every role's fitness on it. The
                 repository's currency reports only the held pins
                 (claude-code, python, inspect-evals, mteb, openai).
              2. Slots and c belong to each model in the profile, each with
                 its reason. The host-wide slots value is gone, and KV stays
                 unified. Each model's section renders its own parallel.
                 Validation refuses a c beyond the card's context, and slots
                 under 1.
              3. A model's card prompts are profile fields with a source:
                 embedding task prompts by prompt type, and an ASR
                 instruction. The MTEB encoder, the ASR task, fitness and
                 capacity read them from the profile. Validation refuses a
                 prompt field on a role the model doesn't declare.
              4. evaluate reloads the set over the router's API before every
                 model run and set run. A run resumed with -from on the same
                 run directory doesn't reload. After the reload it records
                 the idle pool, then drives each loaded model to its
                 high-water mark: its largest image, an 8192-token prompt at
                 a full ubatch, its longest clip. Every pool floor is judged
                 at that warmed peak. Each pool reading carries system RAM
                 beside it (used and available on the host, each model
                 process's resident memory).
              5. A model run is a screen by default. Each tier runs its
                 screening fifth. It stops failed once the one-sided 95%
                 bound falls on the wrong side of the bar, and stops passed
                 once the bound clears it: for a pass rate, the Wilson lower
                 bound at or over the bar; for an ASR WER, the upper bound at
                 or under the ceiling. A tier whose bound still straddles the
                 bar runs on until it is decided or its list ends. MTEB tasks
                 run whole. The record names the tier, the bound and the
                 side it stopped on.
              6. A screen runs its roles' quality tiers at once. The speed
                 floors are measured alone, at fitness and capacity. The
                 concurrent pass also records the pool's least free memory
                 at the warmed peak and the crossing rounds, and stands in
                 for the set stage's concurrent phase. Alone figures and
                 slowdowns belong to the full run.
              7. A screen that passes every gate makes the model a finalist.
                 Its full run reuses the screen's records and adds the rest
                 of each tier, the set stage's alone figures and slowdowns,
                 and the deep stage.
              8. evaluate keeps an index of its results, keyed by model,
                 build, and the SHA-256 of the model's rendered preset
                 section. A stage whose key matches a passed result is
                 reused, and the report says so. A changed profile reruns
                 only the roles whose models' sections changed, plus the set
                 stage. The suites' own caches stay off.
              9. Floors at N in flight: each agent stream ≥ 10 tok/s; each
                 audio stream RTFx ≥ 1; each image question ≤ 10 s; each
                 32-token query ≤ 250 ms. Capacity runs each role model at
                 its own profile slots against these floors, with the pool
                 floor at the warmed peak, and no request may run out of
                 context.
              10. A sweep step chooses a model's slots and c. The model is
                 swept with the rest of its set loaded and idle at their own
                 settings. N runs 1, 2, 4, 8, 16, then one bisection between
                 the last pass and the first fail. c is N times the role's
                 per-session target, capped at the card's context: the
                 role's largest tier request (agent 8192 in and 512 out);
                 a model in several roles takes the largest target. Each
                 step is rendered from the profile with the override,
                 written to the router's preset file and applied through
                 its reload. Each step records:
                 - every stream's rate against the floors at N
                 - TTFT median and p95, and total throughput
                 - the pool at the warmed peak, and system RAM
                 - any request that ran out of context
                 The chosen N is the smaller of two values: the largest N
                 that holds every floor, the pool floor and every session's
                 context; and the knee, past which total throughput gains
                 under 10% a step. The report gives both, and which bound.
                 The sweep ends by restoring the committed render, checked
                 by its SHA-256, and reloading.
              11. A set's verdict states the concurrent sessions it supports
                 on its host: its agent model's chosen N, confirmed by the
                 set stage with every model at its own chosen N.
              12. Set A is swept on the new build, and its chosen slots and c
                 are committed and installed, with the pool floor unchanged
                 and no scheduled reload. Set A's models then pass their
                 screens against their own bars. The set stage holds every
                 model at its chosen N. Each role's screen time is recorded
                 against the 15-25 min estimate, and set A's agent and
                 vision screens run as one run, whose wall time is the
                 chat-and-vision figure against the ~85 min budget. One
                 role's finalist continuation runs as the demonstration.
              13. The Framework keeps only models that passed on it. Mistral
                 Small 4's cached files and align's retired-model directory
                 are deleted: through the router's API where it can, by the
                 architect's exact command where root owns the files.
              14. A pool document, on paper, gives per role (agent, vision,
                 audio, embedding) the open-weight models that run on Strix
                 Halo under the origin rule. For each model:
                 - publisher, origin, licence, architecture and active
                   parameters
                 - its GGUF, quant and size, from the publisher, ggml-org,
                   or a community converter, named
                 - an estimated budget, and whether the router serves it on
                   the build in use
                 - its decode ceiling against the role's floors
                 - admitted, or excluded and why
                 It states the rules it judged by, the estimate method, the
                 model shapes evaluate supports (one model in several roles,
                 per-model prompts, a context under 8192, system RAM that
                 grows with slots, speculative-decoding sidecars, noted not
                 used). It sketches profiles from "max quality near the
                 pool" to "small models, large headroom", including the
                 omni profile, each with its budget and headroom. Every
                 figure is marked estimated or measured, with its build.
              15. STANDARDS states that any open model licence is accepted:
                 Apache-2.0, MIT, OpenMDW-1.1, the NVIDIA Open Model
                 licences, the LFM Open License 1.0, the Llama 3.1 and 4
                 Community licences, and the Gemma Terms of Use. It also
                 states that a model fine-tuned from a Chinese-origin base
                 takes its base's origin.
              16. The evaluation docs, the Strix Halo runbook (the preset
                 file's write access, the build move, the deletions), the
                 findings (set A's sweep, the warmed peak, system RAM, the
                 screen timings), the build log, the package docs, CLAUDE.md,
                 the README and the goal record are current.
Test seams    evaluate's stage runner against a fake router and recorded
              suite results:
              - the screen and its early stop on either side
              - the straddle continuation
              - concurrent role tiers and the folded set phase
              - the finalist continuation
              - index reuse and the rerun of changed roles
              - the reload and the warmed peak
              - the floors at N
              The sweep against a fake router with a reloadable preset file:
              steps, the bisection, the stop rule, the knee, the restore and
              its SHA-256. The profile package: per-model slots and c,
              prompt fields, render, validation. The suites' early-stop
              tests, both sides. The integration probe at SPIKE_ROUTER_URL.
              Measured evidence (1, 12, 13).
Slices        1. Build move and host session (1, 13): the release-note review;
                 the architect's root commands in one sitting (the new
                 b-tag, the preset file made writable by the session's user,
                 the retired directory removed); Mistral Small 4 deleted
                 through the router; the probe and every role's fitness on
                 the new build.
              2. Profile (2, 3): per-model slots and c, the prompt fields;
                 set A's render keeps its values (each model at 4 slots),
                 and its SHA-256 matches the installed file.
              3. Reload, warmed peak, system RAM (4): live on set A.
              4. Screen (5, 6, 7): early stop on either side, concurrent
                 tiers, the folded set phase, the finalist continuation;
                 fakes in the check.
              5. Results index (8): reuse, and a changed section reruns its
                 role plus the set stage.
              6. Floors at N and capacity per model (9, 11): live on set A
                 at 4.
              7. Sweep (10): fakes in the check; one live model swept, then
                 restored.
              8. Set A swept (12): slots and c committed and installed;
                 sessions supported stated.
              9. Set A screened (12): every role passes its own bars;
                 timings; the chat-and-vision figure; one finalist
                 continuation.
              10. Pool document and STANDARDS (14, 15).
              11. Docs and records (16): Decisions and pending edits.
Out of scope  Screening any model beyond set A's four; choosing candidates,
              their profile entries, downloads or admission runs; the
              verdict, and any startup-set change beyond set A's swept slots
              and c. Speculative decoding. Bumping the held pins. ROCm,
              vLLM, SGLang, gateways, the workstations, the admin tool.
              Evaluation as its own system. Changes to clutch,
              spike-harness-driver or personal-agents.
Door          two-way, except:
              - the deleted Mistral Small 4 files and retired-model
                directory (one-way on the host; the Hugging Face files can
                be downloaded again, Mistral's in ~35 min)
              - the cleaned .state run outputs
              - Claude Code subscription usage spent
              The repository reverts by PR. On the Framework the earlier
              b-tag stays in its versioned directory and `current` points
              back. The preset file's write access reverts by one root
              command, and the committed render is restored and checked by
              its SHA-256.
```

## Progress

slices 0/11 committed · standards — · spec — · editor —

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

- main-model (escalation, architect): candidates are evaluated in stages (fitness, capacity,
  reliability, characterization), and a failed gate stops the later stages; supersedes r1's
  full measurement set whatever the verdict and r3's full set for Nemotron.
- main-model (escalation, architect): fitness is a native-capability test under ~3 minutes,
  straight against the router with no consumer.
- main-model (escalation): the stages run as one evaluate command that stops at the first failed
  gate and resumes from a named stage; rejected running the commands by hand.
- main-model (escalation): Nemotron's Q4_0 retry stays, as a fitness-only re-run.
- main-model: a one-turn smoke per consumer opens stage 3, since Claude Code can't run on
  Mistral at all (its template refuses a system message after the first user message).
- main-model: long runs hold a systemd-inhibit sleep block and watch the router's slots, after
  the laptop's suspend stalled a GuideLLM run overnight.

- main-model (escalation 2, architect): role-specific evaluation pipelines (agent, vision,
  audio, embedding) and a set evaluation for co-loaded models become their own task,
  `evaluation`, before any more model analysis; fixtures are graded in rising complexity, not
  single simple cases; a baseline set covers all four core roles; speed is judged beside
  accuracy and capability, with floors set by the hardware.
- main-model (escalation 2): main-model ships trimmed (slices 1-7 plus its records); Nemotron,
  the staged evaluation and the verdict move to `candidates`, after `evaluation`; rejected
  parking main-model on its branch.
- main-model (escalation 2, architect): `candidates` also looks for viable vision, audio and
  embedding candidates to test against set A's incumbents, not only agent models.
- main-model (escalation 2): the r1, r2, r3 and escalation decisions on Nemotron, the staged
  evaluation, the quant retry and the verdict carry to `candidates` as settled, including r1's
  "a passing candidate's set becomes the startup set".

- main-model: every preset section renders `hf-repo = <ID>`, set A's included, because the router
  can download and start a model that isn't cached only from it; set A's render otherwise keeps
  its sections, keys and values (spec review gap 1).
- main-model: the system-RAM footprint column moves to `candidates` with Nemotron (spec review).
- main-model: Mistral Small 4's KV estimate takes the top of 2.8-5.1 GiB; the catalog explains
  the range (standards review).
- main-model: each set names the model each role's requests go to (`[sets.roles]`), beside the
  roles each model declares; the probe and every instrument find their models through
  `Profile.Set`.
- main-model: the probe and every instrument take `-set`, else SPIKE_SET, else the profile's
  startup set.
- main-model: validation refuses a model ID whose tag the router would rename
  (`profile.RouterTag`), so Mistral Small 4's ID ends in `Q4_K_XL`, not `UD-Q4_K_XL`; the tag
  fetches the same files.
- main-model: each chat model records the reasoning setting each consumer sends
  (`[models.consumers]`); Pi 1.3.0 sends no reasoning_effort to Mistral Small 4, so its
  preset's "high" applies, and changing Pi's driver stays out of scope.

- evaluation (layout r1, architect): the reorganization folds into evaluation, since the evaluation decisions shape it; rejected a separate layout task.
- evaluation (layout r1): areas hosting, evaluation, devices/<host-class>, docs, context (marathon only); profiles live with their host class, probe and router with hosting.
- evaluation (layout r1): one Go module, packages by area; rejected per-area modules joined by go.work (workspace-only builds, or pushed pseudo-versions).
- evaluation (layout r1): commands live inside their area; rejected one root cmd/.
- evaluation (layout r1): mise monorepo mode (monorepo_root, config_roots, //area:task, root check runs every area's), stable in mise 2026.10.11; rejected nested configs run by cd.
- evaluation (layout r1): consumer pins stay behind MISE_ENV=consumers in the evaluation area; rejected ungated, which would shadow the architect's own claude and pi.
- evaluation (layout r1): measured records and the card method move to docs; the Strix Halo build log to its device area; context keeps README and goals.
- evaluation (layout r1): a root README is the front page and holds "The answer"; context/README.md keeps vision and capability map.
- evaluation (layout r1): docs carries the evaluation approach; no context toolkit note, since this task adopts the harness.
- evaluation (layout r1): the Strix Halo runbook is written now, from the records; GB10, vLLM and SGLang pages come with their tasks.
- evaluation (r1): Inspect AI 0.3.281 + inspect_evals 0.24.0 run the agent, vision and audio pipelines over the router's OpenAI API from the laptop; rejected native tau2-bench (Python <3.14 vs the 3.14.8 hold), bfcl-eval (no gpt-oss or Gemma 4 handler, idle since 2026-03), lmms-eval (torch), VLMEvalKit, and own fixtures alone.
- evaluation (r1): MTEB 2.26.3 with a prompt-aware client encoder judges embeddings; rejected the stock OpenAI encoder (no task prompts) and own fixtures.
- evaluation (r1): agent tiers BFCL single-turn, BFCL multi-turn, consumer tier (conform pass^5), then τ2.
- evaluation (r1): vision tiers probe image + V*Bench, DocVQA, MMMU; rejected MathVista in place of MMMU.
- evaluation (r1): audio tiers are the Open ASR Leaderboard's English sets plus FLEURS English, WER and RTFx.
- evaluation (r1): embedding uses a seeded MTEB(eng, v2) subset; rejected the full benchmark.
- evaluation (r1): the baseline is each role's assigned set A model; secondary roles go to candidates.
- evaluation (r1): the set stage runs each role alone, then under load, with the footprint; rejected footprint-only and a cross-role workflow fixture.
- evaluation (r1): a quality gate is set A's score within its two-run spread; rejected fixed absolute accuracy thresholds.
- evaluation (r1): stages admission, fitness, capacity, quality, characterization (characterization now in the deep stage); supersedes main-model's four stages.
- evaluation (r1): the Framework moves to the newest b-tag at the start of BUILD.
- evaluation (r2): quality tiers run in cost order, τ2 last; rejected complexity order.
- evaluation (r2): a tier stops early once the one-sided 95% upper bound, after its seeded screening fifth, falls below the bar; rejected stopping only when the bar is out of reach.
- evaluation (r2): OpenAI's gpt-oss compatibility test is removed; BFCL tier 1 covers tool calls for every model and engine.
- evaluation (r2): conform, concurrent, prefix and coldload fold into evaluate as named stages and steps; packages stay.
- evaluation (r2): raw output and run reports stay in .state; set A's baseline tables are committed to docs findings; rejected committing every run's report.
- evaluation (r2, architect): once findings are documented, .state run outputs are removed; logins and caches kept.
- evaluation (r2): sample lists are committed seeded ID lists, the same for every model.
- evaluation (r2): one uv project in the evaluation area holds inspect-ai, inspect-evals and mteb at Python 3.14.8; GuideLLM stays a mise pipx tool.
- evaluation (r3, architect): one model's run, admission through the set stage across its declared roles, fits in 1 hour; supersedes r1's ~2 h per model and role.
- evaluation (r3): τ2 and characterization form a deep stage only models passing every gate reach; rejected a token τ2 inside the hour (~6 tasks x 1 trial, ±40%).
- evaluation (r3, architect): no cloud spend. τ2's user simulator and airline grader are Sonnet through the laptop's pinned Claude Code on the architect's subscription (claude -p, tools off, own system prompt, no session persistence), personal use of the unmodified binary; rejected a cloud API simulator (spend), gemma-4-E4B (weak, takes slots), Haiku (weaker user).
- evaluation (r3): τ2 runs airline and retail only; rejected telecom, whose simulated user needs tools.
- evaluation (r3): the hour's counts are BFCL 100 and 20 + conform pass^5; vision 5/100/150/200; audio 150 per set x 6; embedding's four tasks in full; τ2 12 tasks per domain x 2 trials in the deep stage.
- evaluation (r3, architect): speed floors are absolute and task-based, initial values to tune as metrics come in: agent decode 20 tok/s, prefill 500 tok/s, 8k TTFT 20 s, 10 tok/s per stream at 4; vision 10 s per image question; audio RTFx 5, 1 per stream at 4; embedding 4,300 tok/s, 250 ms per query. Supersedes main-model r1's 24 tok/s floor (carried to candidates) and r1's 40%-of-ceiling gate; ceiling efficiency is a recorded diagnostic.
- evaluation (slice 7, architect): harness fit is evaluated, not assumed. Conform judges every pinned harness against one absolute bar (each applicable capability 5/5, 0 invalid), not against gpt-oss-120b's comparison row; the agent role needs any harness to fit, and the report records the fit profile per model, engine and harness. Supersedes behavior 10's comparison-row judgment; the row stays in the findings as context. Prompted by gpt-oss-120b failing its own row (Claude Code audio-tool 5/5 on b11669, 3/5 on b11572).
- evaluation (slice 7, architect): fit profiles, with each failed cell's reason, are design input for the native Go harness spike: what a harness must do for a local model (force the tool call when offering respond, round-trip reasoning, no blind resend on a parse 500, drop images for a model without vision). The fit tier takes harnesses as a list, so that spike's harness joins it as one more.
- evaluation (slice 8, architect): the hour becomes ~85 min for a chat-and-vision model that passes every gate (measured ~105-120 min before harness fit and the set-stage change); sample counts hold; the set stage in a model's run reuses the model's quality tier 1 as its alone figure, and the set's other roles generate load only while the model's tiers run. Supersedes r2's 1-hour target.
- evaluation (slice 8, architect): set A fails the set stage's 3 GiB floor (2968-2990 MiB) because Gemma 4 26B's memory grows ~250-270 MiB under heavy vision use and holds until reload; diagnose which buffer grows and whether a server setting caps it before choosing between a profile setting, a lower floor, or a reload in the procedure.
- evaluation (slice 8, architect): the pool floor drops from 3072 to 2816 MiB (2.75 GiB), and the vision model's preset caps the image-encoder batch (mtmd-batch-max-tokens = 512; measured cost under 1% on two-image prefill), keeping c 32768. The growth is a high-water mark, not a leak: one image over 256 tokens +200 MiB, a two-image batch +330 MiB, Vulkan scratch +100 MiB. Rejected c 16384 (4 slots sharing 16k against MMMU's 4096-token answers) and image-max-tokens 256 (halves image detail, V*Bench's subject). The preset installs with slice 10's build move, in one root session.
- evaluation (architect, for candidates): a host keeps installed only the models that passed evaluation on it; candidates that fail are deleted from that host once their verdict is recorded. Sets, and the profiles' presets, group only models that passed together, so a host never loads a model whose set did not pass as a whole.
- evaluation (architect, for candidates): a candidate's evaluation is a screen, not a baseline; the full run is for finalists. Four changes, built in candidates: (1) a tier stops early on a pass as well as a fail, once the one-sided 95% lower bound clears the bar; (2) every candidate runs each tier's screening fifth by default, and only finalists, or models whose bound still straddles the bar, run the full tier and the deep stage; (3) the roles' quality tiers run concurrently, since a quality score doesn't depend on speed and each role has its own model, while the speed floors are still measured alone; (4) results are kept by model, build and preset, so a changed profile reruns only its changed roles plus the set stage. Estimated 15-25 min to screen a candidate, extrapolated from slices 6-9, not yet measured. Prompted by set A's two-run baseline taking most of a day. The baseline itself stays two full runs: its bars and spreads are what make a screen trustworthy.
- evaluation (slice 11, architect): every model run starts from freshly loaded models: the set's models are unloaded and reloaded over the router API before each run, and the idle pool is recorded after the reload. Prompted by baseline 1's embedding run failing fitness at 2778 MiB free: the pool fell from 3509 to 2778 MiB over a day of runs as each model kept its peak working memory (26B-A4B +405 MiB, past slice 8's ~300 MiB bound, largely from the deep stage's 8k-token prompts), so a model's verdict depended on run order. Rejected reordering the runs (moves the failure) and a lower floor (chases the growth). Whether a set serving all day needs a scheduled reload or a different floor is a question for candidates.
- evaluation (slice 11, architect): the set stage counts an answer that hit its max_tokens without its own nonce as unanswered, not crossed; only a foreign nonce is a crossing. The vision crossing request's limit rises from 512 to 1024 tokens. Prompted by three cut-off Gemma 4 26B answers (eval 512 in the router journal against 201-458 for the rest) that the checker flagged as crossed. Extends the empty-answer rule.
- evaluation (architect, for candidates and the re-plan): every evaluated set reports the concurrent sessions it supports on its host: the most agent sessions at once that still meet the per-stream floors with the pool floor held, stated even when it is one. Today's capacity stage checks a fixed 4 in flight; candidates turns it into that count. Candidates also looks for a lighter grouping that scores near set A's bars with less of the pool and bandwidth, so the host keeps headroom and concurrency. Prompted by set A filling ~95 of 96 GiB, 4-stream decode at ~22 tok/s against 48 alone, BFCL 2.18x slower with every role working at once, and the 4-stream 8k agentic cell failing on context. How models load and are managed is expected to differ by host once the Framework and the GB10s share the tailnet: role placement across hosts behind a session-sticky gateway (warm-prefix TTFT 0.26 s against 10.2 s cold) is weighed against a full set on each host in the workstation re-plan and the gateway task.
- evaluation (slice 9): τ2 airline runs with no model grader: inspect_evals' airline scorer judges by the final database state, so behavior 13's "airline grader" has nothing to run; Sonnet only simulates the user in both domains.
- evaluation (slice 4): MTEB runs at 2.26.4, the suites project's lock, not behavior 7's 2.26.3; the findings record the version measured.
- evaluation (slice 11): "set A's score" in a tier's bar is the mean of the two baseline runs; a bar is that mean less the two-run spread (a WER ceiling, the mean plus it), rounded toward the stricter side to 0.001. τ2 and harness fit set no bar: the deep stage records and never gates, and harness fit's bar is absolute.
- evaluation (slice 10): the Framework moved to the newest b-tag at slice 10, where the brief's slice order places it, not at the start of BUILD as behavior 14 reads; slices 1-9 ran on b11669.
- evaluation (spec review): admission judges origin by STANDARDS: a model of Chinese origin fails admission unless its profile entry records why no alternative serves (`origin_reason`); validation refuses a reason on any other origin.
- evaluation (spec review, architect): behavior 12 is partly met: a chat-and-vision model's run composes to ~95-105 min against ~85 min (vision 82.1 and 70.9 min, agent 37.2 and 35.9 min, before reuse of tier 1 as the set stage's alone figure); candidates times its first chat-and-vision model and records the measured figure.
- evaluation (spec review, architect): the baseline's cold-load figures are slice 10's build-move table on b11715; the deep stage's cold load is not rerun for the baseline.
- evaluation (spec review, architect): the consumer and suite pins hold while set A's bars stand (claude-code 2.1.301, python 3.14.8, inspect-evals 0.24.0, mteb 2.26.4, openai 3.31.0, against newer releases); bumping a pin rebaselines the roles it affects.
- evaluation (spec review, architect): the standards reviewer's four test questions are fixed in this task: the check fails when the Python env is missing, the ASR prompt has one source, the gate tests read a testdata profile, and testpackage's allow-packages narrow.
- evaluation (architect, for intake): once the experiment completes and the method is proven across hosts, engines and the gateway, evaluation becomes its own system with a management interface: a results store keyed by model, build, preset and host; a per-host run queue; and a run ledger with comparison views (a model against set A across builds and hosts, with its fit profile). Until then it stays the spike's `evaluate` command, and candidates moves the runbook's steps into it (the reload, results by model, build and preset, the supported-sessions count). Rejected building an interface now: candidates is about to change the workflow it would encode.
- screen (r1, architect): candidates splits in two: `screen` builds evaluate's screen and is proven on set A's models; `candidates` runs the screens, the finalists, the lighter profiles, the verdict and the startup set. Rejected one task (~15 slices, screens blocked on the build).
- screen (r1): the Framework moves to the newest b-tag at the start of BUILD (b11734 at PLAN), with the release-note review since b11715, and set A re-checked by the probe and every role's fitness; rejected holding b11715, where set A's bars were measured.
- screen (r1): an agent session is the agentic shape, an 8192-token prompt and 512 out; rejected the short shape and the concurrent run's harness sessions, whose requests vary too much to count by.
- screen (r1, architect): slots are measured per model in a profile, not fixed at 4: performance degradation and memory starvation mark the limit of the hardware and model pairing, and finding each model's best slots is part of the performance evaluation. Supersedes evaluation's "a fixed 4 in flight" and round 1's rec of a count bounded by the profile's slots.
- screen (r1): the all-day pool question is answered by a warmed-peak reading: after the reload, each model is driven to its high-water mark and the pool floor is judged there; rejected a multi-hour soak and a scheduled reload.
- screen (r1): in a screen, the set stage's concurrent phase folds into the quality stage's concurrent role tiers; alone figures and slowdowns are kept for finalists' full runs.
- screen (r1): every candidate whose screen passes every gate is a finalist and gets the full run; rejected the best per role and finalists the architect names.
- screen (r1): results are kept in evaluate's own index, keyed by model, build and the SHA-256 of the model's rendered preset section, standard library only; rejected Inspect's eval_set reuse and MTEB's result cache, which see no preset and no non-suite stage.
- screen (r1): a model's card prompts (embedding task prompts, an ASR instruction) are typed profile fields with a source; rejected a per-model table in the suites.
- screen (r1): the keep-only-passed rule applies now: Mistral Small 4's cached files and align's retired-model directory are deleted, through the router's API or by the architect's exact command.
- screen (r2): a sweep step is applied through the router's preset reload (GET /models?reload=1, no restart), after one root command makes the preset file writable by the session's user; each step is rendered from the profile with an override, and the sweep ends by restoring the committed render, checked by SHA-256. Rejected a root install per step, and a second user-level llama-server outside the router.
- screen (r2): c scales with slots, N times the role's per-session target, capped at the card's context, so memory, not context, is what runs out; rejected holding c.
- screen (r2): each role's per-session target is its largest tier request, measured from the committed lists; a model in several roles takes the largest; rejected 8192 for every role.
- screen (r2): a model's best slots is the smaller of the largest N that holds every floor, the pool floor at the warmed peak and every session's context, and the knee past which total throughput gains under 10% a step; the report gives both and which one bound.
- screen (r2): the 4-stream floors become per-stream floors at N (agent 10 tok/s, audio RTFx 1), and vision (10 s an image question) and embedding (250 ms a 32-token query) are judged per request at N; rejected new N-stream values.
- screen (r2): the sweep steps N = 1, 2, 4, 8, 16, capped by the card's context, then one bisection; rejected every N.
- screen (r2): each model is swept with the rest of its set loaded and idle at their own settings, and the set stage confirms every model at its chosen N; rejected sweeping alone.
- screen (r2): a set's sessions supported is its agent model's chosen N, confirmed by the set stage; rejected the smallest N across the set.
- screen (r2): set A's slots and c are chosen by the sweep at the warmed peak, the pool floor unchanged and no scheduled reload; rejected lowering the floor and a reload schedule.
- screen (r2): the pool survey and the profile sketches land in screen as a paper document, with the model shapes evaluate must support; profile entries, downloads and admission runs wait for candidates.
- screen (r2, architect): any open model licence is accepted: Apache-2.0, MIT, OpenMDW-1.1, the NVIDIA Open Model licences, the LFM Open License 1.0, the Llama 3.1 and 4 Community licences, and the Gemma Terms of Use; the pool's seven licence-pending models are admissible, and the omni profile (Nemotron 3 Nano Omni with EmbeddingGemma 2) is a real option. Supersedes round 2's rec of no more licences beyond Apache-2.0, MIT and OpenMDW-1.1.
- screen (r2): a model fine-tuned from a Chinese-origin base takes its base's origin, so voyage-4-nano, harrier-oss-v1-0.6b and jina-embeddings-v5 are excluded; STANDARDS gains the line.

## Pending edits

- coordinator · ai-hosting.md, "Where personal-agents' parts go": add "how to read a
  model card (spike-model-hosting's docs page on model cards)" to the methods proposed for the
  architecture layer, and say that ai-hosting's per-model presets cite the card values that
  method reads.
- coordinator · roadmap, the spike's profile summary: replace "pinned exactly in the profile"
  and "Currency compares the profile's pins against the newest `b<N>` tag" with a minimum
  build (b11452), with each measurement naming the build it ran on.
- coordinator · ai-hosting.md, "The existing setup: personal-agents", the Serving line: it
  names release build b11529; the Framework's router now runs b11565 from the spike's rendered
  presets and unit, which hold llama.cpp to a minimum build (b11452).
- coordinator · roadmap, consumers summary: the consumers run from the laptop against the
  Framework's router; drop the Mistral sentence; add pass^5 over conform's 9 capabilities and the
  baseline (`docs/findings/baseline.md`) that every later platform is measured against.
- coordinator · roadmap: add task `main-model` after `consumers`, before `rocm`: named model sets
  with roles, swapping sets over the router's API, set-aware instruments, gpt-oss-120b's b11669
  comparison row, and Mistral Small 4 disqualified on Vulkan (3.36 tok/s, CPU fallback #30105).
- coordinator · roadmap, the spike's goal summary: the new boundary: every consumer and
  instrument runs from the laptop, and the Framework only hosts and serves inference.
- coordinator · ai-hosting.md, "Experiment: spike-model-hosting": the Hosts boundary change;
  point the main-model probe to `candidates`, with Mistral Small 4's disqualification in
  `main-model`.
- coordinator · roadmap, the spike's goal summary, and ai-hosting.md, "Experiment:
  spike-model-hosting", Hosts: sessions may do on the Framework whatever needs no sudo (the
  router's HTTP API including load, unload and downloads, user-level SSH reads, logs); root
  stays the architect's, by exact commands.
- coordinator · roadmap: the spike's workstation tasks need re-planning (a `plan` session after
  main-model): the hosts are two GB10 units (aarch64, sm_121, 128 GB unified at 273 GB/s, DGX OS
  7.5 with driver 580 and CUDA 13.0, joined by 200GbE QSFP), not a discrete-VRAM box. The
  re-plan revisits evidence 7; the cuda summary ("sized to its VRAM", and its nvidia-smi
  footprint, since nvidia-smi reports "Memory-Usage: Not Supported" on GB10 and the footprint
  method changes); ai-hosting.md's "discrete VRAM" passages; the "from 2026-10-14" dates and
  needs markers; two-unit serving scope; the dense candidates (decode ceiling ~15 tok/s at 30B,
  ~4 at 128B); and admin's go-cli-sdk need (met).
- coordinator · roadmap: add task `evaluation` after `main-model` (the repository laid out by
  area; evaluate's gated stages admission, fitness, capacity, quality, set and deep, with
  absolute task-based floors and early stop, a chat-and-vision model's run budgeted at ~85 min
  and measured at ~95-105 min; role pipelines on Inspect AI and MTEB; τ2 with Claude Code as the
  simulated user; set A's baseline), then `candidates`
  (candidates for every role, including Nemotron 3.5 Super, through the pipelines against set
  A's bars; the verdict and startup set), both before `rocm`.
- coordinator · ai-hosting.md (line 133) and roadmap.toml, validate task (line 515): repoint
  `context/tool-reliability.md` to `docs/findings/tool-reliability.md`.
- coordinator · ai-hosting.md, "Experiment: spike-model-hosting": candidates are judged by
  evaluation's absolute floors and set A's per-tier bars, not the 24 tok/s floor.
- coordinator · roadmap, experiment.ai's native Go harness spike: it joins this spike's harness-fit
  tier as one more harness, and reads the fit profiles' failed cells and reasons (forcing the tool
  call, reasoning round-trip, resend on a parse 500, image drop) as its design input.
- coordinator · roadmap, the spike's candidates summary: a host keeps only the models that passed on it
  (failed candidates are deleted after their verdict is recorded), and sets load only models that
  passed together.
- coordinator · roadmap, the spike's candidates summary: candidates are screened, not baselined: tiers stop early on a pass or a fail, each tier's screening fifth runs by default and the full tiers and deep stage only for finalists, the roles' quality tiers run concurrently, and results are kept by model, build and preset so a changed profile reruns only its changed roles plus the set stage.
- coordinator · roadmap, the spike's candidates summary: each set's verdict states the concurrent sessions it supports on its host (even one), and candidates looks for a lighter grouping that scores near set A's bars with less of the pool and bandwidth.
- coordinator · roadmap, the workstation re-plan and the gateway task: weigh role placement across hosts behind a session-sticky gateway against a full set on each host; a host-wide check runs the role mix through the gateway under load.
- coordinator · roadmap and ai-hosting.md, "Experiment: spike-model-hosting": at the experiment's intake, propose evaluation as its own goal: a system that keeps results by model, build, preset and host, queues runs per host, and has a management interface (run ledger, comparison views), starting from the `admin` task's go-cli-sdk tool; built only after every spike task has proven the method.
- coordinator · roadmap: add task `screen` after `evaluation`, before `candidates` (evaluate's
  screen with early stop on either side, concurrent role tiers and finalists; the results
  index; the reload and warmed-peak pool reading; per-model slots and c by a sweep with the
  sessions-supported count; card prompts in the profile; the pool document; proven on set A's
  models on the newest build).
- coordinator · roadmap, candidates summary: replace "candidates are screened, not
  baselined: …" with: screens models from screen's pool document with screen's evaluate, gives
  each kept profile its swept slots and c, builds profiles from the pool's sketches (omni
  included), and each verdict states the sessions its set supports.
- coordinator · roadmap, candidates summary: drop "each set's verdict states the concurrent
  sessions … lighter grouping" (screen builds the count, the pool document sketches the
  lighter profiles; candidates judges them).
- coordinator · ai-strategy.md, "Constraint: model origin": a fine-tune of a Chinese-origin
  base takes its base's origin.
- coordinator · ai-hosting.md, "Experiment: spike-model-hosting": slots and c are measured per
  model and host by a sweep; the spike accepts any open model licence (listed).
