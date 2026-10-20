# Per-model slots: the sweep

`evaluate sweep` ([`../evaluation.md`](../evaluation.md), "The sweep") chooses one model's slots
and context, the rest of its set loaded and idle at their own settings. This page records each
sweep's table and the N it chose. A chosen N is a finding until a commit puts it in the profile;
set A's are committed (below, "Set A, swept whole").

## Set A, swept whole, b11734, 2026-10-20

`TMPDIR=.state/tmp go run ./evaluation/cmd/evaluate sweep -set a`, build b11734-f3fb2fcdb: the set
sweep, each model in role order (agent, vision, audio, embedding) against the settings chosen
before it, the models after it at their committed ones. After each sweep its chosen slots and c
were written into `devices/strix-halo/profile.toml` and that render installed and verified before
the next sweep began. The sweep started from render `b987489d` (every model at 4 slots).

| Model | Role | Target (tokens) | Held | Knee | Chosen N | Bound | c | Render installed after |
|---|---|---|---|---|---|---|---|---|
| gpt-oss-120b | agent | 8806 | 4 | 2 | **2** | knee | 17664 | `5cf55a89` |
| Gemma 4 26B-A4B | vision | 5172 | 8 | none | **8** | floor | 41472 | `6e97fdda` |
| gemma-4-E4B | audio | 1956 | 16 | none | **16** | top | 31488 | `5a9b534c` |
| EmbeddingGemma 2 | embedding | 8192 | 16 | none | **16** | top | 8192 (held) | `6643c687` |

The last render, sha256 `6643c687c39a48ea518fab9c47e63aaa9ac842573419abaca2d75c340a87083e`, is the
committed profile's, installed on the Framework (`sha256sum /etc/llama-router/models.ini` reads
the same; no temporary file left), and `/props` reports each model at its committed slots and
`n_ctx`. The probe passed after (`mise run integration`).

**Set A supports 2 concurrent agent sessions on the Strix Halo**: gpt-oss-120b's chosen N. Not yet
confirmed by the set stage, which `screen`'s slice 9 runs.

### gpt-oss-120b (agent)

| N | c | result | slowest stream decode (floor 10) | total decode | TTFT p50 / p95 | warmed pool free | capacity peak free | system RAM used / available |
|---|---|---|---|---|---|---|---|---|
| 1 | 8960 | held | 44.49 tok/s | 44.49 tok/s | 8.09 / 8.09 s | 7610 MiB | 7587 MiB | 101470 / 26605 MiB |
| 2 | 17664 | held | 22.81 tok/s | 50.61 tok/s | 14.25 / 16.58 s | 7286 MiB | 7237 MiB | 101904 / 26170 MiB |
| 4 | 35328 | held | 10.16 tok/s | 54.34 tok/s | 24.52 / 33.76 s | 6621 MiB | 6554 MiB | 102715 / 25359 MiB |
| 6 | 52992 | failed floor (bisection) | 6.10 tok/s | 54.05 tok/s | 33.68 / 52.62 s | 5953 MiB | 5848 MiB | 103329 / 24746 MiB |
| 8 | 70656 | failed floor | 3.96 tok/s | 54.73 tok/s | 41.34 / 69.24 s | 5285 MiB | 5147 MiB | 103848 / 24227 MiB |

**Chosen: N = 2, bound by the knee.** 4 held, its slowest stream 10.16 tok/s against the 10 tok/s
floor, as the baseline's 10.15 did; 6 and 8 failed the floor. But total decode gained 13.7% from 1
to 2 and 7.4% from 2 to 4, under the 10% knee, so the chosen N is the smaller, 2. The decode
is bandwidth-bound: total throughput flattens near 54 tok/s from 4 on, and each further slot only
divides it. The pool never bound (over 5 GiB free at every step): c = N x 8806 is far smaller than
the 131072 gpt-oss-120b ran at before. Its pool is unified, so one session alone may use all
17664 tokens, but with both slots busy each has room for the 8806-token target.

**The first attempt, and why it was rerun.** The sweep's first run, the same day, sized c by a
nominal agent target, 8192 in and 512 out (8704 a session):

| N | c | result | slowest stream decode (floor 10) | TTFT p50 / p95 | warmed pool free | capacity peak free | system RAM used / available |
|---|---|---|---|---|---|---|---|
| 1 | 8704 | held | 45.57 tok/s | 8.07 / 8.07 s | 7619 MiB | 7596 MiB | 101706 / 26369 MiB |
| 2 | 17408 | failed context | - (both streams 500, "Context size has been exceeded") | - | 7296 MiB | 7246 MiB | 101882 / 26193 MiB |

Capacity's agentic request is an 8192-token passage with the chat template and the nonce and
summary ask around it: sent once to gpt-oss-120b, the router counted `prompt_n` 8294. Two
sessions of 8294 + 512 overflow a unified pool of 2 x 8704. The run was stopped after it had
committed N = 1; the profile was reverted and its render restored and verified. The agent target
is now measured as the vision and audio targets were, by the router's count of the request
itself: 8294 in, 512 out (`evaluation/targets.toml`), and the sweep was rerun from the start.

