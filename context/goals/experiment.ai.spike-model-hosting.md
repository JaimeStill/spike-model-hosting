# goal · experiment.ai.spike-model-hosting

- **State:** idle
- **Task:** none
- **Branch:** none

## Tasks

1. [ ] profile
2. [ ] consumers
3. [ ] rocm
4. [ ] engines-strix
5. [ ] gateway
6. [ ] admin (needs: go-cli-sdk v0.1.0)
7. [ ] cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
8. [ ] engines-cuda (needs: the Dell NVIDIA workstation, from 2026-10-14)
9. [ ] validate

## Decisions

- setup: the evidence is the ten items in `context/README.md`, "The evidence".
- setup: kind code, Go; `mise run check` is hermetic, modeled on spike-cli-architecture's, plus
  shellcheck over `scripts/`; `mise run integration` probes a router over HTTP at
  `SPIKE_ROUTER_URL`.
- setup: `mise run currency` covers the module, the Go directive, mise tools, and every engine
  and gateway pin the profile tool lists, against each upstream's newest release tag,
  pre-releases included.
- setup: merge is plain `gh pr merge --merge --delete-branch`; no CI.
- setup: references are keys in the coordinator's catalog: personal-agents, spike-harness-driver,
  tau-examples, tau-protocol. The spike keeps no references files.
- setup: platforms live are llama.cpp (Vulkan, ROCm, CUDA), vLLM, and SGLang (time-boxed on
  Strix Halo); Lemonade only as an install path; Ollama, LM Studio, and TGI on paper.
- setup: llama.cpp keeps its native router; vLLM and SGLang run behind llama-swap.
- setup: gateways live are agentgateway and LiteLLM, against no gateway; Envoy AI Gateway
  (Agent Router) and vLLM production-stack on paper.
- setup: the shared model set is gpt-oss-120b, EmbeddingGemma 2 (270M text model first), and
  gemma-4-E4B, each in its platform's native format. Qwen presets survive only in the restore
  point.
- setup: models of Chinese origin are avoided where an alternative serves (`ai-strategy.md`,
  "Constraint: model origin").
- setup: every engine, gateway, and model tracks its latest upstream release; profiles pin the
  exact versions, and documents name the exact versions in use when written. The Framework moves
  from Arch's lagging llama-cpp package to llama.cpp's upstream Vulkan x64 release.
- setup: consumers run on the Framework. The laptop runs only the repository, its check,
  read-only probes, and HTTP. The spike may change anything on the Framework and the
  workstation.
- setup: Tailscale SSH in accept mode for the Framework and the workstation, set by the architect
  before `profile`; `validate` reverts it to check mode.
- setup: go-ai's model client is spike-harness-driver's `model` package at a pseudo-version.
- setup: profiles are TOML read with BurntSushi/toml; rejected encoding/json, which can't hold
  the presets' reasoning as comments.
- setup: the admin tool builds on go-cli-sdk; rejected the standard library's flag alone, since
  go-cli-sdk is the workspace's CLI standard. It stays sixth, before the workstation tasks: the
  architect expects go-cli-sdk before the workstation arrives.
- setup: a task marked "needs" hands off at LOCATE until its prerequisite exists.

## Pending edits

(none)
