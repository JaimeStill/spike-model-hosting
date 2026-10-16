# spike-model-hosting

A spike for the Standards Lab reference architecture. It compares serving platforms (llama.cpp,
vLLM, SGLang) and gateway approaches (none, agentgateway, LiteLLM) for locally hosted models
across the Strix Halo and Dell NVIDIA host classes, behind one host-class profile schema. The
repository is managed with the marathon workflow; start from `context/README.md`.

- **Standards:** `STANDARDS.md`, with pointers into the architecture repository.
- **Check:** `mise run check` (hermetic). The HTTP probes (the `probe` package) run against
  the router `SPIKE_ROUTER_URL` names, as `mise run integration`.
- **Profiles:** `profiles/` holds the host-class profiles; `go run ./cmd/profile` lists,
  validates, and renders them, and `go run ./cmd/swap` swaps the router between a profile's
  named sets. The `profile` and `swap` packages' docs list their APIs.
- **Goals:** this spike's tasks live in the coordinator's roadmap,
  `standards-lab/context/roadmap.toml`, under `experiment.ai.spike-model-hosting`. Its goal
  record is `context/goals/experiment.ai.spike-model-hosting.md`.
- **Dependencies:** published versions only, never a `replace` directive.
- **References:** the repositories this spike reads are keys in the coordinator's
  `references.toml` and `references.local.toml`. The spike reads them and writes to none of them,
  except personal-agents and spike-harness-driver in its `align` task.
- **Hosts:** the spike may change anything in the hosting layer of the Framework desktop and
  the Dell NVIDIA workstation; root stays the architect's, run from exact commands. The
  Framework only hosts and serves inference. Sessions may do on it whatever needs no sudo: the
  router's HTTP API (including load, unload, and downloads), user-level SSH reads such as
  `amdgpu_top`, and logs. Every consumer and instrument runs from the architect's laptop, where
  nothing is served.
- **Consumers and instruments:** `mise.consumers.toml` pins them, run as
  `MISE_ENV=consumers mise exec -- <tool>`; their untracked state is under `.state/`. The
  commands are `cmd/concurrent`, `cmd/conform`, `cmd/prefix`, and `cmd/coldload`; each package's
  `doc.go` lists its API. Their results are in `context/baseline.md` and
  `context/tool-reliability.md`.
