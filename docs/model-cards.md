# Reading a model card

A model card is the publisher's statement of how the model was trained and evaluated: its
prompt format, its sampling, its reasoning controls, its context. A self-hosted agent runs the
model at the card's values, because those are the settings its quality and its tool calls were
measured at. Every value the card doesn't state is a default from somewhere else (the engine,
the conversion, the client), and is chosen on purpose, not inherited.

## What each value means

| Value | What it controls | Where a card states it |
|---|---|---|
| Temperature | Scales the logits before sampling: below 1 sharpens the distribution toward the likeliest token, above 1 flattens it. 0 is greedy. | A "recommended sampling" section; Hugging Face's `generation_config.json` often repeats it. |
| Top-p | Nucleus truncation: keeps the smallest set of tokens whose probabilities sum to p. 1.0 keeps all. | Same section. |
| Top-k | Keeps the k likeliest tokens. 0 turns it off in llama.cpp. | Same section; often absent. |
| Min-p | Drops tokens below min-p times the top token's probability. 0 turns it off. | Rarely stated; engines default it on (llama.cpp: 0.05). |
| Reasoning effort | How long a reasoning model thinks before answering, as a level the chat template writes into the prompt (gpt-oss: `Reasoning: low/medium/high` in the system message), or as thinking on or off (Gemma 4: a `<|think|>` token at the start of the system prompt). | A "reasoning levels" or "thinking mode" section; the template sets the default. |
| Chat template | The Jinja template that renders messages, tool definitions, and tool results into the token sequence the model was trained on, and the format its output is parsed from. | Shipped with the weights (`chat_template.jinja`, or embedded in a GGUF as `tokenizer.chat_template`). The card names the format (gpt-oss: Harmony). |
| Context length | The longest sequence the model was trained to attend over: a ceiling, not a target. | The card's architecture table, or `max_position_embeddings` in `config.json`; a GGUF's `<arch>.context_length`. |

Related items a reader meets on the same card:

- **Recommended system prompt.** Some cards prescribe one or a required preamble; others only
  show an example. An example is not a requirement. A control written into the system prompt,
  such as gpt-oss's reasoning level or Gemma 4's `<|think|>`, belongs to the template, not to
  the agent's prompt text.
- **Stop and special tokens.** The tokens that end a turn or mark a tool call. gpt-oss's
  `generation_config.json` lists `<|return|>`, `<|call|>`, and `<|endoftext|>` as end tokens;
  Harmony's `<|channel|>` and `<|constrain|>` mark its output's parts. An engine that parses
  the native format handles these; a hand-rolled prompt has to.
- **Multi-turn rules.** Gemma 4's card drops earlier thinking from the history, except on tool
  call turns, where it is kept; gpt-oss's Harmony guide passes the reasoning back after tool
  calls. Both are the client's job ([`tool-reliability.md`](findings/tool-reliability.md), the
  Client layer).
- **Quantization.** A GGUF repository's suffix (`MXFP4`, `Q4_0`, `Q8_0`) is the weights'
  precision. gpt-oss was post-trained in MXFP4 and its card's evals ran in it, so MXFP4 is the
  card's own setting. Another quantization is a deviation whose cost the card doesn't measure;
  EmbeddingGemma 2's card reports full-precision results and warns against float16.
- **The GGUF's embedded metadata.** A conversion can carry the card's sampling as
  `general.sampling.*` keys (`temp`, `top_k`, `top_p`, `min_p`, and others), its template, and
  its context length. They are the converter's copy of the card, present or absent per
  conversion, so the card stays the source.
- **Modality inputs.** A vision or audio model in llama.cpp needs its projector, an `mmproj`
  GGUF, loaded beside the weights; without it the model is text-only. Gemma 4's card asks for
  images before the text and audio after it.
- **Embedding prompt forms.** An embedding model may be trained with task prefixes.
  EmbeddingGemma 2's card gives `task: search result | query: {query}` for a query and
  `title: {title} | text: {content}` (`title: none` without a title) for a document; omitting
  them "still works but reduces precision". The client adds them; the server can't.
  An embedding model doesn't sample, so it has no sampling values.

## Why a model runs at its card's values

