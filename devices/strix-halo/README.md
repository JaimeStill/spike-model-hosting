# The Strix Halo runbook

What the Framework desktop, the Strix Halo host class's one host, needs to serve the spike, and
the root commands that change it. The host's profile is [`profile.toml`](profile.toml); the log
of every llama.cpp build it has served, with each move's release-note review and what was
re-verified, is [`llama-cpp-builds.md`](llama-cpp-builds.md).

The Framework only hosts and serves inference. Sessions may do on it whatever needs no sudo: the
router's HTTP API (including load, unload, and downloads), user-level SSH reads such as
`amdgpu_top`, and logs. Root stays the architect's: a session prepares the files and the exact
commands below, the architect runs them, and the session records the result.

Commands marked **Framework** run in a shell on the Framework, as the architect, with `sudo`
where they need root. Commands marked **laptop** run from the repository's root on the
architect's laptop, with `SPIKE_ROUTER_URL` exported to the router (`http://<host>:8080`); none
needs root. `<host>` is the Framework's name on the tailnet, which nothing committed names.

## The host

- **Hardware:** a Framework Desktop: AMD Ryzen AI Max (Strix Halo) with 128 GB of unified
  memory and the Radeon 8060S (Vulkan device `Vulkan0`, RADV STRIX_HALO), 256 GB/s memory
  bandwidth. The models live on a dedicated NVMe drive (ext4).
- **OS:** Omarchy (Arch Linux). The OS install is the architect's and stays fixed; there is no
  restore point.
  Since 2026-10-19 it runs Omarchy 4.0.5, kernel `7.2.8-9-omarchy`, Mesa and `vulkan-radeon`
  26.2.5, and `linux-firmware` 20260916. Set A's baseline ran on kernel `7.2.5-3-omarchy`, Mesa
  26.2.2 and `linux-firmware` 20260810; the update's re-check is in
  [`llama-cpp-builds.md`](llama-cpp-builds.md), "The OS update, 2026-10-19". An OS update
  changes the Vulkan driver the router runs on, so each one gets the same re-check as a build
  move: the probe, then fitness for every role.
- **The memory pool:** the BIOS's GPU reservation is 512 MB, and the kernel's GTT pool is set
  to 96 GiB (98304 MiB) by `ttm pages_limit` and `amdgpu gttsize`, built into the boot image.
  The steps are personal-agents' `preparation/framework-desktop.md`. Every set is sized against
  that pool, and the evaluation's floor keeps 2816 MiB of it free
  ([`docs/evaluation.md`](../../docs/evaluation.md)). The desktop session's own share of the pool
  is part of every reading.

## Tailscale

The router binds the host's tailnet IPv4 address, so it is unreachable from any other network
the host is on. The rendered unit resolves the address when it starts (`tailscale ip -4`) and
requires `tailscaled.service`, so no address or name is ever rendered or committed.

