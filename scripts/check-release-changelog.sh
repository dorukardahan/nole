#!/usr/bin/env bash
set -euo pipefail

# Verify that CHANGELOG.md documents the release tag about to be published.
# The release workflow runs this on the checked-out tag before any asset is
# built, so a tag pushed while its changes still sit under [Unreleased], or a
# tag whose version does not match the changelog, fails before publishing.
#
# usage: check-release-changelog.sh <tag> [repo-root]

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  printf 'usage: %s <tag> [repo-root]\n' "$0" >&2
  exit 2
fi

TAG=$1
ROOT=${2:-"$(git rev-parse --show-toplevel)"}
CHANGELOG="$ROOT/CHANGELOG.md"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

if [[ ! "$TAG" =~ ^v([0-9]+\.[0-9]+\.[0-9]+)(-[0-9A-Za-z.-]+)?$ ]]; then
  fail "release tag must look like vMAJOR.MINOR.PATCH or vMAJOR.MINOR.PATCH-prerelease: $TAG"
fi

if [ -n "${BASH_REMATCH[2]}" ]; then
  printf 'SKIP: %s is a prerelease tag; CHANGELOG.md only records stable versions\n' "$TAG"
  exit 0
fi

VERSION=${BASH_REMATCH[1]}

[ -f "$CHANGELOG" ] || fail "missing CHANGELOG.md"

# Collect, outside fenced code blocks: the number of headings for VERSION, how
# many of them are dated, whether the section has any body text, and whether a
# compare link for the tag exists.
read -r headings dated body links < <(
  awk -v version="$VERSION" -v tag="$TAG" '
    BEGIN {
      headings = 0; dated = 0; body = 0; links = 0; in_section = 0; in_fence = 0
      heading_prefix = "## [" version "]"
      link_prefix = "[" version "]: "
    }
    /^[[:space:]]*(```|~~~)/ { in_fence = !in_fence; next }
    in_fence { next }
    /^## / {
      in_section = 0
      if (index($0, heading_prefix) == 1) {
        rest = substr($0, length(heading_prefix) + 1)
        if (rest == "" || rest ~ /^[[:space:]]/) {
          headings++
          in_section = 1
          if (rest ~ /^ - [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9][[:space:]]*$/) {
            dated++
          }
        }
      }
      next
    }
    in_section && /[^[:space:]]/ { body = 1 }
    index($0, link_prefix) == 1 {
      url = substr($0, length(link_prefix) + 1)
      sub(/[[:space:]]+$/, "", url)
      n = length(url) - length(tag)
      if (n > 0 && substr(url, n + 1) == tag && substr(url, n - 2, 3) == "...") {
        links++
      }
    }
    END { print headings, dated, body, links }
  ' "$CHANGELOG"
)

if [ "$headings" -eq 0 ]; then
  fail "CHANGELOG.md has no '## [$VERSION] - YYYY-MM-DD' section for $TAG; move the release notes out of [Unreleased] before tagging"
fi
if [ "$headings" -gt 1 ]; then
  fail "CHANGELOG.md has $headings sections for $VERSION; expected exactly one"
fi
if [ "$dated" -ne 1 ]; then
  fail "CHANGELOG.md section for $VERSION must be dated as '## [$VERSION] - YYYY-MM-DD'"
fi
if [ "$body" -ne 1 ]; then
  fail "CHANGELOG.md section for $VERSION is empty"
fi
if [ "$links" -lt 1 ]; then
  fail "CHANGELOG.md is missing the '[$VERSION]: .../compare/...$TAG' link reference"
fi

printf 'OK: CHANGELOG.md documents %s\n' "$TAG"
