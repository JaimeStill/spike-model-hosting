# Standards

The judgement calls the check can't enforce, one line each, for the standards-reviewer. What
`mise run check` enforces is never restated here.

The architecture pages that apply, in the architecture repository:

- `principles/minimal-footprint.md`
- `principles/tool-beside-library.md`
- `principles/composition-root.md`
- `principles/rolling-currency.md`
- `standards/go-elemental/principles/topology-and-naming.md`
- `standards/go-elemental/principles/tests-and-docs.md`

## Profiles and hosts

- A profile holds no host state, and neither it nor its renderer carries a host's tailnet name,
  tailnet address, or hostname. The rendered unit resolves the host's tailnet address when it
  starts, and the host-local values (the service user, HF_HOME, and the models directory) are
  given at render time.
- Every consumer and instrument runs from the architect's laptop, pinned in
  `mise.consumers.toml` and kept off the root PATH; nothing is served there. The Framework only
  hosts and serves inference. Sessions may do on it whatever needs no sudo: the router's HTTP
  API (including load, unload, and downloads), user-level SSH reads, and logs. Root stays the
  architect's, run from exact commands.
- llama.cpp is held to a minimum build, not pinned, and every document and measurement names the
  build it ran on.
- A model of Chinese origin is chosen only when no alternative serves, and its entry records why
  (`standards-lab/context/ai-strategy.md`, "Constraint: model origin").
- Every document names the exact version of each engine, gateway, and model it describes, as
  of when it is written.