### Gemma 4 26B-A4B (vision), beside gpt-oss-120b at 2

| N | c | result | slowest image answer at N (floor 10 s) | total decode | TTFT p50 / p95 | warmed pool free | capacity peak free | system RAM used / available |
|---|---|---|---|---|---|---|---|---|
| 1 | 5376 | held | 1.84 s | 58.03 tok/s | 0.36 / 0.36 s | 8502 MiB | 8502 MiB | 100593 / 27481 MiB |
| 2 | 10496 | held | 3.09 s | 100.03 tok/s | 0.63 / 0.63 s | 8171 MiB | 8170 MiB | 101026 / 27049 MiB |
| 4 | 20736 | held | 5.00 s | 144.84 tok/s | 1.11 / 1.12 s | 7545 MiB | 7545 MiB | 101594 / 26480 MiB |
| 8 | 41472 | held | 9.30 s | 173.59 tok/s | 2.06 / 2.06 s | 6257 MiB | 6253 MiB | 102806 / 25269 MiB |
| 12 | 62208 | failed floor (bisection) | 12.56 s | 207.94 tok/s | 3.13 / 3.14 s | 4997 MiB | 4997 MiB | 103450 / 24625 MiB |
| 16 | 82944 | failed floor | 15.53 s | 240.17 tok/s | 4.03 / 4.04 s | 3742 MiB | 3737 MiB | 105330 / 22745 MiB |

**Chosen: N = 8, bound by the floor**: at 12 the slowest of the image questions took 12.6 s, over
the 10 s floor. Not capped at the agent's N (the architect's decision): the vision model takes the
N its own sweep holds.

### gemma-4-E4B (audio), beside gpt-oss-120b at 2 and Gemma 4 26B-A4B at 8

| N | c | result | slowest stream RTFx (floor 1) | total RTFx | TTFT p50 / p95 | warmed pool free | capacity peak free | system RAM used / available |
|---|---|---|---|---|---|---|---|---|
| 1 | 2048 | held | 4.52x | 4.52x | 0.24 / 0.24 s | 6896 MiB | 6896 MiB | 101664 / 26411 MiB |
| 2 | 4096 | held | 3.96x | 7.99x | 0.37 / 0.37 s | 6830 MiB | 6830 MiB | 101834 / 26241 MiB |
| 4 | 7936 | held | 3.21x | 13.07x | 0.63 / 0.63 s | 6697 MiB | 6697 MiB | 102040 / 26035 MiB |
| 8 | 15872 | held | 2.24x | 18.45x | 1.14 / 1.16 s | 6466 MiB | 6466 MiB | 102202 / 25872 MiB |
| 16 | 31488 | held | 1.39x | 22.67x | 1.77 / 2.17 s | 6016 MiB | 6014 MiB | 102765 / 25310 MiB |

**Chosen: N = 16, bound by the sweep's last step**, as the slice-7 sweep below found beside the
old settings; total RTFx still gained 23% from 8 to 16.

### EmbeddingGemma 2 (embedding), beside the three at their chosen settings

| N | c | result | slowest 32-token query at N (floor 250 ms) | total throughput | query p50 / p95 | warmed pool free | capacity peak free | system RAM used / available |
|---|---|---|---|---|---|---|---|---|
| 1 | 8192 | held | 10.2 ms | 3136 tok/s | 0.01 / 0.01 s | 6016 MiB | 6016 MiB | 102794 / 25280 MiB |
| 2 | 8192 | held | 16.1 ms | 5408 tok/s | 0.01 / 0.02 s | 6016 MiB | 6016 MiB | 102831 / 25244 MiB |
| 4 | 8192 | held | 21.6 ms | 7900 tok/s | 0.02 / 0.02 s | 6016 MiB | 6016 MiB | 102877 / 25197 MiB |
| 8 | 8192 | held | 25.3 ms | 11896 tok/s | 0.03 / 0.03 s | 6015 MiB | 6015 MiB | 102915 / 25160 MiB |
| 16 | 8192 | held | 36.5 ms | 15984 tok/s | 0.04 / 0.04 s | 6015 MiB | 6015 MiB | 102852 / 25223 MiB |

**Chosen: N = 16, bound by the sweep's last step.** c stays 8192 at every step: llama.cpp keeps
no KV memory for an embedding model, so the sweep holds its committed c and sweeps the slots
alone ([`../evaluation.md`](../evaluation.md), "The sweep"). The pool doesn't move with its
slots.

### The confirming set run

`TMPDIR=.state/tmp go run ./evaluation/cmd/evaluate -set a -fresh`, every model at its chosen
setting, interrupted once the quality stage began. Admission, fitness and capacity passed:

