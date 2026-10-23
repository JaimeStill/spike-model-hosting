# profile testdata

- `skeleton.toml`, `skeleton.ini`: a two-model profile, the chat and embedding models every set
  assigns, at different slots (4 and 2), its one set the startup set, and its golden presets
  render.
- `candidate.toml`: the skeleton beside a second set, `candidate`, whose main model takes chat
  and images, carries a `[models.consumers]` block, and is named by the tag the router lists
  (`Q4_K_XL`, not a `UD-Q4_K_XL` file name's); the consumers validation and the router-ID check
  run against it, since the Strix Halo profile holds no such model.
- `strix-halo.ini`: the golden presets render of `../../../devices/strix-halo/profile.toml`.
- `strix-halo.service`: the golden unit render of `../../../devices/strix-halo/profile.toml`,
  with made-up host values (user `svc`, HF_HOME `/srv/hf`, models directory `/srv/models`),
  never a real host's.
- `unified-96gb.ini`: a pinned copy of personal-agents' tracked 96GiB preset,
  `profiles/unified-96gb.ini` at commit `d7be0e81c0d1fe4663d5b3528e3afadae00b4ec8`, copied
  unchanged. The Strix Halo profile's render of set A, its startup set, must equal it in the
  keys and values each model runs at, [*] applied under each model's section as the router
  applies them (so the copy's `[*] parallel = 4` matches each model's own `parallel = 4`), apart
  from the `load-on-startup = true` and `hf-repo = <section
  name>` the profile adds to each of set A's four sections, and the `mtmd-batch-max-tokens` it
  adds to a model that caps its image batch (Gemma 4 26B-A4B's 512). The profile holds set A
  alone; another set's sections would be left out of the comparison and render without
  `load-on-startup`. It is a pin, not a live link: a change
  upstream reaches this spike only when the copy is refreshed on purpose.
