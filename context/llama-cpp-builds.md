# llama.cpp builds on the Framework

The profile holds llama.cpp to a minimum build, and the Framework's router runs the upstream
Vulkan x64 release of one b-number at a time, from its own versioned directory through a
`current` link the unit runs. Each move here names the builds, the date, what the release
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
  with no model more than 17MiB from its b11529 figure ([`memory-budget.md`](memory-budget.md)).
- **Tool-call reliability.** Five `clutch conform` runs on the Framework: Pi passed every cell
  5/5, as on b11529, and one OpenCode cell, `tool`, fell from 4/5 to 3/5. The rates sit beside
  b11529's in [`tool-reliability.md`](tool-reliability.md), which reads the fall as chance.

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
  are in [`memory-budget.md`](memory-budget.md), [`tool-reliability.md`](tool-reliability.md),
  and [`baseline.md`](baseline.md).

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
- **The swap.** `go run ./cmd/swap a` against the router read set A loaded and kept all four,
  with no unload or load: the command's first run against a real router. `swap mistral`
  stopped before any change, since the installed presets, rendered before the candidate sets
  existed, list neither candidate.

### The candidates loaded on b11669

`main-model` slice 5 loaded both candidate sets on b11669 on 2026-10-15, through `cmd/swap`.
Their footprints and the swap timings are in [`memory-budget.md`](memory-budget.md).

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