| Model | Fitness | Capacity at its N |
|---|---|---|
| gpt-oss-120b | decode 49.9 tok/s, 8k prefill 1038 tok/s, TTFT 8.08 s | slowest of 2 streams 24.2 tok/s (floor 10) |
| Gemma 4 26B-A4B | image answer 1.86 s | slowest of 8 image answers 8.41 s (floor 10 s) |
| gemma-4-E4B | RTFx 12.2 | slowest of 16 streams 1.41x (floor 1) |
| EmbeddingGemma 2 | 18542 tok/s, 32-token query 11.4 ms | slowest of 16 queries 35.8 ms (floor 250 ms) |

- **The pool.** Idle after the reload, 6463 MiB free; at the warmed peak, 6015 MiB; at capacity's
  peak, 5965 MiB, the least of any role's. Each is well above the 2816 MiB floor, which is
  unchanged.
- **Sessions supported.** The report states `sessions supported: 2, ggml-org/gpt-oss-120b-GGUF:MXFP4's
  slots; not confirmed: the run didn't reach the set stage`. The set stage that confirms it is
  `screen`'s slice 9.
- An earlier run of the same, without `-fresh`, read the same figures within 1-4% (agent at N
  24.6 tok/s, vision 8.72 s, audio 1.41x, embedding 40.2 ms; capacity's peak 5965 MiB free). It
  wrote no sessions line: an interrupted run then left it out, which is now fixed.

## gemma-4-E4B alone, set A, b11734, 2026-10-19

`go run ./evaluation/cmd/evaluate sweep -set a -role audio`, build b11734-f3fb2fcdb, the audio
role alone (set A assigns gemma-4-E4B nothing else). Its per-session target is the audio role's,
1956 tokens: the ASR tier lists' longest clip, 932 tokens with the ASR prompt, and the task's
1024 out (`evaluation/targets.toml`). The card's context, 131072, never capped a step. Committed
setting: 4 slots, c = 32768.

| N | c | result | slowest stream RTFx (floor 1) | total RTFx | TTFT p50 / p95 | warmed pool free | capacity peak free | system RAM used / available |
|---|---|---|---|---|---|---|---|---|
| 1 | 2048 | held | 4.46x | 4.46x | 0.24 / 0.24 s | 3707 MiB | 3707 MiB | 104808 / 23267 MiB |
| 2 | 4096 | held | 4.03x | 8.13x | 0.36 / 0.37 s | 3641 MiB | 3640 MiB | 104964 / 23110 MiB |
| 4 | 7936 | held | 3.13x | 13.00x | 0.62 / 0.62 s | 3504 MiB | 3504 MiB | 105142 / 22933 MiB |
| 8 | 15872 | held | 2.32x | 18.83x | 1.11 / 1.11 s | 3277 MiB | 3272 MiB | 105698 / 22377 MiB |
| 16 | 31488 | held | 1.40x | 22.70x | 2.19 / 2.20 s | 2824 MiB | 2823 MiB | 106239 / 21835 MiB |

**Chosen: N = 16, bound by the sweep's last step.** Every step held: every stream's RTFx over
the 1x floor at N, no request out of context, the pool's floor (2816 MiB) held. The knee wasn't
reached: total RTFx still gained 21% from 8 to 16. No step failed, so there was no bisection.

- **The pool is what comes next.** At 16 the warmed peak left 2824 MiB free, 8 MiB over the
  floor: each doubling cost the set's pool 66-453 MiB (E4B's KV, 0.5 GiB at c = 32768, scales
  with c), so a step past 16 would likely fail on the pool before the RTFx floor.
- **TTFT** is llama.cpp's prompt evaluation of the probe clip with the ASR prompt, the clip's
  encoding with it; it doubles with N, as the N clips encode one after another.
- **The other models stayed at their own settings.** Each step's `/props` read gpt-oss-120b at
  4 slots and c = 131072, Gemma 4 26B-A4B at 4 and 32768, EmbeddingGemma 2 at 4 and 8192; only
  gemma-4-E4B's section changed, so each reload unloaded only it. Each step reloaded and warmed
  the whole set, as a run does, and took 88-112 s; the sweep took about 8 minutes.
- **The restore.** The sweep ended by installing the committed render, sha256
  `591c0ddbf57c04d3af6576dc8f951801d4c3d6a852dedd92b2d75a08be6f40b8`; `sha256sum
  /etc/llama-router/models.ini` on the Framework read the same, and every model of set A served
  at its committed setting again (gemma-4-E4B at 4 slots, c = 32768). The directory held no
  temporary file after.
- **The probe** passed after the restore (`mise run integration`, set a, b11734).
- **Capacity's requests are the probe clip**, the short timed transcription capacity sends, not
  the target's 36.4 s clip: the step's c gives each of the N sessions room for the target, and the
  context check holds every one of the N answered within it.
