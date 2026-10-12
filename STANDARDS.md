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

- A profile holds no host state: no tailnet name or address and no hostname, and neither the
  profile nor its renderer carries a host's name or address. The rendered unit resolves the
  host's tailnet address when it starts. Host-local values (the service user, HF_HOME, and the
  models directory) are given at render time.
- llama.cpp is held to a minimum build, not pinned, and every document and measurement names the
  build it ran on.
- A model of Chinese origin is chosen only when no alternative serves, and its entry records why
  (`standards-lab/context/ai-strategy.md`, "Constraint: model origin").
- Every document names the exact version of each engine, gateway, and model it describes, as
  of when it is written.
