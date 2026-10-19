# Per-model slots: the sweep

`evaluate sweep` ([`../evaluation.md`](../evaluation.md), "The sweep") chooses one model's slots
and context, the rest of its set loaded and idle at their own settings. This page records each
sweep's table and the N it chose. A chosen N is a finding until a commit puts it in the profile;
`screen`'s slice 8 sweeps all of set A and commits the result.

## gemma-4-E4B, set A, b11734, 2026-10-19

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
