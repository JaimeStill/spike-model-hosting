# The model pool on Strix Halo, on paper

The open-weight models that could serve each of the Framework's four roles (agent, vision,
audio, embedding) under the spike's rules, and the profiles they could make. It is a paper
survey: nothing here was downloaded, loaded, or benchmarked for this page. It feeds the
experiment's restructure (the goal record's redirect, 2026-10-21), which splits benchmarking
into small elements composed per scenario; benchmarking the pool is left to a later spike.

The survey was taken on 2026-10-19 against llama.cpp b11734 (`b11734-f3fb2fcdb`, upstream
Vulkan x64), the build the Framework serves, from the Hugging Face API, the model cards, and
b11734's `src/models` and `tools/mtmd/models`. It counts 47 models: 34 admissible, 11 excluded
on speed, 2 not servable by the router, and about 14 families excluded on origin.

## How to read the figures

Every figure is marked:

- **est.** is a paper estimate made on 2026-10-19 for b11734, by the method below. An estimate
  added on 2026-10-21, after the survey, is marked **est. b11734** and names its source.
- **HF 2026-10-21** is a GGUF's quant and size as the Hugging Face API listed them on that date,
  added after the survey.
- **meas. b*NNNNN*** was measured on the Framework on that build. Every measured figure is set
  A's, or a candidate set's that the spike already loaded, and comes from the findings:
  [the baseline](findings/baseline.md) (decode, the role floors, set A's scores, b11715),
  [the memory budget](findings/memory-budget.md) (each model's pool use, b11565 and b11669),
  [the slots sweep](findings/slots.md) (b11734), and the build log
  ([`llama-cpp-builds.md`](../devices/strix-halo/llama-cpp-builds.md)).

Sizes are the GGUF files as Hugging Face lists them, in GB; budgets are in GiB of the 96 GiB
pool (98304 MiB). "Router" is whether the router serves the model on b11734: **serves** where
the Framework has served it, **arch** where b11734's sources carry the model's architecture
(named) but it has never been loaded, and otherwise why not.

## The rules it was judged by

- **Origin.** A model of Chinese origin is chosen only when no alternative serves
  ([`STANDARDS.md`](../STANDARDS.md)). Every role has alternatives, so every Chinese-origin model
  is excluded on origin. A model fine-tuned from a Chinese-origin base takes its base's origin,
  so a US publisher's fine-tune of Qwen3 is excluded too.
- **Licence.** Any open model licence is accepted (`STANDARDS.md`): Apache-2.0, MIT,
  OpenMDW-1.1, the NVIDIA Open Model licences, the LFM Open License 1.0, the Llama 3.1 and 4
  Community licences, and the Gemma Terms of Use. This admits the seven models an earlier round
  held as licence-pending (Nemotron 3 Nano, Nemotron 3 Nano Omni, LFM2.5-24B-A2B, LFM2.5-Audio,
  Llama 4 Scout, Ultravox v0.5, EmbeddingGemma 300M).
- **Speed floors** ([`evaluation/floors.toml`](../evaluation/floors.toml)): agent decode at
  least 20 tok/s; vision at most 10 s an image question; audio RTFx at least 5; embedding at
  least 4,300 tok/s. A model whose estimated decode falls under its role's floor is excluded on
  speed, on paper.
- **Router support.** The model's architecture, and its projector for vision or audio, must be
  in the build the router runs.
- **The GGUF's source.** The publisher's own GGUF first, then ggml-org's, then a community
  converter's, named; a community conversion is admissible and marked so.

The profile records the origin as the publisher's (`origin`, with `origin_reason` for a
restricted one); it has no field for a fine-tune's base. Admission
([`evaluation/gate`](../evaluation/gate/admission.go)) judges the recorded origin, so a
fine-tune of a Chinese-origin base has to be entered with its base's origin by hand until a
later task adds a field for it. The licence is recorded and never judged: admission fails one
only when it is missing.

## How the estimates are made

- **Decode ceiling.** The Framework's memory bandwidth, 256 GB/s, over the bytes one token reads:
  active parameters times the file's bits per weight. Set A decodes at 52-73% of its ceiling
  (meas. b11715: gpt-oss-120b 52-54% of 92.5 tok/s, Gemma 4 26B-A4B 55% of 116.4, gemma-4-E4B
  71-73% of 53.4), so an estimated decode is 50-55% of the ceiling for a MoE or dense model.
- **Budget.** Weights, plus the projector, plus the KV cache at the context shown, plus about
  1.5 GiB of compute buffers. The two candidate sets the spike loaded used pools within 1.2%
  of the profile's estimates (meas. b11669), and an embedding model keeps no KV cache in llama.cpp: the slots
  sweep found its pool doesn't move with its slots or context (meas. b11734,
  [`findings/slots.md`](findings/slots.md)), so its budget is its weights and buffers.
