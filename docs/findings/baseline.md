# The baseline: llama.cpp on Vulkan, measured from the laptop

The tables measure the Framework's llama.cpp router on b11572 (`b11572-efe3626b1`, the upstream
Vulkan x64 build) serving set A, all four models loaded (`devices/strix-halo/profile.toml`), on
2026-10-12. Every figure was taken from the architect's laptop over HTTP; nothing ran on the
Framework but the router. Each later platform is planned to fill the same tables, from the
same place, with the same workloads. [Set A's baseline on b11715](#set-as-baseline-on-b11715-every-stage-twice),
at the end, is the evaluation's: every stage run twice, and the bars `candidates` is judged by.

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

GuideLLM's JSON for every cell and the `/metrics` snapshots were kept under the untracked
`.state/baseline/`, and removed once the findings were committed. The grid is `MISE_ENV=consumers mise run //evaluation:baseline` with
`SPIKE_ROUTER_URL` set (`evaluation/scripts/baseline.sh`).

## Prefix reuse

`go run ./evaluation/cmd/evaluate prefix` sent each generative model five streamed chat completions,
back to back, that share one prefix of exactly 8192 tokens in the model's own tokenizer (cut through
the router's `/tokenize` and `/detokenize`) and end in five different questions, 32 tokens out each.
The prefix starts with a nonce unique to the run, so the first request is cold. `cache_n` and
`prompt_n` are llama.cpp's own accounting from each response's `timings` (prompt tokens served from
the cache, and evaluated), with the engine's prompt time; TTFT is the client's.

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
gpt-oss-120b's read from NVMe. `go run ./evaluation/cmd/evaluate coldload` timed both restarts from
the laptop, over the same relay, on 2026-10-12. It starts with every model loaded and polls
`/models` every 250 ms. It takes the restart's start as the first poll that doesn't see every model
loaded, so each time is late by up to one poll. A model counts as loaded only when it is listed as
loaded after it was seen gone. The architect ran each restart on the Framework:
`sudo systemctl restart llama-router` for warm, and the same after
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
(`b11669-620d2225c`, upstream Vulkan x64). They were taken from the laptop on
2026-10-15 and 2026-10-16, with GuideLLM 0.8.1 at the consumers environment's pins. The network
path differed from set A's rows above: `tailscale ping` read a direct LAN path at 1-2 ms, not
the DERP relay.

### The grid

`MISE_ENV=consumers mise run //evaluation:baseline -set mistral` ran the cells with the same
workloads and columns as the table above. GuideLLM cut each prompt with
`mistralai/Mistral-Small-4-119B-2603`'s tokenizer. The server counted 1069 and 8749 prompt tokens
for the short and agentic profiles, since the template adds its default system prompt (about 550
tokens) when a request has none.

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
  its bounded rerun, run `20261016T113037Z-agentic-c4`. The rerun used the grid's own
  request count, 16, with a `max_duration` of 3600 s and a 600 s HTTP read timeout. It finished
  all 16 in 1321 s, so neither bound applied. The other three cells are from the first run,
  run `20261016T002154Z`, 00:21 to 00:57 UTC. Both runs' raw files were removed once the
  findings were committed.
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
[`llama-cpp-builds.md`](../../devices/strix-halo/llama-cpp-builds.md), "Mistral's expert
down-projection runs on the CPU", for the source lines). One request of 3776 prompt tokens and 256
out (`ignore_eos`) ran with `top -b -d 2 -p <pid>` sampled over ssh on 2026-10-15:

| Phase | Engine rate | Mistral's `llama-server`, %CPU (2 s samples) |
|---|---|---|
| prompt, 3776 tokens in 10.5 s | 360.9 tok/s | 613-665 (5 samples) |
| generation, 256 tokens in 76.6 s | 3.33 tok/s | 847-858 (38 samples) |
| idle, after | — | 0 |

That is about 8.5 of its 16 threads busy for every generated token. The child logs at verbosity
3, which doesn't print op placement or graph splits. Raising it means changing the preset and
reloading, which is root's, so the source lines stand as the placement's evidence.

### Prefix reuse

`go run ./evaluation/cmd/evaluate prefix -set mistral -timeout 20m` ran on 2026-10-16, 11:53 UTC. It
has the same shape as above: five streamed requests per generative model, sharing an 8192-token
prefix that starts with a run nonce, 32 tokens out each.

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
(`go run ./hosting/cmd/swap`):

| Swap | Unloads | Load | Total |
|---|---|---|---|
| a to mistral, 2026-10-15 | 26B-A4B 1.0s, gpt-oss-120b 1.0s | Mistral Small 4 23.0s | 25.0s |
| mistral to a, 2026-10-16 | Mistral Small 4 1.0s | gpt-oss-120b 16.0s, 26B-A4B 5.0s | 22.0s |

## Set A's baseline on b11715: every stage, twice

Set A ran every stage of `go run ./evaluation/cmd/evaluate` twice on the Framework's router at
b11715 (`b11715-9d1878a43`, upstream Vulkan x64), each role on its assigned model: gpt-oss-120b
(agent), Gemma 4 26B-A4B (vision, `mtmd-batch-max-tokens = 512`), gemma-4-E4B (audio) and
EmbeddingGemma 2 (embedding), all four loaded (`devices/strix-halo/profile.toml`). A baseline is
the set run (`-set a`: each role's tier 1 alone, then all at once) and one model run per role
(`-set a -role <role>`, admission through deep). Everything ran from the laptop over HTTP, the pool read over user-level ssh. `tailscale ping` read a direct LAN path
before every run, 0-2 ms. Every run passed every stage it reached. These figures are the
baseline: `evaluation/bars.toml` takes its bars from the quality table below.

### The runs

| Baseline | Run | Started (UTC) | Fresh reload first | Admission to set | Deep |
|---|---|---|---|---|---|
| 1 | set | 2026-10-17 22:55 | no (fresh since the move to b11715 at 22:53) | 38.0 min | — |
| 1 | agent | 2026-10-17 23:33 | no | 37.2 min | 47.9 min |
| 1 | vision | 2026-10-18 14:29 | no | 82.1 min | 5.2 min |
| 1 | audio | 2026-10-18 15:57 | no | 18.6 min | 5.3 min |
| 1 | embedding | 2026-10-18 16:55 | yes | 5.0 min | — |
| 2 | set | 2026-10-18 17:01 | yes | 38.4 min | — |
| 2 | agent | 2026-10-18 17:41 | yes | 35.9 min | 49.2 min, resumed 18:32 |
| 2 | vision | 2026-10-18 21:05 | yes | 70.9 min | 5.1 min |
| 2 | audio | 2026-10-18 22:22 | yes | 17.9 min | 5.2 min |
| 2 | embedding | 2026-10-18 22:47 | yes | 4.9 min | — |

- **The reloads.** From baseline 1's embedding run on, every run started from freshly loaded
  models ([`evaluation.md`](../evaluation.md), "Before each run"): set A unloaded and loaded again over the router's
  API, 30 s idle, then the run. Baseline 1's first four runs had none; the drift that made the
  reload necessary is in [`memory-budget.md`](memory-budget.md), "The pool over a day of runs".
  Quality scores don't depend on the pool, so those runs stand. Baseline 2's agent run's deep
  stage was resumed (`-from deep -run` on the same run) without a reload, as baseline 1's deep
  stages also ran on the state their gates left.
- **Runs not kept.** Baseline 1's first vision and audio runs, 2026-10-18 00:58 and 02:18 UTC,
  lost every pool reading when Tailscale SSH's check period lapsed (01:00-01:44): the vision
  run's set stage and the audio run's fitness failed "couldn't measure" (`ssh <host>
  amdgpu_top: exit status 255`); the HTTP path was unaffected. The vision run's quality scores
  before the lapse were V*Bench 0.65, DocVQA 0.898 and MMMU 0.720, within the kept runs' range.
  Baseline 1's first embedding run failed fitness on the pool, 2778 MiB free (`memory-budget.md`).
  Baseline 2's agent run's first deep stage failed every τ2 and grid check with `[Errno 122] Disk
  quota exceeded`: inspect_evals' τ2 task writes one copy of the domain's database (airline 6.8
  MB, retail 2.7 MB) per sample to `TMPDIR` and never removes it, about 230 MB an agent run, and
  the laptop's `/tmp` quota was full. The resumed deep stage, and every run after it, ran with
  `TMPDIR` in the untracked `.state/tmp`.
- **Run time.** The vision runs took 82.1 and 70.9 minutes from admission through the set stage,
  and the agent runs 37.2 and 35.9, then about 48 for the deep stage. Against the ~85-minute
  budget for a chat-and-vision model, see [`evaluation.md`](../evaluation.md), "The stages".

### Tools, versions and seeds

The consumers environment's pins (`evaluation/mise.consumers.toml`) and the suites project's
lock (`evaluation/suites/uv.lock`, Python 3.14.8):

| Tool | Version | Used for |
|---|---|---|
| Inspect AI | 0.3.281 | BFCL, the vision tiers, the ASR task, τ2 |
| inspect_evals | 0.24.0 | `bfcl@8-B`, `vstar_bench_*@2-A`, `docvqa@2-A`, `mmmu_multiple_choice@2-A`, `tau2@3-A` |
| MTEB | 2.26.4 | the embedding tasks; the task brief named 2.26.3, and the suites project pins and ran 2.26.4 |
| GuideLLM | 0.8.1 | the deep stage's grid |
| Claude Code | 2.1.301 | harness fit; τ2's simulated user (Sonnet, reported `claude-sonnet-5-5` on every call) |
| Pi | 1.3.0 | harness fit |
| OpenCode | 1.19.3 | harness fit, at the pin `evaluation/mise.consumers.toml` names |

Every tier list in `evaluation/tiers` is drawn with seed 20261016 from its dataset's pinned
revision, and every model runs the same lists: BFCL single-turn 100 and multi-turn 20 (Gorilla
`dac44e7`); V*Bench 100 (`craigwu/vstar_bench@d9ae62c`), DocVQA 150 (`lmms-lab-encoder/DocVQA@539088e`)
and MMMU 200 (`MMMU/MMMU@171b0ef`); 150 utterances from each ASR set (`hf-audio/esb-datasets-test-only-sorted@de79e57`,
FLEURS `google/fleurs@70bb2e8`); τ2 airline and retail 12 tasks each (τ2-bench `377cecf`). The MTEB
tasks run in full. The vision probe is the probe's image, five times.

### Quality: each tier's two scores, spread and bar

Each tier's score is the role model's quality stage. Its bar is the two runs' mean less their
spread (their difference), rounded down to 0.001; a WER's ceiling is the mean plus the spread,
rounded up. No bar was set while the baseline ran, so every tier ran whole.

| Role | Tier | Measure | Baseline 1 | Baseline 2 | Spread | Bar |
|---|---|---|---|---|---|---|
| agent | BFCL single-turn (tier 1) | accuracy | 0.65 | 0.69 | 0.04 | ≥ 0.630 |
| agent | BFCL multi-turn (tier 2) | accuracy | 0.40 | 0.35 | 0.05 | ≥ 0.325 |
| vision | probe image ×5 (tier 1) | pass^5 | 5/5 | 5/5 | — | all five |
| vision | V*Bench (tier 1) | accuracy | 0.62 | 0.61 | 0.01 | ≥ 0.605 |
| vision | DocVQA (tier 2) | mean ANLS | 0.9040 | 0.9037 | 0.0002 | ≥ 0.903 |
| vision | MMMU (tier 3) | accuracy | 0.715 | 0.720 | 0.005 | ≥ 0.712 |
| audio | LibriSpeech test-clean | WER | 0.0221 | 0.0221 | 0 | ≤ 0.023 |
| audio | LibriSpeech test-other | WER | 0.0613 | 0.0613 | 0 | ≤ 0.062 |
| audio | VoxPopuli | WER | 0.0789 | 0.0789 | 0 | ≤ 0.079 |
| audio | AMI | WER | 0.1505 | 0.1505 | 0 | ≤ 0.151 |
| audio | Earnings-22 | WER | 0.1359 | 0.1359 | 0 | ≤ 0.136 |
| audio | FLEURS English | WER | 0.0749 | 0.0749 | 0 | ≤ 0.075 |
| embedding | STSBenchmark | cosine Spearman | 0.8748 | 0.8748 | 0 | ≥ 0.874 |
| embedding | Banking77Classification | accuracy | 0.9227 | 0.9227 | 0 | ≥ 0.922 |
| embedding | SciFact | nDCG@10 | 0.8515 | 0.8515 | 0 | ≥ 0.851 |
| embedding | NFCorpus | nDCG@10 | 0.3658 | 0.3658 | 0 | ≥ 0.365 |

- **The sampled tiers move; the deterministic ones don't.** BFCL runs at the card's sampling
  (temperature 1), so its scores move by 4-5 points between runs, and its bars carry that. The
  vision tiers run at temperature 0, and still moved by up to a point: four requests in flight
  batch differently from run to run. The ASR sets (temperature 0, one request at a time) and
  MTEB (embeddings) gave identical scores to every digit in both runs, so their bars are the
  scores themselves, to the rounding: a candidate must match set A there, not come near it.
- **More readings of the tier 1s.** The set runs measured the same tier 1s alone: BFCL
  single-turn 0.65 and 0.67, V*Bench 0.66 and 0.62, LibriSpeech test-clean 0.0221 and STSBenchmark
  0.8748 both times. V*Bench's four readings span 0.61-0.66, wider than its bar's spread; the
  bars come from the quality stages only.
- **Against the cards.** gemma-4-E4B's FLEURS WER, 0.0749, is under the card's 0.08 (a
  multilingual figure). Gemma 4 26B-A4B's MMMU validation, 0.715-0.720, is near the card's MMMU
  Pro, 73.8%, a harder variant.
- **The E4B flake didn't recur.** gemma-4-E4B answering only `<unused32>` after prompt-cache
  evictions ([#27148](https://github.com/ggml-org/llama.cpp/issues/27148),
  [`llama-cpp-builds.md`](../../devices/strix-halo/llama-cpp-builds.md)) would have shown as a
  WER jump; every set's errors and words matched to the count in both runs.

Harness fit, the agent's tier 3, judges each harness by an absolute bar rather than set A's
score; its two runs are in [`tool-reliability.md`](tool-reliability.md), "Harness fit on b11715".
Pi fit both times; Claude Code and OpenCode fit neither time.

### The floors

Each floor against its two readings, fitness's single stream and capacity's four:

| Role | Floor | Initial value | Baseline 1 | Baseline 2 |
|---|---|---|---|---|
| agent | decode | ≥ 20 tok/s | 48.1 | 50.3 |
| agent | prefill, 8k prompt | ≥ 500 tok/s | 1028 | 1016 |
| agent | TTFT, 8k prompt | ≤ 20 s | 8.09 | 8.25 |
| agent | each of 4 streams' decode | ≥ 10 tok/s | 21.9 | 21.3 |
| vision | one image question | ≤ 10 s | 1.90 | 1.90 |
| audio | RTFx | ≥ 5 | 12.0 | 11.6 |
| audio | each of 4 streams' RTFx | ≥ 1 | 3.07 | 3.08 |
| embedding | throughput | ≥ 4,300 tok/s | 17,975 | 17,040 |
| embedding | 32-token query | ≤ 250 ms | 20.9 | 12.6 |

Every rate clears its floor by at least 2x; the agent's 8k prefill, at 2.03x, has the least room. Ceiling
efficiency, recorded and never gated (decode over 256 GB/s ÷ the bytes a token reads):
gpt-oss-120b 52% and 54% of 92.5 tok/s; Gemma 4 26B-A4B 55% and 55% of 116.4; gemma-4-E4B 71% and
73% of 53.4.

### The pool

Every pool reading, `amdgpu_top -p` over ssh, against the 2816 MiB floor (MiB free of 98304):

| Run | Baseline 1 | Baseline 2 |
|---|---|---|
| set run, least free with every role's tier 1 at once | 3149 | 3146 |
| set run, least free with each role's tier 1 alone (agent, vision, audio, embedding) | 3472, 3236, 3172, 3151 | 3461, 3230, 3171, 3149 |
| agent: fitness idle, capacity peak, set stage least free | 3132, 3125, 3064 | 3484, 3465, 3291 |
| vision: fitness idle, capacity peak, set stage least free | 3026, 3026, 3018 | 3492, 3490, 3223 |
| audio: fitness idle, capacity peak, set stage least free | 2932, 2932, 2925 | 3494, 3492, 3411 |
| embedding: fitness idle, capacity peak, set stage least free | 3471, 3471, 3410 | 3471, 3471, 3414 |

From a fresh load, the set's least free reading of either baseline was 3146 MiB, 330 MiB above
the floor, at the set run's concurrent phase: the vision model's image work, while every other
role works too. Baseline 1's agent, vision and audio rows ran without a reload, on the drift
`memory-budget.md` records.

### The set stage

| Run | Tier 1 alone | Tier 1, with the set's other roles | Slowdown |
|---|---|---|---|
| set run 1: BFCL single / V*Bench / LibriSpeech-clean WER / STSBenchmark | 0.65 / 0.66 / 0.0221 / 0.8748 | 0.69 / 0.66 / 0.0221 / 0.8749 | 1.37 / 1.49 / 2.64 / 4.22 |
| set run 2 | 0.67 / 0.62 / 0.0221 / 0.8748 | 0.65 / 0.64 / 0.0221 / 0.8749 | 1.82 / 1.58 / 2.62 / 5.06 |
| agent runs 1, 2: BFCL single | 0.65, 0.69 | 0.63, 0.65 | 2.18, 1.76 |
| vision runs 1, 2: V*Bench | 0.62, 0.61 | 0.59, 0.64 | 3.47, 2.44 |
| audio runs 1, 2: LibriSpeech-clean WER | 0.0221, 0.0221 | 0.0221, 0.0221 | 2.58, 2.35 |
| embedding runs 1, 2: STSBenchmark | 0.8748, 0.8748 | 0.8749, 0.8749 | 3.74, 3.68 |

No answer crossed in any run: 18 and 22 crossing rounds in the set runs, 2-28 in the model runs.
Load on the set's other roles never errored.

**The cut-off answers, and the checker change.** Under the set run's full load, three vision
crossing answers came back short or empty: rounds 4 and 17 of set run 1 empty (recorded
unanswered), and round 17 of baseline 1's first vision run, "The image shows" (counted crossed,
which failed that run's set stage). The router's journal shows each generated exactly 512 tokens,
the request's `max_tokens`: tasks 16355 (2026-10-17 23:21:27 UTC) and 24693 (23:32:36), and
107169 (2026-10-18 02:06:40), each with a 150-token prompt; every other vision crossing request
in those windows generated 201-458 tokens for its one sentence and the nonce. So the model ran
out of tokens before it reached the nonce; nothing crossed. The set stage now counts an answer
that hit its `max_tokens` with no nonce as unanswered, only an answer carrying a nonce not its
own is crossed, and the vision request allows 1024 tokens (commit `4d802aa`, the architect's
decision). It landed after baseline 1's set, agent, vision and audio runs, which ran the
earlier checker and the 512-token request; baseline 1's kept vision run had one empty vision
answer, recorded unanswered, of 28 rounds. Baseline 1's embedding run and all of baseline 2 ran
the change, and none of their vision answers came back short.

### The deep stage

τ2, 12 seeded tasks a domain, 2 trials each, the simulated user Sonnet through the laptop's
pinned Claude Code on the subscription login (`claude -p`, tools off, its own system prompt, no
session persistence):

| Domain | Run | pass^1 | pass^2 | Samples passed | Wall | Simulator calls | Model calls failed |
|---|---|---|---|---|---|---|---|
| airline | 1 | 0.542 | 0.417 | 13 of 24 | 1240 s | 105 | 0 |
| airline | 2 | 0.667 | 0.583 | 16 of 24 | 1286 s | 110 | 0 |
| retail | 1 | 0.875 | 0.833 | 21 of 24 | 1105 s | 136 | 0 |
| retail | 2 | 0.875 | 0.833 | 21 of 24 | 1144 s | 145 | 0 |

Every simulator call answered, none retried, each reporting `claude-sonnet-5-5` (241 and 255
calls). Airline moved by three samples of 24 between runs; retail not at all. The deep stage is
recorded, never judged, so τ2 sets no bar.

The GuideLLM grid, the workloads and columns of [Throughput and latency](#throughput-and-latency)
above, ran on the deep stage of each generative model's run:

| Model | Run | Cell | Completed | Errored | TTFT median ms | TTFT p95 ms | ITL median ms | Output tok/s | Engine prompt tok/s | Engine gen tok/s |
|---|---|---|---|---|---|---|---|---|---|---|
| gpt-oss-120b | 1 | short 512/256 c1 | 8 | 0 | 614 | 3144 | 20.3 | 41.7 | 939.1 | 48.6 |
| gpt-oss-120b | 1 | short 512/256 c4 | 32 | 0 | 3490 | 3615 | 46.3 | 69.4 | 975.1 | 21.3 |
| gpt-oss-120b | 1 | agentic 8192/512 c1 | 4 | 0 | 8111 | 8355 | 22.0 | 26.3 | 1049.2 | 45.1 |
| gpt-oss-120b | 1 | agentic 8192/512 c4 | 16 | 0 | 18002 | 34809 | 78.7 | 36.6 | 986.9 | 12.1 |
| gpt-oss-120b | 2 | short 512/256 c1 | 8 | 0 | 642 | 2042 | 20.9 | 41.6 | 931.7 | 47.4 |
| gpt-oss-120b | 2 | short 512/256 c4 | 32 | 0 | 3488 | 3650 | 46.2 | 69.3 | 973.0 | 21.3 |
| gpt-oss-120b | 2 | agentic 8192/512 c1 | 4 | 0 | 8076 | 8443 | 22.0 | 26.4 | 1048.9 | 45.1 |
| gpt-oss-120b | 2 | agentic 8192/512 c4 | 15 | 1 | 19693 | 34760 | 80.1 | 35.4 | 990.1 | 12.4 |
| Gemma 4 26B-A4B | 1 | short 512/256 c1 | 8 | 0 | 454 | 1755 | 16.2 | 53.6 | 1476.9 | 61.0 |
| Gemma 4 26B-A4B | 1 | short 512/256 c4 | 32 | 0 | 2819 | 3168 | 31.3 | 97.4 | 1567.0 | 31.7 |
| Gemma 4 26B-A4B | 1 | agentic 8192/512 c1 | 4 | 0 | 5437 | 6635 | 18.4 | 33.8 | 1573.3 | 54.2 |
| Gemma 4 26B-A4B | 1 | agentic 8192/512 c4 | 0 | 16 | — | — | — | — | 1487.6 | — |
| Gemma 4 26B-A4B | 2 | short 512/256 c1 | 8 | 0 | 443 | 1402 | 16.2 | 54.4 | 1525.5 | 61.1 |
| Gemma 4 26B-A4B | 2 | short 512/256 c4 | 32 | 0 | 2775 | 2946 | 31.2 | 99.0 | 1671.4 | 31.8 |
| Gemma 4 26B-A4B | 2 | agentic 8192/512 c1 | 4 | 0 | 5227 | 6338 | 18.1 | 34.5 | 1635.1 | 54.8 |
| Gemma 4 26B-A4B | 2 | agentic 8192/512 c4 | 0 | 16 | — | — | — | — | 1550.1 | — |
| gemma-4-E4B | 1 | short 512/256 c1 | 8 | 0 | 428 | 496 | 25.4 | 37.0 | 1767.4 | 39.0 |
| gemma-4-E4B | 1 | short 512/256 c4 | 32 | 0 | 2261 | 2374 | 29.6 | 107.3 | 1930.7 | 33.5 |
| gemma-4-E4B | 1 | agentic 8192/512 c1 | 4 | 0 | 4242 | 5105 | 27.4 | 27.7 | 2035.8 | 36.3 |
| gemma-4-E4B | 1 | agentic 8192/512 c4 | 0 | 16 | — | — | — | — | 1921.7 | — |
| gemma-4-E4B | 2 | short 512/256 c1 | 8 | 0 | 388 | 394 | 25.5 | 37.1 | 1795.2 | 38.9 |
| gemma-4-E4B | 2 | short 512/256 c4 | 32 | 0 | 2105 | 2199 | 29.5 | 109.5 | 1993.1 | 33.6 |
| gemma-4-E4B | 2 | agentic 8192/512 c1 | 4 | 0 | 4126 | 4856 | 27.3 | 28.0 | 2057.4 | 36.4 |
| gemma-4-E4B | 2 | agentic 8192/512 c4 | 0 | 16 | — | — | — | — | 1936.3 | — |

- **The Gemmas' agentic cell at 4 streams fails, as on b11572.** All 16 requests on both Gemmas
  in both runs ended `Context size has been exceeded.`: four requests of about 8.2k prompt and 512
  out need about 34.9k tokens of their 32768 unified KV.
- **gpt-oss-120b's one errored request is the client's.** In baseline 2's agentic cell at 4
  streams, one request (2026-10-18 19:20:40-19:21:06 UTC) failed in GuideLLM with
  `JSONDecodeError('Unterminated string starting at: line 1 column 64 (char 63)')` after 247
  streamed tokens: GuideLLM couldn't parse a streamed chunk and dropped the connection. The
  router's journal shows the server side of it, `http client error: Connection handling
  canceled` and `cancel task, id_task = 81092` at 19:21:06, after 320 tokens; the cell's other
  15 requests each generated their 512. It is a client parse fault, not the server's.
- **Run to run.** Every completed cell's output and engine generation rates agree within 4%
  between runs; prefill rates and TTFT medians within 10%. b11715 against b11572's table above:
  gpt-oss-120b's short cells are within 3% (41.7 and 41.6 tok/s against 42.7 at one stream), and
  the agentic 4-stream cell held 36.6 and 35.4 tok/s against 36.8.

Prefix reuse, an 8192-token prefix then four warm requests, as above:

| Model | Run | Cold `prompt_n` | Cold TTFT ms | Warm `cache_n`, least | Warm `prompt_n`, most | Warm TTFT median ms |
|---|---|---|---|---|---|---|
| gpt-oss-120b | 1 | 8210 | 10278 | 8256 | 16 | 255 |
| gpt-oss-120b | 2 | 8210 | 10047 | 8256 | 16 | 252 |
| Gemma 4 26B-A4B | 1 | 8220 | 5444 | 8204 | 18 | 205 |
| Gemma 4 26B-A4B | 2 | 8220 | 5218 | 8204 | 18 | 205 |
| gemma-4-E4B | 1 | 8220 | 4153 | 8204 | 18 | 194 |
| gemma-4-E4B | 2 | 8220 | 4102 | 8204 | 18 | 187 |

Cold load wasn't rerun in the deep stage: it needs the architect's restart, so each deep stage
records it not run. The baseline's cold-load figures are the move to b11715's, taken on
2026-10-17 with `evaluate coldload -set a`
([`llama-cpp-builds.md`](../../devices/strix-halo/llama-cpp-builds.md), "b11669 to b11715"):
18.0 and 17.0 s warm, 20.8 and 21.0 s cold, until all four models were loaded, gpt-oss-120b last.


## Set A's floors at N on b11734

Capacity now puts each role model's own slots in flight, N from its profile entry, and judges
each of the N against the role's floor at N (`evaluation/floors.toml`). The agent's N streams
are the agentic shape, an 8192-token prompt and 512 out, in place of the short nonce request
the b11715 baseline sent. One set run screen (`-set a`) on `b11734-f3fb2fcdb`, 2026-10-19 21:11
UTC, every model at its 4 slots, ran admission, fitness and capacity, and was interrupted once
quality began. Every floor at N passed, and no request ran out of context:

| Role | Floor at N | Floor | N = 4, slowest | Each of the 4 |
|---|---|---|---|---|
| agent | decode of each agentic stream | ≥ 10 tok/s | 10.15 | 11.21, 10.15, 17.59, 15.17 |
| vision | each image question, 4 at once | ≤ 10 s | 5.22 | 5.22 each |
| audio | RTFx of each transcription | ≥ 1 | 3.10 | 3.24, 3.24, 3.19, 3.10 |
| embedding | each 32-token query, 4 at once | ≤ 250 ms | 24.2 | 23.5, 24.2, 23.8, 11.5 |

The agent's streams prefilled 8296-8299 prompt tokens each and decoded 425, 506, 505 and 512
tokens. Their slowest stream, at 10.15 tok/s, clears its floor by 1.5%: each stream decodes while
the others prefill their 8k prompts, where the b11715 baseline's short requests decoded at
21.3-21.9. Set A's agent model at 4 slots is at its floor in the agentic shape; the sweep that chooses
each model's slots starts from here. Fitness alone, the same run: decode 52.2 tok/s, 8k
prefill 1046 tok/s, TTFT 8.02 s, one image question 1.86 s, RTFx 12.3, throughput 18,556 tok/s,
a 32-token query 28.0 ms.

The pool, against the 2816 MiB floor: 3506 MiB free idle after the reload, 3068 at the warmed
peak, and **3001** at capacity's peak, the least free reading of every role's run (31 readings
over the agent's), 67 MiB under the warmed peak. The capacity stage took 81 s for the four models.
