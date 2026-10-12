# The baseline: llama.cpp on Vulkan, measured from the laptop

The tables measure the Framework's llama.cpp router on b11572 (`b11572-efe3626b1`, the upstream
Vulkan x64 build) serving set A, all four models loaded (`profiles/strix-halo.toml`), on
2026-10-12. Every figure was taken from the architect's laptop over HTTP; nothing ran on the
Framework but the router. Each later platform fills the same tables, from the same place, with
the same workloads.

**Network path.** The laptop reached the Framework over a Tailscale DERP relay (`iad`), not a
direct connection. `tailscale ping -c 3` read 308, 66 and 51 ms at 16:24 UTC, before the grid;
67 ms at 16:43, after it; and 48, 52 and 49 ms at 16:43, before the prefix runs. Every client
figure below carries that round trip, about 50-60 ms, and the engine figures don't: the gap
between a client TTFT and the engine's prompt time is mostly the relay.

## Throughput and latency

GuideLLM 0.8.1 (Python 3.14.8, the consumers mise environment) drove each model over
`/v1/chat/completions`, streaming, with its synthetic text at fixed token counts: the short
profile is 512 prompt tokens and 256 out, the agentic profile 8192 and 512. Each prompt is cut
to its count by the model's upstream tokenizer (`openai/gpt-oss-120b`, `google/gemma-4-26B-A4B-it`,
`google/gemma-4-E4B-it`), and the chat template adds the rest of the input column: 579 and 8259
tokens on gpt-oss-120b, 528 and 8208 on the Gemmas. Every request generates exactly its output
count (`max_completion_tokens` with `ignore_eos`), so the generation figures don't depend on
what the model would have said. The profile is GuideLLM's concurrent one at 1 and 4 streams, each
stream sending its requests back to back: 8 per stream on the short profile, 4 on the agentic
one, so a cell at 4 streams sends 32 or 16. The cells ran one at a time, 16:24 to 16:43 UTC, with
nothing else sent to the router.

The client columns are GuideLLM's: TTFT (to the first streamed token, reasoning included), ITL
(between tokens after the first), and output throughput (output tokens per second across the
cell, all streams together). The engine columns are llama.cpp's own counters from the model's
`/metrics`, differenced over the cell: prompt tokens over prompt seconds, predicted tokens over
predicted seconds, and the prompt tokens served from the cache. The server sums those seconds
per slot, so at 4 streams the engine rates are each request's own rate, not the cell's; the
client's output throughput is the aggregate.

| Model | Profile | Streams | Completed | Errored | TTFT median ms | TTFT p95 ms | ITL median ms | Output tok/s | Engine prompt tok/s | Engine gen tok/s | Engine cached tokens |
|---|---|---|---|---|---|---|---|---|---|---|---|
| gpt-oss-120b | short 512/256 | 1 | 8 | 0 | 704 | 757 | 20.3 | 42.7 | 973.9 | 48.8 | 490 |
| gpt-oss-120b | short 512/256 | 4 | 32 | 0 | 3578 | 3661 | 47.2 | 68.2 | 948.7 | 21.2 | 5350 |
| gpt-oss-120b | agentic 8192/512 | 1 | 4 | 0 | 8665 | 8988 | 21.7 | 25.7 | 1003.7 | 45.7 | 570 |
| gpt-oss-120b | agentic 8192/512 | 4 | 16 | 0 | 20780 | 36142 | 56.2 | 36.8 | 976.5 | 14.0 | 33320 |
| Gemma 4 26B-A4B | short 512/256 | 1 | 8 | 0 | 529 | 1157 | 16.3 | 53.1 | 1525.3 | 60.9 | 0 |
| Gemma 4 26B-A4B | short 512/256 | 4 | 32 | 0 | 2737 | 2961 | 31.3 | 98.0 | 1699.6 | 31.7 | 4250 |
| Gemma 4 26B-A4B | agentic 8192/512 | 1 | 4 | 0 | 5299 | 6238 | 18.3 | 34.2 | 1684.5 | 54.3 | 523 |
| Gemma 4 26B-A4B | agentic 8192/512 | 4 | 0 | 16 | — | — | — | — | 1616.6 | — | 24609 |
| gemma-4-E4B | short 512/256 | 1 | 8 | 0 | 475 | 496 | 25.5 | 36.6 | 1706.0 | 38.9 | 1056 |
| gemma-4-E4B | short 512/256 | 4 | 32 | 0 | 2154 | 2420 | 29.7 | 107.7 | 1984.5 | 33.4 | 4250 |
| gemma-4-E4B | agentic 8192/512 | 1 | 4 | 0 | 4349 | 5182 | 27.3 | 27.6 | 2096.0 | 36.5 | 11 |
| gemma-4-E4B | agentic 8192/512 | 4 | 0 | 16 | — | — | — | — | 2000.1 | — | 24609 |

