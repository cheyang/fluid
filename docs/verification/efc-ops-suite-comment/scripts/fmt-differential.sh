#!/usr/bin/env bash
# fmt-differential.sh — attribution evidence for the CI `lint` job failure on PR #6000.
#
# The lint job's "Code fmt check" step runs `gofmt -l ./pkg ./cmd ./api` and fails
# on any output. This script runs the identical check on the PR head and on the
# merge base and compares the two violation sets:
#   - identical sets        -> the failure is inherited from the base (pre-existing),
#                              NOT introduced by the PR.
#   - extra files on head   -> the PR introduces new gofmt violations (PR regression).
#
# Usage: bash fmt-differential.sh [<head-ref>] [<base-ref>]
# Defaults: head = current HEAD, base = merge-base of origin/master and HEAD.
# Run from the repository root. Writes results to
# docs/verification/efc-ops-suite-comment/results/ (or $RESULTS_DIR if set).
set -uo pipefail

HEAD_REF="${1:-HEAD}"
BASE_REF="${2:-}"

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if [ -z "$BASE_REF" ]; then
  BASE_REF="$(git merge-base origin/master "$HEAD_REF" 2>/dev/null || true)"
fi
[ -n "$BASE_REF" ] || { echo "fmt-differential: cannot resolve base ref" >&2; exit 2; }

RESULTS_DIR="${RESULTS_DIR:-$REPO_ROOT/docs/verification/efc-ops-suite-comment/results}"
mkdir -p "$RESULTS_DIR"

fmt_violations() { # $1 = ref -> prints sorted violation list
  git checkout -q "$1" 2>/dev/null || return 1
  gofmt -l ./pkg ./cmd ./api | sort
}

trap 'git checkout -q "$HEAD_REF" 2>/dev/null || true' EXIT

fmt_violations "$BASE_REF" > "$RESULTS_DIR/gofmt-base.txt" || exit 2
fmt_violations "$HEAD_REF" > "$RESULTS_DIR/gofmt-head.txt" || exit 2
git checkout -q "$HEAD_REF" 2>/dev/null || true

BASE_COUNT=$(wc -l < "$RESULTS_DIR/gofmt-base.txt" | tr -d ' ')
HEAD_COUNT=$(wc -l < "$RESULTS_DIR/gofmt-head.txt" | tr -d ' ')
NEW=$(comm -13 "$RESULTS_DIR/gofmt-base.txt" "$RESULTS_DIR/gofmt-head.txt")
FIXED_BY_PR=$(comm -23 "$RESULTS_DIR/gofmt-base.txt" "$RESULTS_DIR/gofmt-head.txt")

{
  echo "gofmt -l ./pkg ./cmd ./api  (same check as the CI lint job 'Code fmt check' step)"
  echo "base: $BASE_REF  ($BASE_COUNT files)"
  echo "head: $HEAD_REF  ($HEAD_COUNT files)"
  echo "--- new violations introduced on head (must be empty for PR-clean) ---"
  echo "${NEW:-<none>}"
  echo "--- violations present on base but not head ---"
  echo "${FIXED_BY_PR:-<none>}"
} | tee "$RESULTS_DIR/fmt-differential.txt"

if [ -n "$NEW" ]; then
  echo "VERDICT: PR-REGRESSION — new gofmt violations on head: $NEW" >&2
  exit 1
fi
echo "VERDICT: no new gofmt violations introduced by the head; CI fmt failure (if any) is inherited from the base."
