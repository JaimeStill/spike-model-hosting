# The model sets' memory budgets on the Framework, and swapping between them

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

The binding test is measured: at least the pool floor, 2816MiB (2.75GiB,
`evaluation/floors.toml`), free with every model loaded and 4 requests in flight on each. The 85-90% planning estimate (personal-agents'
`reference/model-selection.md`) budgets weights plus KV; set A's estimate is 90.9% of the pool,
just over it, and the set passes the measured test on both builds. On b11529 the first run,
with the 26B-A4B at `c = 65536` (18084MiB), measured 95425MiB for the set and 2669MiB (2.61GiB)
free, and failed it.

A candidate set swaps gpt-oss-120b and the 26B-A4B (82878MiB measured) out for its main model.
"The candidate sets, idle, b11669" below measures both candidate sets.

## During the concurrent run, b11572

With Pi, Claude Code and the model client streaming at once on b11572, 3.21GiB of the pool stayed
free, above the pool floor. The read is the same as above, taken during a live run of the `concurrent`
command (`go run ./evaluation/cmd/evaluate concurrent -rounds 10`) on b11572 (`b11572-efe3626b1`) on
2026-10-12: Pi, Claude Code and the model client on gpt-oss-120b's 4 slots, and the model client's
embedding stream on EmbeddingGemma 2, all at once. The run passed: 40 chat requests and 1032
embeddings in 51.7 seconds. `evaluation/scripts/footprint.sh` read `amdgpu_top -p` over a
non-interactive ssh eight times, six seconds apart, from 16:44:51 to 16:45:35 UTC. `/slots` showed 3
or 4 of gpt-oss-120b's slots processing at the first seven samples, and none at the last, after the
run ended. EmbeddingGemma 2's batches finish in about 350 ms, so only one sample caught its 4 slots
busy. The two Gemmas carry no stream in this run and sat idle. Every sample read the same figures,
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

The pool kept 470MiB more than the 2816MiB floor. The set is 93MiB larger than the synthetic
figure, and the Gemmas account for 79 of it. They grew during the baseline's grid
([`baseline.md`](baseline.md)), run just before, which sent both 8192-token prompts: read
during its gpt-oss-120b cells, before their own, they were 17388 and 9955. The concurrent run
itself moved nothing between samples; gpt-oss-120b, the model it sends to, is 12MiB over its
synthetic figure.

## The candidate sets, idle, b11669

Each candidate set loaded beside gemma-4-E4B and EmbeddingGemma 2 on b11669
(`b11669-620d2225c`) on 2026-10-15, swapped in by `hosting/cmd/swap` (below), read with
`evaluation/scripts/footprint.sh -set <set> <host> 3 5`. All 4 slots of every model were idle at
all three samples, which read the same figures. Each read follows the set's probe and a few more
requests, so the main model's KV and compute buffers had been used, but nothing was in flight.
System RAM is `free -m` on the host just after. Figures in MiB:

| | Set mistral | Set nemotron | Set a, after the swap back |
|---|---|---|---|
| Main model | Mistral Small 4 `UD-Q4_K_XL` + mmproj `BF16`: 75800 | Nemotron 3.5 Super VL `NVFP4` + mmproj `Q8_0`: 76630 | gpt-oss-120b + 26B-A4B (see above) |
| gemma-4-E4B `Q8_0` + mmproj `Q8_0` | 9942 | 9942 | |
| EmbeddingGemma 2 `Q8_0` | 1828 | 1828 | |
| **set, VRAM + GTT** | 87570 | 88400 | 94632 |
| pool used, with the desktop | 87763 | 88598 | 94847 |
| **estimate** (`devices/strix-halo/profile.toml`) | 88826 | 88039 | |
| **pool free, of 98304** | **10541 (10.29GiB)** | **9706 (9.48GiB)** | **3457 (3.38GiB)** |
| system RAM used, of 128081 (`free -m`) | 96816 | 102372 | 103588 |
| system RAM available | 31265 | 25709 | 24493 |

