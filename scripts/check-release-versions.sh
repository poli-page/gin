#!/usr/bin/env bash
# scripts/check-release-versions.sh: a release's major.minor must be the
# pinned API contract's and the required github.com/poli-page/sdk-go's.
#
# Usage: scripts/check-release-versions.sh vX.Y.Z   (or X.Y.Z, or a prerelease)
#
# The module shares its major.minor with the Poli Page API, and integration
# 0.N requires github.com/poli-page/sdk-go 0.N.x. This script:
#   - fails unless go.mod and example-app/go.mod require
#     github.com/poli-page/sdk-go at a stable vX.Y.Z with the release's
#     major.minor, and neither replaces it;
#   - reads the contract commit pinned in .github/workflows/ci.yml (never its
#     `# vX.Y.Z` comment), lists the contract repository's tags, and fails
#     unless that commit carries a stable tag vX.Y.Z with the release's
#     major.minor.
# Patch numbers are free.
#
# A Go release is the pushed tag itself, so run this before pushing the tag
# (CONTRIBUTING.md, "Each release"); the Release workflow re-runs it on the
# pushed tag. API_CONTRACT_REMOTE overrides the contract repository (to check
# against a local one).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKFLOW="$REPO_ROOT/.github/workflows/ci.yml"
REMOTE="${API_CONTRACT_REMOTE:-https://github.com/poli-page/api-contract.git}"
SDK_MODULE="github.com/poli-page/sdk-go"

fail() { echo "check-release-versions: $1" >&2; exit 1; }

[ "$#" -eq 1 ] || fail "usage: $(basename "$0") vX.Y.Z"
version="${1#v}"
if ! [[ "$version" =~ ^([0-9]+)\.([0-9]+)\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  fail "release version '$1' is not vX.Y.Z"
fi
release_minor="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}"

# The SDK requirement: a `require` line (single or in a block) naming the SDK
# module. go.mod cannot express an upper bound, so the required version itself
# must be of the release's major.minor.
for manifest in go.mod example-app/go.mod; do
  file="$REPO_ROOT/$manifest"
  [ -f "$file" ] || fail "$manifest is missing"
  required="$(
    awk -v mod="$SDK_MODULE" '
      { sub(/\/\/.*/, "") }
      $1 == "require" && $2 == "(" { block = 1; next }
      block && $1 == ")" { block = 0; next }
      block && $1 == mod { print $2 }
      !block && $1 == "require" && $2 == mod { print $3 }
    ' "$file"
  )"
  [ -n "$required" ] || fail "$manifest does not require $SDK_MODULE"
  [ "$(wc -l <<<"$required" | tr -d ' ')" = 1 ] || fail "$manifest requires $SDK_MODULE more than once"
  if awk -v mod="$SDK_MODULE" '
      { sub(/\/\/.*/, "") }
      $1 == "replace" && $2 == "(" { block = 1; next }
      block && $1 == ")" { block = 0; next }
      (block && $1 == mod) || (!block && $1 == "replace" && $2 == mod) { found = 1 }
      END { exit !found }
    ' "$file"; then
    fail "$manifest replaces $SDK_MODULE: a release requires the published SDK"
  fi
  if ! [[ "$required" =~ ^v([0-9]+)\.([0-9]+)\.[0-9]+$ ]]; then
    fail "release v$version needs $SDK_MODULE v$release_minor.x, but $manifest requires '$required' (not a stable vX.Y.Z)"
  fi
  sdk_minor="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}"
  if [ "$sdk_minor" != "$release_minor" ]; then
    fail "release v$version and the required $SDK_MODULE $required ($manifest) differ in major.minor: require $SDK_MODULE v$release_minor.x, or release v$sdk_minor.x"
  fi
done
echo "$SDK_MODULE v$release_minor.x is required, matching release v$version"

contract_sha="$(sed -n 's|.*uses: poli-page/api-contract@\([0-9a-f]\{40\}\).*|\1|p' "$WORKFLOW" | head -n 1)"
[ -n "$contract_sha" ] || fail "no poli-page/api-contract pin found in .github/workflows/ci.yml"

tags="$(git ls-remote --tags "$REMOTE")" || fail "cannot list the tags of $REMOTE"

# An annotated tag is listed twice: its own object, then `^{}` with the commit
# it points at. A lightweight tag is listed once, with its commit.
stable_tags="$(
  awk -v sha="$contract_sha" '$1 == sha { ref = $2; sub(/^refs\/tags\//, "", ref); sub(/\^\{\}$/, "", ref); print ref }' <<<"$tags" \
    | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -u -V || true
)"
short_sha="${contract_sha:0:8}"
if [ -z "$stable_tags" ]; then
  fail "release v$version needs the API contract v$release_minor.x, but the pinned contract commit $short_sha has no stable vX.Y.Z tag: pin a released contract"
fi

while IFS= read -r tag; do
  if [[ "$tag" =~ ^v([0-9]+)\.([0-9]+)\. ]] && [ "${BASH_REMATCH[1]}.${BASH_REMATCH[2]}" = "$release_minor" ]; then
    echo "API contract $tag ($short_sha) matches release v$version"
    exit 0
  fi
done <<<"$stable_tags"

pinned="$(tr '\n' ' ' <<<"$stable_tags" | sed 's/ $//')"
# The newest of the commit's tags, which may carry two (an unchanged contract).
[[ "$(tail -n 1 <<<"$stable_tags")" =~ ^v([0-9]+)\.([0-9]+)\. ]]
fail "release v$version and the pinned API contract $pinned ($short_sha) differ in major.minor: pin the contract v$release_minor.x, or release v${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.x"