Sessions reach the host, for the pool readings and logs, by Tailscale SSH. The tailnet's access
policy sets each SSH rule to **accept** (unattended) or **check** (the first connection after
the rule's check period needs a browser re-authentication). The spike runs in accept mode for
the host alone: the host carries `tag:inference`, and an accept rule from the architect's user to
that tag sits above the tailnet's check rule, which still covers every other device. The
architect sets it, and the host stays in accept mode while the experiments run
(`standards-lab/context/ai-hosting.md`, "Hosts and access").

**The check-mode lapse.** In check mode, once the check period lapses, a non-interactive ssh
fails (`exit status 255`) until someone re-authenticates in a browser; the router's HTTP API is
unaffected. A 44-minute lapse cost two of baseline 1's runs: every pool reading failed, so the
vision run's set stage and the audio run's fitness failed "couldn't measure", and both were run
again ([`docs/findings/baseline.md`](../../docs/findings/baseline.md),
"Runs not kept"). Before a long run, check that a non-interactive `ssh <host> true` succeeds.
If the rule is in check mode, re-authenticate just before the run, or have the architect set it
to accept.

## The llama.cpp install

The router runs upstream's prebuilt release for Vulkan on x64,
`llama-<tag>-bin-ubuntu-vulkan-x64.tar.gz`, which runs on Arch as it is; Arch's own `llama-cpp`
packages trail upstream's model support. The profile holds llama.cpp to a minimum build
(`min_build`), and the host runs any build at or above it; the build in use is the one
`GET /props` reports in `build_info`.

- Each build lives in its own directory, `/opt/llama.cpp/<tag>`, owned by root.
- `/opt/llama.cpp/current` is a link to the build in use, and the unit runs
  `/opt/llama.cpp/current/llama-server`. A move repoints `current` and never edits the unit for
  the build; a rollback points it back.
- The previous build stays in its directory for diagnosis until the next move removes it.

The Framework runs b11734 (`b11734-f3fb2fcdb`), since 2026-10-19; b11715 stays in
`/opt/llama.cpp/b11715`. `screen`'s move removed b11669 and the builds before it (b11529,
b11565 and b11572).

## The rendered unit and presets

Both files are rendered from [`profile.toml`](profile.toml) by the hosting area's `profile`
command, through this area's tasks, and installed by root:

| File | Rendered by | Installed at |
|---|---|---|
| The presets | `mise run //devices/strix-halo:render:presets` | `/etc/llama-router/models.ini` |
| The unit | `mise run //devices/strix-halo:render:unit` | `/etc/systemd/system/llama-router.service` |

The service is `llama-router`. It runs `llama-server` in router mode: no model on its command
line, `--models-preset` at the installed presets, `--no-models-autoload` so no request loads a
model, and `load-on-startup` in the presets for the startup set's models, set A. Each model's
section carries its card's values and, as a comment, the profile's reason for each.

