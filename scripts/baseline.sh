#!/usr/bin/env bash
# baseline runs the consumers task's GuideLLM grid against the router
# SPIKE_ROUTER_URL names, from the laptop: gpt-oss-120b, Gemma 4 26B-A4B and
# gemma-4-E4B, each on a short profile (512 prompt tokens, 256 output) and an
# agentic one (8192, 512), each at 1 and 4 concurrent streams. Every cell
# prints the client's figures (GuideLLM's TTFT, ITL and output throughput)
# beside the engine's own, from the model's /metrics before and after the
# cell. Run it through `MISE_ENV=consumers mise run baseline`; it needs
# GuideLLM, which only the consumers environment pins.
#
# The data is GuideLLM's synthetic text at fixed token counts: the prompt is
# cut to its count by the model's own upstream tokenizer (fetched from Hugging
# Face into the gitignored .state/hf, since the router serves GGUF files that
# carry no tokenizer GuideLLM can read), and the output is held to its count
# with max_completion_tokens and ignore_eos, so every request generates the
# same number of tokens whatever the model would have said. The chat template
# adds a few tokens to the prompt, which the client's input count shows.
#
# Each stream sends a fixed number of requests back to back: 8 per stream on
# the short profile and 4 on the agentic one, so a cell at 4 streams sends 32
# or 16. GuideLLM's JSON for every cell, and the /metrics snapshots, stay under
# .state/baseline/<UTC stamp>/.
#
# The engine columns are llama.cpp's counters, differenced over the cell:
# prompt tokens over prompt seconds, and predicted tokens over predicted
# seconds. The server sums those seconds per slot, so at 4 streams they are
# each request's own rate, not the cell's aggregate; the client's output
# throughput is the aggregate.
#
# Each lookup captures its command's output in a variable before reading it,
# so under set -e a lookup that fails fails the run instead of printing nothing.
set -euo pipefail
cd "$MISE_PROJECT_ROOT"
: "${SPIKE_ROUTER_URL:?set SPIKE_ROUTER_URL to the router to measure}"

router=${SPIKE_ROUTER_URL%/}
stamp=$(date -u +%Y%m%dT%H%M%SZ)
out=.state/baseline/$stamp
mkdir -p "$out"
export HF_HOME=$PWD/.state/hf

build=$(curl -sSf "$router/props" | jq -r .build_info)
models=$(curl -sSf "$router/models")
guidellm_version=$(guidellm --version)
guidellm_version=${guidellm_version##* }

# tokenizer names the upstream Hugging Face repository whose tokenizer cuts a
# router model's synthetic prompts.
tokenizer() {
	case $1 in
	*gpt-oss-120b*) echo openai/gpt-oss-120b ;;
	*gemma-4-26B-A4B*) echo google/gemma-4-26B-A4B-it ;;
	*gemma-4-E4B*) echo google/gemma-4-E4B-it ;;
	*) return 1 ;;
	esac
}

# metric reads one counter from a /metrics snapshot.
metric() {
	awk -v k="llamacpp:$2" '$1 == k { print $2 }' "$1"
}

# snapshot saves the model's /metrics, never loading it.
snapshot() {
	curl -sSfG "$router/metrics" --data-urlencode "model=$1" --data-urlencode autoload=false >"$2"
}

echo "| Model | Profile | Streams | Completed | Errored | TTFT median ms | TTFT p95 ms | ITL median ms | Output tok/s | Engine prompt tok/s | Engine gen tok/s | Engine cached tokens |"
echo "|---|---|---|---|---|---|---|---|---|---|---|---|"
for pattern in gpt-oss-120b gemma-4-26B-A4B gemma-4-E4B; do
	model=$(jq -r --arg p "$pattern" '[.data[] | select(.id | contains($p))][0] | select(.status.value == "loaded") | .id' <<<"$models")
	[ -n "$model" ] || {
		echo "baseline: the router at $router has no loaded model matching $pattern" >&2
		exit 1
	}
	tok=$(tokenizer "$model")
	for shape in short:512:256:8 agentic:8192:512:4; do
		IFS=: read -r profile prompt output per_stream <<<"$shape"
		for streams in 1 4; do
			cell=$out/$pattern-$profile-c$streams
			count=$((per_stream * streams))
			jq -n --arg target "$router" --arg model "$model" --arg tok "$tok" \
				--argjson streams "$streams" --argjson prompt "$prompt" --argjson output "$output" \
				--argjson count "$count" --arg path "$cell.json" '{spec: {
					backend: {kind: "openai_http", target: $target, model: $model, request_format: "/v1/chat/completions"},
					tokenizer: {kind: "huggingface_auto", model: $tok},
					profile: {kind: "concurrent", streams: $streams},
					data: [{kind: "synthetic_text", prompt_tokens: $prompt, output_tokens: $output}],
					constraints: [{kind: "max_requests", count: $count}],
					outputs: [{kind: "json", path: $path}]}}' >"$cell.scenario.json"
			snapshot "$model" "$cell.before.metrics"
			busy=$(metric "$cell.before.metrics" requests_processing)
			[ "${busy%.*}" = 0 ] || echo "baseline: $model had $busy requests in flight before $profile at $streams" >&2
			guidellm run -c "$cell.scenario.json" --disable-console >"$cell.log" 2>&1 || {
				cat "$cell.log" >&2
				echo "baseline: GuideLLM failed on $model, $profile at $streams" >&2
				exit 1
			}
			snapshot "$model" "$cell.after.metrics"
			engine=()
			for pair in prompt_tokens_total:prompt_seconds_total tokens_predicted_total:tokens_predicted_seconds_total; do
				IFS=: read -r tokens seconds <<<"$pair"
				engine+=("$(awk -v t0="$(metric "$cell.before.metrics" "$tokens")" -v t1="$(metric "$cell.after.metrics" "$tokens")" \
					-v s0="$(metric "$cell.before.metrics" "$seconds")" -v s1="$(metric "$cell.after.metrics" "$seconds")" \
					'BEGIN { if (s1 > s0) printf "%.1f", (t1 - t0) / (s1 - s0); else print "—" }')")
			done
			cached=$(awk -v c0="$(metric "$cell.before.metrics" prompt_tokens_cached_total)" \
				-v c1="$(metric "$cell.after.metrics" prompt_tokens_cached_total)" 'BEGIN { printf "%d", c1 - c0 }')
			client=$(jq -r '.benchmarks[0] | .metrics as $m | [
				(.requests.successful | length), ((.requests.errored | length) + (.requests.incomplete | length)),
				($m.time_to_first_token_ms.successful.median | round), ($m.time_to_first_token_ms.successful.percentiles.p95 | round),
				($m.inter_token_latency_ms.successful.median * 10 | round / 10), ($m.output_tokens_per_second.successful.mean * 10 | round / 10)
				] | map(tostring) | join(" | ")' "$cell.json")
			echo "| $pattern | $profile $prompt/$output | $streams | $client | ${engine[0]} | ${engine[1]} | $cached |"
		done
	done
done
echo
echo "Router: $router, build $build; GuideLLM $guidellm_version; results in $out"
