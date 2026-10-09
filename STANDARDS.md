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

- A profile holds no host state: no tailnet name or address and no hostname. A host's name and
  address are given at render time.
- A model of Chinese origin is chosen only when no alternative serves, and its entry records why
  (`standards-lab/context/ai-strategy.md`, "Constraint: model origin").
- Every document names the exact version of each engine, gateway, and model it describes, as
  of when it is written.
