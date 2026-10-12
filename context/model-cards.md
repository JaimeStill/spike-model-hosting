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
  calls. Both are the client's job (see `context/tool-reliability.md`, the Client layer).
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

The spike's round 6 is the case in point (`context/tool-reliability.md`; the goal record's
Decisions, round 6). gpt-oss-120b sometimes opened a JSON `final` after a tool result, which
llama.cpp's parser rejected. A patched chat template cut that failure on one prompt but broke
other consumers' cells, and held only with `reasoning-effort = high`. Both were rejected and
reverted: the embedded template wasn't wrong, and the patch and the effort were per-model
workarounds to carry across every build and conversion. The shape the client needs is
constrained per request (`tool_choice`, `response_format`) instead, and the model stays at its
card.

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

`profiles/strix-halo.toml` carries each value with its provenance, and the renderer writes each
reason into the presets as a comment above the model's section:

- **Typed sampling, traced to the card.** Each generative model sets `temperature`, `top_p`,
  `top_k`, and `min_p`, all required, with `source` naming the page they come from (the card,
  or the README gpt-oss's card defers to) and `reason` saying how. A truncation the card
  doesn't name is turned off (`top_k = 0`, `min_p = 0`) rather than left to the engine. The
  optional `reasoning` (low, medium, high) renders as the template's `reasoning_effort`; set A
  sets none. The embedding model's sampling is refused by validation.
- **Set explicitly, though the engine reads the GGUF.** llama.cpp b11529 and later (the
  Framework runs b11565) apply a GGUF's `general.sampling.*` keys as the server's defaults. The
  Gemma 4 GGUFs carry their cards' three values; gpt-oss-120b's carries none, so it ran at the
  engine's defaults until the preset set the card's. The profile states every value so the
  card, not the conversion, is the source (the goal record's Decisions, align).
- **Raw args for engine-only keys.** Keys only llama.cpp has (`embeddings`, `pooling`,
  `no-mmproj`, `b`, `ub`) go in a per-model `args` map with an `args_reason`. The keys the
  profile types (`temp`, `top-p`, `top-k`, `min-p`, `c`, `chat-template-kwargs`, and others)
  can't be set there.
- **`c` with its reason.** Each model's `c` carries a `c_reason`: the trained length, or the
  memory budget when that binds first, with the KV cost per token behind it.
- **The template is the embedded one.** The profile has no template field; each model runs
  the chat template its GGUF embeds.

## Assumptions

- Assumes the GGUF's embedded template matches the publisher's; the spike checked gpt-oss's
  Harmony rendering in round 6, not the Gemma 4 templates token by token.
- Assumes the quantizations set A runs (Q4_0 and Q8_0 for the Gemma models) cost less than
  their cards' measured margins; no card measures them.
