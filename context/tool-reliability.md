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
- **Conformance rates by build.** Five `clutch conform --provider llama.cpp` runs per set,
  with all four set-A models loaded, Pi 0.99.2 and OpenCode 1.18.34 (the suite's pins), and
  clutch at `37fa153` (the b11565 set ran a build from `5afeac3`, whose code is `37fa153`'s;
  only spike-harness-driver's findings changed). The b11529 set ran from the laptop against
  personal-agents' presets in `align` (spike-harness-driver `context/findings.md`,
  "Capabilities"); the b11565 set ran on the Framework itself, against the profile's rendered
  presets, on 2026-10-12:

  | Build, where it ran | Harness | exchange | cancel | resume | tool | skill | structured | vision | dropped-image | audio-tool |
  |---|---|---|---|---|---|---|---|---|---|---|
  | b11529, laptop | pi | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 |
  | b11529, laptop | opencode | 5/5 | 5/5 | 5/5 | 4/5 | 3/5 | 5/5 | 5/5 | 5/5 | 2/5 |
  | b11565, Framework | pi | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 |
  | b11565, Framework | opencode | 5/5 | 5/5 | 5/5 | 3/5 | 3/5 | 5/5 | 5/5 | 5/5 | 4/5 |

  Pi, constrained to `required`, held every cell. One cell fell: OpenCode's `tool`, from 4/5
  to 3/5; its `skill` held at 3/5 and its `audio-tool` rose from 2/5 to 4/5. At five runs a
  cell moves by one or two on chance alone, so the sets show OpenCode on `auto` failing about a
  third of its `tool`, `skill`, and `audio-tool` exchanges on both builds (6 of 15 on b11529,
  5 of 15 on b11565), not a regression. The failures differ in kind: every b11529 failure was a
  text answer instead of `respond`, while on b11565 the two `tool` failures and the `audio-tool`
  failure were text answers and both `skill` failures were the HTTP 500 "does not match the
  expected peg-native format". b11565 leaves the gpt-oss parser
  rules unchanged ([`llama-cpp-builds.md`](llama-cpp-builds.md)), so the parse 5xx that
  `required` keeps from Pi still reaches a client on `auto`.
- **Sampling.** b11529 applies a GGUF's `general.sampling.*` keys as the server default
  (`/props` `default_generation_settings`). The Gemma 4 GGUFs carry their card's values;
  gpt-oss-120b's carries none, so it ran at llama.cpp's 0.8 / 40 / 0.95 / 0.05 until the preset
  set the card's. personal-agents' `profiles/unified-96gb.ini` and the spike's
  `profiles/strix-halo.toml` state each card's values.
- **Levers for OpenCode.** Three levers could cut OpenCode's failures. `consumers` answered the
  second (see "Reasoning round-trip by consumer"); `gateway` takes up the other two:
  - *Force the shape.* OpenCode sends `tool_choice: "auto"`. Its `chat.params` plugin hook
    edits a request's parameters before it is sent, and may set `required` the way Pi's bridge
    does; whether it reaches `tool_choice` is unconfirmed. A gateway can inject `required` for
    every client instead, which matches the convention's server layer.
  - *Send reasoning back.* gpt-oss expects its reasoning from earlier tool-call turns in later
    requests, as `reasoning_content` on llama.cpp, and tool calling degrades without it
    ([alde.dev](https://alde.dev/blog/proper-tool-calling-with-gpt-oss/); llama.cpp
    [discussion #15396](https://github.com/ggml-org/llama.cpp/discussions/15396)). OpenCode
    1.19.0 already sends it, with or without its per-model `interleaved` setting.
  - *Retry the parse 5xx.* Related 500s with OpenCode and gpt-oss on reasoning plus tool calls
    are reported in llama.cpp [#19703](https://github.com/ggml-org/llama.cpp/issues/19703) and
    [#20500](https://github.com/ggml-org/llama.cpp/issues/20500). `gateway` evaluates a bounded
    resample and a fallback model.

## Reasoning round-trip by consumer

Pi, Claude Code, and OpenCode each send gpt-oss's reasoning back on the turn after a tool call,
as seen on the wire. Each consumer ran one turn that had to call its read tool on a file in a
scratch directory and then answer with the code the file held. The runs used llama.cpp b11572
(`b11572-efe3626b1`) and gpt-oss-120b (ggml-org `MXFP4`), from the laptop against the
Framework's router, on 2026-10-12. Each consumer's requests passed through a `wire.Recorder`,
which drops credentials before forwarding and records them only as `[redacted]`. The `wire`
package's `RoundTrip` integration tests run these turns and save the recordings under
`.state/wire/`.

| Consumer | Sends reasoning back after a tool call | As |
|---|---|---|
| Pi 1.1.0 (`llama.cpp` provider, OpenAI chat) | yes | `reasoning_content` on the assistant message beside `tool_calls` |
| Claude Code 2.1.296 (Anthropic messages) | yes | `thinking` blocks, before `tool_use`, in the assistant message |
| OpenCode 1.19.0 (`@ai-sdk/openai-compatible`, OpenAI chat), `interleaved` set | yes | `reasoning_content` on the assistant message beside `tool_calls` |
| OpenCode 1.19.0, `interleaved` unset | yes | the same |

- **Claude Code on the router.** Claude Code ran from its own configuration directory,
  `.state/claude`, on the architect's subscription login, with no API key or auth token. It
  had `ANTHROPIC_BASE_URL` at the router and `ANTHROPIC_MODEL` plus the opus, sonnet and haiku
  defaults all mapped to gpt-oss-120b. Every model request reached the router: three streamed
  `POST /v1/messages?beta=true` and a `HEAD /api/hello` (404). It sends the login's bearer
  token to the base URL, and the proxy kept the token from reaching the router or the recording.
- **The thinking request.** Claude Code asks for adaptive thinking at high effort, even on a
  model it doesn't know. The first request's parameters, without `system`, `messages`, `tools`
  and `metadata`:

  ```json
  {"context_management":{"edits":[{"type":"clear_thinking_20251015","keep":"all"}]},
   "max_tokens":32000,"model":"ggml-org/gpt-oss-120b-GGUF:MXFP4",
   "output_config":{"effort":"high"},"stream":true,
   "thinking":{"type":"adaptive","display":"updates"}}
  ```

  Its `anthropic-beta` header names `interleaved-thinking-2025-05-14`, `effort-2025-11-24`,
  `thinking-display-updates-2026-08-18` and `context-management-2025-06-27`, among others.
  b11572 answers without an error.
- **Thinking streams back, unsigned.** Each response streamed a `thinking` content block
  (`content_block_start`, then `thinking_delta`s) that carried gpt-oss's reasoning, followed by a
  `signature_delta` with an empty signature. After each tool call, the next request carried the
  earlier turns' assistant messages as `[thinking, tool_use]`, with the whole reasoning text and
  no signature. The turn called
  Read twice: once with a relative path, which failed, and again with the absolute path. Both
  thinking blocks went back.
- **Pi.** On the turn after its `read` call, Pi's assistant message carried `content`,
  `reasoning_content` and `tool_calls`, and the reasoning was gpt-oss's own (111 characters on
  that turn).
- **OpenCode.** OpenCode ran with gpt-oss-120b's `interleaved` set to
  `{"field": "reasoning_content"}`, in an explicit configuration file (`OPENCODE_CONFIG`) that
  OpenCode merges beneath the driver's own. On each turn after a `read` call, the assistant
  message carried `content`, `reasoning_content` and `tool_calls`, and the reasoning was the text
  the previous response had streamed as `reasoning_content` deltas, byte for byte. The same turn
  with the setting left out sent the same field, matching its stream too. So OpenCode 1.19.0
  sends reasoning back on llama.cpp's OpenAI chat with or without `interleaved`; the setting
  changes nothing on the wire here. OpenCode also sent it back on the conform suite's vision
  turns on Gemma 4 26B-A4B, which the setting doesn't name.

## pass^5 by consumer

Only Pi meets OpenAI's bar. `go run ./cmd/conform -k 5` runs clutch's conform suite (clutch at
`9981a3e`, the `conform` package) five times per consumer, the consumers in turn within each
trial. The runs used llama.cpp b11572 (`b11572-efe3626b1`, Vulkan) and gpt-oss-120b (ggml-org
`MXFP4`), with all four set-A models loaded, from the laptop against the Framework's router, on
2026-10-12. Each consumer's requests pass through a `wire.Recorder` of its own. A model request
counts as invalid when the router answers it with an error, as a status or as an error event in
a stream that began with 200. The `passk` package builds the cells:

- **Pi 1.1.0** on its `llama.cpp` provider, its agent directory under `.state/conform/pi`.
  The driver's bridge sets `tool_choice: "required"` on every request that offers `respond`.
- **Claude Code 2.1.296** over the router's Anthropic messages, from `.state/claude` on the
  subscription login, every model alias mapped to gpt-oss-120b, as in the round-trip run.
- **OpenCode 1.19.0** with gpt-oss-120b's `interleaved` set to `reasoning_content`. Its
  `tool_choice` stays `auto`: forcing it is `gateway`'s.

A capability is a task, and it passes at 5 only when all five trials pass. pass^5 is the
share of the nine tasks that do; the bar is 0 invalid and pass^5 of at least 90%, which over
nine tasks means 9 of 9 (8 of 9 is 88.9%). n/a counts as not passing. Two sets ran back to
back. Set 2 is the tool's own report. Set 1 ran before the tool counted streamed errors, so its
invalid counts were recounted from its saved recordings with the same rule:

| Set | Consumer | exchange | cancel | resume | tool | skill | structured | vision | dropped-image | audio-tool | pass^5 | invalid | Bar |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | Pi 1.1.0 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 9/9 (100%) | 0 | meets |
| 1 | Claude Code 2.1.296 | 5/5 | 5/5 | 5/5 | 0/5 | 0/5 | 5/5 | 5/5 | n/a | 3/5 | 5/9 (55.6%) | 0 | fails |
| 1 | OpenCode 1.19.0 | 5/5 | 5/5 | 5/5 | 4/5 | 3/5 | 5/5 | 5/5 | 5/5 | 2/5 | 6/9 (66.7%) | 17 | fails |
| 2 | Pi 1.1.0 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 9/9 (100%) | 0 | meets |
| 2 | Claude Code 2.1.296 | 5/5 | 5/5 | 5/5 | 1/5 | 0/5 | 5/5 | 5/5 | n/a | 3/5 | 5/9 (55.6%) | 8 | fails |
| 2 | OpenCode 1.19.0 | 5/5 | 5/5 | 5/5 | 5/5 | 4/5 | 5/5 | 5/5 | 5/5 | 2/5 | 7/9 (77.8%) | 14 | fails |

Each set took about 16 minutes (15m33s and 17m17s).

- **Pi meets the bar.** Constrained to `required`, it passed all 90 of its capability runs
  across the two sets with no invalid request, as on b11529 and b11565.
- **Claude Code fails it.** Its `tool`, `skill` and `audio-tool` failures are the ones
  OpenCode on `auto` shows: Claude Code sends no `tool_choice`, and gpt-oss answered in text
  instead of calling the `respond` tool ("the exchange ended without a structured response"),
  or, in `skill`, called `respond` with a motto it made up, without loading the skill
  ("Keep the gear moving."). `dropped-image` is n/a on all five trials: Claude Code drops no
  image, since every model it knows takes one, so it sends gpt-oss the image as it is. The
  cell isn't runnable over the router without changing clutch's profile, so it counts against
  the 9 of 9. The 8 invalid requests in set 2 were one `audio-tool` request in trial 4: its
  stream ended in the parse error, and Claude Code resent the same request seven times over 40
  seconds, each answered 500 at once.
- **OpenCode fails it.** With reasoning round-tripped, `tool`, `skill` and `audio-tool` still
  failed in about the proportion they did without it (11 of 30 runs failing on b11529 and b11565,
  10 of 30 here). Every invalid request was the parse error in a stream that began with 200,
  each after a tool result. OpenCode resends the same request with growing waits (about 4, 6,
  10, 17 and 31 seconds), and the exchange passes when a resend gets through; two `audio-tool`
  failures in set 2 were the resends running out. The parse 5xx reaches a client on `auto`
  even with its reasoning sent back.
- **Comparing with align's and profile's rates.** Those sets ran Pi 0.99.2 and OpenCode
  1.18.34 under clutch at `37fa153`; these run Pi 1.1.0 and OpenCode 1.19.0, so a comparison
  of Pi's rates crosses Pi's major version, 0.99 to 1.x, as well as a build (b11529 and b11565
  to b11572). Pi held every cell on all three.

### The engine row

The engine row fails OpenAI's bar on a field name, and its tool calls sit at the bar's line.
The row is OpenAI's compatibility test for gpt-oss implementations, the one
[verifying gpt-oss implementations](https://developers.openai.com/cookbook/articles/gpt-oss/verifying-implementations)
sets its bar with: openai/gpt-oss at `7b58334` (`7b583341fe16729127f6d5b94a7b09ccae97e1a1`),
`compatibility-test/`, 30 cases over six tools, each run five times through the OpenAI Agents
SDK. It ran from the laptop against the Framework's router on 2026-10-12, on the same build
(`b11572-efe3626b1`) and model (`ggml-org/gpt-oss-120b-GGUF:MXFP4`), in Chat Completions mode
with `-k 5`, on Node 26.11.1 (tsx 4.23.15, which the suite's `npm start` needs but doesn't
pin). `mise run gpt-oss-compat` in the consumers environment, with `SPIKE_ROUTER_URL` set,
fetches the suite into `.state/`, installs it from its lockfile, runs it, and prints the table
below. It measures the engine and model at the Chat Completions layer, with no consumer between, not the
`/v1/messages` Claude Code uses. A run counts only when its response is valid and it called the
case's tool with arguments the tool's schema accepts:

| Set | Invalid | pass@5 | pass^5 | Tool call alone: pass@5 | pass^5 | Wrong-input calls | Runtime | Bar |
|---|---|---|---|---|---|---|---|---|
| 1 | 150 of 150 | 0/30 (0%) | 0/30 (0%) | 29/30 | 27/30 | 35 | 7m27s | fails |
| 2 | 150 of 150 | 0/30 (0%) | 0/30 (0%) | 29/30 | 28/30 | 33 | 7m23s | fails |

- **Every response is invalid for one field name.** The suite counts a Chat Completions response
  valid only when some assistant message carries its reasoning as a non-empty string in
  `reasoning`, the field the cookbook names. llama.cpp sends it as `reasoning_content`, which
  the suite records and doesn't accept, so all 300 runs fail on shape, whatever the tool call
  did. No request errored: the router answered every one. The 0% is the field's, not the
  model's tool calling; the bar fails on it all the same.
- **The tool calls alone come close.** Reading the suite's own per-run record of the tool
  call, gpt-oss called the right tool with schema-valid arguments in 143 and 144 of 150 runs.
  Every miss was `detect_language`, answered in text instead of a call ("The phrase “Bonjour
  tout le monde” is French."): case 14 ("What language is 'Bonjour tout le monde'?") on all
  ten trials, case 10 once in set 1 and case 11 once in each set. That is the same text-for-a-
  call failure OpenCode and Claude Code show on `auto`; the suite sends no `tool_choice`.
  27 of 30 is 90.0%, not over the bar's 90%, and 28 of 30 is 93.3%: at the line, not clear of it.
- **Wrong-input calls don't fail a run.** The 35 and 33 are schema-valid calls whose arguments
  differ from the case's expected ones, all `generate_chart` and `query_database`: a title or
  axis label the prompt didn't ask for, a `limit` of 100 added or a `limit` of 10 left out.
  The suite counts them and, outside `--strict`, passes them.

## The comparison row: pass^5 on b11669

`main-model` judges each candidate main model against gpt-oss-120b on the same build and
pins, so gpt-oss-120b's pass^5 ran again after the move to b11669. It ran as
`go run ./cmd/conform -set a -k 5` with `SPIKE_ROUTER_URL=http://framework:8080`, from the
laptop (host `terminal`) against the Framework's router, on 2026-10-15. The router reported
`b11669-620d2225c` in `/props` `build_info`, with set A's four models loaded. The model was
gpt-oss-120b (ggml-org `MXFP4`) and clutch was at `9981a3e`, as on b11572. The consumers ran at
the consumers environment's pins. Pi 1.3.0 ran on its `llama.cpp` provider with `required` on
`respond`. Claude Code 2.1.301 ran over `/v1/messages` from `.state/claude` on the
subscription login. OpenCode 1.19.1 ran with `interleaved` set to `reasoning_content` and
`tool_choice` left at `auto`. The run's `CLAUDE_*` variables were unset. One set ran, in
16m20s, and no request met a transient router error, so no consumer's set was rerun:

| Consumer | exchange | cancel | resume | tool | skill | structured | vision | dropped-image | audio-tool | pass^5 | invalid | Bar |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Pi 1.3.0 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 5/5 | 9/9 (100%) | 0 | meets |
| Claude Code 2.1.301 | 5/5 | 5/5 | 5/5 | 0/5 | 0/5 | 5/5 | 5/5 | n/a | 5/5 | 6/9 (66.7%) | 0 | fails |
| OpenCode 1.19.1 | 5/5 | 5/5 | 5/5 | 4/5 | 3/5 | 5/5 | 5/5 | 5/5 | 3/5 | 6/9 (66.7%) | 18 | fails |

This row is the bar the candidates are judged against. For Claude Code, a candidate must meet
or beat each cell's count, with no more than 0 invalid requests. Pi's bar stays 9/9 at 5/5
with 0 invalid. OpenCode is measured, not judged.

- **Pi.** It held every cell with no invalid request, as it has on every build since b11529.
- **Claude Code's `tool` and `skill`.** Both are the text-for-a-call failure of
  the b11572 rows. In `tool`, gpt-oss called `lookup_code` and then, on all five trials, answered
  the code as JSON text (`{"code": "ZX-596F89"}`) instead of calling `respond`. That turn
  streamed no thinking block, so after the tool result the model went straight to its final.
  In `skill`, three trials called `respond` with a motto the model made up ("When it matters
  most", "Unknown", "Stay ready, stay clutch") without loading the skill. One loaded the skill
  and then asked in text what the motto was, and one asked without loading it. `dropped-image`
  stays n/a: Claude Code sends the image as it is.
- **Claude Code's `audio-tool` and invalid count.** `audio-tool` passed 5/5, up from 3/5 in
  both b11572 sets. No parse error reached Claude Code. In b11572's set 2, one `audio-tool`
  request had failed with the error and Claude Code resent it seven times, which made all 8
  invalid requests. The only non-2xx responses it got here were its `HEAD /api/hello` probes
  (404), which aren't model requests.
- **OpenCode.** Every one of its 18 invalid requests was the parse error, HTTP 200 then
  "does not match the expected peg-native format". Of the 18, 14 came
  in `audio-tool` (3, 3, 3 and 5 in trials 1 to 4) and 4 in `skill` (1 in trial 3 and 3 in
  trial 4). As on b11572, OpenCode resends with growing waits, and an exchange fails when the
  resends run out or the model answers in text. The `tool`, `skill` and `audio-tool` cells failed
  5 of 15 runs. On b11572 they failed 6 of 15 in set 1 and 4 of 15 in set 2, so this is
  the same proportion.
- **Against b11572.** Pi: 9/9 then and now. Claude Code: `tool` 0/5 against 0/5 and 1/5,
  `skill` 0/5 against 0/5 and 0/5, `audio-tool` 5/5 against 3/5 and 3/5, so pass^5 rose from
  5/9 to 6/9, and invalid was 0 against 0 and 8. OpenCode: `tool` 4/5 against 4/5 and 5/5,
  `skill` 3/5 against 3/5 and 4/5, `audio-tool` 3/5 against 2/5 and 2/5, so pass^5 was 6/9
  against 6/9 and 7/9, and invalid was 18 against 17 and 14. Only Claude Code's
  `audio-tool` moved more than five runs move on chance, and its rise was the absent parse
  error. It came on one set, so it may not hold.
- **What changed and why.** Three things changed between the rows, and the record pins none of
  them as a cause:
  - *The build's template number formatting.* [#30362](https://github.com/ggml-org/llama.cpp/pull/30362)
    ([`llama-cpp-builds.md`](llama-cpp-builds.md)) changes the prompt only where a tool's
    schema or a call's arguments carry a float. clutch's conform tools carry none: the only
    number they declare is `structured`'s integer `count`. The recordings keep responses
    and headers, not request bodies, so they don't show whether a consumer's own tools carry a
    float. The gpt-oss parser is unchanged at b11669, so the parse 5xx still reaches clients on
    `auto`, and OpenCode's invalid count shows it does.
  - *Pi, 1.1.0 to 1.3.0.* This is a minor bump. Pi's major move, 0.99 to 1.x, came
    between align's rows and consumers'. Pi changed no cell.
  - *Claude Code, 2.1.296 to 2.1.301.* Its `tool` and `skill` failures are the model's, the
    same as on b11572. Its `audio-tool` gain can't be split between this bump, the build, and
    chance.

## Which task answers which layer

- `profile`: `profiles/strix-halo.toml` sets each model's card sampling and leaves reasoning to
  the template, and the `profile` package renders both into llama.cpp's presets
  ([`model-cards.md`](model-cards.md)).
- `consumers`: pass^k per model, engine, and consumer against 0 invalid and 90%, and each
  consumer's reasoning round-trip on tool turns (Pi, Claude Code and OpenCode all send it
  back, seen on the wire).
- `gateway`: the bounded resample and fallback on a parse 5xx, per gateway.
- `validate`: the convention per platform: whether each engine constrains `required`, named
  calls, and strict `json_schema`, and parses its models' native tool format.

## Assumptions

- Assumes a constrained `tool_choice` costs no answer quality the consumers notice; vLLM's docs
  promise a parsable call, not a good one.
- Assumes the parse 5xx stays rare enough under constraint and round-tripped reasoning that one
  or two resamples clear it.
