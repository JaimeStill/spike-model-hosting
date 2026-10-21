// Package gate runs evaluate's gated stages for one model of a profile's set against the router:
// admission, fitness, capacity, quality, set and deep, in that order, stopping at the first that
// fails, so the deep stage runs only once every gate passed. A set run, with no model, takes every
// model of the set through them together.
//
// A run is a screen by default: admission, fitness and capacity, then the quality stage, every
// sampled tier its screening fifth and on until its 95% bound decides it, the roles' tiers at once;
// the set and deep stages are reported not run. A screen that passes every stage makes the model,
// or the set run's models, a finalist. A full run (Runner.Full) continues a finalist's screen: it
// reuses the screen's admission, fitness, capacity and warm-up and every unit the screen ran whole,
// runs the rest of each tier the screen stopped and judges the whole list, then runs the set stage
// and the deep stage. A full run given no screen screens first. A set run's admission, fitness,
// capacity and deep run model by model (each check naming its model), the set's own checks once.
//
//   - Admission judges the model on paper, from the profile and the router's build: licence
//     recorded (failing only when missing), origin recorded and judged by STANDARDS (a model of
//     Chinese origin, profile.OriginChina, fails unless its entry records why it was chosen,
//     its origin_reason), the set's roles declared, the profile's context within the card's,
//     the set's memory budget leaving the pool's floor free, and the router's build at or above
//     the profile's minimum.
//   - Before the first stage after admission, a run that resumes nothing starts from freshly
//     loaded models (Warm): it reloads the set over the router's API (swap.Reload), waits
//     Runner.Settle, reads the idle pool, drives each of the set's models to its high-water mark
//     (an 8192-token prompt to each generative model, the vision role's largest image and the
//     audio role's longest clip in the committed tier lists, which Peaks finds, and the embedding
//     role's largest input, its c less embedReserve) and reads the pool at that warmed peak. Every
//     pool floor is judged at the warmed peak.
//   - Fitness smokes each role the set assigns the model with the probe's request for it (a tool
//     call, an image, an audio clip, an embedding), measures the role's single-stream floors from
//     llama.cpp's timings or the client's clock, and judges the pool at the warmed peak, the idle
//     reading beside it. The audio clip goes as the quality stage's ASR task sends one: the model's
//     ASR prompt, from the profile, first, the clip after, at temperature 0 with reasoning off, the
//     request capacity's audio streams send too. Embeddings go as the MTEB encoder sends them:
//     the smoke sentence and the 32-token query (32 tokens of content) after the model's query
//     prompt, the throughput batch after its document prompt, from the profile.
//   - Capacity puts N requests in flight on the model per role, N its slots from the profile
//     (Runner.slotsOf), through the concurrent package's run, and judges each of the N against
//     the role's floor at N: the agent's streams in the agentic shape (an 8192-token prompt, 512
//     out), each decode at the per-stream floor; the audio role's timed transcriptions, each RTFx
//     at its per-stream floor; then N of fitness's timed image questions or 32-token queries at
//     once, each at fitness's floor. No crossed nonce, the embedding tolerance held, no request
//     out of context (llama.cpp's context-size errors, as the router passes them through), and
//     the pool's floor free at capacity's own peak, beside fitness's warmed one.
//   - Quality runs every role of the run at once, each on its own model, each role's tiers in
//     order: a quality score doesn't depend on speed, which fitness and capacity measured alone.
//     In a screen the pass runs inside the set stage's concurrent window, which it stands in for:
//     the pool's least free memory over it, against the pool's floor, and rounds of nonce
//     requests to every role's model of the set, no answer crossed (both as the set stage judges
//     them, below). A model run's pass is its own roles' tiers, the set's other models loaded and
//     answering only the crossing rounds. Early stop: a sampled tier (a BFCL or vision benchmark,
//     an ASR set) runs its list's screening fifth whole, then stops failed once its one-sided 95%
//     bound falls on the wrong side of its bar, or passed once the bound clears it, and runs on,
//     to its list's end at most, while the bounds straddle the bar; the check records the stop
//     (Stop): its side, its bound and the samples run. A full run sends the samples the screen
//     stopped as a list of their own, the tier's rest/list.txt, without a bar, and judges the whole
//     list.
//   - Quality runs the agent role's tiers (AgentTiers), cheapest first, and stops at the first tier
//     that fails. Tier 1 is BFCL's single-turn questions, 100 seeded across its 13 leaderboard
//     categories; tier 2 its multi-turn conversations, 20 seeded across its 4; both inspect_evals'
//     stock bfcl task through the suites project's agent_quality.py, Inspect AI's openai-api
//     provider at the model's slots in flight (--slots), at the router's defaults (the card's
//     sampling, the template's reasoning) with parallel tool calls allowed. Each tier's accuracy is
//     judged against its floor, by check name (agent-<tier>), under [bars] in evaluation/bars.toml,
//     and recorded, never failing, without one; a floor stops the tier early on either side as the
//     vision tiers' does. Each check records the samples run and passed, their model requests and
//     working time, the wall time, the data's Gorilla commit, the task's inspect_evals version and
//     Inspect's release. Tier 3 is harness fit: conform's pass^5 through every pinned harness
//     (passk.Harnesses: Pi, Claude Code, OpenCode) on the set's roles (passk.Runner), each held to
//     one absolute bar (passk.Judge): every applicable capability 5/5 and 0 invalid requests, a
//     capability the harness reports n/a excluded and noted. A harness drops at its first failed
//     trial; the tier ends once every harness has dropped or run all five. Each harness's fit is
//     recorded, agent-conform-<harness>: whether it fits, the trials it ran, each cell's passes,
//     its invalid requests, why its first failed trial failed and its wall time. The tier's
//     verdict, agent-conform, fails only when no harness fits. Its recordings and table, the fit
//     profile, go in the run directory's conform/.
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
//     Inspect AI task, asr_quality.py, which sends each clip with the card's ASR prompt, given it
//     as --prompt, through Inspect's openai-api provider, one at a time. Each set's corpus WER
//     (every word error over every reference word, after the leaderboard's English normalizer) is
//     judged against its ceiling, by tier name (asr-<set>), under [bars_at_most] in
//     evaluation/bars.toml, and recorded, never failing, without one. A ceiling also stops the set
//     early: after the screening fifth, failed once the one-sided 95% lower bound on the WER (a
//     ratio estimator's, utterance-clustered) is above it, or passed once the upper bound is at or
//     under it. Each check records the RTFx (the audio's seconds over the requests'), the
//     utterances, errors and words, the wall time, the dataset's revision, the normalizer's source
//     and Inspect's release, and, for a set the model's card reports, the card's figure beside
//     ours.
//   - Quality runs the vision role's tiers (VisionTiers), cheapest first, through the suites
//     project's vision_quality.py, Inspect AI's openai-api provider at the model's slots in flight,
//     and stops at the first tier that fails. Tier 1 is the probe's image and question five times,
//     at the router's defaults, passing on all five (pass^5, whatever the bars hold), and V*Bench,
//     100 seeded questions; tier 2 DocVQA, 150 seeded validation questions; tier 3 MMMU, 200 seeded
//     validation multiple-choice questions across its 30 subjects. The benchmarks are
//     inspect_evals' stock tasks, their samples picked by Inspect's sample-id filtering from the
//     tier lists in evaluation/tiers, at temperature 0 with reasoning off. Each benchmark's score,
//     the accuracy or DocVQA's mean ANLS, is judged against its floor, by check name
//     (vision-<tier>), under [bars] in evaluation/bars.toml, and recorded, never failing, without
//     one. A floor also stops the tier early: after the screening fifth, failed once the one-sided
//     95% Wilson upper bound on the pass rate is under it, a sample passing at a score of 0.5 or
//     more (a mean ANLS is never above that pass rate, so the bound holds for it too), or passed
//     once the Wilson lower bound on the mean score is at or over it. Each check records the
//     samples run and passed, the request and wall time, the dataset's revision, the tasks'
//     inspect_evals versions and Inspect's release, and, for a tier the model's card reports, the
//     card's figure beside ours.
//   - Set, a full run's alone, runs tier 1s (TierOne) on the set's role models, all of them loaded:
//     the agent's BFCL single-turn, the vision probe and V*Bench, the audio role's LibriSpeech
//     test-clean and the embedding role's STSBenchmark, the cheapest of its MTEB tasks, through the
//     quality stage's suites, whole and without their bars. A set run runs each role's alone, role
//     after role, then every role's at once. A model run takes each of its roles' alone figure,
//     score and wall time, from its quality stage's tier 1 records, the same lists at the same
//     settings, and runs only its own roles' tier 1s, at once, while each of the set's other role
//     models carries load for exactly that window: capacity's request for the role, sent one after
//     another by Runner.LoadWorkers workers (one by default: one a role keeps every model resident
//     and interleaving, and memory and crossing don't depend on how hard), started with the model's
//     tiers and stopped when they finish, and recorded as load (requests answered and errored,
//     requests and tokens a second), never as a scored tier. Each unit's score and wall time are
//     recorded, the concurrent one beside the alone one's with its change and slowdown, and each
//     role's tier 1 slowdown, never judged. The stage gates the concurrent phase: the pool's least
//     free memory, read as capacity reads it, against the pool's floor, and no crossed answer, from
//     rounds of capacity's nonce requests sent to every role's model at once, every
//     Runner.CrossEvery, while the tiers run, each answer carrying its own nonce and no other and
//     each embedding vector its own input's (a screen's quality pass judges the same two over its
//     tiers); a request that errored, came back empty, or hit its max_tokens (finish_reason
//     "length") carrying no nonce at all is recorded unanswered, not crossed, so long as each role
//     had one checked; only an answer carrying a nonce not its own is crossed. The vision role's
//     nonce request allows 1024 tokens. A load whose every finished request errored fails it too.
//     Its output is in the run's set/alone and set/concurrent. A model's full run resumed at the
//     set stage needs the quality records it reuses, and is refused without them.
//   - Deep, a full run's alone, records and never judges; a measurement that couldn't be taken
//     fails it, as in every stage. For the agent role it runs τ2 airline and retail (Tau2Domains),
//     12 seeded tasks each (evaluation/tiers/agent-tau2-<domain>.txt), each task Tau2Trials times,
//     through the suites project's tau2_quality.py: inspect_evals' stock τ2 tasks, unforked, the
//     model under test through Inspect's openai-api provider, the simulated user Sonnet through the
//     laptop's pinned Claude Code on the subscription login, claude_code.py's claude-code/sonnet
//     provider (claude -p, tools off, its own system prompt, no session persistence).
//     inspect_evals' airline scorer has no model grader, so the simulator is the only Claude Code
//     use. Each domain records pass^1 and pass^2 (τ-bench's C(c,k)/C(n,k) a task, averaged), the
//     wall and working time, the simulator's calls and the model Claude Code reported for them, and
//     the model's own failed calls; a simulator call that failed past its retries makes the domain
//     a measurement that couldn't be taken, since the stock task carries on with a silent user. The
//     tau2 simulator check rolls the calls and reported models up. For a generative model it runs
//     the GuideLLM grid (GridCells: short 512/256 and agentic 8192/512 at 1 and 4 streams, as
//     evaluation/scripts/baseline.sh does, the prompt cut by the card's tokenizer), each cell's
//     client figures beside the engine's from /metrics; and prefix reuse, a PrefixTokens prefix
//     then four warm requests, as evaluate prefix sends them. Last, cold load: with
//     Runner.Restarts, it asks for two restarts, ColdLoadWarm and then ColdLoadCold, root commands
//     the architect runs on the router's host, and times each with the coldload package's Tracker
//     until every model of the set (the startup set) is loaded again; without it, it is recorded
//     not run, naming the commands. Its output is in the run's deep/. A set run takes each model's
//     τ2, grid and prefix reuse in turn, under the run directory's <Slug(model)>/deep, then cold
//     load once.
//
// The sweep (Sweep) is no stage: it chooses one model's slots and c on the router, the rest of its
// set loaded at their own settings, N through SweepSteps until a step fails, then one bisection.
// Each step's c is N times the model's per-session target (Targets), rounded up to a multiple of
// 256 and capped at the card's context, where a session short of its target fails on context with
// no request; an embedding model's c stays its committed c (llama.cpp keeps it no KV pool), its
// steps sweeping the slots alone. Each other step renders the profile with the override
// (profile.Profile.Override), installs it (Presets), has the router read it again
// (router.ReloadPresets), loads the model again and checks its server runs at the step's setting,
// then warms the set as a run does (Warm, its 8192-token prompt cut to a smaller c) and runs
// capacity on the model at N. A step holds when every floor at N, the pool floor and every
// session's context hold; the chosen N is the smaller of the largest N that held and the knee
// (KneeGain), and the report names which bound. The sweep ends on every exit by restoring the
// committed render, checked by its SHA-256 on the host (Sweep.Restore).
//
// A set sweep (SetSweep) sweeps a set's models one after another in role order (SweepOrder):
// after each model's sweep it writes the chosen N and its step's c into the profile file, with
// reasons citing the sweep (SweepReasons), and installs that render as the committed one, so each
// later model is swept against the settings chosen before it.
//
// Every request carries autoload=false; a run refuses a model the router doesn't list loaded
// before any stage (a run that reloads, any model of the set), and loads only the set it reloads.
// A run resumed from a later stage, or a full run continuing its screen, reloads nothing and keeps
// the warm-up of the run it resumes.
//
// A run given an Index reuses the passed results it holds and records its own. A model's results
// are kept under its ModelKey: the model, the router's build and the SHA-256 of its preset section
// (profile.Profile.PresetSection), with its card's prompts and consumer reasoning beside them. Each
// role's fitness and capacity records of a model are one entry, and so is each quality unit (an
// MTEB task, an ASR set, a vision or BFCL tier), except harness fit, which sends to every role's
// model and is kept under the set's key, a SHA-256 over the set's role assignments and each of its
// models' keys; the set stage of a run (its model, or the set run) is one entry under the set's key
// too. Every entry is kept under the method's key as well, the floors, bars, suites' pins, tier
// lists and probe media, and holds a copy of its checks, so it outlives its run directory. A unit
// whose entry matches is reused, each check naming the run it was measured in (Check.ReusedFrom): a
// screen reuses a full run's entry or else a screen's, and a full run only a full run's. A failed
// unit is never kept. A changed profile thus reruns the roles of the models whose sections changed,
// harness fit and the set stage. Admission, the reload and warm-up, the deep stage and the gates of
// the whole set's pool are measured in every run: fitness's warmed peak, and a screen's quality
// pass's two gates, over the units it runs or, when it reuses them all, one crossing round.
// Capacity's peak, the model's own slots in flight, is kept with its role's record: the warmed
// peak, measured fresh, holds every model's KV, fixed when it loads. A full run continuing a screen
// that reused a stopped tier reads that tier's screen output from the run that measured it.
// Runner.Fresh reuses nothing and still records.
//
// The API:
//   - Runner runs the stages, judging quality by its Bars (package tier's) and the conform tier by
//     harness fit, and keeping the suites' output in its Dir; Runner.Run returns the Report, a
//     screen's or, with Runner.Full, a full run's (ModeScreen, ModeFull), continuing a prior screen
//     that made a finalist, and resumes from a named stage by reusing a prior run's passed results
//     for the stages before it. Runner.Roles are the roles the set assigns the model, or every role
//     for a set run, a Runner with no Model, whose directory is named for SetSubject. ErrNotLoaded
//     marks a refused run. TierOne names a role's tier 1 units, which the set stage runs.
//   - Stages and the Stage* constants name the stages; Roles and the Role* constants name the
//     roles, RoleAgent being the profile's chat role (ProfileRole, ParseRole).
//   - Index is the results index, kept as <state>/evaluate/IndexFile, beside the run
//     directories: LoadIndex reads it, Index.Save writes it, and Runner.Index and Runner.Fresh put
//     a run on it. Each Entry is a passed unit's records under its key; Keys are a run's, in its
//     Report, each of the set's models' a ModelKey.
//   - Floors are the per-role floors, data in evaluation/floors.toml, which LoadFloors reads:
//     the single-stream ones fitness judges, and the floors at N capacity judges each of a model's
//     N requests in flight by.
//   - Sessions is a set run's sessions supported, in its Report: the set's agent model's slots,
//     confirmed when the run's set stage passed with every model at its own slots.
//   - Report, StageResult and Check are a run's record: the report its mode, whether it made a
//     finalist and, for a full run, when its screen started; each check its measured value beside
//     the floor it was judged against (AtLeast or AtMost), in a set run the model it judges, and,
//     for a tier a screen stopped early, its Stop, the side (StopFail or StopPass), bound and
//     samples run; each stage ending StatusPass, StatusFail or StatusNotRun; Ceiling compares the
//     measured decode with the speed the host's memory bandwidth allows the model, recorded and
//     never gated. WriteReport, ReadReport and WriteSummary write and read it; RunDir and LatestRun
//     place it under the state root, as <state>/evaluate/<UTC time>-<Slug(model)>/ReportFile.
//   - Footprint reads the pool, a Pool, with the host's system RAM beside it, a RAM; SSH reads
//     both over ssh, amdgpu_top, free -b and ps, and ParseFootprint parses that reading,
//     ParseAmdgpuTop its pool and ParseRAM its RAM.
//   - Warm is a run's reload and warm-up, its WarmStep and WarmRequest records, the requests
//     named WarmPrompt, WarmImage, WarmClip and WarmInput; Peaks finds a role's Peak media (UV
//     through vision_quality.py peak and asr_quality.py peak), which ReadPeak reads from its
//     PeakFile.
//   - Sweep is the sweep, Sweep.Run's SweepReport its record: each SweepStep with each set
//     model's Setting served, its checks and each role's RoleRate (total throughput and time to
//     first token), then the held N, the knee and the chosen N with its bounds (BoundFloor,
//     BoundPool, BoundContext, BoundKnee, BoundTop), and the Restore that ended it. Sweep.Restore
//     restores alone; a restore that fails wraps ErrRestore. SweepSteps and KneeGain are the
//     steps and the knee's threshold; WriteSweep writes the report as a run directory's SweepFile,
//     named for SweepSubject, and WriteSweepSummary and WriteRestoreSummary write it for a reader.
//   - SetSweep is the set sweep, SetSweep.Run returning each model's SweepReport with the Install
//     of its committed setting; SweepOrder lists the models it sweeps, each a Swept with its roles;
//     ProfileFile is the profile file it commits into, FileProfile the TOML file on disk
//     (profile.SetSetting); SweepReasons are the paragraphs it adds to each reason, beginning
//     SweptMark, which WithSweep puts in place of a reason's earlier one; WriteSetSweepSummary
//     writes it for a reader.
//   - Presets is the router's presets file, which a sweep installs and sums; SSHPresets reaches it
//     over ssh, a temporary file in its directory renamed over it. Targets are the roles'
//     per-session targets, each a Target, data in evaluation/targets.toml, which LoadTargets reads;
//     Targets.Of returns a role's, and Target.Tokens the context one session at it holds.
//   - MTEB runs one MTEB task, ASR one ASR set's tier, Vision one vision tier and Agent one BFCL
//     tier; UV runs each through the suites project (SuitesDir) with uv, each suite but the ASR
//     task (one clip at a time) given its model's slots as --slots. uv, and GuideLLM, start in a
//     process group of their own, killed whole when the run's context is done, so an interrupt
//     leaves none of the suite's Python processes running. ReadTier reads a vision or
//     BFCL tier's Inspect logs into a TierResult, whose Score and Passes are the tier's;
//     VisionTiers and AgentTiers name the tiers (each a VisionTier or AgentTier), VisionCheck and
//     AgentCheck a tier's check, list and bar name, and InspectEvalsVersion is the inspect_evals
//     the project pins. Conform runs the conform tier (passk.Runner); ConformTier names the tier,
//     ConformK its trials and ConformReport its table's file. ReadMTEB reads an MTEB result, MTEB's
//     <task>.json and the encoder's encoder.json, into an MTEBResult, and ReadASR an Inspect JSON
//     log into an ASRResult, whose WER and RTFx are the set's. MTEBVersion and InspectVersion are
//     the MTEB and Inspect AI the project pins (package evaluation/suites). EmbeddingTasks name the
//     MTEB tasks, AudioSets the ASR sets, ASRTier a set's tier, and TiersDir where the tier lists
//     are.
//   - Tau2 runs one τ2 domain, UV through tau2_quality.py, its simulator UV.Claude on the
//     UV.ClaudeConfig login; ReadTau2 reads its summary (Tau2Summary, beside its compressed Inspect
//     log) into a Tau2Result, whose PassK is pass^k. Tau2Domains, Tau2Trials, Tau2Check and
//     Tau2Simulator name the domains, trials, check and list, and simulator. Grid runs one GuideLLM
//     cell, GuideLLM with the pinned executable; ReadGrid reads its JSON into a GridResult;
//     GridCells and GridCell.Name name the cells. PrefixTokens is prefix reuse's prefix;
//     ColdLoadWarm and ColdLoadCold are cold load's root commands.
//   - Media are the image and audio clip the stages send, the probe's (LoadMedia); WAVSeconds
//     is a clip's length. The ASR prompt fitness, capacity and the set stage send with the clip,
//     and UV.ASR passes asr_quality.py, is the profile's (profile.Prompts.ASR), its one source;
//     so are the embedding prompts UV.Task passes mteb_quality.py (profile.Prompts.Embedding),
//     whose query and document prompts fitness, capacity and the set stage prefix to theirs.
package gate
