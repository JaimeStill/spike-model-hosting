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

## Task brief · profile

```
Problem       Every later task renders its serving setup from a host-class profile, and none
              exists. The Framework's router runs b10809 with presets drifted from
              personal-agents' tracked profile and Qwen models; EmbeddingGemma 2 needs llama.cpp
              b11452 or newer. The spike needs a profile schema, a Strix Halo profile serving the
              shared model set, and a restore point for what runs today. The Framework moves
              to llama.cpp's newest upstream release; the profile pins that exact build, and the
              README and record name it. Evidence 1.
Behaviors     1. Before the Framework's router changes, its presets, systemd unit, model list,
                 and build are saved on the Framework, outside any repository, with a command
                 that puts them back; its dry run lists exactly what it restores.
              2. A profile missing a required field (host class, memory budget, backend, engine
                 and pinned build, bind, slots, KV mode, models) fails validation naming the field.
              3. A profile holding host state (a tailnet name or address, a hostname) fails
                 validation; the host's name and address are given at render time.
              4. Each model entry names its publisher and origin; a model of Chinese origin fails
                 validation unless the entry records why no alternative serves.
              5. Rendering the Strix Halo profile yields llama.cpp router presets with shared
                 defaults and one section each for gpt-oss-120b (ggml-org MXFP4 GGUF),
                 EmbeddingGemma 2 (Q8_0 GGUF, embeddings, no mmproj: the 270M text model), and
                 gemma-4-E4B (Q8_0 GGUF with its mmproj), and nothing else.
              6. Each model's context comes from personal-agents' context-sizing method, and its
                 recorded reason renders as a comment above its preset section.
              7. Rendering yields the router's systemd unit: the pinned engine build, router mode,
                 the rendered presets, 4 slots with a unified KV cache, and a bind to the tailnet
                 address given at render time.
              8. The profile tool lists every pin with its upstream repository, and
                 `mise run currency` reports a pin behind its upstream's newest release tag as
                 `<profile>: llama.cpp <pin> -> <latest>`.
              9. Installed on the Framework, the rendered unit and presets serve exactly the three
                 models: from the laptop over HTTP, /health is ok, /models lists the three,
                 gpt-oss-120b and gemma-4-E4B answer a chat, and EmbeddingGemma 2 returns
                 768-dimension embeddings.
Test seams    The profile package's load, validate, and render API, with golden renders of the
              Strix Halo profile; one integration-tagged HTTP probe against a router URL from the
              environment.
Slices        1. Walking skeleton (2, 5 for one model): load, validate, render presets, golden test.
              2. The full Strix Halo profile (3, 4, 5, 6): the shared model set, reasons, origin
                 and host-state rules.
              3. Unit and pins (7, 8): the rendered unit, the pin listing, currency's pin lines.
              4. On the Framework (1, 9): capture the restore point, install the newest upstream
                 Vulkan x64 release (pinned in the profile) and the rendered unit and presets,
                 run the probe.
Out of scope  The consumer suite and measurements; ROCm, vLLM, SGLang, llama-swap; gateways; the
              admin tool; a CUDA profile (the schema allows one, nothing renders it); changes to
              personal-agents or outpost; Qwen presets.
Door          two-way — nothing published or tagged; the Framework's prior router comes back from
              the restore point
```

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
