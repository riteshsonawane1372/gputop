#!/usr/bin/env bash
# Copyright 2026 The gputop Authors
# SPDX-License-Identifier: Apache-2.0
#
# Semantic versions derived from git tags, used by the release workflows.
#
#   scripts/version.sh prerelease          next main prerelease, e.g. v0.0.2-main.7
#                                          (next patch after the latest stable tag,
#                                          numbered by commits since that tag);
#                                          prints nothing when HEAD is a stable tag
#   scripts/version.sh stable patch|minor|major
#                                          next stable version, e.g. v0.1.0
#   scripts/version.sh previous TAG        tag the changelog of TAG starts from:
#                                          the previous stable tag for a stable TAG,
#                                          the previous tag of any kind otherwise
set -euo pipefail

semver='^v[0-9]+\.[0-9]+\.[0-9]+$'

# tags lists version tags newest first; prereleases sort below their release.
tags() {
	git -c versionsort.suffix=- tag --list 'v*' --sort=-v:refname
}

latest_stable() {
	tags | grep -E "$semver" | head -n1 || true
}

case "${1:-}" in
prerelease)
	if git tag --points-at HEAD | grep -qE "$semver"; then
		exit 0
	fi
	base=$(latest_stable)
	if [[ -n $base ]]; then
		count=$(git rev-list --count "$base..HEAD")
	else
		base=v0.0.0
		count=$(git rev-list --count HEAD)
	fi
	IFS=. read -r major minor patch <<<"${base#v}"
	echo "v$major.$minor.$((patch + 1))-main.$count"
	;;
stable)
	base=$(latest_stable)
	IFS=. read -r major minor patch <<<"${base:-v0.0.0}"
	major=${major#v}
	case "${2:-}" in
	patch) patch=$((patch + 1)) ;;
	minor) minor=$((minor + 1)) patch=0 ;;
	major) major=$((major + 1)) minor=0 patch=0 ;;
	*)
		echo "usage: $0 stable patch|minor|major" >&2
		exit 2
		;;
	esac
	echo "v$major.$minor.$patch"
	;;
previous)
	tag=${2:?usage: $0 previous TAG}
	if [[ $tag =~ $semver ]]; then
		candidates=$(tags | grep -E "$semver" || true)
	else
		candidates=$(tags)
	fi
	# The first candidate listed after TAG is the one before it.
	awk -v tag="$tag" 'found { print; exit } $0 == tag { found = 1 }' <<<"$candidates"
	;;
*)
	sed -n '4,15p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit 2
	;;
esac
