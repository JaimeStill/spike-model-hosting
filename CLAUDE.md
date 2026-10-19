# spike-model-hosting

A spike for the Standards Lab reference architecture. It compares serving platforms (llama.cpp,
vLLM, SGLang) and gateway approaches (none, agentgateway, LiteLLM) for locally hosted models
across the Strix Halo and Dell NVIDIA host classes, behind one host-class profile schema.
`README.md` is the front page; the repository is managed with the marathon workflow, so start
from `context/README.md`.

- **Standards:** `STANDARDS.md`, with pointers into the architecture repository.
- **Layout:** one Go module in a mise monorepo. `hosting/` holds the profile, router, swap, and
  probe packages and their commands; `evaluation/` the consumers, instruments, gated stages, and
  Python suites; `devices/<host class>/` each host class's `profile.toml`, runbook (`README.md`),
  and build log; `docs/` the evaluation approach, the model cards, and, under `docs/findings/`,
  the measured results; `context/` marathon's files only. Each area's `mise.toml` holds its
  tasks, run from the root as `mise run //<area>:<task>`.
- **Check:** `mise run check` (hermetic). The HTTP probes (the `probe` package) run against
  the router `SPIKE_ROUTER_URL` names, as `mise run integration`.
- **Profiles:** `devices/*/profile.toml` are the host-class profiles; `go run ./hosting/cmd/profile`
  validates and renders them, lists their engines, and describes a set, and
  `go run ./hosting/cmd/swap` swaps the router between a profile's named sets. Every command takes
  a profile by host-class name (`strix-halo`, the default), found from the module root, or by
  path. The `profile` and `swap` packages' docs list their APIs.
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
- **Consumers and instruments:** `evaluation/mise.consumers.toml` pins them, run from the root as
  `MISE_ENV=consumers mise -C evaluation exec -- <tool>` (mise loads that file only in
  `evaluation/`); their untracked state is under the root's `.state/`.
- **Evaluation:** `go run ./evaluation/cmd/evaluate -set a -role <role>` screens a model through
  the stages admission, fitness, capacity, and quality (tiers early stopped on either side, the
  roles at once), judged by `evaluation/floors.toml` and `evaluation/bars.toml` (set A's two-run
  baseline); `-full -run <dir>` continues a finalist's screen with the rest of each tier, the set
  stage, and deep; `-set a` alone is a set run, `-from <stage>` resumes. The agent's tier 3 is harness fit: conform pass^5
  through every pinned harness, one absolute bar. The instruments also run alone as steps
  (`conform`, `concurrent`, `prefix`, `coldload`; `-h` lists them). evaluate reloads the set over the
  router's API before each run and judges the pool at the warmed peak; set `TMPDIR` to
  `.state/tmp` first (τ2 leaves a database copy per sample). Reports go to `.state/evaluate/`; once findings are committed, run outputs are
  removed. `docs/evaluation.md` is the method; each package's `doc.go` lists its API.
- **Findings:** `docs/findings/baseline.md` (speed, prefix, cold load, set A's scores and bars),
  `docs/findings/tool-reliability.md` (pass^5 and harness fit), and
  `docs/findings/memory-budget.md` (the pool); the Framework's builds are in
  `devices/strix-halo/llama-cpp-builds.md`.
