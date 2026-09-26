#!/bin/sh
# Print the CHANGELOG.md section for one version as GitHub release notes.
# Usage: release-notes.sh <version> [changelog]   (version without the leading v)
set -eu

ver=${1:?usage: release-notes.sh <version> [changelog]}
file=${2:-CHANGELOG.md}

notes=$(awk -v ver="$ver" '
	index($0, "## [" ver "]") == 1 { found = 1; next }
	found && /^## \[/ { exit }
	found && /^\[[^]]+\]: / { exit }
	found { print }
' "$file" | sed -e '/./,$!d')

if [ -z "$notes" ]; then
	printf 'err: no CHANGELOG entry for %s in %s\nfix: add a "## [%s] - YYYY-MM-DD" section\n' \
		"$ver" "$file" "$ver" >&2
	exit 1
fi

printf '%s\n' "$notes"

compare=$(grep -F "[$ver]: " "$file" | sed 's/^[^:]*: //' || true)
if [ -n "$compare" ]; then
	printf '\n**Full diff:** %s\n' "$compare"
fi
