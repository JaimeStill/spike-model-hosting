#!/usr/bin/env bash
# currency reports every dependency, toolchain, and pin that trails its latest
# release, one line each, and exits non-zero when it reports any. It reads the
# network and writes nothing. Run it through `mise run currency`.
#
# Each scan captures its command's output in a variable before reading it, so
# under set -e a scan command that fails fails currency instead of reporting
# nothing.
set -euo pipefail
cd "$MISE_PROJECT_ROOT"
export GOWORK=off

stale=0
report() {
	echo "$1"
	stale=1
}

go_minor=$(mise latest go | cut -d. -f1,2)

for mod in $GO_MODULES; do
	# Direct requirements with a newer version within their major.
	updates=$(cd "$mod" && go list -m -u -f \
		'{{if and (not .Main) (not .Indirect) .Update}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' all)
	while read -r line; do
		[ -n "$line" ] && report "$mod/go.mod: $line"
	done <<<"$updates"

	# The go directive's minor against the current Go minor.
	directive=$(cd "$mod" && go mod edit -json | jq -r .Go | cut -d. -f1,2)
	[ "$directive" = "$go_minor" ] || report "$mod/go.mod: go $directive -> go $go_minor"
done

# mise tools pinned below their latest version.
outdated=$(mise outdated --bump --local --json |
	jq -r 'to_entries[] | "\(.key) \(.value.requested) -> \(.value.bump)"')
while read -r line; do
	[ -n "$line" ] && report "mise.toml: $line"
done <<<"$outdated"

# The engine and gateway pins of every host-class profile, against the newest
# release tag of each pin's upstream repository. Pre-releases count, since
# llama.cpp publishes every build as one. The profile tool lists the pins, one
# per line as "<profile> <name> <pin> <owner/repo>"; until the profile task
# adds it, there are no pins to scan.
if [ -d cmd/profile ]; then
	pins=$(go run ./cmd/profile pins)
	while read -r profile name pin repo; do
		[ -n "$profile" ] || continue
		latest=$(gh api "repos/$repo/releases?per_page=1" --jq '.[0].tag_name')
		[ "$pin" = "$latest" ] || report "$profile: $name $pin -> $latest"
	done <<<"$pins"
fi

exit "$stale"
