#!/usr/bin/env bash
# tau2-tiers draws the deep stage's τ2 task lists, evaluation/tiers/agent-tau2-<domain>.txt, with
# one seed: 12 of τ2 airline's 50 base-split tasks, and 12 of retail's 114. The suites project's
# `tau2_quality.py ids` lists a domain's task IDs from inspect_evals' own τ2 task, whose data ships
# inside inspect_evals, after its dataset line, and evaluation/cmd/tierlist draws the list from them
# with package evaluation/tier's Generate, so the same inspect_evals and seed draw the same lists
# again.
#
# Usage: tau2-tiers.sh
#
# Run it through `MISE_ENV=consumers mise run //evaluation:tau2-tiers`, which puts the pinned uv
# and Python on PATH. It rewrites the committed lists.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
export GOWORK=off

# The domains and their counts, as package evaluation/gate's Tau2Domains and Tau2Tasks name them.
count=12
seed=20261016

for d in airline retail; do
	ids=$(uv run --project evaluation/suites --locked --no-python-downloads --python "$(command -v python)" \
		python evaluation/suites/tau2_quality.py ids --domain "$d")
	go run ./evaluation/cmd/tierlist -tier "agent-tau2-$d" -n "$count" -seed "$seed" <<<"$ids" >"evaluation/tiers/agent-tau2-$d.txt"
	echo "tau2-tiers: evaluation/tiers/agent-tau2-$d.txt"
done
