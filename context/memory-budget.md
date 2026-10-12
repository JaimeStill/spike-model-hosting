# Set A's memory budget on the Framework

The table measures set A with all four models loaded on llama.cpp b11565 (`b11565-f9a95aeb2`,
the upstream Vulkan x64 build) in the Framework's 96GiB GTT pool (98304MiB), with 4 requests in
flight on each, on 2026-10-12. Beside it is the prior measurement, on b11529
(`b11529-8ae386707`) in `align`. Footprints are `VRAM + GTT` per process from `amdgpu_top -p`;
pool used is its header's `GTT used`. The canonical b11529 table, with the 0- and 1-in-flight
samples, is personal-agents' `reference/memory-footprint.md`, "Set A, measured". Figures in
MiB:

| Model | `c` | Weights | KV at `c`, estimated | Compute buffers / other, b11565 | Estimated | Measured, b11565 | Measured, b11529 |
|---|---|---|---|---|---|---|---|
| gpt-oss-120b `MXFP4` | 131072 | 60444 | 4608 | 465 | 65052 | 65517 | 65507 |
| Gemma 4 26B-A4B `Q4_0` + mmproj `Q8_0` | 32768 | 14696 | 640 | 2036 | 15336 | 17372 | 17371 |
| gemma-4-E4B `Q8_0` + mmproj `Q8_0` | 32768 | 8183 | 512 | 1255 | 8695 | 9950 | 9967 |
| EmbeddingGemma 2 `Q8_0` | 8192 | 282 | — | 1590 | 282 | 1872 | 1869 |
| **set A** | | 83605 | 5760 | 5346 | 89365 (87.27GiB) | 94711 (92.49GiB) | 94714 (92.49GiB) |
| pool used, with the desktop | | | | | | 94926 | 94926 |
| **pool free, of 98304** | | | | | | **3378 (3.30GiB)** | **3378 (3.30GiB)** |

On b11565, `/slots` showed all 4 slots of every model processing at each of four samples, ten
seconds apart, which read the same figures: 1500-token chat completions on the three
generative models, and 64-input embedding requests on EmbeddingGemma 2. With none in flight,
the four measured 94630 and the pool had 3463MiB free. No model moved more than 17MiB between
the builds, and the pool used is the same to the MiB.

Weights are the ggml-org GGUF file sizes, mmproj included (63.38GB; 14.61 + 0.80GB; 8.03 +
0.55GB; 296MB). KV at `c` is bytes per token × `c` (36,864, 20,480, and 16,384 bytes per
token), by personal-agents' `reference/context-sizing.md`; the 26B-A4B's figure counts only its
full-attention layers, and EmbeddingGemma 2 has none. Compute buffers / other is measured minus
weights minus KV: the compute buffers, the 26B-A4B's sliding-window caches (window 1024), and
EmbeddingGemma 2's 8192-token batch (`b = ub = 8192`). The KV pool is shared across the 4 slots
(`--kv-unified`): from 0 to 4 in flight, no model grew more than 17MiB on b11529, and on
b11565 none of the generative models did and EmbeddingGemma 2 grew 38MiB.

The binding test is measured: at least 3GiB of the pool free with every model loaded and 4
requests in flight on each. The 85-90% planning estimate (personal-agents'
`reference/model-selection.md`) budgets weights plus KV; set A's estimate is 90.9% of the pool,
just over it, and the set passes the measured test on both builds. On b11529 the first run,
with the 26B-A4B at `c = 65536` (18084MiB), measured 95425MiB for the set and 2669MiB (2.61GiB)
free, and failed it.

The Mistral Small 4 swap takes gpt-oss-120b and the 26B-A4B out (82878MiB measured) and puts
Mistral Small 4 in, about 69GiB at `Q4`. That figure is estimated, not measured.

## During the concurrent run, b11572

With Pi, Claude Code and the model client streaming at once on b11572, 3.21GiB of the pool
stayed free, above the 3GiB bar. The read is the same as above, taken during a live run of the `concurrent` command
(`go run ./cmd/concurrent -rounds 10`) on b11572 (`b11572-efe3626b1`) on 2026-10-12: Pi, Claude
Code and the model client on gpt-oss-120b's 4 slots, and the model client's embedding stream on
EmbeddingGemma 2, all at once. The run passed: 40 chat requests and 1032 embeddings in 51.7
seconds. `scripts/footprint.sh` read `amdgpu_top -p` over a non-interactive ssh eight times, six
seconds apart, from 16:44:51 to 16:45:35 UTC. `/slots` showed 3 or 4 of gpt-oss-120b's slots
processing at the first seven samples, and none at the last, after the run ended.
EmbeddingGemma 2's batches finish in about 350 ms, so only one sample caught its 4 slots busy.
The two Gemmas carry no stream in this run and sat idle. Every sample read the same figures,
shown beside the synthetic 4-in-flight measurement on b11565 above. Figures in MiB:

| Model | Synthetic, 4 in flight on each, b11565 | During the concurrent run, b11572 |
|---|---|---|
| gpt-oss-120b `MXFP4` | 65517 | 65529 |
| Gemma 4 26B-A4B `Q4_0` + mmproj `Q8_0` | 17372 | 17412 |
| gemma-4-E4B `Q8_0` + mmproj `Q8_0` | 9950 | 9989 |
| EmbeddingGemma 2 `Q8_0` | 1872 | 1874 |
| **set A** | 94711 | 94804 |
| pool used, with the desktop | 94926 | 95018 |
| **pool free, of 98304** | **3378 (3.30GiB)** | **3286 (3.21GiB)** |

The pool kept 214MiB more than the 3GiB bar. The set is 93MiB larger than the synthetic
figure, and the Gemmas account for 79 of it. They grew during the baseline's grid
([`baseline.md`](baseline.md)), run just before, which sent both 8192-token prompts: read
during its gpt-oss-120b cells, before their own, they were 17388 and 9955. The concurrent run
itself moved nothing between samples; gpt-oss-120b, the model it sends to, is 12MiB over its
synthetic figure.