The unit's host values belong to the machine and are never committed: the service user, the
Hugging Face cache (`HF_HOME`), and the models directory come from `SPIKE_SERVICE_USER`,
`SPIKE_HF_HOME`, and `SPIKE_MODELS_DIR` in the environment at render time (or the `profile`
command's `-user`, `-hf-home`, and `-models-dir`).

## Moving to a new build

The root commands for one build move, in the order the architect runs them in one session: a
to d in one sitting, then the cold-load pair (e) when the evaluation asks for it. The values
shown are the current move's, b11715 to b11734; a new move replaces the tags, the asset's
sha256, and the rendered files' sums, and records itself in
[`llama-cpp-builds.md`](llama-cpp-builds.md): the release-note review before, and what was
re-verified after. Step c is the b11734 sitting's own: the preset directory's write access and
the deletions; a later move keeps only its removal of the builds before the previous one.

### Before the session: render the files

A session renders the presets and the unit at the commit the move installs and copies them to
the Framework's home directory, each named by the first eight hex digits of its sha256. Nothing
here needs root.

```bash
# laptop
mise run //devices/strix-halo:render:presets > /tmp/models.ini
SPIKE_SERVICE_USER=<user> SPIKE_HF_HOME=<hf cache> SPIKE_MODELS_DIR=<models dir> \
  mise run //devices/strix-halo:render:unit > /tmp/llama-router.service
sha256sum /tmp/models.ini /tmp/llama-router.service
scp /tmp/models.ini <host>:models.ini.9c983739
scp /tmp/llama-router.service <host>:llama-router.service.83b0a95c
```

For b11734 the sums are `9c983739f3142f01a92726b56d286150c3f21c3df50f8953c957a815401962ea`
(presets) and `83b0a95c16769d0ee7abb7f6b9e9aec9ccb61d7f57831f508465645e31c51554` (unit). The
presets differ from the installed ones (b11715's, `039c5fa7`) only in two reason comments, the
26B-A4B's c and Mistral Small 4's c, which `evaluation`'s editor rewrote after b11715's
install. The unit is unchanged, and installed anyway, so the installed pair is the rendered
pair.

### a. Install the new build beside the old

```bash
# Framework
tag=b11734
asset="llama-${tag}-bin-ubuntu-vulkan-x64.tar.gz"
sha=2c8b590c325960eb474c08c7a3b02b3c8eda054aa03352ed864948dc4ae1668d
work=$(mktemp -d)
curl -fsSL -o "$work/$asset" "https://github.com/ggml-org/llama.cpp/releases/download/${tag}/${asset}"
echo "${sha}  $work/$asset" | sha256sum -c
sudo mkdir "/opt/llama.cpp/${tag}"
sudo tar xzf "$work/$asset" -C "/opt/llama.cpp/${tag}" --strip-components=1 --no-same-owner
sudo chown -R root:root "/opt/llama.cpp/${tag}"
rm -rf "$work"
"/opt/llama.cpp/${tag}/llama-server" --version
"/opt/llama.cpp/${tag}/llama-server" --list-devices
readlink /opt/llama.cpp/current
```

Expect `...: OK` from `sha256sum`; the new build's version (build 11734, commit `f3fb2fcdb`); a
`Vulkan0` device, the Radeon 8060S (RADV STRIX_HALO); and `current` still the old build.
`mkdir` without `-p` stops the step if the directory already exists. The router keeps running
the old build throughout. When the new presets add a key, check that the new build knows its
flag: for b11715, `llama-server --help | grep -A1 -- '--mtmd-batch-max-tokens'` printed the
flag's help line, default 1024. b11734's presets add none.

### b. Install the presets and the unit, keeping the old ones

```bash
# Framework
old=b11715
sudo cp -p /etc/llama-router/models.ini "/etc/llama-router/models.ini.${old}"
sudo cp -p /etc/systemd/system/llama-router.service "/etc/llama-router/llama-router.service.${old}"
echo "9c983739f3142f01a92726b56d286150c3f21c3df50f8953c957a815401962ea  $HOME/models.ini.9c983739" | sha256sum -c
echo "83b0a95c16769d0ee7abb7f6b9e9aec9ccb61d7f57831f508465645e31c51554  $HOME/llama-router.service.83b0a95c" | sha256sum -c
sudo install -m 0644 -o root -g root "$HOME/models.ini.9c983739" /etc/llama-router/models.ini
sudo install -m 0644 -o root -g root "$HOME/llama-router.service.83b0a95c" /etc/systemd/system/llama-router.service
sudo systemctl daemon-reload
diff "/etc/llama-router/models.ini.${old}" /etc/llama-router/models.ini
cmp "/etc/llama-router/llama-router.service.${old}" /etc/systemd/system/llama-router.service && echo unit unchanged
```

Expect two `OK`s and the diff the session predicted; for b11734, two comment hunks (lines 41-42,
the 26B-A4B's c, "under the 2.75GiB pool floor"; line 113, Mistral Small 4's c, "under
main-model's 3GiB floor") and no value changed, and `unit unchanged`. The unit's backup is kept
beside the presets, not in `/etc/systemd/system`. The running router read its presets when it
started, so a load before the restart uses the old presets' values; check that the old build
knows any new key (b11669 had known `mtmd-batch-max-tokens` since b11452).

### c. Open the preset directory, and delete what the host doesn't keep

The b11734 sitting's own step, before the restart: the preset directory's write access
("The preset file's write access", below), the deletions ("The models the host keeps", below),
and the builds before the previous one. Run it as the service user, the architect's account on
the Framework: `id -gn` names the service user's group, and the unit's environment names the
cache, so nothing here names a host value.

```bash
# Framework
sudo chgrp "$(id -gn)" /etc/llama-router && sudo chmod 0775 /etc/llama-router
stat -c '%A %U:%G %n' /etc/llama-router /etc/llama-router/models.ini
hf=$(systemctl show llama-router -p Environment --value | tr ' ' '\n' | sed -n 's/^HF_HOME=//p')
du -sh "$hf/hub/models--unsloth--Mistral-Small-4-119B-2603-GGUF" "${hf}-retired"
[ -d "$hf/hub" ] && rm -rf -- "$hf/hub/models--unsloth--Mistral-Small-4-119B-2603-GGUF" "${hf}-retired"
ls "$hf/hub" "$(dirname "$hf")"
sudo rm -rf /opt/llama.cpp/b11529 /opt/llama.cpp/b11565 /opt/llama.cpp/b11572 /opt/llama.cpp/b11669
ls /opt/llama.cpp
```

Expect `drwxrwxr-x root:<group> /etc/llama-router` and `-rw-r--r-- root:root` for
`models.ini`; `du` near 70G for Mistral Small 4 and 132G for the retired directory (2026-10-19);
the cache's `hub` holding the five other models' directories and no Mistral one, and no
retired directory beside the cache; and `/opt/llama.cpp` holding b11715, b11734 and `current`.
The cache and the retired directory belong to the service user, so their deletion needs no
`sudo`; the `[ -d ]` test stops it if the cache wasn't found. Mistral Small 4 is unloaded, so
no process holds its files.

### d. Repoint `current` and restart the router

Start the watch on the laptop first, with the startup set loaded; it waits for the restart and
times it:

```bash
# laptop
go run ./evaluation/cmd/evaluate coldload -set a
```

```bash
# Framework, once the watch is waiting
sudo ln -sfn b11734 /opt/llama.cpp/current && sudo systemctl restart llama-router
```

Expect the watch to print when the router answered again and when each of set A's four models
was loaded again, then the build before and after: `b11715-9d1878a43`, then
`b11734-f3fb2fcdb`. This restart is the move's own; it isn't a warm measurement, since the new
build's files were just written. Then probe the new build:

```bash
# laptop
mise run integration
```

Expect every probe to pass. If one fails, roll back (f) before anything else.

### e. The cold-load pair, on the new build

The cold-load figures are measured on the build the evaluation runs on. Run each watch on the
laptop, then its restart on the Framework once the watch is waiting:

```bash
# laptop, before each restart
go run ./evaluation/cmd/evaluate coldload -set a
```

```bash
# Framework, the warm restart
sudo systemctl restart llama-router
```

```bash
# Framework, the cold restart: the page cache dropped first
sync && echo 3 | sudo tee /proc/sys/vm/drop_caches && sudo systemctl restart llama-router
```

The same pair is the deep stage's last step: `go run ./evaluation/cmd/evaluate -set a -role agent
-restart` asks for each restart on stderr, naming its command, when it reaches cold load, about
85 minutes into an agent run on set A. Without `-restart`, the deep stage records cold load as
not run. b11715's pair is in
[`llama-cpp-builds.md`](llama-cpp-builds.md), "b11669 to b11715".

### f. Roll back

```bash
# Framework
old=b11715
sudo ln -sfn "$old" /opt/llama.cpp/current
sudo install -m 0644 -o root -g root "/etc/llama-router/models.ini.${old}" /etc/llama-router/models.ini
sudo install -m 0644 -o root -g root "/etc/llama-router/llama-router.service.${old}" /etc/systemd/system/llama-router.service
sudo systemctl daemon-reload
sudo systemctl restart llama-router
```

```bash
# laptop
curl -s "$SPIKE_ROUTER_URL/props" | jq -r .build_info
mise run integration
```

Expect the old build (`b11715-9d1878a43`) and a passing probe. The new build's directory stays
for diagnosis until the next move removes it. A rollback leaves the preset directory's write
access and the deletions as they are; each has its own revert below.

## The preset file's write access

A sweep step (`screen`'s behavior 10) rewrites the router's presets and applies them with
`GET /models?reload=1`, with no restart: the reload re-reads the file `--models-preset` names,
unloads a running model whose section changed, and loads none. So the service user may write
the preset file's directory, `/etc/llama-router`: root keeps the directory and the files, and
the directory's group is the service user's, with group write (`0775`). It is the one root grant
the spike makes to a session, by the goal record's decision `screen` (r2).

The grant is on the directory, not the file, because it survives the normal install: `install`
replaces `models.ini` with a new root-owned `0644` file, which drops any group or ACL set on the
old one, and leaves the directory as it is. So a session writes the presets by rename: it writes
the new file beside `models.ini` in `/etc/llama-router` and moves it over `models.ini`, and the
router never reads a half-written file. A file a sweep left there is the service user's until the
sweep restores the committed render (checked by its SHA-256) or the next install replaces it.
The write access also lets the service user remove the backups kept beside the presets.

`evaluate sweep` ([`docs/evaluation.md`](../../docs/evaluation.md), "The sweep") is the session's
one writer, over user-level ssh and with no `sudo`: each step writes a temporary file,
`/etc/llama-router/.models.ini.XXXXXX` (`mktemp`), makes it `0644`, renames it over `models.ini`
and asks `GET /models?reload=1`; it writes no other file. It refuses to start unless the
installed file is the committed render, and ends every exit, an error, SIGINT and SIGTERM among
them, by installing the committed render and checking its SHA-256 on the host. A sweep killed
before its restore (SIGKILL, or the laptop lost mid-step) leaves a step's file, which names
itself in its first comment line; restore it from the laptop and check it:

```bash
# laptop
go run ./evaluation/cmd/evaluate sweep -restore -set a
mise run //devices/strix-halo:render:presets | sha256sum
ssh <host> sha256sum /etc/llama-router/models.ini
```

Expect `restored: the committed render, sha256 <sum>, installed and verified`, and the two sums
equal.

It reverts by one root command, which also hands back a `models.ini` a sweep left behind:

```bash
# Framework
sudo chown root:root /etc/llama-router /etc/llama-router/models.ini && sudo chmod 0755 /etc/llama-router && sudo chmod 0644 /etc/llama-router/models.ini
```

## The models the host keeps

The Framework keeps only models that passed on it (the goal record's decision `screen` (r1)),
and only the models its profile holds: set A's four. The profile holds set A alone since the
answer redirect (the goal record's decision `answer` (r1), 2026-10-23), so no other model has a
preset and the router lists none. The deletions so far, each by the architect's command:

- **Mistral Small 4's cached files** (`unsloth/Mistral-Small-4-119B-2603-GGUF`, its three
  UD-Q4_K_XL splits and `mmproj-BF16.gguf`, about 70G), deleted at `screen` on 2026-10-19 (step
  c), disqualified at fitness on b11669
  ([`docs/findings/tool-reliability.md`](../../docs/findings/tool-reliability.md)). The router's
  API can't delete them: `DELETE /models?model=unsloth/Mistral-Small-4-119B-2603-GGUF:Q4_K_XL`
  answered 500, "not removable (not from cache)", since the router deletes only a model whose
  source is the cache, and every model the profile holds is a preset (`GET /models` lists each
  with `source` `preset` and `can_remove` false). So the deletion is from the cache directly.
- **align's retired-model directory**, the cache's sibling on the models drive (`<hf
  cache>-retired`, about 132G), deleted at `screen` on 2026-10-19 (step c): where `align` moved
  the models it retired out of the router's cache rather than delete them (Qwen3-Embedding-4B,
  gemma-4-31B-it, Qwen3-Coder-Next, Qwen3.8-27B, and unsloth's gpt-oss-120b). The router never
  read it.
- **Nemotron 3.5 Super's cached files**
  (`ggml-org/NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-GGUF`, its NVFP4 weights and Q8_0 mmproj,
  77.9 GB), deleted at `answer` on 2026-10-23 by the architect's redirect (decisions `answer`
  (r2, r3)): it measured 12.1 tok/s on b11669, under the floor
  ([`docs/findings/memory-budget.md`](../../docs/findings/memory-budget.md)), and candidates
  belong to experiment.ai's spike-model-benchmarks.

All belong to the service user. No deletion reverts on the host; the Hugging Face files can be
downloaded again.

### The answer sitting: set A only

One sitting installs the set-A-only presets, keeping the installed ones (`a65fd306`) beside
them, and deletes Nemotron 3.5 Super's directory from the cache. The unit is unchanged: the
profile's change removes preset sections alone, and the unit's render
(`hosting/profile/testdata/strix-halo.service`) is the same, so it isn't installed. A session
renders the presets at the commit the sitting installs and copies them to the Framework's home
directory; nothing there needs root.

```bash
# laptop
mise run //devices/strix-halo:render:presets > /tmp/models.ini
sha256sum /tmp/models.ini
scp /tmp/models.ini <host>:models.ini.<sha8>
```

```bash
# Framework
sudo cp -p /etc/llama-router/models.ini /etc/llama-router/models.ini.a65fd306
echo "<sha256>  $HOME/models.ini.<sha8>" | sha256sum -c
sudo install -m 0644 -o root -g root "$HOME/models.ini.<sha8>" /etc/llama-router/models.ini
hf=$(systemctl show llama-router -p Environment --value | tr ' ' '\n' | sed -n 's/^HF_HOME=//p')
du -sh "$hf/hub/models--ggml-org--NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-GGUF"
[ -d "$hf/hub" ] && rm -rf -- "$hf/hub/models--ggml-org--NVIDIA-Nemotron-3.5-Super-VL-120B-A12B-GGUF"
ls "$hf/hub"
```

Expect one `OK`; `du` near 73G (77.9 GB); and the cache's `hub` holding set A's four models'
directories and no Nemotron one. The cache belongs to the service user, so its deletion needs no
`sudo`; the `[ -d ]` test stops it if the cache wasn't found. Nemotron 3.5 Super is unloaded, so
no process holds its files.

The router reads the new presets by a reload first ("The preset file's write access", above),
with no restart:

```bash
# laptop
curl -s "$SPIKE_ROUTER_URL/models?reload=1" > /dev/null
curl -s "$SPIKE_ROUTER_URL/models" | jq -r '.data[] | "\(.id) \(.status.value)"'
```

Expect set A's four models, each `loaded`, and no other. If the reload leaves the removed
presets listed, restart the router, which reads the file afresh:

```bash
# Framework
sudo systemctl restart llama-router
```

Then check the installed presets against the committed render, and probe:

```bash
# laptop
mise run //devices/strix-halo:render:presets | sha256sum
ssh <host> sha256sum /etc/llama-router/models.ini
mise run integration
```

Expect the two sums equal (`<sha256>`) and every probe to pass. It reverts by installing the
kept file, `sudo install -m 0644 -o root -g root /etc/llama-router/models.ini.a65fd306
/etc/llama-router/models.ini`, and a reload; Nemotron's files don't come back with it.

## The router's state, without root

These need no root, and sessions run them:

- **The build and the models:** `curl -s "$SPIKE_ROUTER_URL/props" | jq -r .build_info`, and
  `GET /models` for each model's status.
- **Swap sets:** `go run ./hosting/cmd/swap <set>` unloads the models the incoming set doesn't
  hold, then loads the rest, timing each step.
- **Reload a set:** unload and load its models over the API; evaluate does this itself before
  every run ([`docs/evaluation.md`](../../docs/evaluation.md), "Before each run").
- **The pool:** `mise run //evaluation:footprint -- -set a <host> [samples] [seconds between]`,
  `amdgpu_top -p` over a non-interactive ssh; evaluate's own readings add `free -b` and `ps`.
- **The logs:** `journalctl -u llama-router` over user-level ssh.
