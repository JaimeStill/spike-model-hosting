// Package gate runs evaluate's gated stages for one model of a profile's set against the router:
// admission, fitness, capacity, quality, set and deep, in that order, stopping at the first that
// fails. Admission, fitness and capacity are built; quality, set and deep are named stages a run
// reports as not built yet, which later work fills in.
//
//   - Admission judges the model on paper, from the profile and the router's build: licence and
//     origin recorded (failing only when missing), the set's roles declared, the profile's
//     context within the card's, the set's memory budget leaving the pool's floor free, and the
//     router's build at or above the profile's minimum.
//   - Fitness smokes each role the set assigns the model with the probe's request for it (a
//     tool call, an image, an audio clip, an embedding), measures the role's single-stream
//     floors from llama.cpp's timings or the client's clock, and reads the pool idle.
//   - Capacity puts the profile's slots of requests in flight on the model per role, through
//     the concurrent package's run: no crossed nonce, the embedding tolerance held, the pool's
//     floor free at the peak, and the role's 4-stream floor.
//
// Every request carries autoload=false; a run refuses a model the router doesn't list loaded
// before any stage, and never loads one.
//
// The API:
//   - Runner runs the stages; Runner.Run returns the Report, and resumes from a named stage by
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
//   - Media are the image and audio clip the stages send, the probe's (LoadMedia); WAVSeconds
//     is a clip's length.
package gate
