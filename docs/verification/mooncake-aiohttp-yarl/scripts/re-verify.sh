#!/usr/bin/env bash
# re-verify.sh — re-run the F1 contract test against the current PR #6212 head.
#
# Adapted from the review-finding-verifier stock re-verify.sh: the stock runner
# only knows gotest/ginkgo layers, while this harness's only layer is a shell
# contract test (pip resolution), so this script implements the same flow:
#   resolve PR head from verify-manifest.json's "pr" -> graft harness -> run ->
#   report Fixed / Still-broken per finding (contract polarity: pass == fixed).
#
# Usage (from a checkout of this verify branch):
#   bash docs/verification/mooncake-aiohttp-yarl/scripts/re-verify.sh [<ref>]
# With no <ref>, fetches the current PR head from the manifest's pr URL.
# Exit 0 iff every finding is fixed.
set -euo pipefail

TOPIC="mooncake-aiohttp-yarl"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="$HERE/verify-manifest.json"
FIXED_REF="${1:-}"

command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi

if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  echo "re-verify: resolving PR head via 'git fetch https://$host/$owner/$repo.git pull/$num/head'"
  git fetch --quiet "https://$host/$owner/$repo.git" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: head under test = $FIXED_REF"

LAST_REVIEWED_FILE="$HERE/.last-reviewed"
LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE" 2>/dev/null || echo '')"
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>} (review delta: ${LAST_REVIEWED:-base}..$FIXED_REF)"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
RUNTIME_DIR="$(mktemp -d)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; rm -rf "$RUNTIME_DIR" 2>/dev/null || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null
# graft the harness onto the code under test
git checkout "$HARNESS_SRC" -- "docs/verification/$TOPIC" 2>/dev/null || true

OUT="$RUNTIME_DIR/contract.out"
set +e
bash "docs/verification/$TOPIC/scripts/check-requirements-resolution.sh" > "$OUT" 2>&1
set -e
RESULT="$(awk -F'\t' '$1=="MooncakeRequirementsResolve"{print $2}' "$OUT" | tail -1)"
RESULT="${RESULT:-missing}"

mkdir -p "$HERE/results/reverify" 2>/dev/null || true
cp -f "$OUT" "$HERE/results/reverify/contract-$FIXED_REF.out" 2>/dev/null || true

printf '\n================  RE-VERIFY  ================\n'
printf '%-6s %-9s %-14s %s\n' ID POLARITY VERDICT DETAIL
case "$RESULT" in
  pass) verdict="FIXED";        detail="requirements resolve for cp312/manylinux x86_64"; rc=0;;
  fail)  verdict="STILL-BROKEN"; detail="pip resolution failed; see results/reverify/contract-$FIXED_REF.out"; rc=1;;
  *)     verdict="HARNESS-UPDATE"; detail="contract test produced no result line"; rc=1;;
esac
printf '%-6s %-9s %-14s %s\n' "F1" "contract" "$verdict" "$detail"
printf '\nReview delta this round: %s..%s\n' "${LAST_REVIEWED:-base}" "$FIXED_REF"
printf 'Advance the marker after the round:\n  echo %s > %s\n' "$FIXED_REF" "$LAST_REVIEWED_FILE"
exit "$rc"
