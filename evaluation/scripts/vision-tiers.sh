#!/usr/bin/env bash
# vision-tiers draws the vision role's tier lists, evaluation/tiers/vision-<benchmark>.txt, with
# one seed: 100 of V*Bench's 191 questions, 150 of DocVQA's 5349 validation questions, and 200 of
# MMMU's 847 validation multiple-choice questions, across its 30 subjects. The suites project's
# `vision_quality.py ids` lists a benchmark's sample IDs from inspect_evals' own task, at the
# dataset revision inspect_evals pins, after its dataset line, and evaluation/cmd/tierlist draws
# the list from them with package evaluation/tier's Generate, so the same revisions and seed draw
# the same lists again.
#
# Usage: vision-tiers.sh
#
# Run it through `MISE_ENV=consumers mise run //evaluation:vision-tiers`, which puts the pinned uv
# and Python on PATH. It reads the network, downloading each benchmark into the state directory's
# caches, .state/cache/huggingface and .state/cache/inspect_evals (DocVQA's validation and test
# parquet are some 16GB), and rewrites the committed lists.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
export GOWORK=off HF_HOME="$root/.state/cache/huggingface" INSPECT_EVALS_CACHE_DIR="$root/.state/cache/inspect_evals"

# The benchmarks and their counts, as vision_quality.py's BENCHMARKS and package evaluation/gate's
# VisionTiers name them.
declare -A counts=([vstar]=100 [docvqa]=150 [mmmu]=200)
seed=20261016

for bench in vstar docvqa mmmu; do
	ids=$(uv run --project evaluation/suites --locked --no-python-downloads --python "$(command -v python)" \
		python evaluation/suites/vision_quality.py ids --tier "$bench")
	go run ./evaluation/cmd/tierlist -tier "vision-$bench" -n "${counts[$bench]}" -seed "$seed" <<<"$ids" >"evaluation/tiers/vision-$bench.txt"
	echo "vision-tiers: evaluation/tiers/vision-$bench.txt"
done