- **Headroom.** The pool less the profile's budget and about 0.2 GiB for the desktop. The pool
  floor, 2.75 GiB, must stay inside it at the warmed peak.

## Agent

| Model | Publisher (origin) | Licence | Architecture, active | GGUF, quant, size | Budget | Router | Ceiling → decode (floor 20 tok/s) | Status |
|---|---|---|---|---|---|---|---|---|
| gpt-oss-120b | OpenAI (US) | Apache-2.0 | MoE, 5.1B | ggml-org MXFP4 63.4 GB | 64.0 GiB at c 131072, meas. b11565 | serves | 92.5 → 48.1-50.3 meas. b11715 | incumbent |
| gpt-oss-20b | OpenAI (US) | Apache-2.0 | MoE, 3.6B | ggml-org MXFP4 12.1 GB (eagle3 sidecar) | ~15 GiB at c 131072, est. | arch | ~123 → ~65, est. | admissible |
| Nemotron 3.5 Super VL 120B-A12B | NVIDIA (US) | OpenMDW-1.1 | hybrid Mamba MoE, 12B; vision | ggml-org NVFP4 76.9 GB, Q4_0 71.8 GB (mtp sidecar) | 74.8 GiB idle with mmproj, meas. b11669 | serves | ~38 est. → 12.1 meas. b11669 | admissible; under the floor |
| Nemotron 3.5 Lightning 30B-A3B | NVIDIA (US) | OpenMDW-1.1 | hybrid MoE, ~3B; text | ggml-org Q4_0 18.9 GB (mtp sidecar) | ~21 GiB, est. | arch (nemotron-h) | ~135 → ~70, est. | admissible |
| Trinity Mini 26B-A3B | Arcee AI (US) | OpenMDW-1.1 | MoE 128/8, ~3B; text | arcee-ai Q4_0 15.2 GB | ~18 GiB, est. | arch (afmoe) | ~145 → ~75, est. | admissible |
| Gemma 4 26B-A4B (as agent) | Google (US) | Apache-2.0 | MoE, 3.8B; vision | ggml-org Q4_0 14.6 GB | 17.0 GiB at c 32768, meas. b11565 | serves | 116.4 → 61 meas. b11715 | admissible; loaded as set A's vision model |
| Gemma 4 12B Unified | Google (US) | Apache-2.0 | dense 12B; vision and audio | ggml-org Q4_0 7.2 GB (mtp sidecar) | ~10 GiB at c 32768, est. | arch (gemma4uv, gemma4ua) | ~35 → ~19-24, est. | admissible; borderline |
| Granite 4.2 8B | IBM (US) | Apache-2.0 | dense; text | ibm-granite Q4_0 5.1 GB | ~8 GiB, est. | arch | ~51 → ~27, est. | admissible |
| Granite 4.2 3B | IBM (US) | Apache-2.0 | dense; text | ibm-granite Q4_0 2.1 GB | ~4 GiB, est. | arch | ~120 → ~60, est. | admissible |
| Ministral 3 8B / 3B | Mistral (France) | Apache-2.0 | dense; vision | mistralai Q4_K_M 5.2 / 2.2 GB, BF16 mmproj | ~8 / ~4 GiB, est. | arch (mistral3, pixtral) | ~49 / ~119 → ~26 / ~60, est. | admissible |
| Ministral 3 14B | Mistral (France) | Apache-2.0 | dense; vision | mistralai Q4_K_M 8.2 GB | ~11 GiB, est. | arch | ~31 → ~17, est. | admissible; likely under the floor |
| OLMo 3 7B Instruct | Ai2 (US) | Apache-2.0 | dense | unsloth (community) Q4_0 4.2 GB | ~7 GiB, est. | arch (olmo2) | ~60 → ~31, est. | admissible; tool-call template unverified |
| Phi-4-mini 3.8B | Microsoft (US) | MIT | dense | unsloth (community) Q4_K_M 2.5 GB | ~5 GiB, est. | arch (phi3) | ~103 → ~55, est. | admissible |
| Apriel 1.6 15B Thinker | ServiceNow (US) | MIT | dense; vision | bartowski (community) Q4_0 8.3 GB | ~11 GiB, est. | arch | ~31 → ~17, est. | admissible; likely under the floor |
| Nemotron 3 Nano 30B-A3B | NVIDIA (US) | NVIDIA Open Model | hybrid MoE, ~3B | ggml-org Q4_K_M 22.4 GB | ~25 GiB, est. | arch | ~110, est. | admissible; superseded by Lightning |
| LFM2.5-24B-A2B | Liquid AI (US) | LFM Open 1.0 | MoE 24B, 2.45B active (the card) | LiquidAI Q4_K_M 14.5 GB; ggml-org's Q4_K_M the same, HF 2026-10-21 | ~16 GiB, est. | arch (lfm2moe) | ~172 → ~86-95, est. b11734 (2.45B active at Q4_K_M's 4.85 bits a weight) | admissible |
| Llama 4 Scout 109B-A17B | Meta (US) | Llama 4 Community | MoE, 17B; vision | unsloth (community) Q4_0 61.2 GB | ~66 GiB, est. | arch | ~27 → ~14, est. | excluded: under the floor, est. |
| Devstral Small 2, Mistral Small 3.2, Magistral Small (24B); OLMo 3.1 32B; Granite 4.2 30B; Gemma 4 31B; Muse Glimmer 30B | US, France | Apache-2.0 | dense 24-32B | Q4_0 13.5-18.3 GB | 15-21 GiB, est. | arch | 14-19, est. | excluded: a dense ceiling under the floor |
| Mistral Small 4 | Mistral (France) | Apache-2.0 | MoE | unsloth (community) UD-Q4_K_XL | 74.0 GiB with mmproj, meas. b11669 | falls back to the CPU on Vulkan (llama.cpp #30105) | 3.36 meas. b11669 | excluded: failed fitness ([baseline](findings/baseline.md)); its files are deleted |
| Qwen 3.x, GLM-5.x, DeepSeek V4, MiMo, Kimi, MiniMax, Hunyuan, LongCat | China | | | | | | | excluded: origin |

## Vision

Set A's vision model answers an image question in 1.90 s (meas. b11715) against the 10 s floor;
a MoE or small dense model clears it on paper, a dense model of 24B or more is borderline.

| Model | Publisher (origin) | Licence | Architecture, active | GGUF, quant, size | Budget | Router | Against the floor | Status |
|---|---|---|---|---|---|---|---|---|
| Gemma 4 26B-A4B | Google (US) | Apache-2.0 | MoE, 3.8B | ggml-org Q4_0 14.6 GB + Q8_0 mmproj 0.81 GB | 17.0 GiB, meas. b11565 | serves | 1.90 s meas. b11715 | incumbent |
| gemma-4-E4B | Google (US) | Apache-2.0 | dense, 4B effective | ggml-org Q8_0 8.0 GB + mmproj 0.56 GB | 9.7 GiB, meas. b11565 | serves | ceiling 53.4, est. | admissible; loaded as set A's audio model |
| gemma-4-E2B | Google (US) | Apache-2.0 | dense, 2B effective | ggml-org Q8_0 5.0 GB + mmproj 0.56 GB | ~6.5 GiB, est. | arch | ceiling ~105, est. | admissible; the smallest Gemma 4 |
| Gemma 4 12B Unified | Google (US) | Apache-2.0 | dense 12B; image and audio | ggml-org Q4_0 7.2 GB + mmproj 0.16 GB | ~10 GiB, est. | arch (gemma4uv) | ceiling ~35, est. | admissible |
| Nemotron 3.5 Super VL | NVIDIA (US) | OpenMDW-1.1 | as the agent's | as the agent's | as the agent's | serves | image check passed, meas. b11669 | admissible |
| Granite Vision 4.1 4B | IBM (US) | Apache-2.0 | dense 4B | ibm-granite Q8_0 3.6 GB + f16 mmproj 1.2 GB | ~6 GiB, est. | arch (granite4-vision) | document reading | admissible |
| Ministral 3 3B / 8B / 14B | Mistral (France) | Apache-2.0 | as the agent's | as the agent's | as the agent's | arch (pixtral) | ~1.9 / ~4.5 / ~7.1 s, est. b11734 (Gemma 4 26B-A4B's 1.90 s, meas. b11715, scaled by its 116.4 ceiling over each's) | admissible; the 14B near the floor |
| Apriel 1.6 15B | ServiceNow (US) | MIT | dense 15B | as the agent's | ~11 GiB, est. | arch | borderline at 10 s, est. | admissible |
| Nemotron 3 Nano Omni 30B-A3B | NVIDIA (US) | NVIDIA Open Model Agreement | hybrid MoE, ~3B; chat, image, audio, video | ggml-org Q4_K_M 22.4 GB + Q8_0 mmproj 1.7 GB | ~26 GiB, est. | arch (nemotron-omni) | ceiling ~110, est. | admissible |
| Mistral Small 3.2 24B, Gemma 4 31B, Llama 4 Scout | US, France | Apache-2.0, Llama 4 Community | dense 24-31B; MoE 17B | | 15-66 GiB, est. | arch | ceiling 14-27, est. | excluded: a dense ceiling's speed |
| GLM-4.5V, Qwen3-VL, MiniCPM-V, Hunyuan-VL, dots, DeepSeek OCR | China | | | | | | | excluded: origin |

## Audio (transcription through chat audio input)

| Model | Publisher (origin) | Licence | Architecture | GGUF, quant, size | Budget | Router | Ceiling (floor RTFx 5) | Status |
|---|---|---|---|---|---|---|---|---|
| gemma-4-E4B | Google (US) | Apache-2.0 | dense, 4B effective | ggml-org Q8_0 8.0 GB + mmproj 0.56 GB | 9.7 GiB, meas. b11565 | serves (gemma4a) | RTFx 12.0 meas. b11715 | incumbent |
| gemma-4-E2B | Google (US) | Apache-2.0 | dense, 2B effective | ggml-org Q8_0 5.0 GB | ~6.5 GiB, est. | arch | ~105 tok/s, est. | admissible |
| Gemma 4 12B Unified | Google (US) | Apache-2.0 | dense 12B | as the agent's | ~10 GiB, est. | arch (gemma4ua) | ~35 tok/s, est. | admissible |
| Voxtral Mini 3B 2507 | Mistral (France) | Apache-2.0 | dense 3B, Whisper encoder | ggml-org Q4_K_M 2.5 GB + Q8_0 mmproj 0.72 GB | ~4.5 GiB, est. | arch (whisper-enc) | ~104 tok/s, est. | admissible |
| Granite Speech 4.1 2B | IBM (US) | Apache-2.0 | dense 2B | ibm-granite Q8_0 1.96 GB + f16 mmproj 1.16 GB, HF 2026-10-21 (cstr's community q4_k is 2.94 GB) | ~4 GiB, est. | arch (granite-speech) | high, est. | admissible; the publisher's GGUF |
| Nemotron 3 Nano Omni | NVIDIA (US) | NVIDIA Open Model Agreement | as the vision row's | as the vision row's | ~26 GiB, est. | arch | ~110 tok/s, est. | admissible |
| Ultravox v0.5 (Llama 3.1 8B) | Fixie (US) | MIT adapter on the Llama 3.1 Community base | dense 8B | ggml-org Q4_K_M 4.9 GB + mmproj 1.4 GB | ~7.5 GiB, est. | arch | ~52 tok/s, est. | admissible |
| LFM2.5-Audio 1.5B | Liquid AI (US) | LFM Open 1.0 | dense 1.5B (LFM2 1.2B base) | LiquidAI Q8_0 1.25 GB + Q8_0 mmproj 0.29 GB (its tokenizer and vocoder, 0.28 GB, for audio out), HF 2026-10-21 | ~3 GiB, est. | unverified | ~200 tok/s, est. b11734 (the 1.2B LM at Q8_0's 8.5 bits a weight) | admissible; router support unverified |
| Voxtral Small 24B 2507 | Mistral (France) | Apache-2.0 | dense 24B | bartowski (community) Q4_0 13.5 GB + 1.4 GB | ~16 GiB, est. | arch | ~19 tok/s, RTFx likely under 5, est. | excluded: speed, est. |
| Voxtral Mini 4B Realtime | Mistral (France) | Apache-2.0 | dense 4.4B (Ministral 3 3B base), streaming | ggml-org Q4_K_M 2.55 GB + Q8_0 mmproj 1.06 GB, HF 2026-10-21 | | the server part isn't merged (llama.cpp #30629) | | not servable |
| Parakeet TDT 0.6B v3 | NVIDIA (US) | CC-BY-4.0 | | whisper.cpp format only | | no llama.cpp GGUF | | not servable |
| Qwen3-ASR, Qwen3-Omni, MiMo-Audio | China | | | | | | | excluded: origin |

## Embedding

The quality bars equal EmbeddingGemma 2's own scores. Every admissible model is under 600M
parameters, far above the 4,300 tok/s floor on paper (est.); EmbeddingGemma 2 measured 17,975
tok/s (meas. b11715), 18,542 tok/s in the slots sweep's set run (meas. b11734,
[`findings/slots.md`](findings/slots.md)), and 15,984 tok/s at 16 slots (meas. b11734).

The throughput column, est. b11734, scales EmbeddingGemma 2's 18,542 tok/s (Q8_0, fitness's
batch of 16 inputs of 500 tokens) by its 271M parameters over each model's, active parameters
for a MoE: an encoder's batch is compute-bound, so its tokens a second fall with the weights each
token passes through. It ignores the quant and how each architecture's kernels run on Vulkan.

| Model | Publisher (origin) | Licence | Architecture, active | Context | GGUF, quant, size | Budget | Router | Throughput (floor 4,300 tok/s) | Status |
|---|---|---|---|---|---|---|---|---|---|
| EmbeddingGemma 2 | Google (US) | Apache-2.0 | dense encoder, 271M | 8192 | ggml-org Q8_0 0.29 GB | 1.8 GiB, meas. b11565 | serves | 18,542 meas. b11734 | incumbent |
| harrier-oss-v1-270m | Microsoft (US; Gemma 3 270M base, Google) | MIT | dense encoder (Gemma 3), 270M | 32768 | mykor (community) Q8_0 0.29 GB | ~1.8 GiB, est. | arch (gemma-embedding) | ~18,600, est. b11734 | admissible; its card publishes MTEB prompts |
| nomic-embed-text-v2-moe | Nomic (US) | Apache-2.0 | MoE encoder, 475M, 305M active | 512 | ggml-org Q8_0 0.51 GB | ~1 GiB, est. | arch (nomic-bert-moe) | ~16,500, est. b11734 | admissible; its 512 context truncates NFCorpus |
| nomic-embed-text-v1.5 | Nomic (US) | Apache-2.0 | dense encoder (nomic-bert), 137M | 8192 | nomic-ai Q8_0 0.15 GB | ~1.5 GiB, est. | arch (nomic-bert) | ~36,700, est. b11734 | admissible |
| granite-embedding-english-r2 / small-r2 | IBM (US) | Apache-2.0 | dense encoder (ModernBERT), 149M / 47M | 8192 | mradermacher (community) Q8_0 0.16 / 0.05 GB, HF 2026-10-21 | ~1.5 GiB, est. | arch (modern-bert) | ~33,700 / ~107,000, est. b11734 | admissible; community conversion |
| snowflake-arctic-embed-l-v2.0 | Snowflake (US; XLM-R base, Meta) | Apache-2.0 | dense encoder (XLM-RoBERTa large), 568M | 8192 | Casual-Autopsy (community, the most downloaded) Q8_0 0.63 GB, HF 2026-10-21 | ~2.5 GiB, est. | arch (bert) | ~8,800, est. b11734 | admissible; community conversion |
| mxbai-embed-large-v1 | Mixedbread (Germany) | Apache-2.0 | dense encoder (BERT large), 335M | 512 | mixedbread-ai F16 0.67 GB; ChristianAzinn (community, the most downloaded) Q8_0 0.36 GB, HF 2026-10-21 | ~1 GiB, est. | arch (bert) | ~15,000, est. b11734 | admissible; 512 context |
| EmbeddingGemma 300M (v1) | Google (US) | Gemma Terms of Use | dense encoder (Gemma 3), 300M | 2048 (its card) | ggml-org Q8_0 0.33 GB | ~1.8 GiB, est. | arch | ~16,700, est. b11734 | admissible |
| harrier-oss-v1-27b; e5-mistral-7b | Microsoft (US) | MIT | dense decoder (Gemma 3; Mistral), 27B; 7.1B | 131072; 32768 (config.json) | Abiray (community) Q8_0 28.7 GB / Q4_K_M 16.6 GB; second-state (community) Q8_0 7.7 GB / Q4_K_M 4.4 GB; the most downloaded, HF 2026-10-21 | 18 / 6 GiB, est. | arch | ~190; ~700, est. b11734 | excluded: throughput, est. |
| voyage-4-nano; harrier-oss-v1-0.6b; jina-embeddings-v5-text-small | Voyage AI, Microsoft, Jina (Qwen3 base) | | dense decoder (Qwen3), 0.35B; 0.6B; 0.6B | 40960; 32768; 32768 (config.json) | jsonMartin (community) Q8_0 0.37 GB; mradermacher (community) Q8_0 0.64 GB; jinaai's own (v5-text-small-retrieval) Q8_0 0.64 GB; the most downloaded, HF 2026-10-21 | | | not estimated | excluded: origin, by their Chinese-origin base |
| Qwen3-Embedding, bge, gte | China | | | | | | | | excluded: origin |

## The model shapes evaluate supports

The pool holds shapes beyond set A's one model per role; evaluate takes each of these as it is:

- **One model in several roles.** Gemma 4 12B Unified, Nemotron 3 Nano Omni, and gemma-4-E4B
  serve more than one role; a set's `roles` maps one model to several.
- **Per-model prompts.** A model's card prompts sit in its profile entry (`prompts`): an
  embedding model's MTEB task prompts, such as harrier-oss-v1-270m's, and the audio role's
  transcription instruction.
- **A context under 8192.** nomic-embed-text-v2-moe and mxbai-embed-large-v1 take 512 tokens;
  the warm-up cuts its 8192-token prompt to a smaller `c`.
- **System RAM that grows with slots.** A hybrid Mamba model keeps context checkpoints in system
  RAM, outside the pool (Nemotron 3.5 Super's instance held 5549 MiB resident, meas. b11669);
  the footprint reports system RAM beside the pool.
- **Speculative-decoding sidecars.** Many ggml-org repositories carry mtp-, eagle3-, or dflash-
  draft weights beside the main GGUF (gpt-oss-20b, the Nemotron 3.5 models, Gemma 4 12B
  Unified). They are noted and not used: no profile loads a draft model.
- **An embedding model's budget.** It holds no KV cache in llama.cpp (meas. b11734,
  [`findings/slots.md`](findings/slots.md)), so its slots and context don't move its budget.

## Profiles, from the most quality to the most headroom

Sketches on paper, for b11734: none is in the profile. Each gives its roles as agent / vision /
audio / embedding, and headroom is the pool less the budget and the desktop's ~0.2 GiB.

| Profile | Agent / vision / audio / embedding | Budget | Headroom | Trade |
|---|---|---|---|---|
| A · max quality, near the pool (set A) | gpt-oss-120b / Gemma 4 26B-A4B / gemma-4-E4B / EmbeddingGemma 2 | 92.5 GiB, meas. b11565 | 3.3 GiB, meas. b11565; 3.0 GiB at the warmed peak, meas. b11734 | the strongest agent; 4 streams decode at 21-22 tok/s each (meas. b11715). Variant: Nemotron 3.5 Super VL as agent and vision, 9.5 GiB free idle (meas. b11669), but 12.1 tok/s, under the floor |
| B · strong agent, one small multimodal model | gpt-oss-120b / Gemma 4 12B Unified (vision and audio) / EmbeddingGemma 2 | ~76 GiB, est. | ~20 GiB, est. | headroom for more agent sessions; the 12B's vision and audio quality untested |
| C · mid-size MoE agent | gpt-oss-20b or Nemotron 3.5 Lightning / Gemma 4 26B-A4B / gemma-4-E4B / EmbeddingGemma 2 | ~44-50 GiB, est. | ~46-52 GiB, est. | a faster agent (~65-70 tok/s, est.) that has to clear set A's BFCL bars |
| D · two generative models | Gemma 4 26B-A4B (agent and vision, c 131072) / gemma-4-E4B / EmbeddingGemma 2 | ~31 GiB, est. | ~65 GiB, est. | the agent's quality and harness fit are untested; room for many sessions |
| E · small models, large headroom | Granite 4.2 8B or Ministral 3 8B / gemma-4-E4B (vision and audio) / EmbeddingGemma 2; or Gemma 4 12B Unified for all three generative roles | ~12-20 GiB, est. | ~76-84 GiB, est. | the quality bars likely bind on BFCL, MMMU, and V\*Bench |
| G · omni | Nemotron 3 Nano Omni (agent, vision, audio) / EmbeddingGemma 2 | ~28 GiB, est. | ~68 GiB, est. | one generative model for three roles, admissible under the NVIDIA Open Model Agreement |