The card's values are the ones the publisher trained and evaluated at; its benchmark numbers,
and its tool-call behavior, hold only there. An agent built against a model is built against
that configuration, and a deviation is a new configuration nobody has measured.

The `align` task's sixth plan round is the case in point
([`tool-reliability.md`](findings/tool-reliability.md); the goal record's Decisions, "align,
round 6"). gpt-oss-120b sometimes opened a JSON `final` after a tool
result, which llama.cpp's parser rejected. A patched chat template cut that failure on one
prompt but broke other consumers' cells, and held only with `reasoning-effort = high`. Both
were rejected and reverted: the embedded template wasn't wrong, and the patch and the effort
were per-model workarounds to carry across every build and conversion. The shape the client
needs is constrained per request (`tool_choice`, `response_format`) instead, and the model stays
at its card.

The engine's defaults are also a deviation. llama.cpp's own sampling is temperature 0.8, top-k
40, top-p 0.95, min-p 0.05 (on b11529); a model whose card says otherwise and whose GGUF
carries no `general.sampling.*` keys runs at those unless the preset says otherwise.

## Set A's values

| Model | Card | Sampling (temp / top-p / top-k / min-p) | Reasoning | Context in the profile |
|---|---|---|---|---|
| gpt-oss-120b, `ggml-org/gpt-oss-120b-GGUF:MXFP4` | [openai/gpt-oss-120b](https://huggingface.co/openai/gpt-oss-120b); sampling from the [openai/gpt-oss README](https://github.com/openai/gpt-oss), "Recommended Sampling Parameters" | 1.0 / 1.0 / 0 / 0. The README names temperature and top_p only. | Unset: the template's default, `medium`. | 131072, its trained length (`config.json` `max_position_embeddings`; the card doesn't state one). |
| Gemma 4 26B-A4B, `ggml-org/gemma-4-26B-A4B-it-GGUF:Q4_0` | [google/gemma-4-26B-A4B-it](https://huggingface.co/google/gemma-4-26B-A4B-it), "1. Sampling Parameters" | 1.0 / 0.95 / 64 / 0. The card names no min_p. | Unset: the template's default. | 32768 of the card's 256K: the shared memory budget binds. |
| Gemma 4 E4B, `ggml-org/gemma-4-E4B-it-GGUF:Q8_0` | [google/gemma-4-E4B-it](https://huggingface.co/google/gemma-4-E4B-it), "1. Sampling Parameters" | 1.0 / 0.95 / 64 / 0. The card names no min_p. | Unset: the template's default. | 32768, a quarter of the card's 128K. |
| EmbeddingGemma 2, `ggml-org/embeddinggemma-2-GGUF:Q8_0` | [google/embeddinggemma-2](https://huggingface.co/google/embeddinggemma-2), via the [GGUF repository](https://huggingface.co/ggml-org/embeddinggemma-2-GGUF), whose card names only its source | None: it doesn't sample. Mean pooling, 768 dimensions. | None. | 8192, the card's context window (the GGUF's `context_length` key reads 262144). |

personal-agents' `reference/model-tiers.md` ("~90-96GB (unified memory)") states the same
values and the rule behind them, and `reference/context-sizing.md` gives the method behind each
`c`: the trained length and the memory budget are two ceilings, and the smaller binds.

## How the profile applies this

`devices/strix-halo/profile.toml` sets the values above, each with its source and reason, and the
`profile` package renders each reason into the presets as a comment above the model's section.
The package's doc comments define the fields and what validation refuses. Three rules from this
page shape the profile:

- **Every sampling value is stated, though the engine reads the GGUF.** llama.cpp b11529 and
  later apply a GGUF's `general.sampling.*` keys as the server's defaults. The Gemma 4 GGUFs
  carry their cards' three values; gpt-oss-120b's carries none, so it ran at the engine's
  defaults until the preset set the card's. The profile states every value so the card, not the
  conversion, is the source, and turns off a truncation the card doesn't name (`top_k = 0`,
  `min_p = 0`) rather than leave it to the engine (the goal record's Decisions, align).
- **Reasoning is left to the template.** A model's optional `reasoning` renders as the
  template's `reasoning_effort`; set A sets none.
- **The template is the embedded one.** The profile has no template field; each model runs the
  chat template its GGUF embeds.

## How the evaluation reads the card

The evaluation ([`evaluation.md`](evaluation.md)) reads the card in five places:

- **Admission** checks the profile's context against the card's (`card_context`), and fails a
  model whose `c` passes it.
- **The card's prompts** are the requests' own: the embedding encoder adds EmbeddingGemma 2's
  task prompts (its "Task Instruction Prefixes", by MTEB task type), and every transcription
  sends gemma-4-E4B's ASR prompt first and the clip after it, the card's order for audio. The
  profile holds both, each model's `prompts` with its `source`, for the encoder, fitness,
  capacity, and the ASR task alike.
- **The card's tokenizer** cuts the GuideLLM grid's prompts to their token counts: the deep
  stage takes it from the model's `card_url`.
- **The card's own scores** are recorded beside ours where the card reports one for a tier, and
  never judged: gemma-4-E4B's FLEURS 0.08 (a multilingual figure, against our English-only set)
  and Gemma 4 26B-A4B's MMMU Pro 73.8% (a harder variant than our MMMU validation).
- **The card's sampling** is what the agent tiers and harness fit run at: the router's
  defaults, from the presets. The vision benchmarks and the transcriptions run at temperature 0
  with reasoning off instead, so a score or a speed measures the answer, not a sample of it or
  the thinking before it: at the card's temperature 1, gemma-4-E4B sometimes reasoned 145-169
  tokens before transcribing, which cut fitness's RTFx to 1.2-1.5. The vision probe stays at the
  card's values, since its pass^5 measures the served model as a client sees it.

## Context catalog

Set A's four models, those of `devices/strix-halo/profile.toml`, and the two main-model
candidates the spike loaded, Mistral Small 4 and Nemotron 3.5 Super, with what a larger context
would cost. All figures are computed from each model's `config.json`, not measured;
[`memory-budget.md`](findings/memory-budget.md) holds the measured footprints.

**Method.** A full-attention layer caches K and V for every token: layers × KV heads × head
dim × 2 (K and V) × 2 bytes (f16, the cache type the presets leave in place). A sliding-window
(SWA) layer caches only its window, so gpt-oss's and the Gemmas' SWA layers are counted once at
their window, per sequence, and only their full-attention layers grow with the context.
Mistral Small 4 uses multi-head latent attention (MLA): each layer caches one latent of
`kv_lora_rank` plus the rope dimension, shared by K and V, as an MLA-aware engine stores it.
Nemotron 3.5 Super's Mamba-2 layers keep a fixed recurrent state per sequence, at f32
(`mamba_ssm_cache_dtype`): SSM heads × head dim × state size, plus the convolution's
(kernel − 1) × (inner size + 2 × groups × state size). Both KV columns count the cache the
router would allocate at that context: the per-token cost times the context, which the 4 slots
share on `--kv-unified`, plus the fixed part for each of the 4 sequences. The two columns differ
only in the context, so where a model already runs at the maximum they are equal, and their
difference is what raising c to the maximum would cost. Compute buffers and the router's context
checkpoints are not counted.

| Model | Maximum context | Cached layers | KV bytes/token, f16 | Fixed per sequence | KV at the spike's c | KV at the maximum context | The spike's c, and why |
|---|---|---|---|---|---|---|---|
| gpt-oss-120b ([config](https://huggingface.co/openai/gpt-oss-120b/blob/main/config.json), [card](https://huggingface.co/openai/gpt-oss-120b)) | 131072, `max_position_embeddings`; the card states none | 18 full of 36 (8 KV heads × 64); 18 SWA at 128 tokens | 36,864 | 4.5 MiB (SWA) | 4.52 GiB | 4.52 GiB at 131072 | 131072, its trained length: the harness model runs at its ceiling. |
| Gemma 4 26B-A4B ([config](https://huggingface.co/google/gemma-4-26B-A4B-it/blob/main/config.json), [card](https://huggingface.co/google/gemma-4-26B-A4B-it)) | 262144, the card's 256K and `max_position_embeddings` | 5 full of 30 (2 KV heads × 512); 25 SWA (8 × 256) at 1024 tokens | 20,480 | 200 MiB (SWA) | 1.41 GiB | 5.78 GiB at 262144 | 32768: the shared budget binds; at 65536 set A left 2.61 GiB free, under the 2.75 GiB pool floor. |
| Gemma 4 E4B ([config](https://huggingface.co/google/gemma-4-E4B-it/blob/main/config.json), [card](https://huggingface.co/google/gemma-4-E4B-it)) | 131072, the card's 128K and `max_position_embeddings` | 4 full of the 24 that own a cache (2 × 512; the last 18 of 42 reuse earlier layers' KV); 20 SWA (2 × 256) at 512 tokens | 16,384 | 20 MiB (SWA) | 0.58 GiB | 2.08 GiB at 131072 | 32768, a quarter of its trained length: the audio and small model, sized with set A's budget. |
| EmbeddingGemma 2 ([config](https://huggingface.co/google/embeddinggemma-2/blob/main/config.json), [card](https://huggingface.co/google/embeddinggemma-2)) | 8192, the card's context window (`max_position_embeddings` reads 262144) | 4 full of 24 (1 × 512); 20 SWA (2 × 256) at 512 tokens (the card's table says 1024) | 8,192 | 20 MiB (SWA) | 0.14 GiB | 0.14 GiB at 8192 | 8192, the card's window: an input has to fit one pass, and nothing is kept between requests. |
| Mistral Small 4 ([config](https://huggingface.co/mistralai/Mistral-Small-4-119B-2603/blob/main/config.json), [card](https://huggingface.co/mistralai/Mistral-Small-4-119B-2603)) | 262144, the card's 256k (`max_position_embeddings` reads 1048576) | 36 MLA layers, latent 256 + rope 64 | 23,040 | none | 2.81 GiB | 5.63 GiB at 262144 (22.5 GiB at 1048576) | 131072, gpt-oss-120b's, so candidates compare at set A's main-model context; 262144 would leave the pool under main-model's 3 GiB floor. |
| Nemotron 3.5 Super VL ([config](https://huggingface.co/nvidia/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-BF16/blob/main/config.json), [card](https://huggingface.co/nvidia/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-BF16)) | 1048576, the card's "up to 1M" and `max_position_embeddings` | 8 attention of 88 (2 KV heads × 128); 40 Mamba-2 (128 heads × 64, state 128, 8 groups, kernel 4); 40 MoE with no cache | 8,192 | 164.7 MiB (recurrent, f32) | 1.64 GiB | 8.64 GiB at 1048576 | 131072, gpt-oss-120b's, for the comparison; rejected 262144 and 1M. Its 8 checkpoints per slot hold ~5.1 GiB of recurrent state in system RAM, outside the pool. |

Four models allow a larger context than the spike runs them at. Nemotron 3.5 Super's cache grows
slowest, 8 KiB per token: its 1M costs 8 GiB of attention KV. Mistral Small 4 doubles to its
262144 for 2.8 GiB more. Gemma 4 26B-A4B's full-attention KV goes from 0.625 GiB at 32768 to
5 GiB at its 262144, which set A's budget can't hold. Gemma 4 E4B could reach its 131072 for
1.5 GiB more. gpt-oss-120b already runs at its maximum, and EmbeddingGemma 2 at its card's
window.

Gemma 4 26B-A4B's full-attention layers set `attention_k_eq_v`, using K as V; an engine that
stores one tensor for both halves that part, which the table doesn't assume.

Mistral Small 4's KV at `c = 131072` is 2.8-5.1 GiB, depending on how the engine lays out the
MLA cache. The low end, the table's 2.81 GiB, caches only the latent (`kv_lora_rank` + rope,
320 values per layer and token). llama.cpp b11669 does this: it loads `mistral4` through its
DeepSeek-2 code, which allocates a K-only cache when the GGUF carries the MLA key lengths. The
high end also caches the latent's 256 values a second time, as V (5.06 GiB, rounded up to 5.1).
The mistral set's estimated budget ([memory budget](findings/memory-budget.md)) takes the high
end, so the estimate holds for either layout.

## Assumptions

- Assumes the GGUF's embedded template matches the publisher's; the spike checked gpt-oss's
  Harmony rendering in `align`'s sixth plan round, not the Gemma 4 templates token by token.
- Assumes the quantizations set A runs (Q4_0 and Q8_0 for the Gemma models) cost less than
  their cards' measured margins; no card measures them.
