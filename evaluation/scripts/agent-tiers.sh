#!/usr/bin/env bash
# agent-tiers draws the agent role's BFCL tier lists, evaluation/tiers/agent-<tier>.txt, with one
# seed: 100 of BFCL's 3641 single-turn questions across its 13 leaderboard categories, and 20 of
# its 800 multi-turn conversations across its 4. The suites project's `agent_quality.py ids` lists
# a tier's sample IDs from inspect_evals' own bfcl task, at the Gorilla commit inspect_evals pins,
# after its dataset line, and evaluation/cmd/tierlist draws the list from them with package
# evaluation/tier's Generate, so the same commit and seed draw the same lists again.
#
# Usage: agent-tiers.sh
#
# Run it through `MISE_ENV=consumers mise run //evaluation:agent-tiers`, which puts the pinned uv
# and Python on PATH. It reads the network, cloning BFCL's data into the state directory's cache,
# .state/cache/inspect_evals/BFCL, and rewrites the committed lists.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"
export GOWORK=off INSPECT_EVALS_CACHE_DIR="$root/.state/cache/inspect_evals"

# The tiers and their counts, as agent_quality.py's TIERS and package evaluation/gate's
# AgentTiers name them.
declare -A counts=([bfcl-single]=100 [bfcl-multi]=20)
seed=20261016

for t in bfcl-single bfcl-multi; do
	ids=$(uv run --project evaluation/suites --locked --no-python-downloads --python "$(command -v python)" \
		python evaluation/suites/agent_quality.py ids --tier "$t")
	go run ./evaluation/cmd/tierlist -tier "agent-$t" -n "${counts[$t]}" -seed "$seed" <<<"$ids" >"evaluation/tiers/agent-$t.txt"
	echo "agent-tiers: evaluation/tiers/agent-$t.txt"
done