Both candidates fit with room: each leaves over 9GiB of the pool free at idle, more than three
times the 2.75GiB floor, where set A leaves 3.38GiB. The pool used lands within 1.2% of each estimate:
Mistral 1063MiB under its 88826, Nemotron 559MiB over its 88039. Both estimates left the main
model's compute buffers out.

The budget's `measured_mib` is the pool used with Slots requests in flight on every model
(`profile.MemoryBudget`), so these idle figures don't go into the profile: each candidate keeps
its estimate until the 4-in-flight run measures it. The mistral set's run won't happen on
b11669, since Mistral Small 4 is disqualified at fitness
([`tool-reliability.md`](tool-reliability.md), "Mistral Small 4: disqualified at fitness"), so
its idle row is the set's only measurement.

System RAM is not the pool. Nemotron's instance held 5549MiB resident (`ps` RSS) after its
probe, where its context checkpoints live (`ctx-checkpoints = 8` caps them; its
`args_reason`); Mistral's held about 310MiB. `free -m`'s used figure includes more than these
processes, so read it beside the pool, not as a sum of them.

## Swapping sets, b11669

`go run ./hosting/cmd/swap <set>` swaps the router over its API, unloads first, and times each step
(the `swap` package). The three swaps of 2026-10-15, on b11669, in the order run. Each
candidate's first load downloaded its weights through the router's own Hugging Face fetch;
the router log separates the download from the load, the child's `load_model: loading model`
line marking where the download ended:

| Swap | Unloads | Load | Of which download | Of which load | Total |
|---|---|---|---|---|---|
| a to mistral | 26B-A4B 1.0s, gpt-oss-120b 1.0s | Mistral Small 4: 2152.0s | 74.16GB in 2129s (35m29s), 34.8MB/s | 23s | 2154.0s |
| mistral to nemotron | Mistral Small 4 1.0s | Nemotron 3.5 Super: 2778.0s | 77.89GB in 2754s (45m54s), 28.3MB/s, one connection drop retried at 19:43 | 23s | 2779.0s |
| nemotron to a | Nemotron 3.5 Super 1.0s | gpt-oss-120b 16.0s, 26B-A4B 6.0s | none, both cached | | 23.0s |

gemma-4-E4B and EmbeddingGemma 2 are in every set and stayed loaded through all three swaps.
With the files cached, a swap is about half a minute: set A back in 23.0s, and each
candidate's own load from the cache 23s by the router log, so a cached swap to either candidate
would take about 25s. A first swap to a candidate is bound by the download.

The Framework keeps only models that passed on it, so on 2026-10-19 `screen` deleted Mistral
Small 4's cached files (70G), with align's retired-model directory (132G) beside the cache
([the Strix Halo runbook](../../devices/strix-halo/README.md), "The models the host keeps").
The router's API couldn't delete them, since a preset's model isn't removable; the architect's
command did. Nemotron 3.5 Super's files (77.89GB, the table above) were deleted on 2026-10-23,
at `answer`, by the architect's redirect: it measured 12.1 tok/s on b11669, under the floor, and
the profile no longer holds either candidate's set.

## Gemma 4 26B-A4B's growth under vision use, b11669

Set A held 2968-2990MiB free in three set-stage runs, under a 3072MiB pool floor, because Gemma 4
26B-A4B grows under vision use and holds what it grows until it is reloaded. The growth is a high-water
mark, not a leak: the image encoder's working buffers size up from their warm-up size to the largest
image batch the model has encoded, and Vulkan's scratch buffers to the largest operation it has run.
Each step happens once and holds. Two settings bound part of it. Neither could be tested live
before it was installed: the router takes no arguments on a load, and a scratch preset needs a
root edit.

### What was measured

Each step below is one controlled load on b11669 (`b11669-620d2225c`) on 2026-10-17, sent
through the router's API with the other three models of set A loaded and idle. The 26B-A4B was
reloaded over the API (`/models/unload`, `/models/load`) before each sequence. Each reading
is `amdgpu_top -p` for the 26B-A4B's process: VRAM + GTT, and the "Requested" split between
device-local and host-visible memory. Beside it are the pool's free memory and the process's
RSS from `ps`. Images are V*Bench's (craigwu/vstar_bench, 1500-3300px a side); a 512-token image
is one at the server's cap; requests ran at temperature 0 with 32-64 tokens out. Figures in MiB:

