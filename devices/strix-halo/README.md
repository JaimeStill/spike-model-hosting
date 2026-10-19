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
  <!-- OS version: not recorded yet. Add the Omarchy release and kernel once the architect's OS
  update lands. -->
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
architect sets it and reverts the host to check mode when the spike completes.

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

The Framework runs b11715 (`b11715-9d1878a43`); b11669 stays in `/opt/llama.cpp/b11669`.

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

The root commands for one build move, in the order the architect runs them in one session. The
values shown are the last move's, b11669 to b11715; a new move replaces the tags, the asset's
sha256, and the rendered files' sums, and records itself in
[`llama-cpp-builds.md`](llama-cpp-builds.md): the release-note review before, and what was
re-verified after.

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
scp /tmp/models.ini <host>:models.ini.039c5fa7
scp /tmp/llama-router.service <host>:llama-router.service.83b0a95c
```

For b11715 the sums were `039c5fa7decb312629cfe89e2982510d34d68d5dca01754fdcdcdb6182edfb93`
(presets) and `83b0a95c16769d0ee7abb7f6b9e9aec9ccb61d7f57831f508465645e31c51554` (unit). The
presets differed from the installed ones only in Gemma 4 26B-A4B's section: its
`mtmd-batch-max-tokens = 512` and the comment giving its reason. The unit was unchanged, and
installed anyway, so the installed pair is the rendered pair.

### a. Install the new build beside the old

```bash
# Framework
tag=b11715
asset="llama-${tag}-bin-ubuntu-vulkan-x64.tar.gz"
sha=b3405b06ab9b56975d0b1bc8dcdd15a8bace74fc5e4cf1e66c1677a99e590364
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

Expect `...: OK` from `sha256sum`; the new build's version (`version: 11715 (9d1878a43)`); a
`Vulkan0` device, the Radeon 8060S (RADV STRIX_HALO); and `current` still the old build.
`mkdir` without `-p` stops the step if the directory already exists. The router keeps running
the old build throughout. When the new presets add a key, check that the new build knows its
flag: for b11715, `llama-server --help | grep -A1 -- '--mtmd-batch-max-tokens'` printed the
flag's help line, default 1024.

### b. Install the presets and the unit, keeping the old ones

```bash
# Framework
old=b11669
sudo cp -p /etc/llama-router/models.ini "/etc/llama-router/models.ini.${old}"
sudo cp -p /etc/systemd/system/llama-router.service "/etc/llama-router/llama-router.service.${old}"
echo "039c5fa7decb312629cfe89e2982510d34d68d5dca01754fdcdcdb6182edfb93  $HOME/models.ini.039c5fa7" | sha256sum -c
echo "83b0a95c16769d0ee7abb7f6b9e9aec9ccb61d7f57831f508465645e31c51554  $HOME/llama-router.service.83b0a95c" | sha256sum -c
sudo install -m 0644 -o root -g root "$HOME/models.ini.039c5fa7" /etc/llama-router/models.ini
sudo install -m 0644 -o root -g root "$HOME/llama-router.service.83b0a95c" /etc/systemd/system/llama-router.service
sudo systemctl daemon-reload
diff "/etc/llama-router/models.ini.${old}" /etc/llama-router/models.ini
cmp "/etc/llama-router/llama-router.service.${old}" /etc/systemd/system/llama-router.service && echo unit unchanged
```

Expect two `OK`s and the diff the session predicted; for b11715, nine added lines in the
26B-A4B's section, the reason's comment and `mtmd-batch-max-tokens = 512`, and `unit
unchanged`. The unit's backup is kept beside the presets, not in `/etc/systemd/system`. The
running router read its presets when it started, so a load before the restart uses the old
presets' values; check that the old build knows any new key (b11669 had known
`mtmd-batch-max-tokens` since b11452).

### c. Repoint `current` and restart the router

Start the watch on the laptop first, with the startup set loaded; it waits for the restart and
times it:

```bash
# laptop
go run ./evaluation/cmd/evaluate coldload -set a
```

```bash
# Framework, once the watch is waiting
sudo ln -sfn b11715 /opt/llama.cpp/current && sudo systemctl restart llama-router
```

Expect the watch to print when the router answered again and when each of set A's four models
was loaded again, then the build before and after: `b11669-620d2225c`, then
`b11715-9d1878a43`. This restart is the move's own; it isn't a warm measurement, since the new
build's files were just written. Then probe the new build:

```bash
# laptop
mise run integration
```

Expect every probe to pass. If one fails, roll back (e) before anything else.

### d. The cold-load pair, on the new build

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

### e. Roll back

```bash
# Framework
old=b11669
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

Expect the old build (`b11669-620d2225c`) and a passing probe. The new build's directory stays
for diagnosis until the next move removes it.

## The router's state, without root

These need no root, and sessions run them:

- **The build and the models:** `curl -s "$SPIKE_ROUTER_URL/props" | jq -r .build_info`, and
  `GET /models` for each model's status.
- **Swap sets:** `go run ./hosting/cmd/swap <set>` unloads the models the incoming set doesn't
  hold, then loads the rest, timing each step.
- **Reload a set:** unload and load its models over the API; the evaluation does this before
  every run ([`docs/evaluation.md`](../../docs/evaluation.md), "Before each run").
- **The pool:** `mise run //evaluation:footprint -- -set a <host> [samples] [seconds between]`,
  `amdgpu_top -p` over a non-interactive ssh.
- **The logs:** `journalctl -u llama-router` over user-level ssh.
