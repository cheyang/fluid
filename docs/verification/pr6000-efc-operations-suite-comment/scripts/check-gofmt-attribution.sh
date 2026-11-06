#!/usr/bin/env bash
# check-gofmt-attribution.sh — differential gofmt attribution for PR #6000.
#
# Replicates the CI "Code fmt check" step (gofmt -l ./pkg ./cmd ./api) on two
# pristine trees (git archive of BASE and HEAD) and attributes any flagged
# file: pre-existing (flagged at BASE too) vs introduced by the PR (new at
# HEAD). Exits non-zero iff HEAD flags a file that BASE does not, or the PR's
# own changed file is flagged.
#
# Usage: check-gofmt-attribution.sh [BASE_REF [HEAD_REF]]
#   defaults: BASE_REF = recorded round-1 merge-base, HEAD_REF = HEAD
set -euo pipefail

BASE_REF="${1:-da69206da6388e46a8e9e32834d6b22e53fe14a9}"
HEAD_REF="${2:-HEAD}"
CHANGED_FILE="pkg/ddc/efc/operations/operations_suite_test.go"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

flagged() { # $1=ref -> prints sorted gofmt-flagged files
  local ref="$1" dir="$TMP/$2"
  mkdir -p "$dir"
  git archive "$ref" | tar -x -C "$dir"
  (cd "$dir" && gofmt -l ./pkg ./cmd ./api 2>/dev/null || true) | sort
}

flagged "$BASE_REF" base > "$TMP/base.txt"
flagged "$HEAD_REF" head > "$TMP/head.txt"

echo "gofmt-flagged at BASE ($BASE_REF):";  sed 's/^/  /' "$TMP/base.txt"
echo "gofmt-flagged at HEAD ($HEAD_REF):";  sed 's/^/  /' "$TMP/head.txt"

NEW_FAILURES="$(comm -13 "$TMP/base.txt" "$TMP/head.txt")"
rc=0
if [ -n "$NEW_FAILURES" ]; then
  echo "FAIL: HEAD introduces NEW gofmt failures absent at BASE:"; sed 's/^/  /' <<<"$NEW_FAILURES"; rc=1
fi
if grep -qx "$CHANGED_FILE" "$TMP/head.txt"; then
  echo "FAIL: the PR's own changed file is gofmt-flagged: $CHANGED_FILE"; rc=1
fi
[ "$rc" -eq 0 ] && echo "PASS: no new gofmt failures vs base; changed file is gofmt-clean (any red 'Code fmt check' is baseline-inherited)"
exit "$rc"
