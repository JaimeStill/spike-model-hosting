# spike-model-hosting

A spike for the Standards Lab reference architecture. It compares serving platforms (llama.cpp,
vLLM, SGLang) and gateway approaches (none, agentgateway, LiteLLM) for locally hosted models
across the Strix Halo and Dell NVIDIA host classes, behind one host-class profile schema. The
repository is managed with the marathon workflow; start from `context/README.md`.

- **Standards:** `STANDARDS.md`, with pointers into the architecture repository.
- **Check:** `mise run check` (hermetic). The HTTP probes against a running router run as
  `mise run integration`.
- **Goals:** this spike's tasks live in the coordinator's roadmap,
  `standards-lab/context/roadmap.toml`, under `experiment.ai.spike-model-hosting`. Its goal
  record is `context/goals/experiment.ai.spike-model-hosting.md`.
- **Dependencies:** published versions only, never a `replace` directive.
- **References:** the repositories this spike reads are keys in the coordinator's
  `references.toml` and `references.local.toml`. The spike reads them and never writes to them.
- **Hosts:** the spike may change anything on the Framework desktop and the Dell NVIDIA
  workstation. On the architect's laptop it runs only this repository, its check, read-only
  probes, and HTTP: nothing is installed or served there.