| After | 26B-A4B, VRAM + GTT | Change from fresh | Requested device-local / host-visible | Pool free | RSS |
|---|---|---|---|---|---|
| a fresh load | 17336-17337 | | 16522 / 814 | 3241 | 208 |
| one image of 255 tokens (912x576), under the warm-up size | 17380 | +44 | 16533 / 847 | 3205 | 293 |
| one 512-token image | 17538-17549 | +202-213 | 16677 / 861 | 3041-3047 | 398-600 |
| more single images, one at a time or 4 in flight | 17538-17550 | no further change | | | |
| two images of the same size in one prompt, the first images it saw | 17873-17874 | +537 | 17004 / 869 | 2716-2718 | 498-513 |
| two images of different sizes (2258x1500, 2505x1500) in one prompt, the first images it saw | 17549 | +213, as one image | 16685 / 864 | 3041 | 600 |
| 4 prompts of 6027 text tokens at once, before any image | 17457 | +120 | 16628 / 830 | 3128 | 1856 |
| the same text after the vision steps | 17963 | +80-90 over them | 17093 / 870 | 2623 | 3442 |
| further multi-image, single-image and concurrent prompts | 17963 | no further change | | | 5750-7504 |

The set stage's tier 1 sends single images (the probe and V*Bench), so it reaches the single-image
level plus the text step: about +300MiB, 17600-17640. That matches the growth seen in the set runs. The
set run of 2026-10-17 (14:04Z) read 3241MiB free at its fresh start, while the agent's tier ran
alone, and 2990MiB from the vision role's tier on, through the rest of the stage. A model that sees
multi-image prompts (MMMU's, or a user's) reaches +627MiB: 17963, with 2623MiB of the pool free.

### Which buffer

Each candidate is ruled in or out by the evidence:

- **The image encoder's compute buffer, ruled in.** The mmproj's graph is reserved at load for a
  warm-up image of 256 tokens. That is fixed for Gemma 4's projector "to avoid OOM on warmup"
  (`tools/mtmd/clip.cpp`, `set_warmup_n_tokens(256)`, at b11669). The server lets each image
  reach 512 tokens: the log's `cap image_max_tokens (original=1120) to 512 (n_ubatch = 512)`.
  It also encodes consecutive images that resize to the same patch grid in one batch, up to
  `--mtmd-batch-max-tokens` (default 1024; `encode_mtmd_chunk` in
  `tools/server/server-context.cpp`). An image under 256 tokens grows the model 44MiB. The first
  512-token image grows it a further ~160MiB, and the first two-image batch (~980 tokens) a further
  ~330MiB, so the growth scales with the encoder's batch tokens past the warm-up size. Two images
  of different sizes in one prompt are encoded one at a time and grow the model only as one image
  does. Once the largest batch has run, nothing grows: the scheduler reallocates the buffer
  larger and never shrinks it.
- **Vulkan's scratch buffers, ruled in, the smaller part.** ggml-vulkan's `prealloc_x`, `prealloc_y` and
  `prealloc_split_k` grow to the largest dequantization or split-k operation a graph runs, and
  are never freed (`ggml_vk_preallocate_buffers`). Its host-visible staging grows the same way.
  The host-visible share rose 814 to 870MiB. The text-only step, +120MiB on the first full
  512-token ubatches, is this kind of growth in the language model's backend: the server's
  warm-up decodes a couple of tokens, not a full ubatch.
- **The language model's graph re-reservation, ruled out.** Every task's timings in the router log
  read `graph reallocs = 0 / 0 (same size) / (changed size)`, image tasks included.
- **KV and slots, ruled out.** The unified KV for the 4 slots is allocated whole at load.
- **The prompt cache (`--cache-ram`, `--cache-reuse`), ruled out.** It lives in system RAM. RSS rose
  from 208MiB to 7.5GiB over the steps, under its 8192MiB default, while the pool figure stood
  still. The log's `making room for prompt cache entry` lines are its evictions.
- **The multimodal embedding cache, ruled out as a pool cost.** Image embeddings are host-side
  copies of each batch's output, in RSS with the prompt cache.
