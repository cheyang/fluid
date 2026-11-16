#!/usr/bin/env bash
# re-verify.sh — driver for the PR #6255 (Go 1.26.11 toolchain bump) harness.
#
# Usage:  bash scripts/re-verify.sh [<fixed-ref>]
#         (no ref -> resolves the current PR head from verify-manifest.json's
#          `pr` URL, machine-independently via `git fetch <repo> pull/<n>/head`)
#
# Grafts docs/verification/go12611-toolchain-bump/ onto the fixed ref, runs the
# contract checks (digest, vuln, consistency, build+unit), and reports per
# check Fixed / Still-broken. Exit 0 iff every check passes.
#
# Checks (all contract polarity — they assert intended-correct behavior):
#   C4-digest   pinned image digest == live golang:1.26.11-bookworm index digest
#   C1-vuln     GO-2026-6859 / stdlib advisories cleared at symbol level
#   C5-consistency  no stale 1.26.10 pins; workflow/travis/go.mod updated
#   C2-build    go build + vet with the PR toolchain
#   C2-unit     make-test-equivalent unit suite (FLUID_UNIT_TEST on xargs!)
#
# The premise check (P0, base branch) is NOT re-run here: it decides whether
# the problem existed before the PR, which the base evidence in results/
# already answers. To re-do it by hand:
#   git worktree add /tmp/fluid-base <base-sha>
#   cd /tmp/fluid-base && GOTOOLCHAIN=go1.26.10 GOFLAGS=-mod=vendor govulncheck ./cmd/... ./pkg/... ./api/...
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found: $MANIFEST" >&2; exit 2; }

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  cloneurl="https://$host/$owner/$repo.git"
  echo "re-verify: resolving PR head via git fetch $cloneurl pull/$num/head"
  git fetch --quiet "$cloneurl" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: fixed-ref = $FIXED_REF"

git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
RESULTS_DIR="$TOPIC_DIR/results/reverify-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$RESULTS_DIR"

cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
git checkout "$HARNESS_SRC" -- "$TOPIC_DIR" 2>/dev/null || { echo "re-verify: failed to graft harness" >&2; exit 2; }

ALL=1
run_check() { # $1 = id, $2 = script, $3 = log
  local id="$1" script="$2" log="$3"
  if bash "$HERE/$script" | tee "$log"; then
    printf '%-16s %-14s %s\n' "$id" "FIXED" "$(grep "CHECK $id" "$log" | head -1 | sed 's/^CHECK [^ ]* //')"
  else
    printf '%-16s %-14s %s\n' "$id" "STILL-BROKEN" "see $log"
    ALL=0
  fi
}

printf '\n================  RE-VERIFY (PR #6255 harness)  ================\n'
printf '%-16s %-14s %s\n' CHECK VERDICT DETAIL
run_check C4-digest        check-digest.sh       "$RESULTS_DIR/digest.log"
run_check C5-consistency   check-consistency.sh  "$RESULTS_DIR/consistency.log"
run_check C1-vuln          check-vuln.sh         "$RESULTS_DIR/vuln.log"
run_check C2-build         run-unit-tests.sh     "$RESULTS_DIR/build-unit.log"

LAST_REVIEWED_FILE="$TOPIC_DIR/.last-reviewed"
if [ -s "$LAST_REVIEWED_FILE" ]; then
  LR="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE")"
else
  LR="$(git merge-base origin/master "$FIXED_REF" 2>/dev/null || echo base)"
fi
printf '\nReview delta this round: %s..%s\n' "$LR" "$FIXED_REF"
printf 'After reviewing that delta, advance the marker:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
  "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"

if [ "$ALL" -eq 1 ]; then echo "RESULT: all contract checks pass."; exit 0
else echo "RESULT: some checks still broken."; exit 1; fi
