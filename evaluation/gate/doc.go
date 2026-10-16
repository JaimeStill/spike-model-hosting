// Package gate runs evaluate's gated stages for one model of a profile's set against the router:
// admission, fitness, capacity, quality, set and deep, in that order, stopping at the first that
// fails. Admission, fitness and capacity are built, and quality for the embedding and audio
// roles; set and deep, and quality for the other roles, are named stages a run reports as not
// built yet, which later work fills in.
//
//   - Admission judges the model on paper, from the profile and the router's build: licence and
//     origin recorded (failing only when missing), the set's roles declared, the profile's
//     context within the card's, the set's memory budget leaving the pool's floor free, and the
//     router's build at or above the profile's minimum.
//   - Fitness smokes each role the set assigns the model with the probe's request for it (a
//     tool call, an image, an audio clip, an embedding), measures the role's single-stream
//     floors from llama.cpp's timings or the client's clock, and reads the pool idle. The audio
//     clip goes as the quality stage's ASR task sends one: the card's ASR prompt first, the clip
//     after, at temperature 0 with reasoning off, the request capacity's audio streams send too.
//   - Capacity puts the profile's slots of requests in flight on the model per role, through
//     the concurrent package's run: no crossed nonce, the embedding tolerance held, the pool's
//     floor free at the peak, and the role's 4-stream floor.
//   - Quality runs the embedding role's MTEB tasks (EmbeddingTasks: STSBenchmark,
//     Banking77Classification, SciFact, NFCorpus), each in full, through the uv project
//     evaluation/suites, whose encoder sends the router's /v1/embeddings with EmbeddingGemma 2's
//     task prompts. Each task's main score is judged against its bar, by task name, in
//     evaluation/bars.toml, and recorded, never failing, without one; early stop doesn't apply
//     to a continuous score. Each check records MTEB's version, the prompts the encoder applied,
//     and the task's wall time.
//   - Quality runs the audio role's ASR sets (AudioSets: the Open ASR Leaderboard's English
//     LibriSpeech test-clean and test-other, VoxPopuli, AMI and Earnings-22, and FLEURS English),
//     150 seeded utterances each, the tier lists in evaluation/tiers, through the suites project's
//     Inspect AI task, asr_quality.py, which sends each clip with the card's ASR prompt through
//     Inspect's openai-api provider, one at a time. Each set's corpus WER (every word error over
//     every reference word, after the leaderboard's English normalizer) is judged against its
//     ceiling, by tier name (asr-<set>), under [bars_at_most] in evaluation/bars.toml, and
//     recorded, never failing, without one. A ceiling also stops the set early: after the
//     screening fifth, once the one-sided 95% lower bound on the WER (a ratio estimator's,
//     utterance-clustered) is above it. Each check records the RTFx (the audio's seconds over the
//     requests'), the utterances, errors and words, the wall time, the dataset's revision, the
//     normalizer's source and Inspect's release, and, for a set the model's card reports, the
//     card's figure beside ours.
//
// Every request carries autoload=false; a run refuses a model the router doesn't list loaded
// before any stage, and never loads one.
//
// The API:
//   - Runner runs the stages, judging quality by its Bars (package tier's) and keeping MTEB's
//     output in its Dir; Runner.Run returns the Report, and resumes from a named stage by
//     reusing a prior run's passed results for the stages before it. Runner.Roles are the roles
//     the set assigns the model. ErrNotLoaded marks a refused run.
//   - Stages and the Stage* constants name the stages; Roles and the Role* constants name the
//     roles, RoleAgent being the profile's chat role (ProfileRole, ParseRole).
//   - Floors are the per-role floors, data in evaluation/floors.toml, which LoadFloors reads.
//   - Report, StageResult and Check are a run's record, each check its measured value beside
//     the floor it was judged against; Ceiling compares the measured decode with the speed the
//     host's memory bandwidth allows the model, recorded and never gated. WriteReport,
//     ReadReport and WriteSummary write and read it; RunDir and LatestRun place it under the
//     state root, as <state>/evaluate/<UTC time>-<Slug(model)>/report.json.
//   - Footprint reads the pool; SSH reads it with amdgpu_top over ssh, and ParseAmdgpuTop parses
//     that reading.
//   - MTEB runs one MTEB task, and ASR one ASR set's tier; UV runs either through the suites
//     project (SuitesDir) with uv. ReadMTEB reads an MTEB result, MTEB's <task>.json and the
//     encoder's encoder.json, into an MTEBResult, and ReadASR an Inspect JSON log into an
//     ASRResult, whose WER and RTFx are the set's. MTEBVersion and InspectVersion are the MTEB
//     and Inspect AI the project pins (package evaluation/suites). AudioSets name the ASR sets,
//     ASRTier a set's tier, and TiersDir where the tier lists are.
//   - Media are the image and audio clip the stages send, the probe's (LoadMedia); WAVSeconds
//     is a clip's length.
package gate
