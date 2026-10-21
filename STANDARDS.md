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
  `evaluation/mise.consumers.toml` and kept off the root PATH; nothing is served there. The
  Framework only hosts and serves inference. Sessions may do on it whatever needs no sudo: the
  router's HTTP API (including load, unload, and downloads), user-level SSH reads, and logs. Root
  stays the architect's, run from exact commands.
- llama.cpp is held to a minimum build, not pinned, and every document and measurement names the
  build it ran on.
- A model of Chinese origin is chosen only when no alternative serves, and its entry records why
  (`standards-lab/context/ai-strategy.md`, "Constraint: model origin").
- A model fine-tuned from a Chinese-origin base takes its base's origin, whoever publishes the
  fine-tune.
- Any open model licence is accepted: Apache-2.0, MIT, OpenMDW-1.1, the NVIDIA Open Model
  licences, the LFM Open License 1.0, the Llama 3.1 and 4 Community licences, and the Gemma
  Terms of Use.
- Every document names the exact version of each engine, gateway, and model it describes, as
  of when it is written.

## Evaluation

- A speed floor is absolute and task-based, what the role's work needs, held as data in
  `evaluation/floors.toml`; never a fraction of one model's score.
- A quality bar is set A's score less its two-run spread, in `evaluation/bars.toml`; never a
  fixed accuracy threshold. A tier without a bar records and never fails.
- Every model runs the same committed, seeded sample lists (`evaluation/tiers`), and every
  benchmark is the stock task, unforked.
- Harness fit holds every harness to one absolute bar; a new harness joins as one more entry.
- No cloud spend and no API keys: a simulated user or grader is Claude Code on the architect's
  subscription, through the pinned binary.
- Every model run starts from freshly loaded models, so no verdict depends on run order.
- Raw run output stays untracked under `.state/`; the findings in `docs/findings/` are the
  record, and run outputs are removed once they are committed.
