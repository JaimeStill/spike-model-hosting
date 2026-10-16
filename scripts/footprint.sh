#!/usr/bin/env bash
# footprint samples a set's memory on the host that serves the router, the
# way context/memory-budget.md measures it: each llama-server process's
# VRAM + GTT from `amdgpu_top -p`, and the pool used from its header's GTT
# figure. Each sample also counts the slots processing on each of the set's
# models, from the router's /slots, so a sample shows what was in flight when
# it was read.
#
# Usage: footprint.sh [-set name] [-profile path] <ssh destination> [samples] [seconds between]
#
# The set is -set, or SPIKE_SET, or else the profile's startup set; the
# profile is -profile, profiles/strix-halo.toml by default. Every model of the
# set must be loaded: the script checks GET /models first. It is read-only on
# the host: one non-interactive ssh per sample runs amdgpu_top (with --no-pc,
# so it reads no performance counter) and ps. The router is SPIKE_ROUTER_URL;
# its /slots reads carry autoload=false, so they never load a model. Start it
# beside the run to be measured.
set -euo pipefail
: "${SPIKE_ROUTER_URL:?set SPIKE_ROUTER_URL to the router whose host to sample}"
usage="usage: footprint.sh [-set name] [-profile path] <ssh destination> [samples] [seconds between]"
set_name=${SPIKE_SET:-}
profile_path=profiles/strix-halo.toml
while [ $# -gt 0 ]; do
	case $1 in
	-set) set_name=${2:?$usage} && shift 2 ;;
	-profile) profile_path=${2:?$usage} && shift 2 ;;
	*) break ;;
	esac
done
dest=${1:?$usage}
samples=${2:-6}
every=${3:-10}
router=${SPIKE_ROUTER_URL%/}

# The profile command describes the set, one fact a line (go run ./cmd/profile -h).
desc=$(GOWORK=off go run ./cmd/profile set -set "$set_name" "$profile_path")
set_name=$(awk '$1 == "set" { print $2 }' <<<"$desc")
mapfile -t ids < <(awk '$1 == "model" { print $2 }' <<<"$desc")

models=$(curl -sSf "$router/models")
for id in "${ids[@]}"; do
	status=$(jq -r --arg id "$id" '[.data[] | select(.id == $id)][0].status.value // "unlisted"' <<<"$models")
	[ "$status" = loaded ] || {
		echo "footprint: the router at $router lists $id as $status, not loaded; set $set_name must be loaded" >&2
		exit 1
	}
done

for ((i = 1; i <= samples; i++)); do
	at=$(date -u +%H:%M:%S)
	busy=()
	for id in "${ids[@]}"; do
		slots=$(curl -sSfG "$router/slots" --data-urlencode "model=$id" --data-urlencode autoload=false)
		busy+=("${id#*/}=$(jq '[.[] | select(.is_processing)] | length' <<<"$slots")")
	done
	# One ssh reads both, so the PIDs amdgpu_top names are the ones ps maps to
	# models in the same moment.
	read_out=$(ssh -o BatchMode=yes "$dest" 'amdgpu_top -p --no-pc; echo "--- ps"; ps -o pid=,args= -C llama-server')
	echo "sample $i at $at UTC, set $set_name; slots processing: ${busy[*]}"
	awk -v set="$set_name" '
		/^--- ps/ { ps = 1; next }
		ps {
			alias = ""
			for (f = 2; f < NF; f++) if ($f == "--alias") alias = $(f + 1)
			if (alias != "") name[$1] = alias
			next
		}
		/GTT .*\/.* MiB/ && !pool {
			split($0, a, "GTT ")
			split(a[2], b, "/")
			used = b[1] + 0
			split(b[2], c, " ")
			pool = c[1] + 0
			next
		}
		/^ *llama-server *\(/ {
			pid = $0; sub(/^[^(]*\( */, "", pid); sub(/\).*/, "", pid)
			v = $0; sub(/.*VRAM +/, "", v); v += 0
			g = $0; sub(/.*GTT +/, "", g); g += 0
			mem[pid] = v + g
		}
		END {
			for (pid in mem) { printf "  %-45s %6d MiB\n", (pid in name ? name[pid] : "pid " pid), mem[pid]; total += mem[pid] }
			printf "  %-45s %6d MiB\n", "set " set ", VRAM + GTT", total
			printf "  %-45s %6d MiB\n", "pool used (GTT)", used
			printf "  %-45s %6d MiB (%.2f GiB)\n", "pool free, of " pool, pool - used, (pool - used) / 1024
		}' <<<"$read_out"
	if ((i < samples)); then sleep "$every"; fi
done
