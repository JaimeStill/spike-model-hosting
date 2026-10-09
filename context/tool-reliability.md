# Tool-call reliability

A model served locally drifts from the shape a consumer needs: a tool call the parser rejects,
or a text answer where a tool call was due. The spike follows the field's layered convention,
not per-model patches: each layer owns one failure, and reliability is measured, not assumed.

## The convention

| Layer | Rule | Source |
|---|---|---|
| Server | A shape the consumer needs every time is constrained, not prompted: `tool_choice` `"required"` or a named tool, and `response_format` `json_schema` (strict). The server compiles it to a grammar, so the output parses. `auto` stays unconstrained and can answer in text. | vLLM: [tool calling](https://docs.vllm.ai/en/latest/features/tool_calling/) (`required` since 0.8.3 and named calls use structured outputs: "guaranteed a validly-parsable function call - not a high-quality one"; `auto` may be malformed). llama.cpp: [tools/server/README.md](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) (`tool_choice`, `response_format`, `json_schema`) |
| Profile | Each model runs its official embedded chat template at its card's sampling and reasoning settings. A template override is for a template that is broken, not one a model sometimes strays from. | llama.cpp [docs/function-calling.md](https://github.com/ggml-org/llama.cpp/blob/master/docs/function-calling.md) (native formats, Harmony among them; `--chat-template-file` "when appropriate"); [openai/gpt-oss README](https://github.com/openai/gpt-oss) ("Recommended Sampling Parameters": `temperature=1.0`, `top_p=1.0`); [google/gemma-4-26B-A4B-it](https://huggingface.co/google/gemma-4-26B-A4B-it) and [google/gemma-4-E4B-it](https://huggingface.co/google/gemma-4-E4B-it) ("1. Sampling Parameters": 1.0, 0.95, 64) |
| Client | On a turn that called tools, the client sends the model's reasoning back with the call (`reasoning_content`, or a `reasoning` item). gpt-oss reasons through tool calls, and the earlier reasoning stays in its prompt until a `final`. | [OpenAI Harmony guide](https://developers.openai.com/cookbook/articles/openai-harmony) ("pass the previous chain-of-thought back in" after tool calls); [verifying gpt-oss implementations](https://developers.openai.com/cookbook/articles/gpt-oss/verifying-implementations) (a `reasoning` field in Chat Completions); llama.cpp [#27720](https://github.com/ggml-org/llama.cpp/issues/27720) (malformed channel headers fell from 7/120 to 0/120 once the client replayed `reasoning_content`) |
| Harness | A call that fails its schema goes back to the model as a tool error, which it retries. A parse failure at the server never reaches the harness as a call, so the harness can't repair it. | Pi validates `respond` against the exchange's schema (spike-harness-driver `context/findings.md`, "Payloads") |
| Gateway | A parse 5xx ("does not match the expected peg-native format") is a sampling failure: a bounded resample (1–2 retries) at the same settings, then fallback to another model group. | [LiteLLM reliability](https://docs.litellm.ai/docs/proxy/reliability) (`num_retries`, ordered `fallbacks`, `allowed_fails`, `cooldown_time`); [agentgateway failover](https://agentgateway.dev/docs/kubernetes/latest/llm/failover/) (priority groups, with a health policy that evicts on 5xx) |
| Measurement | Reliability is pass^k: every one of k trials passes, per model, engine, and consumer. OpenAI's bar for a gpt-oss implementation is 0 invalid requests and over 90% on pass@k and pass^k. | τ-bench, [arXiv 2406.12045](https://arxiv.org/abs/2406.12045) (pass^k); [verifying gpt-oss implementations](https://developers.openai.com/cookbook/articles/gpt-oss/verifying-implementations) |

## What the spike observed

On llama.cpp b11529 (`b11529-8ae386707`, upstream Vulkan x64) with gpt-oss-120b (ggml-org
`MXFP4`), Pi 0.99.2, and OpenCode 1.18.34:

- **The failure.** After a tool result, gpt-oss often skips a fresh analysis and opens
  `<|channel|>final <|constrain|>json<|message|>`. Harmony's guide names `<|constrain|>` for a
  tool call's arguments. b11529's gpt-oss PEG parser accepts a constraint only on a tool call
  or on a `response_format` final, so the request fails with HTTP 500, "The model produced
  output that does not match the expected peg-native format". It broke clutch's Pi `tool`,
  `audio-tool`, and `skill` conformance cells. llama.cpp [#25321](https://github.com/ggml-org/llama.cpp/issues/25321)
  is the same parser rejecting an unmarked gpt-oss final (closed as not planned), and
  [PR #25332](https://github.com/ggml-org/llama.cpp/pull/25332), a lenient `bare_final` arm,
  is open and unmerged. Neither covers `final` with `<|constrain|>`.
- **The patched template, rejected.** A copy of ggml-org's embedded template rendered earlier
  tool calls as `<|start|>assistant<|channel|>commentary to=functions.NAME <|constrain|>json<|message|>`,
  the form the model emits, instead of the recipient in the role without `<|constrain|>`. It
  cut P(`final` first after a tool result) from 0.84 to under 0.01 on one prompt, but Pi's
  `skill` still failed 1 in 3 and OpenCode's `skill` and `audio-tool` went from passing to
  failing 3 in 3. Holding the gain took `reasoning-effort = high` as well. Harmony allows the
  recipient in the role or the channel, so the embedded template isn't wrong; the patch was a
  per-model workaround to carry across every build and conversion, for a shape the server can
  constrain instead. Both were reverted in personal-agents.
- **Forcing the shape.** With `tool_choice: "required"` on the requests that offer `respond`,
  the grammar leaves gpt-oss no `final`: 20 of 20 samples of the failing request returned a
  valid call. clutch's Pi bridge now sets it on every request that offers `respond`. Over 5
  `clutch conform` runs, Pi passed every cell 5 of 5, and OpenCode, still on `auto`, passed
  `tool` 4/5, `skill` 3/5, and `audio-tool` 2/5, each failure a text answer instead of
  `respond` (spike-harness-driver `context/findings.md`, "Capabilities").
- **Sampling.** b11529 applies a GGUF's `general.sampling.*` keys as the server default
  (`/props` `default_generation_settings`). The Gemma 4 GGUFs carry their card's values;
  gpt-oss-120b's carries none, so it ran at llama.cpp's 0.8 / 40 / 0.95 / 0.05 until the preset
  set the card's. personal-agents' `profiles/unified-96gb.ini` states each card's values.

## Which task answers which layer

- `profile`: the schema carries per-model sampling and reasoning settings, rendered into each
  platform's presets.
- `consumers`: pass^k per model, engine, and consumer against 0 invalid and 90%, and each
  consumer's reasoning round-trip on tool turns (Pi sends `reasoning_content`; OpenCode
  doesn't).
- `gateway`: the bounded resample and fallback on a parse 5xx, per gateway.
- `validate`: the convention per platform: whether each engine constrains `required`, named
  calls, and strict `json_schema`, and parses its models' native tool format.

## Assumptions

- Assumes a constrained `tool_choice` costs no answer quality the consumers notice; vLLM's docs
  promise a parsable call, not a good one.
- Assumes the parse 5xx stays rare enough under constraint and round-tripped reasoning that one
  or two resamples clear it.
