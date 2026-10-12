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
  It reports and changes nothing; the line is a new reading for the footprint and baseline
  runs.
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

- The integration probe runs against the router once the architect installs b11572; this
  section records it then.