- **Allocator fragmentation, ruled out.** The figures repeat to within 2MiB across the six fresh
  loads, and repeated loads add nothing once the largest batch has run.

### What caps it, and what it would cost

- **`mtmd-batch-max-tokens = 512`** caps the multi-image step (+324-335MiB), since it keeps each
  image in a batch of its own. The model still sees the same tokens, one image at a time. Its
  cost, read from the log: a two-image prompt's prefill took 3120-3162ms batched (999 tokens)
  and 3151ms unbatched (1006 tokens), within 1%. It leaves the single-image step.
- **`image-max-tokens = 256`** (or `-ub 256`, which caps images to the same through the server's
  ubatch check) would keep the encoder within its warm-up reservation, and cap the single-image
  step too. It halves the tokens each image gets. V*Bench asks about small details in images of
  1500-3300px, so its cost is the vision score, which only a run would measure.
- **Nothing caps Vulkan's scratch growth** short of the `GGML_VK_*` environment of the router's
  unit, a root change. None of the variables found reduces it.

Neither setting was tested. `POST /models/load` takes the model's name and no arguments
(`post_router_models_load` in `tools/server/server-models.cpp`). A `--models-dir` entry,
which the user could add, loads with only the global section's options. So a scratch preset
needs a section in the root-owned `/etc/llama-router/models.ini`, which is the architect's to add.

### The settings chosen

The profile sets `mtmd-batch-max-tokens = 512` on Gemma 4 26B-A4B, which takes the multi-image
step off at no measured cost, and the pool floor is 2816MiB (2.75GiB), with `c` kept at 32768.
With the batch cap, the model's worst case is bounded at the single-image level, about fresh +
300MiB (17640), leaving about 2940MiB free with the other three idle: under 3072, and about
120MiB above 2816. The batch cap installed with the move to b11715
([`llama-cpp-builds.md`](../../devices/strix-halo/llama-cpp-builds.md), "b11669 to b11715").

The options rejected:

- **`c = 16384` on the 26B-A4B, keeping a 3072MiB floor.** Its 5 full-attention layers' KV at
  20,480 bytes a token would fall from 640 to 320MiB (an estimate by the method above), but the
  4 slots would share 16k tokens against MMMU's 4096-token answers.
- **`image-max-tokens = 256`.** It halves each image's detail, which is V*Bench's subject.
- **A reload in the procedure, before the set stage.** The growth happens within the set stage,
  on its first V*Bench image, so a reload before the stage only resets the figure the stage
  then grows again.

## The pool over a day of runs, b11715

Set A's free pool fell run after run through baseline 1, because each model keeps the largest
working memory it has needed until it is reloaded, and the runs follow one another without one.
The figures are fitness's and the set stage's own readings (`amdgpu_top -p` over ssh) on
`b11715-9d1878a43`, 2026-10-17 to 2026-10-18; set A was loaded fresh at the move to b11715 and
not reloaded until the last row. Figures in MiB free, of 98304:

| Reading | When (UTC) | Pool free |
|---|---|---|
| set A loaded fresh, after the move ([`llama-cpp-builds.md`](../../devices/strix-halo/llama-cpp-builds.md)) | 10-17 22:53 | 3509 |
| set run, the least free while every role's tier 1 ran at once | 10-17 23:16-23:33 | 3149 |
| agent run, fitness idle | 10-17 23:33 | 3132 |
| vision run, fitness idle | 10-18 14:30 | 3026 |
| vision run, set stage's least free | 10-18 15:15-15:51 | 3018 |
| audio run, fitness idle | 10-18 15:57 | 2932 |
| audio run, set stage's least free | 10-18 16:09-16:15 | 2925 |
| embedding run, fitness idle: **fails the 2816 floor** | 10-18 16:21 | **2778** |
| after unloading and reloading set A over the router's API | 10-18 16:55 | 3509 |

Each model's process against its fresh figure in the table at the top, read idle just after the
failed embedding run (`amdgpu_top -p`, VRAM + GTT, MiB):

