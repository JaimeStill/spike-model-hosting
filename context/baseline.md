# The baseline: llama.cpp on Vulkan, measured from the laptop

The tables measure the Framework's llama.cpp router on b11572 (`b11572-efe3626b1`, the upstream
Vulkan x64 build) serving set A, all four models loaded (`profiles/strix-halo.toml`), on
2026-10-12. Every figure was taken from the architect's laptop over HTTP; nothing ran on the
Framework but the router. Each later platform is planned to fill the same tables, from the
same place, with the same workloads.

**Network path.** The laptop reached the Framework over a Tailscale DERP relay (`iad`), not a
direct connection. `tailscale ping -c 3` read 308, 66 and 51 ms at 16:24 UTC, before the grid;
67 ms at 16:43, after it; and 48, 52 and 49 ms at 16:43, before the prefix runs. Every client
figure below carries that round trip, about 50-60 ms, and the engine figures don't: the gap
between a client TTFT and the engine's prompt time is mostly the relay.

## Throughput and latency

GuideLLM 0.8.1 (Python 3.14.8, the consumers mise environment) drove each model over
`/v1/chat/completions` across the relay above, streaming, with its synthetic text at fixed token counts: the short
profile is 512 prompt tokens and 256 out, the agentic profile 8192 and 512. Each prompt is cut
to its count by the model's upstream tokenizer (`openai/gpt-oss-120b`, `google/gemma-4-26B-A4B-it`,
`google/gemma-4-E4B-it`), and the chat template adds the rest of the input column: 579 and 8259
tokens on gpt-oss-120b, 528 and 8208 on the Gemmas. Every request generates exactly its output
count (`max_completion_tokens` with `ignore_eos`), so the generation figures don't depend on
what the model would have said. GuideLLM's `concurrent` load profile ran at 1 and 4 streams,
each stream sending its requests back to back: 8 per stream on the short profile, 4 on the agentic
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
(`--kv-unified`), and four requests of 8208 prompt tokens and 512 out need 34880. The
[memory budget](memory-budget.md) sized the 26B-A4B down from 65536 to fit the pool; at 32768 it holds about three requests
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

A router restart takes about 21 s until all four models are loaded, warm or cold, set by
gpt-oss-120b's read from NVMe. `go run ./cmd/coldload` timed both restarts from the laptop,
over the same relay, on 2026-10-12. It starts with every
model loaded and polls `/models` every 250 ms. It takes the restart's start as the first poll
that doesn't see every model loaded, so each time is late by up to one poll. A model counts as
loaded only when it is listed as loaded after it was seen gone. The architect ran each restart
on the Framework: `sudo systemctl restart llama-router` for warm, and the same after
`sync && echo 3 | sudo tee /proc/sys/vm/drop_caches` for cold.

| Restart | Router up | EmbeddingGemma 2 | gemma-4-E4B | 26B-A4B | gpt-oss-120b | All four |
|---|---|---|---|---|---|---|
| warm | 0.8 s | 2.5 s | 8.2 s | 11.7 s | 21.5 s | 21.5 s |
| cold (page cache dropped) | 0.6 s | 2.3 s | 7.5 s | 11.5 s | 21.0 s | 21.0 s |

Warm and cold are the same within a poll, because the page cache can't hold set A. The
Framework has 128 GB of RAM, and set A's 96 GiB GTT pool takes most of it. After the cold run,
`free -m` showed 25520 MiB of buffers and cache and 103610 MiB used. Set A's files are far
larger than 25 GiB (gpt-oss-120b alone is about 60 GiB), on an ext4 NVMe drive. So even a
"warm" restart reads most of set A from disk again.

## Mistral Small 4 on b11669: disqualified at fitness (speed)

Mistral Small 4 is disqualified at the fitness stage on Vulkan, Strix Halo, b11669: one stream
on the short profile, the engine generated at 3.36 tok/s against the 24 tok/s floor.
[`tool-reliability.md`](tool-reliability.md), "Mistral Small 4: disqualified at fitness", lists
the stages that didn't run and the llama.cpp PRs that would trigger a re-probe.

The figures are for set `mistral`: unsloth's Mistral Small 4 `UD-Q4_K_XL` with its mmproj `BF16`
at `c = 131072`, beside gemma-4-E4B and EmbeddingGemma 2. The router was b11669
(`b11669-620d2225c`, upstream Vulkan x64). They were taken from the laptop (host `terminal`) on
2026-10-15 and 2026-10-16, with GuideLLM 0.8.1 at the consumers environment's pins. The network
path differed from set A's rows above: `tailscale ping` read a direct LAN path at 1-2 ms, not
the DERP relay.

### The grid

`MISE_ENV=consumers mise run baseline -set mistral` ran the cells with the same workloads and
columns as the table above. GuideLLM cut each prompt with `mistralai/Mistral-Small-4-119B-2603`'s
tokenizer. The server counted 1069 and 8749 prompt tokens for the short and agentic profiles,
since the template adds its default system prompt (about 550 tokens) when a request has none.

