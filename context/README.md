# spike-model-hosting

spike-model-hosting records what llama.cpp's Vulkan router on the Framework desktop (Strix Halo)
establishes for the self-hosted model stack: one host-class profile serving three consumers at
once (Pi, Claude Code, and go-ai's model client), their tool-call reliability, a memory budget on
unified memory, and a method for measuring a model. The spike decides nothing itself.

The spike is the sub-goal `experiment.hosting.spike-model-hosting` in the coordinator's roadmap,
`standards-lab/context/roadmap.toml`, where its tasks live; its goal record is
[`goals/experiment.hosting.spike-model-hosting.md`](goals/experiment.hosting.spike-model-hosting.md).

## The question

What does llama.cpp's Vulkan router on the Framework establish for the self-hosted stack: a
host-class profile, Pi, Claude Code, and go-ai's model client at once, tool-call reliability, a
memory budget on unified memory, and a measurement method?

The answer, its evidence, and the spike that owns each question this spike no longer asks are in
the root [`README.md`](../README.md), "The answer".

## The decision it informs

experiment.hosting's intake, which decides what the `ai-hosting` specification holds for the
self-hosted model stack. The question is framed, and the answer lands, in
`standards-lab/context/ai-hosting.md`.

## Rules

- **Model origin:** models of Chinese origin are avoided where an alternative serves
  (`standards-lab/context/ai-strategy.md`, "Constraint: model origin").
- **Versions:** every document names the exact versions in use when it is written, and every
  measurement the versions it ran on. A profile holds llama.cpp to a minimum build, a b-number,
  and a host runs any build at or above it; the build in use is the one the router's `/props`
  reports in `build_info`. llama.cpp is tracked by b-number because its semver releases ship no
  binaries and lag its model support. Other versions are pinned exactly, and
  `mise run currency` reports the pins that trail.
- **The Framework** (Strix Halo, 96 GB GTT pool) hosts and serves inference only. The spike may
  change anything in its hosting layer, including the runtime, its configuration, and the
  models. Sessions may do on it whatever needs no sudo: the router's HTTP API (including load,
  unload, and downloads), user-level SSH reads such as `amdgpu_top`, and logs, by Tailscale SSH
  as the runbook describes. Root stays the architect's, run from exact commands. Only the OS
  install and the Tailscale registration stay fixed, and there is no restore point.
- **The architect's laptop** runs every consumer (Pi, Claude Code, OpenCode, and the `model`
  client) and every instrument, pinned in `evaluation/mise.consumers.toml` and run from the root
  as `MISE_ENV=consumers mise -C evaluation exec -- <tool>`, off the root PATH so the
  architect's own Claude Code and Pi stay untouched. Their untracked state is under the root's
  `.state/`; Claude Code runs from its own `CLAUDE_CONFIG_DIR` there, on the architect's
  subscription login, with `ANTHROPIC_BASE_URL` at the router. Nothing is served on the laptop.
- **No host is named:** no tailnet names or addresses, no hostnames. The profile and its
  renderer carry no host's name or address; the rendered unit resolves the host's tailnet
  address when it starts, and host-local values (the service user, HF_HOME, and the models
  directory) are given at render time.

## Capabilities

- **Layout:** one Go module, a mise monorepo of areas, each with its own `mise.toml` of tasks
  (`mise run //<area>:<task>`): `hosting/`, `evaluation/`, `devices/<host class>/`, `docs/`,
  and `context/`. The root [`README.md`](../README.md) maps them.
- **Host-class profile:** the `profile` package and command load, validate, and render a TOML
  profile to llama.cpp's router presets and systemd unit; each set assigns the model each role's
  requests go to, and every model carries its slots, context, and card prompts with their
  reasons. [`devices/strix-halo/profile.toml`](../devices/strix-halo/profile.toml) holds the
  Framework's sets, with set A the startup set.
- **Router state and set swap:** the `router` package reads and drives a router over HTTP; the
  `swap` package and command swap the router between a profile's sets, or reload one in place.
- **Probe:** the `probe` package's HTTP probes, `mise run integration`, against the router
  `SPIKE_ROUTER_URL` names.
- **Consumers:** the `consumers` package sets up Pi, Claude Code, and OpenCode against the
  router, and spike-harness-driver's `model` package stands in for go-ai's model client.
- **Evaluation:** `go run ./evaluation/cmd/evaluate` takes a model, or a set's role, through
  gated stages on absolute floors and set A's bars, runs each instrument alone as a step
  (`conform`, `concurrent`, `prefix`, `coldload`), and sweeps a model's slots and context
  (`sweep`). The method is in [`evaluation.md`](../docs/evaluation.md); the screen, full run, and
  results index are tested against a fake router.
- **Wire recording:** the `wire` package records what a consumer sends the router and what comes
  back, and its integration tests record each consumer's reasoning round-trip.
- **Findings:** the baseline, tool-call reliability, the memory budget, and per-model slots,
  under [`docs/findings/`](../docs/findings); the model cards and the model pool on paper, under
  [`docs/`](../docs).
- **Host runbook:** the Framework's, for llama.cpp on Vulkan, with its build log, in
  [`devices/strix-halo/`](../devices/strix-halo/README.md).

## References

The spike reads these repositories, each a key in the coordinator's `references.toml`, with its
local checkout in `references.local.toml`: `personal-agents` (the running llama.cpp setup, the
tier, context-sizing, and memory-footprint methods), `spike-harness-driver` (its `model` client
and router findings), `tau-examples` (local-model configurations), and `tau-protocol` (per-model
capability options). It wrote to personal-agents and spike-harness-driver only in its `align`
task.