| Model | Fresh | After the day's runs | Growth |
|---|---|---|---|
| gpt-oss-120b | 65517 | 65606 | +89 |
| Gemma 4 26B-A4B | 17337 | 17742 | +405 |
| gemma-4-E4B | 9950 | 10139 | +189 |
| EmbeddingGemma 2 | 1872 | 1869 | none |
| pool used, with the desktop | 94795 | 95526 | +731 |

The mechanism is the high-water mark above, not a leak. The 26B-A4B's +405 is past the
single-image bound of about +300 that the 2816 floor was sized for: on top of its image work, the
deep stage's 8192-token prompts (the GuideLLM grid's agentic cells, whose four-stream cell fails
on context after prefilling, and prefix reuse's 8192-token prefix) grow the language model's
Vulkan scratch buffers on their first full ubatches, as the text-only step above did. The largest
single fall, 2925 to 2778 between the audio run's set stage and the embedding run, is the audio
run's deep stage, the same prompts on gemma-4-E4B. The desktop's share (Hyprland, quickshell and
Xwayland, up since September) didn't change.

So a model's verdict depended on where it ran in the day. Every model run, and the set run, now
starts from freshly loaded models ([`evaluation.md`](../evaluation.md), "Before each run"). Before each run the four
models are unloaded and loaded again over the router's API (`POST /models/unload`, then
`POST /models/load` for gpt-oss-120b, the 26B-A4B, gemma-4-E4B and EmbeddingGemma 2, each polled
on `GET /models`), the run waits 30 s with every model loaded and idle, and the idle pool is
recorded. Each of the six reloads took 29-35 s to unload and load (gpt-oss-120b's load was 16-17
s of it), and every one came back to 94795 MiB used, 3509 MiB free, the fresh figure to the MiB.
Within one run the growth is bounded by what that run sends, which the set stage's reading
catches. Read just before each of baseline 2's reloads, one run's growth alone left the pool
above the floor:

| Before reloading for | After | Pool free |
|---|---|---|
| baseline 2's set run | baseline 1's embedding run (itself after a reload) | 3410 |
| baseline 2's agent run | baseline 2's set run | 3146 |
| baseline 2's vision run | baseline 2's agent run, its deep stage included | 3287 |
| baseline 2's audio run | baseline 2's vision run, its deep stage included | 3125 |
| baseline 2's embedding run | baseline 2's audio run, its deep stage included | 3264 |

Reordering the runs (which moves the failure) and a lower floor (which chases the growth) were
rejected. A reload still can't help within the set stage, as "The settings chosen" says, but it
stops one run's growth reaching the next.

Whether a set serving all day, with no evaluation's reloads, needs a scheduled reload or a
different floor is left to experiment.ai's spike-model-benchmarks.

## Set A at its warmed peak, b11734

evaluate now reloads the set itself and judges the pool's floor at the warmed peak
([`evaluation.md`](../evaluation.md), "Before each run"). On `b11734-f3fb2fcdb`, 2026-10-19
(20:06 UTC), set A's agent run reloaded the four models in 26 s and, after 30 s idle, read
94798 of 98304 MiB used: **3506 MiB free**, system RAM 104448 MiB used and 23627 available.
The warm-up then sent each generative model an 8192-token prompt (8220-8271 prompt tokens),
the 26B-A4B the tier lists' largest image (V\*Bench 62, 5759x1440), gemma-4-E4B the longest clip
(Earnings-22 4483338/281.wav, 36.4 s) and EmbeddingGemma 2 an 8184-token input. At that warmed
peak the pool read 95236 MiB used: **3068 MiB free**, 438 MiB less than idle and 252 MiB over the
2816 MiB floor, so set A is under its budget at the warmed peak. System RAM read 106139 MiB used
and 21936 available; the model processes' RSS went from 190-217 MiB each to 204 (gpt-oss-120b),
1205 (the 26B-A4B), 486 (gemma-4-E4B) and 210 MiB (EmbeddingGemma 2). Capacity's peak, four
streams on gpt-oss-120b beside an embedding stream, read 3050 MiB free, 18 MiB under the
warmed peak, with gpt-oss-120b's RSS at 874 MiB. The run was interrupted once quality began.
