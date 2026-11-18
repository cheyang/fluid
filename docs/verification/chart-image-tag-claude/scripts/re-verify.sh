#!/usr/bin/env bash
# re-verify.sh — re-run this harness against the current PR head (or a fixed ref).
# Adapted from the review-finding-verifier reference re-verify.sh for a shell-script
# harness (kind "shell"): contract tests are FIXED iff the script exits 0.
#
# Usage (from a checkout of the branch that contains this harness):
#   bash docs/verification/chart-image-tag-claude/scripts/re-verify.sh [<fixed-ref>]
#
# With no ref it resolves the current PR head from manifest.pr
# (git fetch <repo-url> pull/<n>/head — machine-independent). Exit 0 iff all
# findings are Fixed.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="$HERE/verify-manifest.json"
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  cloneurl="https://$host/$owner/$repo.git"
  echo "re-verify: resolving PR head via 'git fetch $cloneurl pull/$num/head'"
  git fetch --quiet "$cloneurl" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: fixed-ref = $FIXED_REF"

BASE_SHA="$(jq -r '.baseSha // empty' "$MANIFEST")"
LAST_REVIEWED_FILE="$HERE/.last-reviewed"
LAST_REVIEWED="$([ -s "$LAST_REVIEWED_FILE" ] && tr -d '[:space:]' < "$LAST_REVIEWED_FILE" || echo "$BASE_SHA")"
echo "re-verify: last-reviewed = $LAST_REVIEWED  (review delta = $LAST_REVIEWED..$FIXED_REF)"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
RUNTIME_DIR="$(mktemp -d)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; rm -rf "$RUNTIME_DIR" 2>/dev/null || true; }
trap cleanup EXIT
cp -r "$HERE" "$RUNTIME_DIR/harness"

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
git checkout "$HARNESS_SRC" -- docs/verification/chart-image-tag-claude 2>/dev/null \
  || { echo "re-verify: failed to graft harness" >&2; exit 2; }

printf '\n================  RE-VERIFY  ================\n'
printf '%-6s %-9s %-12s %-14s %s\n' ID POLARITY LAYER VERDICT DETAIL
ALL_FIXED=1

# F1 — chart image tags must exist on Docker Hub (contract; FIXED iff exit 0)
if bash "$RUNTIME_DIR/harness/scripts/check_image_tags.sh" > "$RUNTIME_DIR/F1.out" 2>&1; then F1=pass; else F1=fail; fi
if [ "$F1" = "pass" ]; then V="FIXED"; else V="STILL-BROKEN"; ALL_FIXED=0; fi
printf '%-6s %-9s %-12s %-14s %s\n' F1 contract integration "$V" "check_image_tags.sh rc=$F1 ($(grep -c MISSING "$RUNTIME_DIR/F1.out" 2>/dev/null || echo 0) missing)"
cp -f "$RUNTIME_DIR/F1.out" "$HERE/results/reverify-F1.out" 2>/dev/null || true

# F2 — every commit base..head must have a Signed-off-by matching author name+email (contract)
RANGE="${BASE_SHA:-origin/master}..$FIXED_REF"
if bash "$RUNTIME_DIR/harness/scripts/check_dco.sh" "$RANGE" > "$RUNTIME_DIR/F2.out" 2>&1; then F2=pass; else F2=fail; fi
if [ "$F2" = "pass" ]; then V="FIXED"; else V="STILL-BROKEN"; ALL_FIXED=0; fi
printf '%-6s %-9s %-12s %-14s %s\n' F2 contract unit "$V" "check_dco.sh rc=$F2 over $RANGE"
cp -f "$RUNTIME_DIR/F2.out" "$HERE/results/reverify-F2.out" 2>/dev/null || true

printf '\nReview delta this round: %s..%s\n' "$LAST_REVIEWED" "$FIXED_REF"
printf 'After reviewing that delta, advance the marker:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
       "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"
if [ "$ALL_FIXED" -eq 1 ]; then echo "RESULT: all findings fixed."; exit 0; else echo "RESULT: not all findings fixed."; exit 1; fi
