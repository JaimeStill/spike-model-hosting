# Moving the Framework to a new llama.cpp build

The root commands for one build move, in the order the architect runs them in one session, as
planned for b11669 to b11715 (`evaluation` slice 10). Root stays the architect's: a session
prepares the files and these commands, and records the move in
[`llama-cpp-builds.md`](llama-cpp-builds.md) afterwards. The Strix Halo runbook takes this
procedure in whole.

Each build lives in its own directory, `/opt/llama.cpp/<tag>`, and the unit runs
`/opt/llama.cpp/current/llama-server`, so a move repoints `current` and never edits the unit for
the build. The presets are `/etc/llama-router/models.ini` and the unit
`/etc/systemd/system/llama-router.service`, both rendered from [`profile.toml`](profile.toml);
the service is `llama-router`.

Commands marked **Framework** run in a shell on the Framework, as the architect, with `sudo`
where they need root. Commands marked **laptop** run from the repository's root on the
architect's laptop, with `SPIKE_ROUTER_URL=http://framework:8080` exported; none needs root.

## Before the session: the rendered files

A session renders the presets and the unit at the commit the move installs and copies them to
the Framework's home directory, named by the first eight hex digits of their sha256, as
`models.ini.9eeb9f04` was before. Nothing here needs root.

```bash
# laptop
mise run //devices/strix-halo:render:presets > /tmp/models.ini
SPIKE_SERVICE_USER=jaime SPIKE_HF_HOME=/mnt/models/hf-cache SPIKE_MODELS_DIR=/home/jaime/models \
  mise run //devices/strix-halo:render:unit > /tmp/llama-router.service
sha256sum /tmp/models.ini /tmp/llama-router.service
scp /tmp/models.ini framework:models.ini.039c5fa7
scp /tmp/llama-router.service framework:llama-router.service.83b0a95c
```

For b11715 the sums are `039c5fa7decb312629cfe89e2982510d34d68d5dca01754fdcdcdb6182edfb93`
(presets) and `83b0a95c16769d0ee7abb7f6b9e9aec9ccb61d7f57831f508465645e31c51554` (unit). The
presets differ from the installed ones only in Gemma 4 26B-A4B's section: its
`mtmd-batch-max-tokens = 512` and the comment giving its reason. The unit is unchanged; it is
installed anyway, so the installed pair is the rendered pair.

## a. Install the new build beside the old

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
"/opt/llama.cpp/${tag}/llama-server" --help | grep -A1 -- '--mtmd-batch-max-tokens'
readlink /opt/llama.cpp/current
```

Expect `...: OK` from `sha256sum`; `version: 11715 (9d1878a43)`; a `Vulkan0` device, the
Radeon 8060S (RADV STRIX_HALO); the flag's help line, default 1024; and `current` still
`b11669`. `mkdir` without `-p` stops the step if the directory already exists. The router keeps
running b11669 throughout.

## b. Install the presets and the unit, keeping the old ones

```bash
# Framework
sudo cp -p /etc/llama-router/models.ini /etc/llama-router/models.ini.b11669
sudo cp -p /etc/systemd/system/llama-router.service /etc/llama-router/llama-router.service.b11669
echo "039c5fa7decb312629cfe89e2982510d34d68d5dca01754fdcdcdb6182edfb93  $HOME/models.ini.039c5fa7" | sha256sum -c
echo "83b0a95c16769d0ee7abb7f6b9e9aec9ccb61d7f57831f508465645e31c51554  $HOME/llama-router.service.83b0a95c" | sha256sum -c
sudo install -m 0644 -o root -g root "$HOME/models.ini.039c5fa7" /etc/llama-router/models.ini
sudo install -m 0644 -o root -g root "$HOME/llama-router.service.83b0a95c" /etc/systemd/system/llama-router.service
sudo systemctl daemon-reload
diff /etc/llama-router/models.ini.b11669 /etc/llama-router/models.ini
cmp /etc/llama-router/llama-router.service.b11669 /etc/systemd/system/llama-router.service && echo unit unchanged
```

Expect two `OK`s; a diff of nine added lines in the 26B-A4B's section, the reason's comment and
`mtmd-batch-max-tokens = 512`; and `unit unchanged`. The unit's backup is kept beside the
presets, not in `/etc/systemd/system`. The running router read its presets when it started, and
b11669 knows the new key too (it has since b11452), so a load before the restart is safe.

## c. Repoint `current` and restart the router

Start the watch on the laptop first, with set A loaded; it waits for the restart and times it:

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
`b11715-9d1878a43`. This restart is the move's own. It is not the warm measurement: the
weights are in the page cache, but the new build's files were just written, and it isn't the
deep stage's restart. Then probe the new build from the laptop:

```bash
# laptop
mise run integration
```

Expect every probe to pass. If one fails, roll back (e) before anything else.

## d. The cold-load pair, on the new build

The cold-load pair is measured on b11715, the build the baseline runs on. It is the deep stage's
last step: with `-restart`, the run asks for each restart on stderr, naming its command, and
times it. `-from deep` on slice 9's run directory isn't right after a build change. It would
reuse that run's gates, run on b11669, under a report that names one build, b11715. A new
run on the new build is the clean measurement:

```bash
# laptop
go run ./evaluation/cmd/evaluate -set a -role agent -restart
```

It runs every stage on b11715 before the deep stage. By slice 9's times for gpt-oss-120b, that is
about 22 minutes of quality, 13 of the set stage, and 50 of τ2 airline and retail, the grid and
prefix reuse, so the first restart is asked for about 85 minutes after the start. When the run
asks:

```bash
# Framework, when the run asks for the warm restart
sudo systemctl restart llama-router
```

```bash
# Framework, when the run asks for the cold restart
sync && echo 3 | sudo tee /proc/sys/vm/drop_caches && sudo systemctl restart llama-router
```

Expect the run to record `cold load warm` and `cold load cold`, each with the router's answer
and each of set A's models loaded again, timed from the restart's start. The run doubles as a
full agent run on b11715, which the baseline may count. Its τ2 stage spends Claude Code
subscription usage, as slice 9's did.

If the architect can't wait for the deep stage, the same pair can be timed in this session by
the coldload step alone, which uses the same tracker as the deep stage. Run each watch on the
laptop, then its restart on the Framework once the watch is waiting:

```bash
# laptop, then the warm restart on the Framework
go run ./evaluation/cmd/evaluate coldload -set a
# laptop, then the cold restart on the Framework
go run ./evaluation/cmd/evaluate coldload -set a
```

These figures sit outside any run's report, and the baseline's deep stage then records cold load
as not run unless the architect is at hand for it.

## e. Roll back to b11669

```bash
# Framework
sudo ln -sfn b11669 /opt/llama.cpp/current
sudo install -m 0644 -o root -g root /etc/llama-router/models.ini.b11669 /etc/llama-router/models.ini
sudo install -m 0644 -o root -g root /etc/llama-router/llama-router.service.b11669 /etc/systemd/system/llama-router.service
sudo systemctl daemon-reload
sudo systemctl restart llama-router
```

```bash
# laptop
curl -s http://framework:8080/props | jq -r .build_info
mise run integration
```

Expect `b11669-620d2225c` and a passing probe. `/opt/llama.cpp/b11715` stays for diagnosis
until the next move removes it.
