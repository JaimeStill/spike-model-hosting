# profile testdata

- `skeleton.toml`, `skeleton.ini`: a one-model profile and its golden presets render.
- `strix-halo.ini`: the golden presets render of `../../profiles/strix-halo.toml`.
- `unified-96gb.ini`: a pinned copy of personal-agents' tracked 96GiB preset,
  `profiles/unified-96gb.ini` at commit `d7be0e81c0d1fe4663d5b3528e3afadae00b4ec8`, copied
  unchanged. The Strix Halo profile's render must equal it in sections, keys and values, apart
  from the `load-on-startup = true` the profile adds to each model's section. It is a pin, not a
  live link: a change upstream reaches this spike only when the copy is refreshed on purpose.
