#!/usr/bin/env bash
# asr-tiers draws the audio role's tier lists, evaluation/tiers/asr-<set>.txt: 150 utterances of
# each Open ASR Leaderboard English set the quality stage runs, with one seed, from every ID of
# the set the leaderboard scores. The suites project's `asr_quality.py ids` lists a set's IDs at
# its pinned revision, after its dataset line, and evaluation/cmd/tierlist draws the list from
# them with package evaluation/tier's Generate, so the same revisions and seed draw the same
# lists again.
#
# Usage: asr-tiers.sh
#
# Run it through `MISE_ENV=consumers mise run //evaluation:asr-tiers`, which puts the pinned uv
# and Python on PATH. It reads the network, downloading each set's parquet into the state
# directory's Hugging Face cache, .state/cache/huggingface, and rewrites the committed lists.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
export GOWORK=off HF_HOME="$root/.state/cache/huggingface"

# The sets, as asr_quality.py's SETS and package evaluation/gate's AudioSets name them.
sets=(librispeech-clean librispeech-other voxpopuli ami earnings22 fleurs-en)
count=150
seed=20261016

for set in "${sets[@]}"; do
	ids=$(uv run --project evaluation/suites --locked --no-python-downloads --python "$(command -v python)" \
		python evaluation/suites/asr_quality.py ids --set "$set")
	go run ./evaluation/cmd/tierlist -tier "asr-$set" -n "$count" -seed "$seed" <<<"$ids" >"evaluation/tiers/asr-$set.txt"
	echo "asr-tiers: evaluation/tiers/asr-$set.txt"
done
