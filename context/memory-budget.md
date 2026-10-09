# Set A's memory budget on the Framework

Set A, all four loaded on llama.cpp b11529 (the upstream Vulkan x64 build) in the Framework's
96GiB GTT pool (98304MiB), with 4 requests in flight on each model. Footprints are `VRAM + GTT`
per process from `amdgpu_top -p`. The canonical table, with the 0- and 1-in-flight samples, is
personal-agents' `reference/memory-footprint.md`, "Set A, measured". Figures in MiB:

| Model | `c` | Weights | KV at `c`, estimated | Compute buffers / other | Estimated | Measured |
|---|---|---|---|---|---|---|
| gpt-oss-120b `MXFP4` | 131072 | 60444 | 4608 | 455 | 65052 | 65507 |
| Gemma 4 26B-A4B `Q4_0` + mmproj `Q8_0` | 32768 | 14696 | 640 | 2035 | 15336 | 17371 |
| gemma-4-E4B `Q8_0` + mmproj `Q8_0` | 32768 | 8183 | 512 | 1272 | 8695 | 9967 |
| EmbeddingGemma 2 `Q8_0` | 8192 | 282 | — | 1587 | 282 | 1869 |
| **set A** | | 83605 | 5760 | 5349 | 89365 (87.27GiB) | 94714 (92.49GiB) |
| pool used, with the desktop's ~210MiB | | | | | | 94926 |
| **pool free, of 98304** | | | | | | **3378 (3.30GiB)** |

Weights are the ggml-org GGUF file sizes, mmproj included (63.38GB; 14.61 + 0.80GB; 8.03 +
0.55GB; 296MB). KV at `c` is bytes per token × `c` (36,864, 20,480, and 16,384 bytes per
token), by personal-agents' `reference/context-sizing.md`; the 26B-A4B's figure counts only its
full-attention layers, and EmbeddingGemma 2 has none. Compute buffers / other is measured minus
weights minus KV: the compute buffers, the 26B-A4B's sliding-window caches (window 1024), and
EmbeddingGemma 2's 8192-token batch (`b = ub = 8192`). The KV pool is shared across the 4 slots
(`--kv-unified`): no model grew more than 17MiB from 0 to 4 in flight.

The binding test is measured: at least 3GiB of the pool free with every model loaded and 4
requests in flight on each. The 85-90% planning estimate (personal-agents'
`reference/model-selection.md`) budgets weights plus KV; set A's estimate is 90.9% of the pool,
just over it, and the set passes the measured test. The first run, with the 26B-A4B at
`c = 65536` (18084MiB), measured 95425MiB for the set and 2669MiB (2.61GiB) free, and failed it.

The Mistral Small 4 swap takes gpt-oss-120b and the 26B-A4B out (82878MiB measured) and puts
Mistral Small 4 in, about 69GiB at `Q4`. That figure is estimated, not measured.