**The Gemmas can't hold four agentic requests.** Every agentic request at 4 streams failed on
both Gemma models with llama.cpp's `Context size has been exceeded.`, after its prompt and a few
output tokens. Each runs at `c = 32768` with the KV pool shared across its 4 slots
(`--kv-unified`), and four requests of 8208 prompt tokens and 512 out need 34880. The memory
budget sized the 26B-A4B down from 65536 to fit the pool; at 32768 it holds about three requests
of this shape at once. gpt-oss-120b, at 131072, holds four. The rows' engine prompt rate is the
prefill of those failed requests; their generation rate is left out, since they generated almost
nothing.

The engine's cached tokens are the chat template's fixed head, reused across requests (gpt-oss's
system preamble is about 60 tokens); GuideLLM's prompts share no text with one another. At 4
streams the TTFT is mostly waiting: the server prefills the four prompts together, so each
request's first token waits for the others' prefill, and on the agentic profile a request that
arrives while three are generating waits for its own 8192-token prefill beside them.

GuideLLM's JSON for every cell and the `/metrics` snapshots are kept under the gitignored
`.state/baseline/`. The grid is `MISE_ENV=consumers mise run baseline` with `SPIKE_ROUTER_URL`
set (`scripts/baseline.sh`).

## Prefix reuse

`go run ./cmd/prefix` sent each generative model five streamed chat completions, back to back,
that share one prefix of exactly 8192 tokens in the model's own tokenizer (cut through the
router's `/tokenize` and `/detokenize`) and end in five different questions, 32 tokens out each.
The prefix starts with a nonce unique to the run, so the first request is cold. `cache_n` and
`prompt_n` are llama.cpp's own accounting from each response's `timings` (prompt tokens served
from the cache, and evaluated), with the engine's prompt time; TTFT is the client's.

| Model | Request | `cache_n` | `prompt_n` | Engine prompt ms | TTFT ms |
|---|---|---|---|---|---|
| gpt-oss-120b | 1 | 62 | 8209 | 8281 | 10718 |
| gpt-oss-120b | 2 | 8257 | 11 | 116 | 416 |
| gpt-oss-120b | 3 | 8257 | 15 | 161 | 466 |
| gpt-oss-120b | 4 | 8257 | 16 | 162 | 529 |
| gpt-oss-120b | 5 | 8257 | 16 | 167 | 485 |
| Gemma 4 26B-A4B | 1 | 0 | 8220 | 4993 | 5244 |
| Gemma 4 26B-A4B | 2 | 8204 | 13 | 169 | 460 |
| Gemma 4 26B-A4B | 3 | 8204 | 17 | 182 | 435 |
| Gemma 4 26B-A4B | 4 | 8204 | 18 | 184 | 439 |
| Gemma 4 26B-A4B | 5 | 8204 | 18 | 185 | 459 |
| gemma-4-E4B | 1 | 0 | 8220 | 3938 | 4220 |
| gemma-4-E4B | 2 | 8204 | 13 | 84 | 381 |
| gemma-4-E4B | 3 | 8204 | 17 | 82 | 351 |
| gemma-4-E4B | 4 | 8204 | 18 | 84 | 387 |
| gemma-4-E4B | 5 | 8204 | 18 | 85 | 367 |

Every later request found the whole prefix cached, the template's head included, and evaluated
only its question: TTFT fell from 4-11 seconds to under 0.55 on every model, of which about 0.3
is the relay and the client. The requests went unpinned, as a consumer sends them, and the
server's slot choice by prompt similarity put each on the slot holding the prefix. The same run
with every request pinned to slot 0 (`-slot 0`, llama.cpp's `id_slot`) read the same `cache_n`
and `prompt_n` on every request, with warm TTFTs of 393-505 ms, so pinning isn't needed to read
reuse turn to turn on this router. gpt-oss-120b's cold request took 2.4 seconds past its prompt
time unpinned, against 0.4 pinned; the warm requests don't show it.

## Cold load

Pending: the architect's restarts. `go run ./cmd/coldload` times them from the laptop: started
with every model loaded, it polls `/models` every 250 ms, takes the restart's start as the first
poll that doesn't see every model loaded, and records when the router answers again and when
each model is listed as loaded after it was gone. Two runs fill this section: after a warm
restart, and after a restart with the page cache dropped.
