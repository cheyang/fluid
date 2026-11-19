#!/usr/bin/env bash
# L1 (deterministic) — PR #6270: azure/setup-helm 5.0.1 -> 5.1.0 pin checks.
#
# Contracts asserted (all polarity=contract; PASS = healthy):
#   PIN_IDENTICAL      both workflows pin the same azure/setup-helm ref
#   PIN_COMMENT_MATCH  the '# vX.Y.Z' comment matches on both pins
#   PIN_TAG_GENUINE    the pinned SHA equals the upstream tag it claims
#                      (git ls-remote https://github.com/azure/setup-helm.git)
#   PIN_NO_STRAGGLERS  no other file in .github/ references a different
#                      azure/setup-helm version
#
# Usage: check-pin.sh [REPO_DIR]   (REPO_DIR defaults to cwd)
# Emits "<check>\tpass|fail" lines; exit 0 iff all pass.
set -uo pipefail

REPO_DIR="${1:-.}"
EXPECTED_SHA="2f5e7bc67f1f9972ab16657bb795f36d8f04de0e"   # azure/setup-helm v5.1.0
EXPECTED_VER="v5.1.0"
WORKFLOWS=( ".github/workflows/kind-e2e.yml" ".github/workflows/backward-compatibility-e2e.yml" )

status() { printf '%s\t%s\n' "$1" "$2"; }

shas=(); vers=(); ok=1
for wf in "${WORKFLOWS[@]}"; do
  f="$REPO_DIR/$wf"
  if [ ! -f "$f" ]; then echo "missing file: $f" >&2; ok=0; continue; fi
  line="$(grep -E 'uses:\s*azure/setup-helm@' "$f" || true)"
  sha="$(printf '%s' "$line" | sed -E 's/.*azure\/setup-helm@([0-9a-f]{40}).*/\1/')"
  ver="$(printf '%s' "$line" | sed -nE 's/.*#\s*(v[0-9.]+).*/\1/p')"
  echo "$wf -> sha=$sha comment=$ver"
  shas+=("$sha"); vers+=("$ver")
done

if [ "${#shas[@]}" -eq 2 ] && [ "${shas[0]}" = "${shas[1]}" ]; then status PIN_IDENTICAL pass; else status PIN_IDENTICAL fail; ok=0; fi
if [ "${#vers[@]}" -eq 2 ] && [ "${vers[0]}" = "${vers[1]}" ]; then status PIN_COMMENT_MATCH pass; else status PIN_COMMENT_MATCH fail; ok=0; fi

# genuine-tag check against upstream (no local clone needed)
tag_sha="$(git ls-remote https://github.com/azure/setup-helm.git "refs/tags/${EXPECTED_VER}^{}" "refs/tags/${EXPECTED_VER}" 2>/dev/null | awk '{print $1}' | sort -u | tail -1)"
echo "upstream ${EXPECTED_VER} resolves to: ${tag_sha:-<none>}"
if [ -n "$tag_sha" ] && [ "${shas[0]:-}" = "$EXPECTED_SHA" ] && [ "$tag_sha" = "$EXPECTED_SHA" ]; then
  status PIN_TAG_GENUINE pass
else
  status PIN_TAG_GENUINE fail; ok=0
fi

stragglers="$(grep -rEo 'azure/setup-helm@[0-9a-f]{40}.*' "$REPO_DIR/.github" 2>/dev/null | grep -v "$EXPECTED_SHA" || true)"
if [ -z "$stragglers" ]; then status PIN_NO_STRAGGLERS pass; else status PIN_NO_STRAGGLERS fail; echo "$stragglers"; ok=0; fi

exit $((1-ok))