| Model | Profile | Streams | Completed | Errored | TTFT median ms | TTFT p95 ms | ITL median ms | Output tok/s | Engine prompt tok/s | Engine gen tok/s | Engine cached tokens | Cell took |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Mistral Small 4 | short 512/256 | 1 | 8 | 0 | 1724 | 1984 | 297.8 | 3.3 | 353.9 | 3.4 (3.36) | 4462 | 622 s |
| Mistral Small 4 | short 512/256 | 4 | 32 | 0 | 6830 | 6923 | 357.3 | 10.6 | 351.1 | 2.8 | 21950 | 773 s |
| Mistral Small 4 | agentic 8192/512 | 1 | 4 | 0 | 25737 | 26257 | 302.8 | 2.8 | 323.3 | 3.3 | 2235 | 723 s |
| Mistral Small 4 | agentic 8192/512 | 4 | 16 | 0 | 39852 | 112718 | 560.8 | 6.2 | 309.5 | 1.8 | 6783 | 1321 s |

- **Fitness.** The short profile at one stream generated at 3.36 tok/s by the engine's counters
  (3.29 by GuideLLM's output rate), a seventh of the 24 tok/s floor. gpt-oss-120b generated
  48.8 on the same cell. At 4 streams the cell's aggregate output rose to 10.6 tok/s, while each
  request ran at 2.8.
- **The agentic cell at 4 streams ran twice.** The first run stalled overnight, and the row is
  its bounded rerun, `.state/baseline/20261016T113037Z-agentic-c4`. The rerun used the grid's own
  request count, 16, with a `max_duration` of 3600 s and a 600 s HTTP read timeout. It finished
  all 16 in 1321 s, so neither bound applied. The other three cells are from the first run,
  `.state/baseline/20261016T002154Z`, 00:21 to 00:57 UTC.
- **Why the first agentic c4 run stalled.** The laptop suspended. `systemd-logind` put it to
  sleep at 01:08:51 UTC, nine seconds after the cell's 8th request completed, while the third
  wave of four requests was streaming. It woke for five minutes at 01:27 and then slept until
  11:24. The server finished those four requests: the engine's predicted-token counter rose by
  12 × 512, and the four slots held 8749 + 511 tokens with nothing processing. But the laptop's
  sockets were never reset. GuideLLM's HTTP backend sets no read timeout by default
  (`timeout: None`), so its four workers waited in `epoll_wait` on connections that had
  received 185-3779 bytes each, until they were stopped at 11:29. It was neither a request
  timeout, a worker deadlock, nor the scenario. Every long run since holds a `systemd-inhibit`
  block on sleep, idle and the lid switch, beside a monitor that checks the router's slots are
  busy.

### The CPU fallback while generating

Mistral's process works the CPU hard while generating, because Vulkan refuses its F32 expert
down-projections (`MUL_MAT_ID` with `GGML_PREC_F32`; see
[`llama-cpp-builds.md`](llama-cpp-builds.md), "Mistral's expert down-projection runs on the
CPU", for the source lines). One request of 3776 prompt tokens and 256 out (`ignore_eos`) ran
with `top -b -d 2 -p <pid>` sampled over ssh on 2026-10-15:

| Phase | Engine rate | Mistral's `llama-server`, %CPU (2 s samples) |
|---|---|---|
| prompt, 3776 tokens in 10.5 s | 360.9 tok/s | 613-665 (5 samples) |
| generation, 256 tokens in 76.6 s | 3.33 tok/s | 847-858 (38 samples) |
| idle, after | — | 0 |

That is about 8.5 of its 16 threads busy for every generated token. The child logs at verbosity
3, which doesn't print op placement or graph splits. Raising it means changing the preset and
reloading, which is root's, so the source lines stand as the placement's evidence.

### Prefix reuse

`go run ./cmd/prefix -set mistral -timeout 20m` ran on 2026-10-16, 11:53 UTC. It has the same
shape as above: five streamed requests per generative model, sharing an 8192-token prefix that
starts with a run nonce, 32 tokens out each.

| Model | Request | `cache_n` | `prompt_n` | Engine prompt ms | TTFT ms |
|---|---|---|---|---|---|
| Mistral Small 4 | 1 | 556 | 8205 | 27607 | 29691 |
| Mistral Small 4 | 2 | 8749 | 9 | 508 | 831 |
| Mistral Small 4 | 3 | 8749 | 13 | 571 | 892 |
| Mistral Small 4 | 4 | 8749 | 14 | 582 | 902 |
| Mistral Small 4 | 5 | 8749 | 14 | 573 | 896 |
| gemma-4-E4B | 1 | 0 | 8220 | 4040 | 4148 |
| gemma-4-E4B | 2 | 8204 | 13 | 80 | 183 |
| gemma-4-E4B | 3 | 8204 | 17 | 81 | 185 |
| gemma-4-E4B | 4 | 8204 | 18 | 81 | 183 |
| gemma-4-E4B | 5 | 8204 | 18 | 81 | 183 |

Every later request found the whole prefix cached, and its TTFT fell from 29.7 s to under 0.9 s.
The cold request's 556 cached tokens are the template's default system prompt, left by the grid.
The warm requests evaluate their question in about 0.5 s, against 0.12-0.19 s on set A's models.
gemma-4-E4B's warm TTFT is about 0.2 s lower than on b11572, since this path was direct rather
than relayed.

### Load times

The cold load didn't run. It needs a router restart, which is root's, and the stage is skipped
for a disqualified candidate. Swaps over the router's API, with Mistral's files cached
(`go run ./cmd/swap`):

| Swap | Unloads | Load | Total |
|---|---|---|---|
| a to mistral, 2026-10-15 | 26B-A4B 1.0s, gpt-oss-120b 1.0s | Mistral Small 4 23.0s | 25.0s |
| mistral to a, 2026-10-16 | Mistral Small 4 1.0s | gpt-oss-120b 16.0s, 26B-A4B 5.0s | 22.0s |
