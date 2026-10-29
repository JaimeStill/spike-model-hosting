# llama.cpp builds on the Framework

The profile holds llama.cpp to a minimum build, and the Framework's router ran the upstream
Vulkan x64 release of one b-number at a time, from its own versioned directory through a
`current` link the unit ran, until its retirement on 2026-10-29. Each move here names the builds, the date, what the release
notes between them changed for set A, and what was re-verified on the new build. Every build
is read from the router's `/props` `build_info`.

## b11529 to b11565, 2026-10-12

- **From:** b11529 (`b11529-8ae386707`), installed by `align`.
- **To:** b11565 (`b11565-f9a95aeb2`), the newest b-tag when the move ran.
- **Reviewed:** the 36 commits in `b11529...b11565` (`gh api
  repos/ggml-org/llama.cpp/compare/b11529...b11565`), read for the chat parser and
  `/v1/messages`, Vulkan MoE, Gemma 4 input and embeddings, flags and preset keys, and sampling
  defaults.

### What the notes change for set A

- **Chat parser and `/v1/messages`.** [#30210](https://github.com/ggml-org/llama.cpp/pull/30210)
  (b11531) rewrites how the server calls the chat code. The gpt-oss and Gemma 4 parser rules
  are unchanged, so the `<|constrain|>` final that `tool-reliability.md` describes still fails
  the same way. OpenAI chat completions and `/v1/messages` both go through the rewritten path.
  The server docs drop two request fields, `generation_prompt` and `parse_tool_calls`, which
  clutch never sends.
- **Gemma 4 input and embeddings.** [#30160](https://github.com/ggml-org/llama.cpp/pull/30160)
  (b11537) reorders the token-embedding reads in the graph. That changes Gemma 4's per-layer
  embeddings (gemma-4-E4B) and the path image and audio input take.
  [#30259](https://github.com/ggml-org/llama.cpp/pull/30259) adds an attention-mask input to the
  multimodal graph; it touches no Gemma 4 projector.
- **Vulkan MoE.** [#29998](https://github.com/ggml-org/llama.cpp/pull/29998) (b11554) rewrites
  how Vulkan's MoE matrix multiply handles duplicate experts, the path gpt-oss-120b and the
  26B-A4B run on.
- **Slots.** [#30295](https://github.com/ggml-org/llama.cpp/pull/30295) (b11552) stops a request
  that pins a busy slot from moving it to the wrong context.
- **Sampling defaults.** [#30223](https://github.com/ggml-org/llama.cpp/pull/30223) changes only
  backend sampling, which is off by default. The GGUF `general.sampling.*` defaults and the
  presets' values apply as before.
- **Other.** [#30014](https://github.com/ggml-org/llama.cpp/pull/30014) hides ggml's internal
  symbols. [#30228](https://github.com/ggml-org/llama.cpp/pull/30228) adds `context_length` to
  `/models`; outpost reads `meta.n_ctx`, which is unchanged.
- **Flags and preset keys.** Only `--slot-save-path` gains an environment variable,
  `LLAMA_ARG_SLOT_SAVE_PATH` ([#30272](https://github.com/ggml-org/llama.cpp/pull/30272)). No
  flag is renamed or removed; router mode, the preset keys, `--models-max`, and
  `load-on-startup` are unchanged. No preset key changes meaning, so the presets did not
  change.

### What was re-verified on b11565

- **The probe.** The integration probe passed on b11529 with the rendered setup installed, then
  on b11565 with the same rendered unit and presets: `/health`, the four models loaded with no
  manual load, gpt-oss-120b's chat and `/v1/messages` tool call (the rewritten chat path), the
  26B-A4B's image and gemma-4-E4B's audio (the reordered embedding reads), EmbeddingGemma 2's
  768 dimensions, the build at or above the minimum, and each model's default sampling.
- **The footprint.** Set A at 4 in flight leaves 3.30GiB of the pool free, as on b11529,
  with no model more than 17MiB from its b11529 figure
  ([`memory-budget.md`](../../docs/findings/memory-budget.md)).
- **Tool-call reliability.** Five `clutch conform` runs on the Framework: Pi passed every cell
  5/5, as on b11529, and one OpenCode cell, `tool`, fell from 4/5 to 3/5. The rates sit beside
  b11529's in [`tool-reliability.md`](../../docs/findings/tool-reliability.md), which reads the fall
  as chance.

## b11565 to b11572, 2026-10-12

- **From:** b11565 (`b11565-f9a95aeb2`), installed by `profile`.
- **To:** b11572 (`b11572-efe3626b1`), the newest b-tag when the move ran.
- **Reviewed:** the 7 commits in `b11565...b11572` (`gh api
  repos/ggml-org/llama.cpp/compare/b11565...b11572`), read for the chat parser, `/v1/messages`
  and thinking, Vulkan MoE, Gemma 4 input and embeddings, flags and preset keys, and sampling
  defaults.

### What the notes change for set A

- **Chat parser, `/v1/messages`, and thinking.** No commit touches the chat code, the
  Anthropic `/v1/messages` endpoint, or how reasoning is split into thinking content. Both
  endpoints still take b11531's rewritten path, the gpt-oss and Gemma 4 parser rules are
  unchanged, and the `<|constrain|>` final that `tool-reliability.md` describes still fails
  the same way. Claude Code meets `/v1/messages` and its thinking blocks as b11565 served them.
- **Vulkan MoE.** Nothing changes for Vulkan.
  [#30373](https://github.com/ggml-org/llama.cpp/pull/30373) (b11569) brings Metal's MoE
  matrix multiply to the duplicate-expert behavior Vulkan gained in b11554.
- **Gemma 4 input and embeddings.** Nothing changes. The two new decision-model architectures,
  [#30378](https://github.com/ggml-org/llama.cpp/pull/30378) (b11571) and
  [#30383](https://github.com/ggml-org/llama.cpp/pull/30383) (b11572), touch the server only
  for decision requests, and split Qwen3's graph to share its backbone; none of set A runs on
  either path.
- **Graph reallocations.** [#30329](https://github.com/ggml-org/llama.cpp/pull/30329) (b11567)
  counts the times a compute graph's buffers are reserved again during inference, logs each
  unexpected one, and adds a `graph reallocs` line to each slot's timings in the router log.
  It reports and changes nothing; the footprint and baseline runs can read the new line.
- **Sampling defaults.** Unchanged. [#30095](https://github.com/ggml-org/llama.cpp/pull/30095)
  fixes draft sampling in the speculative example only. The GGUF `general.sampling.*` defaults
  and the presets' values apply as before.
- **Other.** [#29898](https://github.com/ggml-org/llama.cpp/pull/29898) fixes the web UI's
  text-attachment preview; [#30299](https://github.com/ggml-org/llama.cpp/pull/30299) changes
  how the tests link.
- **Flags and preset keys.** Only `--kv-unified` changes, gaining the speculative example
  (#30095); the server's flag is as before. No flag is renamed or removed; router mode, the
  preset keys, `--models-max`, and `load-on-startup` are unchanged. No preset key changes
  meaning, so the presets did not change.

### What was re-verified on b11572

- **The install.** The architect installed b11572 from its Vulkan x64 asset (sha256 checked)
  into `/opt/llama.cpp/b11572`. `--list-devices` showed Vulkan0 (RADV STRIX_HALO). Then the
  architect repointed `current` and restarted `llama-router`. All four models loaded with no
  manual load, and `/props` reports `b11572-efe3626b1`.
- **The probe.** `mise run integration` against the router passed on b11572, with the same
  rendered unit and presets. The footprint, the conform rates, and the baseline on this build
  are in [`memory-budget.md`](../../docs/findings/memory-budget.md),
  [`tool-reliability.md`](../../docs/findings/tool-reliability.md), and
  [`baseline.md`](../../docs/findings/baseline.md).

## b11572 to b11669, 2026-10-15

- **From:** b11572 (`b11572-efe3626b1`), installed by `consumers`.
- **To:** b11669 (`b11669-620d2225c`), the b-tag `main-model` planned on.
- **Reviewed:** the 97 commits in `b11572...b11669` (`gh api
  repos/ggml-org/llama.cpp/compare/b11572...b11669`) and the PRs they name, read for the chat
  output and the parsers, template output, the embedding path and the prompt cache, Vulkan on
  AMD, sliding-window reuse, slots, Nemotron support, flags and preset keys, sampling defaults,
  and `/v1/messages` thinking. Where a question needed the source, the review read it at the
  `b11669` tag.

### What the notes change for set A

- **Chat output.** [#30389](https://github.com/ggml-org/llama.cpp/pull/30389) (the chat
  output rewrite) makes a rendered prompt a list of parts, each marked as template text or
  user input, so a later PR can stop parsing special tokens inside user input. Nothing uses the
  marks yet: the server joins the parts back into one string
  (`tools/server/server-common.cpp`, `out_session.prompt().str()`), so every endpoint sends the
  model the same text as on b11572. The gpt-oss and Gemma 4 parsers change only in how they
  append to the prompt. The `<|constrain|>` final that `tool-reliability.md` describes still
  fails the same way.
- **Template number formatting.** [#30362](https://github.com/ggml-org/llama.cpp/pull/30362)
  makes `tojson` write a float as Python's `json.dumps` does, with all its digits and its
  `.0`: `12.0` was `12`, `48.858844` was `48.8588`, `1000000.0` was `1e+06`. `|string` and
  `|safe` of a float change the same way. A float in a tool's schema or a tool call's
  arguments now reaches the model as it would through transformers, which changes the prompt
  whenever a consumer's tools carry one. [#30483](https://github.com/ggml-org/llama.cpp/pull/30483)
  writes `NaN` and `Infinity` in `tojson` as `json.dumps` does.
  [#30533](https://github.com/ggml-org/llama.cpp/pull/30533) gives each loop iteration its own
  scope, so a variable set in one iteration no longer leaks into the next, as in Python's
  Jinja. All three bring llama.cpp's template output closer to the reference; none needs a
  preset change.
- **The embedding path and the RAM prompt cache.** EmbeddingGemma 2 runs pooled (`embeddings`,
  mean pooling). [#30445](https://github.com/ggml-org/llama.cpp/pull/30445) skips the LM head
  and its logits buffer for a pooled context, which for EmbeddingGemma 2 had already lost its
  logits buffer at b11572 (no LM head). [#30461](https://github.com/ggml-org/llama.cpp/pull/30461)
  removes a redundant expand from the embedding graphs; its PR reports EmbeddingGemma 2's
  embeddings bit-exact. [#30522](https://github.com/ggml-org/llama.cpp/pull/30522) fixes the
  startup abort #30461 caused for an embedding model started without `--embeddings`, which the
  preset always sets. [#30416](https://github.com/ggml-org/llama.cpp/pull/30416),
  [#30439](https://github.com/ggml-org/llama.cpp/pull/30439) and
  [#30446](https://github.com/ggml-org/llama.cpp/pull/30446) pad the cache-less attention an
  embedding model runs to 256 keys, with the embeddings unchanged.
  [#30498](https://github.com/ggml-org/llama.cpp/pull/30498) stops saving embedding and rerank
  slots to the RAM prompt cache (`--cache-ram`, 8GiB by default), since nothing reads them
  back: EmbeddingGemma 2's instance no longer fills that cache in system RAM. Completions are
  cached as before.
- **Vulkan KV on AMD.** [#27703](https://github.com/ggml-org/llama.cpp/pull/27703) copies a
  layer's f16 K and V into one contiguous scratch buffer before flash attention, so the reads
  spread across all memory channels. It runs only on AMD outside RDNA4, so on Strix Halo
  (RDNA 3.5), with an f16 cache, which every set-A model uses, and only for a batch of at least
  256 query tokens: prompt processing, never single-token generation. The scratch is one
  layer's K and V at the current KV length, at most 256MiB for gpt-oss-120b at c = 131072 (a
  full-attention layer's 8 KV heads of 64 at f16), held in the instance's compute buffers. The
  PR measures prompt processing faster at depth on a Radeon 8060S and generation unchanged.
- **Vulkan bf16.** [#29069](https://github.com/ggml-org/llama.cpp/pull/29069) lets Vulkan run a
  matrix multiply whose activations are bf16 by widening them to f32, where it refused the
  operation before; bf16 activations come from a graph that asks for them, such as a bf16
  depthwise-convolution kernel (#28937). Set A's graphs produce none, and bf16 weights, such as
  Mistral Small 4's mmproj-BF16, were already supported. A mixture-of-experts multiply
  (`MUL_MAT_ID`) with bf16 activations is still refused.
- **Vulkan's F32 `MUL_MAT_ID`.** Still refused. `ggml_backend_vk_device_supports_op` in
  `ggml/src/ggml-vulkan/ggml-vulkan.cpp` at b11669 (line 15505) returns false for a
  `MUL_MAT_ID` whose precision is `GGML_PREC_F32`, so the scheduler runs it on the CPU. Mistral
  Small 4's expert down-projections ask for that precision, and they stay on the CPU on this
  build; [#30105](https://github.com/ggml-org/llama.cpp/pull/30105), which would run them on
  Vulkan, is open and unchanged since 2026-10-08.
- **Gemma 4 sliding-window reuse.** [#30580](https://github.com/ggml-org/llama.cpp/pull/30580)
  fixes an off-by-two in the threshold that decides whether a slot state restored from the RAM
  prompt cache or a slot file covers a sliding-window model's window. The threshold asked for
  two cells more than the window holds, so a restored state was rejected and the slot rolled
  back to an older checkpoint, or reprocessed the prompt. Gemma 4 26B-A4B, gemma-4-E4B and
  gpt-oss-120b all have sliding-window layers: switching a slot back to an earlier
  conversation from the RAM cache now reprocesses only the tokens after the window (on
  gemma-4-E4B, 17 instead of 53 in the PR). Prefix reuse measured on earlier builds can read
  better on b11669 where a run restores from the RAM cache.
- **Slot selection.** [#30567](https://github.com/ggml-org/llama.cpp/pull/30567) makes the
  extra tasks of a split request (`n` above 1, decision questions) take empty slots first, then
  the least recently used, and save a state they overwrite to the prompt cache; every slot
  choice now uses one eviction rule. A request with `n` = 1, which every consumer and
  instrument sends, picks its slot as before. [#30574](https://github.com/ggml-org/llama.cpp/pull/30574)
  also saves a draft model's state in a slot file; set A runs no draft.
- **The Ministral 3 and Qwen3-Coder handlers.** Both change only in the chat-output rewrite's
  one line, which appends to the prompt as a part (#30389); their detection, reasoning tags and
  parsers are as on b11572. They matter to the candidates, not set A: by
  `common_chat_template_detect_params_init` in `common/chat.cpp` at b11669, Mistral Small 4's
  upstream template (`[SYSTEM_PROMPT]`, `[TOOL_CALLS]` and `[ARGS]`, no `[CALL_ID]`) takes the
  Ministral 3 handler, and Nemotron 3.5 Super's (`<tool_call>`, `<function=`, `<parameter=`,
  `<think>`) takes the Qwen3-Coder handler with reasoning on. "The candidates loaded on
  b11669" below confirms that each GGUF's embedded template takes the same handler.
- **Nemotron support.** [#30520](https://github.com/ggml-org/llama.cpp/pull/30520) adds
  Nemotron 3.5 Super VL and Nemotron Bolt 74B A9B: the `NemotronH_Omni_Reasoning_V3`
  architecture in the Nemotron-H graph, and its vision encoder in `mtmd`. b11669 is the first
  build that runs the nemotron set's main model, as the profile's `[engine]` comment says; set
  A runs on no part of it.
- **Other.** [#30405](https://github.com/ggml-org/llama.cpp/pull/30405) maps into a backend
  buffer only the ranges of a model file that buffer's tensors use, so a model with per-layer
  token embeddings no longer pins its lazily read embedding table into the GPU buffer. That may
  lower gemma-4-E4B's footprint (it has per-layer embeddings); the next footprint run shows it.
  [#29741](https://github.com/ggml-org/llama.cpp/pull/29741) changes how Vulkan shares host
  memory between several GPUs, with a single GPU unaffected;
  [#29877](https://github.com/ggml-org/llama.cpp/pull/29877) applies only to devices with no
  cooperative matrix and no integer dot product, and
  [#30215](https://github.com/ggml-org/llama.cpp/pull/30215) only to NVIDIA's coopmat2, so
  neither runs on Strix Halo. [#30582](https://github.com/ggml-org/llama.cpp/pull/30582) fixes
  the web UI's agentic turn limit. The ggml library moves to 0.27.0 and llama.cpp to 0.7.0
  ([#30603](https://github.com/ggml-org/llama.cpp/pull/30603)).
- **Sampling defaults.** Unchanged: `common/common.h` gains only two fields, the draft's batch
  size and whether `--cache-ram` was given. The GGUF `general.sampling.*` defaults and the
  presets' values apply as before.
- **`/v1/messages` thinking.** Unchanged: `tools/server/server-chat.cpp` has no commit in the
  range. A request's `thinking` with `type` `enabled` sets the reasoning budget from
  `budget_tokens`; `disabled`, `adaptive` and `output_config.effort` are not read.
  [#20479](https://github.com/ggml-org/llama.cpp/pull/20479), which maps Claude's
  `thinking.type` and `output_config.effort` (and OpenAI's and OpenRouter's reasoning fields)
  to the template's reasoning, is open and unchanged since 2026-06-26. Claude Code meets
  `/v1/messages` and its thinking blocks as b11572 served them.
- **Flags and preset keys.** Two changes: `--spec-draft-n-ubatch` (`LLAMA_ARG_SPEC_DRAFT_N_UBATCH`,
  [#30459](https://github.com/ggml-org/llama.cpp/pull/30459)) sets a draft model's physical
  batch, and `--cache-ram`'s default becomes 0 for decision models (#30498). No flag is
  renamed or removed. `ctx-checkpoints` (`-ctxcp`, default 32 per slot) and
  `checkpoint-min-step` (default 8192) are as before, and `tools/server/server-models.cpp`
  has no change, so router mode, the preset keys, `--models-max`, and `load-on-startup` are
  unchanged. No note requires a preset change, so the presets did not change for this move.

### What was re-verified on b11669

- **The install.** The architect installed b11669 from its Vulkan x64 asset (sha256 checked)
  into `/opt/llama.cpp/b11669`, repointed `current` and restarted `llama-router`. All four of
  set A's models loaded with no manual load, and `/props` reports `b11669-620d2225c`.
- **The probe.** `mise run integration` against the router passed on b11669, set a, with the
  same rendered unit and presets as on b11572: `/health`, the four models loaded, the build at
  or above the minimum, each model's default sampling, gpt-oss-120b's chat and `/v1/messages`
  tool call (`get_weather` `{"city":"Paris"}`), the 26B-A4B's image and gemma-4-E4B's audio
  answers, and EmbeddingGemma 2's 768 dimensions.
- **The swap.** `go run ./hosting/cmd/swap a` against the router read set A loaded and kept all
  four, with no unload or load: the command's first run against a real router. `swap mistral`
  stopped before any change, since the installed presets, rendered before the candidate sets
  existed, list neither candidate.

### The candidates loaded on b11669

`main-model` slice 5 loaded both candidate sets on b11669 on 2026-10-15, through `hosting/cmd/swap`.
Their footprints and the swap timings are in
[`memory-budget.md`](../../docs/findings/memory-budget.md).

- **The presets, first.** Two faults in the presets kept the candidates from loading, and both
  were fixed before the swaps. First, the router renames a section to its canonical tag, the
  quant type at the end of the tag (`canonical_tag` in `common/preset.cpp`), so
  `[unsloth/...:UD-Q4_K_XL]` was listed as `:Q4_K_XL`. Mistral's router ID is now
  `unsloth/Mistral-Small-4-119B-2603-GGUF:Q4_K_XL`, and `profile.Validate` refuses an ID the
  router would rename. Second, a section with no `hf-repo` gets one only by merging with the
  router's entry for a model already in its cache. Neither candidate was cached, so its process
  would have started with no model, and `llama-server` then runs as a second router (the
  `is_router_server` check in `tools/server/server.cpp`). Every section now renders
  `hf-repo = <its ID>`; installed as sha256 `9eeb9f04…`.
- **Mistral Small 4.** The files are
  `UD-Q4_K_XL/Mistral-Small-4-119B-2603-UD-Q4_K_XL-0000{1,2,3}-of-00003.gguf`, 74.16GB.
  `find_best_model` in `common/download.cpp` takes the first file matching `Q4_K_XL[.-]`, and
  only these splits match. `/props` names the first split as the model path, ftype
  `Q4_K - Medium`. The mmproj is `mmproj-BF16.gguf`, by the router log's `loaded multimodal
  model` line. The handler is Ministral 3: the served template has `[SYSTEM_PROMPT]`,
  `[TOOL_CALLS]` and `[ARGS]` and no `[CALL_ID]`, which is what
  `common_chat_template_detect_params_init` checks first. That function logs its choice only at
  debug level, which the router's default verbosity doesn't print, so the handler was read by
  applying the function's checks to the template `/props` serves. It is unsloth's version of
  the template, which carries `reasoning_effort`, and `/props` reports
  `supports_reasoning_effort`. The load took 23s after the download.
- **Mistral's expert down-projection runs on the CPU.** The shared mixture-of-experts graph
  (`build_moe_ffn` in `src/llama-graph.cpp`) calls
  `ggml_prec_set_src(experts, GGML_PREC_F32, 1)` on the expert down-projection for
  `LLM_ARCH_MISTRAL4` ("src1 can exceed F16 range"). That call writes op_params[3], and Vulkan's
  `supports_op` refuses a `MUL_MAT_ID` with it, so the scheduler places the op on the CPU. The
  log doesn't print the placement at default verbosity, but the run shows it. While generating,
  Mistral's process held 840-890% CPU (`top`, 2s samples during the probe), about 8.5 of its 16
  threads, and 0% when idle. One request generated at 3.4 tokens/s (209 tokens; the server's
  `timings`). Nemotron's process peaked at 60% CPU and generated at 12.1 tokens/s on the same
  kind of request. This is the open PR #30105 above.
- **Mistral's probe.** `SPIKE_SET=mistral mise run integration` passed. A second, verbose
  probe run failed only its image check: Mistral answered "Please upload the image you'd like
  me to describe." Repeated, the same request (871 prompt tokens each time, the image
  included) failed 3 of 15 times. Each failure claimed it could not see an image. In two of
  them the reasoning named the three shapes and then called them "a description rather than an
  actual image file". The other 12, and 6 more with the image placed first, named every shape
  and colour. So the image reaches the model, and the failure is the model's own answer, at
  its card's temperature 0.7 and `reasoning_effort` high. Chat, the `/v1/messages` tool call
  (`get_weather` `{"city":"Paris"}`), gemma-4-E4B's audio and EmbeddingGemma 2 passed every
  run.
- **Nemotron 3.5 Super VL.** The file is `NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-NVFP4.gguf`,
  76918795552 bytes, the 120B weights. The 1.35GB `lilicorr-...-NVFP4.gguf` was not fetched:
  the cache holds only the weights and the mmproj, and `/props` names the weights as the model
  path. The mmproj is `mmproj-NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-Q8_0.gguf`. The handler
  is Qwen3-Coder: the template has `<tool_call>`, `<function=` and `<parameter=`, and reads
  `enable_thinking`. Reasoning is on: a plain chat returned `reasoning_content`, and the
  server logged that the template preserves reasoning by default. The context checkpoints are
  8 per slot: the preset's `ctx-checkpoints = 8` reaches the process as `--swa-checkpoints 8`,
  an alias of the same flag in `common/arg.cpp`. One connection drop during the download was
  retried by the fetch itself. The load took 23s after the download. `SPIKE_SET=nemotron mise
  run integration` passed, and so did a verbose rerun, image included ("a red circle, a blue
  square, and a green triangle on a white background").
- **Back to set A.** `swap a` reloaded gpt-oss-120b and the 26B-A4B from the cache in 23.0s,
  and `mise run integration` on set A passed.

## b11669 to b11715, 2026-10-17

- **From:** b11669 (`b11669-620d2225c`), installed by `main-model`.
- **To:** b11715 (`b11715-9d1878a43`), the newest b-tag when `evaluation` slice 10 began. Its
  Vulkan x64 asset is `llama-b11715-bin-ubuntu-vulkan-x64.tar.gz`, sha256
  `b3405b06ab9b56975d0b1bc8dcdd15a8bace74fc5e4cf1e66c1677a99e590364`.
- **Reviewed:** the 46 commits in `b11669...b11715` (`gh api
  repos/ggml-org/llama.cpp/compare/b11669...b11715`), read for the router, Vulkan, mtmd and the
  image encoder (the `mtmd-batch-max-tokens` cap and Gemma 4's image warm-up), the RAM prompt
  cache, gpt-oss tool-call parsing, the embeddings endpoint, and audio. Where a question needed
  the source, the review read it at the `b11715` tag. No commit in the range touches
  `tools/server/server-context.cpp`, `server-task.cpp`, `server-chat.cpp`, `common/arg.cpp`,
  `common/preset.cpp`, `src/llama-kv-cache.cpp`, `src/llama-context.cpp`,
  `common/parsers/gpt-oss.cpp`, `common/parsers/gemma4.cpp`, or the Gemma 4, gpt-oss and
  EmbeddingGemma graphs (`src/models/gemma4.cpp`, `openai-moe.cpp`, `gemma-embedding2.cpp`).

### What the notes change for set A

- **The router.** [#29584](https://github.com/ggml-org/llama.cpp/pull/29584) (the web UI's
  model discovery) changes the router's download API and its listing, not loading. `GET /models`
  gains each cached model's `sidecars` (such as `["mmproj"]`), an `incomplete_sidecars` list, and
  an `incomplete` status for a download that never finished. `POST /models` takes a `sidecars`
  list, `["mmproj"]` by default as `-hf` pulls. `/models/unload` on a downloading model now
  pauses it and keeps its partial files, where it cancelled and deleted them before; `DELETE
  /models` drops them. The router remembers unfinished downloads in
  `$HF_HOME/unfinished-downloads.json`, inside the cache the service user owns. Loading,
  unloading, the presets, `load-on-startup` and `--models-max` are unchanged, and a set-A tag
  resolves the same files: `find_best_model` and `find_best_sibling` are as on b11669, with the
  sidecar keywords moved into one list. `hosting/router` reads only `status.value`, so the new
  `incomplete` value reads as not loaded. [#21752](https://github.com/ggml-org/llama.cpp/pull/21752)
  makes the HTTP client honor `HTTP_PROXY` and `HTTPS_PROXY`; the unit sets neither.
- **Vulkan.** [#30468](https://github.com/ggml-org/llama.cpp/pull/30468) turns off importing host
  memory into GPU buffers on AMD ("amdgpu re-faults all pages of every imported host allocation
  on each submission"). The import runs only with more than one Vulkan device
  (`vk_instance.multi_device`), so on Strix Halo's one device nothing changes; only a warning's
  wording does.
- **mtmd and the image encoder.** The range adds Muse Spark's vision and audio
  ([#30616](https://github.com/ggml-org/llama.cpp/pull/30616)) and Voxtral Realtime's streamed
  audio ([#30629](https://github.com/ggml-org/llama.cpp/pull/30629)), and lets LightOnOCR keep an
  image's native size ([#30600](https://github.com/ggml-org/llama.cpp/pull/30600)). Each adds a
  projector type or changes one set A doesn't run. Gemma 4's image projector still reserves its
  graph for a 256-token warm-up image (`set_warmup_n_tokens(256)` in `tools/mtmd/clip.cpp`), so
  the growth `memory-budget.md` measured on b11669 has the same cause on b11715.
  `--mtmd-batch-max-tokens` is unchanged: default 1024, passed to mtmd as `batch_max_tokens`
  (`tools/server/server-context.cpp`), and a preset key without its dashes, since
  `common/preset.cpp` maps each key to the argument it names. The flag arrived with mtmd's
  batching API ([#24384](https://github.com/ggml-org/llama.cpp/pull/24384), 2026-06-13) and
  is in b11452, the profile's minimum build, so the vision model's new `mtmd-batch-max-tokens =
  512` needs no higher minimum.
- **Audio.** #30629 makes mtmd's audio preprocessor optional, for Voxtral Realtime's
  stream-only input. Gemma 4's audio projector (`GEMMA4UA`) keeps its raw-waveform path, now
  one of three encoder-free projectors in the same list. gemma-4-E4B's audio is as on b11669.
- **The RAM prompt cache (`--cache-ram`).** Unchanged: the server code that saves, evicts
  ("making room for prompt cache entry") and restores slot states has no commit in the range.
  Nothing in the range matches gemma-4-E4B answering only `<unused32>` after those evictions, seen
  once in slice 8b and not since. Two open reports are near it. In
  [#27148](https://github.com/ggml-org/llama.cpp/issues/27148), the RAM prompt cache restores an
  unrelated conversation into a fresh slot; it was reproduced on Strix Halo with `kv_unified`
  true and sequential requests, and its fix, [#27624](https://github.com/ggml-org/llama.cpp/pull/27624)
  (clear stale state when reusing an LRU-selected slot), is open. In
  [#26088](https://github.com/ggml-org/llama.cpp/issues/26088), Gemma 4 emits only `<unusedNN>`
  tokens after a long idle, reproduced on CUDA and ROCm and not on Vulkan. The symptom, if it
  recurs, is to be recorded with the router log around it.
- **gpt-oss tool calls.** [#30670](https://github.com/ggml-org/llama.cpp/pull/30670) (Beam) adds a
  Harmony XML handler, detected before gpt-oss's only for a template with `<|constrain|>xml` and
  `rfl:parameter`. gpt-oss's template has neither, so it keeps the GPT-OSS handler, whose parser
  is unchanged. The `<|constrain|>` final that fails the peg-native parse with a 500
  (`tool-reliability.md`) fails the same way;
  [#25332](https://github.com/ggml-org/llama.cpp/pull/25332), which would accept it, is open and
  unchanged since 2026-07-06. #30616 adds a Muse Spark handler, after gpt-oss's in the detection
  order.
- **Templates and vocabularies.** #30670 adds a `list` filter for an object to the template
  engine, which set A's templates already render without. #30616 adds `<|eot|>` to the token
  texts read as end of generation; `/tokenize` on the router splits `<|eot|>` into ordinary
  pieces for gpt-oss-120b, the 26B-A4B and gemma-4-E4B, so none of their vocabularies holds it.
  #30670's rule for a normal `</s>` applies only to a vocabulary with Beam's
  `<rfl:parameter name="` token.
- **The embeddings endpoint.** Unchanged: no commit touches the server's embedding path or
  EmbeddingGemma 2's graph.
- **Graphs.** The shared mixture-of-experts graph gains an optional post-expert norm (#30616),
  passed only by Muse Spark, and Mellum joins Mistral Small 4 in asking for F32 precision on the
  expert down-projection ([#30584](https://github.com/ggml-org/llama.cpp/pull/30584)). Vulkan
  still refuses that precision, and [#30105](https://github.com/ggml-org/llama.cpp/pull/30105) is
  open and unchanged since 2026-10-08, so Mistral Small 4's down-projections stay on the CPU.
  [#30613](https://github.com/ggml-org/llama.cpp/pull/30613) shares K-pool views only in
  GLM-5-Next and Qwen4-Exp. The attention-temperature input gains a power form (#30670), used
  only when a model's GGUF asks for it; set A's don't.
- **Other.** New models: Kolibri-1, Beam, Muse Spark, Mellum 2.1's MTP, and Granite Switch's
  decision heads (which add a `text` and `decisions` output for one model type). The rest is
  CUDA, SYCL, OpenCL, Hexagon, CPU on s390x, conversion, CI and the web UI.
- **Sampling defaults.** Unchanged: `common/common.h` gains only a decision-model type. The GGUF
  `general.sampling.*` defaults and the presets' values apply as before.
- **Flags and preset keys.** `common/arg.cpp` and `common/preset.cpp` have no commit in the range,
  so no flag or preset key is added, renamed or removed, and router mode, `--models-max` and
  `load-on-startup` are unchanged. No note requires a preset change or a higher minimum build.
  The presets change for this move only by the vision model's image batch cap, which
  `evaluation` slice 8 chose on b11669 and which installs with the move.

### The move

The root commands, in the order the architect runs them, are the runbook's "Moving to a new
build" ([`README.md`](README.md)). The architect ran its steps a to d on 2026-10-17.

- **The install.** b11715 is installed at `/opt/llama.cpp/b11715` (`llama-server --version`:
  `version: 0.7.0-dev (build 11715, commit 9d1878a43)`). The rendered `models.ini` is installed;
  its diff against the b11669 one is the nine expected lines, ending
  `mtmd-batch-max-tokens = 512`. The unit is unchanged. The b11669 files are kept as
  `/etc/llama-router/*.b11669`, and b11669 stays in its versioned directory.
- **The switch.** `current` points at b11715 and `llama-router` restarted. All four of set A's
  models loaded with no manual load, and `/props` reports `b11715-9d1878a43`.
- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed on
  b11715, set a: every package ok, in 11.2s.
- **The image batch cap.** The 26B-A4B's child process runs with
  `--mtmd-batch-max-tokens 512` (`ps -eo args` on the Framework, read over user-level SSH).
- **The idle pool.** With set A loaded and idle, `amdgpu_top -p --no-pc` read
  `GTT 94795/98304 MiB` twice, at 22:53:03 and 22:53:18 UTC: 3509 MiB free, above the 2816 MiB
  floor.
- **Cold load.** `evaluate coldload -set a` timed each restart from the laptop, in one sitting,
  from the restart seen to each model listed loaded. The router's load mode is its default,
  `auto` (mmap). A cold restart dropped the page cache first (`sync` and `drop_caches`); a warm
  one didn't. The logs were kept under `.state/coldload/` and removed once the findings were
  committed (the architect's decision, slice 11). These are the baseline's cold-load figures:

  | Restart | Router up | EmbeddingGemma 2 | gemma-4-E4B | 26B-A4B | gpt-oss-120b, all loaded |
  |---|---|---|---|---|---|
  | switch to b11715 | 1.2s | 3.0s | 8.7s | 12.0s | 21.5s |
  | warm | 0.5s | 2.0s | 7.2s | 10.0s | 18.0s |
  | warm, again | 0.5s | 2.3s | 7.3s | 10.0s | 17.0s |
  | cold | 0.5s | 2.5s | 8.2s | 10.8s | 20.8s |
  | cold, again | 0.5s | 2.3s | 7.8s | 11.5s | 21.0s |

  Cold is about 3s slower than warm for some 85 GB of weights. That gap is small enough that
  loading looks bound by the copy into device memory more than by reading the disk; this is an
  observation from two pairs, not a measured cause.

## The OS update, 2026-10-19

The architect updated the Framework's OS and rebooted it, with the router still on b11715
(`b11715-9d1878a43`). The update changes the Vulkan driver under the router:

| Package | Before (set A's baseline) | After |
|---|---|---|
| kernel | `7.2.5-3-omarchy` | `7.2.8-9-omarchy` |
| `mesa`, `vulkan-radeon` | 26.2.2-1 | 26.2.5-1 |
| `linux-firmware` | 20260810-2 | 20260916-1 |
| Omarchy | not recorded | 4.0.5 |

The router came back with set A loaded and 3656 MiB of the pool free. The re-check, from the
laptop on 2026-10-19:

- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed, every
  package ok, in 12.1s.
- **Fitness, every role.** Each role's run started from a reload of set A and was interrupted
  five minutes in, once its quality stage had begun; the interrupted run still writes its report.
  Every gate before quality passed, and fitness took seconds, well under the ~3 minutes the
  stage is budgeted:

  | Role | Fitness | Capacity | Measured against the floors |
  |---|---|---|---|
  | embedding | 0.9s | 0.5s | 12,945 tok/s; 32-token query 32 ms |
  | audio | 0.5s | 2.0s | RTFx 12.4; slowest of 4 streams 3.19 |
  | vision | 7.0s | 13.0s | one image question 1.8 s |
  | agent | 15.5s | 5.4s | decode 49.5 tok/s; 8k prefill 1046 tok/s, TTFT 8.0 s; slowest of 4 streams 21.1 tok/s |

  The idle pool read 3513 to 3642 MiB free after each reload, and the least free at capacity's
  peak was 3495 MiB, against the 2816 MiB floor.
- **Quality on the new driver.** The embedding run finished every stage inside the five
  minutes and passed: STSBenchmark 0.875, Banking77 0.923, SciFact 0.852 and NFCorpus 0.366,
  each at or above its bar, and the set stage held 3543 MiB free with no crossed answer. The
  audio run's first two sets finished before the interrupt: LibriSpeech test-clean WER 0.022 and
  test-other 0.061, each under its ceiling.
- **A connection lapse.** Between the embedding and audio runs, the reload's requests to the
  router failed to connect for 134 s (`curl: (28) Failed to connect`), then went through. The
  router's unit stayed up throughout (active since the boot), and the reload completed. The
  cause wasn't found.

## b11715 to b11734, 2026-10-19

- **From:** b11715 (`b11715-9d1878a43`), installed by `evaluation`, on the OS update's driver.
- **To:** b11734 (`b11734-f3fb2fcdb`), the newest b-tag when `screen`'s BUILD began. Its Vulkan
  x64 asset is `llama-b11734-bin-ubuntu-vulkan-x64.tar.gz`, sha256
  `2c8b590c325960eb474c08c7a3b02b3c8eda054aa03352ed864948dc4ae1668d`.
- **Reviewed:** the 19 commits in `b11715...b11734` (`gh api
  repos/ggml-org/llama.cpp/compare/b11715...b11734`), read for Vulkan on gfx1151, the router
  (`server-models`, the presets and their reload), tool-call parsing, mtmd's vision and audio,
  and the embeddings endpoint. Where a question needed the source, the review read it at the
  `b11734` tag. No commit in the range touches `tools/server/`, `tools/mtmd/`, `common/` (so
  neither `common/arg.cpp`, `common/preset.cpp` nor `common/parsers/`), `src/llama-graph.cpp`,
  or the Gemma 4, gpt-oss and EmbeddingGemma graphs. 32 of the range's 55 changed files are in
  the Hexagon, CUDA, SYCL, CANN and WebGPU backends; most of the rest are tests, CI, scripts,
  conversion and the web UI.

### What the notes change for set A

- **Vulkan.** One line, from [#28851](https://github.com/ggml-org/llama.cpp/pull/28851)
  (non-contiguous `SUM` on CPU, CUDA, Vulkan and WebGPU): Vulkan's `supports_op` takes `SUM`
  over any contiguously allocated F32 tensor, where it shared `SUM_ROWS`'s contiguous-rows test
  before. No graph set A runs calls `ggml_sum`: `src/models/gemma4.cpp`, `openai-moe.cpp`,
  `gemma-embedding2.cpp`, `src/llama-graph.cpp`, `tools/mtmd/clip.cpp`, and the four Gemma 4
  projectors under `tools/mtmd/models/` have none at `b11734`. Nothing else in the Vulkan
  backend changes.
- **The router.** Unchanged: no commit touches `tools/server/`. Loading, unloading, the presets,
  `load-on-startup`, `--models-max`, `GET /models?reload=1` and `DELETE /models` behave as on
  b11715. A reload re-reads the file `--models-preset` names, unloads a running model whose
  section changed, and never loads one (`server-models.cpp`, `load_models`); `DELETE /models`
  removes only a model whose source is the cache, and refuses a preset's model ("not removable
  (not from cache)").
- **Tool-call parsing.** Unchanged: `common/parsers/` and the chat code have no commit. The
  `<|constrain|>` final that fails gpt-oss's peg-native parse (`tool-reliability.md`) fails the
  same way; [#25332](https://github.com/ggml-org/llama.cpp/pull/25332) is open and unchanged
  since 2026-07-06.
- **mtmd, vision and audio.** Unchanged: no commit touches `tools/mtmd/`. Gemma 4's image
  warm-up reservation, `--mtmd-batch-max-tokens` and the `GEMMA4UA` audio path are as on b11715.
- **The embeddings endpoint.** Unchanged. [#30607](https://github.com/ggml-org/llama.cpp/pull/30607)
  adds bidirectional attention and a `dense_2` output projection to the Qwen3 graph, for Qwen3
  embedding GGUFs that carry them; EmbeddingGemma 2's graph is untouched.
- **The RAM prompt cache.** Unchanged; [#27624](https://github.com/ggml-org/llama.cpp/pull/27624)
  is open and unchanged since 2026-10-10.
- **Graphs and CPU.** [#30791](https://github.com/ggml-org/llama.cpp/pull/30791) vectorizes the
  CPU's SiLU and `expf`; set A runs every layer on the GPU, so only a CPU fallback would see it.
  [#30105](https://github.com/ggml-org/llama.cpp/pull/30105), Vulkan's F32 expert down-projection,
  is open and unchanged since 2026-10-08, so Mistral Small 4's disqualification stands.
- **Tools.** [#30497](https://github.com/ggml-org/llama.cpp/pull/30497) makes `gguf-split` and
  `export-lora` refuse a truncated GGUF; the router uses neither.
- **Sampling defaults, flags and preset keys.** Unchanged: `common/` has no commit in the range,
  so no flag or preset key is added, renamed or removed. No note requires a preset change or a
  higher minimum build. The presets the move installs differ from the installed ones only in two
  reason comments that `evaluation`'s editor rewrote after b11715's install (the 26B-A4B's c and
  Mistral Small 4's c); the unit is unchanged.

### The move

The root commands, in the order the architect runs them in one sitting, are the runbook's
"Moving to a new build" ([`README.md`](README.md)), with the preset directory's write access and
the deletions of "The models the host keeps". The rendered presets' sha256 is
`9c983739f3142f01a92726b56d286150c3f21c3df50f8953c957a815401962ea`; the unit's is
`83b0a95c16769d0ee7abb7f6b9e9aec9ccb61d7f57831f508465645e31c51554`, the installed one. The
architect ran steps a to d on 2026-10-19, with no output differing from the runbook's.

- **The install.** b11734 is installed at `/opt/llama.cpp/b11734` (`llama-server --version`:
  `version: 0.7.0-dev (build 11734, commit f3fb2fcdb)`). The rendered `models.ini` and the unit
  are installed, so the installed presets are the committed render (`9c983739`). The b11715
  files are kept as `/etc/llama-router/*.b11715`.
- **The preset directory and the deletions.** `/etc/llama-router` is `drwxrwxr-x
  root:<group>`, the service user's group, and `models.ini` is still `root:root` `0644`. The
  cache's `hub` holds the five models' directories the profile keeps (set A's four and
  Nemotron 3.5 Super, still unscreened), with no Mistral Small 4 directory and no retired
  directory beside the cache; the models drive went from 356G used to 155G. `GET /models` still
  lists Mistral Small 4, a preset, unloaded, now with no sidecars on disk. `/opt/llama.cpp` holds
  b11715, b11734 and `current`.
- **The switch.** `current` points at b11734 and `llama-router` restarted at 19:27:01 UTC. The
  `evaluate coldload -set a` watch saw the router up in 0.5s and all four of set A's models
  loaded in 16.5s (EmbeddingGemma 2 2.0s, gemma-4-E4B 3.3s, 26B-A4B 4.8s, gpt-oss-120b 16.5s),
  with no manual load; `/props` reports `b11734-f3fb2fcdb`. The new build's files had just been
  written, so this isn't a warm or cold measurement, and the move's cold-load pair (the
  runbook's step e) waits for the deep stage's `-restart`.
- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed on b11734,
  set a: every package ok, in 8.8s.
- **The idle pool.** With set A loaded and idle after the restart, `amdgpu_top -p --no-pc` read
  `GTT 94798/98304 MiB` at 19:27:45 UTC: 3506 MiB free, above the 2816 MiB floor.
- **Fitness, every role.** As at the OS update: each role's run started from a reload of set A
  over the API (30 s idle, then a footprint reading of 3506 MiB free each time) with `TMPDIR` in
  `.state/tmp`, and was interrupted once its quality stage had begun. Admission, fitness and
  capacity passed for every role:

  | Role | Fitness | Capacity | Measured against the floors |
  |---|---|---|---|
  | embedding | 0.7s | 0.5s | 17,657 tok/s; 32-token query 13 ms |
  | audio | 0.5s | 2.0s | RTFx 12.4; slowest of 4 streams 3.18 |
  | vision | 7.1s | 10.1s | one image question 1.8 s |
  | agent | 15.2s | 5.0s | decode 50.5 tok/s; 8k prefill 1048 tok/s, TTFT 8.0 s; slowest of 4 streams 23.5 tok/s |

  The idle pool read 3464 to 3491 MiB free at fitness, and the least free at capacity's peak was
  3446 MiB (the agent's), against the 2816 MiB floor. The figures sit with the OS update's on
  b11715; the embedding's throughput and query time read faster, from one run each.

### Per-model slots, 2026-10-19

`screen`'s slice 2 moved the slots from the profile's one host-wide value into each model's
entry, rendered as each section's own `parallel` beside `[*]`'s `kv-unified = true`; every model
keeps 4, so set A's values are unchanged. The new render, sha256
`591c0ddbf57c04d3af6576dc8f951801d4c3d6a852dedd92b2d75a08be6f40b8`, replaced `9c983739` with no
root command and no restart, by the runbook's "The preset file's write access": written beside
`models.ini` in `/etc/llama-router`, renamed over it, and applied with `GET /models?reload=1`.

- **The install.** `sha256sum /etc/llama-router/models.ini` on the Framework reads `591c0ddb…`,
  the committed render. The file is now the service user's (`-rw-r--r-- <user>:<group>`), as a
  rename leaves it, until the next install replaces it with a root-owned one.
- **The reload.** It unloaded nothing: each model's arguments are the same as before
  (`--parallel 4` and `--kv-unified` on each), so set A stayed loaded, and `/props` reports
  `total_slots` 4 for each of its models.
- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed on b11734,
  set a, with its new slots check (each model's `total_slots` against its profile entry).

### Set A's swept slots and c, 2026-10-20

`screen`'s slice 8 swept set A whole (`evaluate sweep -set a`) and committed each model's chosen
slots and c ([`../../docs/findings/slots.md`](../../docs/findings/slots.md)): gpt-oss-120b 2 slots,
c = 17664; Gemma 4 26B-A4B 8, c = 41472; gemma-4-E4B 16, c = 31488; EmbeddingGemma 2 16,
c = 8192. The sweep installed each model's render as it committed it, with no root command and
no restart, by the runbook's "The preset file's write access"; before it began, the profile's
reasons were rewritten as model facts, installed as render `b987489d` the same way.

- **The install.** The committed render is sha256
  `6643c687c39a48ea518fab9c47e63aaa9ac842573419abaca2d75c340a87083e`; `sha256sum
  /etc/llama-router/models.ini` on the Framework reads the same, with no temporary file beside
  it, the file still the service user's (`-rw-r--r-- <user>:<group>`) until the next install.
  It replaced `591c0ddb`.
- **The reload.** Each model's section changed, so each reload unloaded and the sweep loaded
  again only the model it committed; `/props` reports `total_slots` and `n_ctx` 2 and 17664,
  8 and 41472, 16 and 31488, 16 and 8192.
- **The pool.** Idle after a reload of the set, 6463 MiB free; at the warmed peak 6015 MiB, at
  capacity's peak 5965 MiB, against the 2816 MiB floor (up from 3506 idle at four slots each,
  mostly gpt-oss-120b's c falling from 131072 to 17664).
- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed on b11734,
  set a, each model's `total_slots` matching its profile entry.

### Set A's pre-sweep slots and c restored, 2026-10-21

By the goal record's redirect of 2026-10-21, the startup set went back to the settings it ran on
before the sweep, the sweep's result kept in
[`../../docs/findings/slots.md`](../../docs/findings/slots.md) ("The startup set restored"):
4 slots each; c = 131072 (gpt-oss-120b), 32768 (Gemma 4 26B-A4B), 32768 (gemma-4-E4B), 8192
(EmbeddingGemma 2). Installed with `evaluate sweep -restore -set a`, with no root command and no
restart, by the runbook's "The preset file's write access".

- **The install.** The committed render is sha256
  `84086e3c02bf63658a58d0e3e7926a976f7fd27f135f6fe7584b0c427567c08c`, its keys the pre-sweep
  render's; `sha256sum /etc/llama-router/models.ini` on the Framework reads the same, with no
  temporary file beside it, the file the service user's until the next install. It replaced
  `6643c687`.
- **The reload.** The restore reloaded set A and reported every model serving at its committed
  setting; `/props` reports `total_slots` 4 for each, `n_ctx` 131072, 32768, 32768 and 8192.
- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed on b11734,
  set a.

### The Mistral reason corrected, 2026-10-21

The Mistral Small 4 entry's `c_reason`, which renders as a comment above its section, now cites
the pool's 2.75GiB floor (2816MiB, `evaluation/floors.toml`) in place of the 3GiB it had before
slice 8; no key line changed. Installed with `evaluate sweep -restore -set a`, with no root
command and no restart.

- **The install.** The committed render is sha256
  `a65fd306088ea3897f586645b2592c07e5fe3e578b50ca7e82a081c945097260`, its keys
  `84086e3c`'s; `sha256sum /etc/llama-router/models.ini` on the Framework reads the same, with no
  temporary file beside it. It replaced `84086e3c`.
- **The reload.** The restore reported every model of set A serving at its committed setting;
  `/props` reports `total_slots` 4 for each, `n_ctx` 131072, 32768, 32768 and 8192.
- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed on b11734,
  set a.

### Set A only, 2026-10-23

By the goal record's redirect of 2026-10-23 (decisions `answer` (redirect r1 to r3)), the
profile holds set A alone, and the Framework keeps only set A's models. The architect ran the
runbook's "The answer sitting: set A only" ([`README.md`](README.md)); the build is unchanged,
b11734.

- **The install.** The committed render is sha256
  `96732ef954c60ad9c1385b278955ff50b5b5f9a8b85c2699fc509353ad7a6761`, installed `root:root`
  `0644`; `sha256sum /etc/llama-router/models.ini` on the Framework reads the same. It replaced
  `a65fd306`, kept as `/etc/llama-router/models.ini.a65fd306`. The kept file is the service
  user's (`<user>:<group>`), not root's: `cp -p` keeps the owner, and the file it copied was the
  one `evaluate sweep -restore` wrote on 2026-10-21.
- **The deletion.** Nemotron 3.5 Super's directory
  (`models--ggml-org--NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-GGUF`) is deleted from the cache's
  `hub`, which now holds set A's four models' directories.
- **The reload.** `GET /models?reload=1` dropped both removed presets (Mistral Small 4 and
  Nemotron 3.5 Super) with no restart. `GET /models` lists set A's four, each loaded, and no
  other; `/props` reports `b11734-f3fb2fcdb`.
- **The probe.** `SPIKE_ROUTER_URL=http://<host>:8080 mise run integration` passed on b11734,
  set a.

## The router retired, 2026-10-29

spike-model-engines retired the router on 2026-10-29; the build was b11734 to the end. The
Framework serves set A from spike-model-engines' `llama` container (ggml's Vulkan image, b11960).

- **Removed:** the `llama-router` unit; `/etc/llama-router` and its 8 files; `/opt/llama.cpp`
  (b11715, b11734, and `current`); the Hugging Face cache on the models drive (set A's four
  repositories, 82G); the `~/models` link and `/mnt/models/gguf`; the render copies in the home
  directory; and outpost's links in `~/.local/bin`.
- **Kept:** the models drive, mounted and holding no model; the ufw route rules; the `docker`
  group; and `/srv/models`.
- **The rebuild:** the profile would rebuild the router. Its tasks re-render the unit and
  presets from `profile.toml`, the architect installs them by the runbook
  ([`README.md`](README.md)), and the builds and the 82G of models download again.
