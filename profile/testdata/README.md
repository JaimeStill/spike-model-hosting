# profile testdata

- `skeleton.toml`, `skeleton.ini`: a one-model profile, its one set the startup set, and its
  golden presets render.
- `strix-halo.ini`: the golden presets render of `../../profiles/strix-halo.toml`.
- `strix-halo.service`: the golden unit render of `../../profiles/strix-halo.toml`, with made-up
  host values (user `svc`, HF_HOME `/srv/hf`, models directory `/srv/models`), never a real
  host's.
- `unified-96gb.ini`: a pinned copy of personal-agents' tracked 96GiB preset,
  `profiles/unified-96gb.ini` at commit `d7be0e81c0d1fe4663d5b3528e3afadae00b4ec8`, copied
  unchanged. The Strix Halo profile's render must equal it in sections, keys and values, apart
  from the `load-on-startup = true` the profile adds to each startup-set model's section (set A
  holds all four). It is a pin, not a live link: a change upstream reaches this spike only
  when the copy is refreshed on purpose.
