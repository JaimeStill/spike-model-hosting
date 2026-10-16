#!/usr/bin/env bash
# gpt-oss-compat runs OpenAI's gpt-oss compatibility test (the gpt-oss
# repository's compatibility-test/, 30 cases) against the router
# SPIKE_ROUTER_URL names, over Chat Completions with -k 5, and prints the
# suite's summary, the cases that failed, and the build and runtime it ran on.
# It is the consumers task's engine row: it measures the engine and model at
# the Chat Completions layer, not the Anthropic messages the consumers use.
# Run it from the laptop through
# `MISE_ENV=consumers mise run //evaluation:gpt-oss-compat`.
#
# The suite is downloaded code, so it is fetched at a pinned commit into its
# own directory under the repository root's gitignored .state/, its dependencies installed from
# its lockfile with install scripts off. Its package.json starts it with tsx,
# which it doesn't depend on, so tsx is installed beside it at a pinned version.
# The router ignores the API key, so the suite gets a dummy one, and no real
# OpenAI or Anthropic key reaches it.
#
# Each lookup captures its command's output in a variable before reading it,
# so under set -e a lookup that fails fails the run instead of printing nothing.
set -euo pipefail
# The repository root, two directories above the script, holds .state/.
cd "$(dirname -- "${BASH_SOURCE[0]}")/../.."
: "${SPIKE_ROUTER_URL:?set SPIKE_ROUTER_URL to the router to test}"

commit=7b583341fe16729127f6d5b94a7b09ccae97e1a1
tsx_version=4.23.15
tries=5
router=${SPIKE_ROUTER_URL%/}
suite=.state/gpt-oss-compat/$commit

build=$(curl -sSf "$router/props" | jq -r .build_info)
models=$(curl -sSf "$router/models")
model=$(jq -r '[.data[].id | select(test("gpt-oss-120b"))][0] // empty' <<<"$models")
[ -n "$model" ] || {
	echo "gpt-oss-compat: the router at $router lists no gpt-oss-120b model" >&2
	exit 1
}
node_version=$(node --version)

if [ ! -f "$suite/package-lock.json" ]; then
	rm -rf "$suite"
	mkdir -p "$suite"
	curl -sSfL "https://codeload.github.com/openai/gpt-oss/tar.gz/$commit" |
		tar -xz -C "$suite" --strip-components=2 "gpt-oss-$commit/compatibility-test"
fi
if [ ! -x "$suite/node_modules/.bin/tsx" ]; then
	(cd "$suite" && npm ci --ignore-scripts --no-audit --no-fund &&
		npm install --no-save --ignore-scripts --no-audit --no-fund "tsx@$tsx_version")
fi

# The README's step 1: one provider entry, Chat Completions only. jq quotes
# the router and model as JSON strings, which TypeScript reads as they are.
cat >"$suite/providers.ts" <<EOF
export const PROVIDERS = {
  router: {
    apiBaseUrl: $(jq -n --arg v "$router/v1" '$v'),
    apiKey: "dummy",
    apiType: ["chat"],
    modelName: $(jq -n --arg v "$model" '$v'),
    providerDetails: {},
  },
};
EOF

log=$(mktemp "${TMPDIR:-/tmp}/gpt-oss-compat.XXXXXX")
trap 'rm -f "$log"' EXIT
start=$(date +%s)
(cd "$suite" && env -u ANTHROPIC_API_KEY OPENAI_API_KEY=dummy \
	node node_modules/.bin/tsx index.ts --provider router -k "$tries") >"$log" 2>&1
elapsed=$(($(date +%s) - start))

rollout=$(sed -n 's/^Results written to //p' "$log")
[ -n "$rollout" ] || {
	cat "$log" >&2
	echo "gpt-oss-compat: the suite wrote no results" >&2
	exit 1
}

sed -n '/^Summary:/,$p' "$log"
echo
# A request the router answered with an error throws out of the run, so the
# suite writes an error record and leaves that attempt out of its analysis;
# it still counts against pass@k and pass^k as a missing success.
errors=$(jq -r 'select(.error) | "  case \(.test_case) (\(.tool_name)): \(.error)"' "$rollout")
echo "Errored runs (not in the suite's counts):"
echo "${errors:-  none}"
failures=$(jq -rs --argjson k "$tries" '
	map(select(.result) | {c: .test_case, t: .tool_name, ok: .success,
		valid: .result.validResponse, called: (.result.toolCallingDetails.calledToolAtLeastOnce // false)})
	| group_by(.c)[]
	| select((map(.ok) | all | not) or length < $k)
	| "  case \(.[0].c) (\(.[0].t)): \(map(select(.ok)) | length)/\($k) passed, \(map(select(.valid | not)) | length) invalid, \(map(select(.called | not)) | length) without the tool call"' "$rollout")
echo "Cases short of $tries/$tries:"
echo "${failures:-  none}"
# The suite counts a Chat Completions response valid only when its message
# carries the reasoning in a field named `reasoning`; these are the field
# checks of each invalid response, tallied.
causes=$(jq -rs 'map(select(.result and (.result.validResponse | not)) | .result.details | tojson)
	| group_by(.)[] | "  \(length) x \(.[0])"' "$rollout")
echo "Invalid responses by field check:"
echo "${causes:-  none}"
# The same pass@k and pass^k on the tool call alone, response shape aside:
# the right tool, with arguments its schema accepts. An errored run is a miss.
cases=$(grep -c . "$suite/cases.jsonl")
toolcall=$(jq -rs --argjson k "$tries" --argjson n "$cases" '
	map(select(.result)) | group_by(.test_case) | map(map(select(.result.successToolCall)) | length)
	| "  pass@\($k): \(map(select(. > 0)) | length)/\($n), pass^\($k): \(map(select(. == $k)) | length)/\($n)"' "$rollout")
echo "Tool call alone:"
echo "$toolcall"
echo
echo "Router: $router, build $build, model $model"
echo "Suite: openai/gpt-oss@$commit compatibility-test, Node $node_version, tsx $tsx_version"
echo "Runtime: $((elapsed / 60))m$((elapsed % 60))s"
echo "Rollout: $suite/${rollout##*/}"
