#!/usr/bin/env bash
# currency reports every dependency, toolchain, and pin that trails its latest
# release, one line each, and exits non-zero when it reports any. It reads the
# network and writes nothing. Run it through `mise run currency`. The actions it
# checks are those of the project root's workflows and, when the repository nests
# the project in a subdirectory, of the git top-level's. A tool directive's module
# is reported beside the direct requirements unless the module also requires it
# directly. An image pin is a compose or CI image: line or a compose service's
# Dockerfile FROM line, compared with the highest tag that carries the pin's
# variant suffix (18.6-alpine -> -alpine) and at least one dot, so a bare build
# number (Grafana's 98813352) never counts as a release.
#
# Each scan captures its command's output in a variable before reading it, so
# under set -e a scan command that fails (a go list missing a go.sum entry)
# fails currency instead of reporting nothing.
# See architecture/principles/rolling-currency.md
set -euo pipefail
cd "$MISE_PROJECT_ROOT"
export GOWORK=off

stale=0
report() {
	echo "$1"
	stale=1
}

matches() {
	local pattern=$1 file
	local files=()
	shift
	for file in "$@"; do
		if [ -e "$file" ]; then files+=("$file"); fi
	done
	[ ${#files[@]} -gt 0 ] || return 0
	grep -ho "$pattern" "${files[@]}" || [ $? -eq 1 ]
}

report_lines() {
	local prefix=$1 lines=$2 line
	while read -r line; do
		if [ -n "$line" ]; then
			report "$prefix: $line"
		fi
	done <<<"$lines"
}

report_outdated_requirements() {
	local mod=$1 updates
	updates=$(cd "$mod" && go list -m -u -f \
		'{{if and (not .Main) (not .Indirect) .Update}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' all)
	report_lines "$mod/go.mod" "$updates"
}

tool_modules() {
	local out err
	err=$(mktemp) || return
	if ! out=$(go list -f '{{.Module.Path}}' tool 2>"$err"); then
		cat "$err" >&2
		rm -f "$err"
		return 1
	fi
	rm -f "$err"
	if [ -n "$out" ]; then
		sort -u <<<"$out"
	fi
}

report_outdated_tool_modules() {
	local mod=$1 list updates
	local tools=()
	list=$(cd "$mod" && tool_modules)
	if [ -n "$list" ]; then
		mapfile -t tools <<<"$list"
		updates=$(cd "$mod" && go list -m -u -f \
			'{{if and (not .Main) .Indirect .Update}}{{.Path}} {{.Version}} -> {{.Update.Version}}{{end}}' "${tools[@]}")
		report_lines "$mod/go.mod" "$updates"
	fi
}

report_outdated_go_directive() {
	local mod=$1 go_minor=$2 directive
	directive=$(cd "$mod" && go mod edit -json | jq -r .Go | cut -d. -f1,2)
	[ "$directive" = "$go_minor" ] || report "$mod/go.mod: go $directive -> go $go_minor"
}

report_outdated_mise_tools() {
	local outdated
	outdated=$(mise outdated --bump --local --json |
		jq -r 'to_entries[] | "\(.key) \(.value.requested) -> \(.value.bump)"')
	report_lines "mise.toml" "$outdated"
}

workflow_files() {
	local root top
	root=$(pwd -P) || return
	top=$(git rev-parse --show-toplevel 2>/dev/null || echo "$root")
	printf '%s\n' "$root"/.github/workflows/*.yml
	if [ "$top" != "$root" ]; then
		printf '%s\n' "$top"/.github/workflows/*.yml
	fi
}

report_outdated_actions() {
	local workflows=("$@") actions uses action pin repo latest
	actions=$(matches 'uses: *[^ ]*@[^ ]*' "${workflows[@]}" | sed 's/uses: *//' | sort -u)
	for uses in $actions; do
		action=${uses%@*}
		pin=${uses#*@}
		repo=$(echo "$action" | cut -d/ -f1,2)
		latest=$(gh api "repos/$repo/releases/latest" --jq .tag_name)
		[ "$pin" = "$latest" ] || report ".github/workflows: $action $pin -> $latest"
	done
}

latest_tag() {
	local image=$1 pin=$2 suffix pattern latest
	suffix=${pin#"${pin%%[!0-9.]*}"}
	pattern="^[0-9]+(\.[0-9]+)+${suffix//./\\.}\$"
	latest=$(crane ls "$image" | grep -E "$pattern" | sed "s/${suffix}\$//" | sort -V | tail -n1) || return
	echo "$latest$suffix"
}

report_outdated_image() {
	local where=$1 ref=$2 image pin latest
	image=${ref%:*}
	pin=${ref##*:}
	latest=$(latest_tag "$image" "$pin")
	[ "$pin" = "$latest" ] || report "$where: $image $pin -> $latest"
}

report_outdated_image_lines() {
	local workflows=("$@") images ref
	images=$(matches '^ *image: *[^ ]*' compose.yml compose/*.yml "${workflows[@]}" |
		sed 's/^ *image: *//' | tr -d "\"'" | sort -u)
	for ref in $images; do
		report_outdated_image images "$ref"
	done
}

report_outdated_dockerfiles() {
	local dockerfile ref
	for dockerfile in compose/*/Dockerfile; do
		if [ -e "$dockerfile" ]; then
			ref=$(sed -n 's/^FROM  *\([^ ]*\).*/\1/p' "$dockerfile" | head -n1)
			report_outdated_image "$dockerfile" "$ref"
		fi
	done
}

go_minor=$(mise latest go | cut -d. -f1,2)
for mod in $GO_MODULES; do
	report_outdated_requirements "$mod"
	report_outdated_tool_modules "$mod"
	report_outdated_go_directive "$mod" "$go_minor"
done
report_outdated_mise_tools
workflow_list=$(workflow_files)
mapfile -t workflows <<<"$workflow_list"
report_outdated_actions "${workflows[@]}"
report_outdated_image_lines "${workflows[@]}"
report_outdated_dockerfiles
exit "$stale"
