#!/usr/bin/env bash
# re-verify.sh — topic-specific re-verification for PR fluid-cloudnative/fluid#6291
# (bump actions/setup-go 7.0.0 -> 7.1.0). Adapted from the
# review-finding-verifier skill's stock re-verify.sh for a script-kind harness
# (no go/jq/gotest layers here).
#
# Usage (from a checkout of the branch that CONTAINS this harness):
#   bash docs/verification/setupgo-v710/scripts/re-verify.sh [<fixed-ref>]
#
# With no <fixed-ref>, the current PR head is resolved machine-independently via
# the manifest's "pr" URL (git fetch <repo-url> pull/<n>/head). The harness is
# grafted onto the fixed ref, the unit layer (check_setupgo_pins.py) runs, and
# per-finding verdicts are printed. Exit 0 iff every contract finding is Fixed.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(cd "$HERE/.." && pwd)"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
RESULTS="$TOPIC_DIR/results"
mkdir -p "$RESULTS"

cd "$(git rev-parse --show-toplevel)"
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path_="${rest#*/}"
  owner="$(printf '%s' "$path_" | cut -d/ -f1)"
  repo="$(printf '%s' "$path_" | cut -d/ -f2)"
  num="$(printf '%s' "$path_" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  cloneurl="https://$host/$owner/$repo.git"
  echo "re-verify: resolving PR head via 'git fetch $cloneurl pull/$num/head'"
  git fetch --quiet "$cloneurl" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: fixed-ref=$FIXED_REF"

LAST_REVIEWED_FILE="$TOPIC_DIR/.last-reviewed"
LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE" 2>/dev/null || true)"
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (review delta = ${LAST_REVIEWED:-base}..$FIXED_REF)"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
git checkout "$HARNESS_SRC" -- docs/verification/setupgo-v710 2>/dev/null || {
  echo "re-verify: failed to graft harness paths" >&2; exit 2; }

OUT="$RESULTS/reverify.out"
if python3 docs/verification/setupgo-v710/scripts/check_setupgo_pins.py >"$OUT" 2>&1; then RC=0; else RC=1; fi

printf '\n================  RE-VERIFY  ================\n'
printf '%-4s %-9s %-6s %-12s %s\n' ID POLARITY LAYER VERDICT DETAIL
ALL_FIXED=1
for pair in "C1:pin-bump,go-version-input" "C2:tag-identity"; do
  id="${pair%%:*}"; matches="${pair#*:}"
  pass=0; fail=0; miss=0
  TAB="$(printf '\t')"
  IFS=','; for m in $matches; do
    if grep -qF "${m}${TAB}pass" "$OUT"; then pass=$((pass+1));
    elif grep -qF "${m}${TAB}fail" "$OUT"; then fail=$((fail+1));
    else miss=$((miss+1)); fi
  done; unset IFS
  if [ "$fail" -eq 0 ] && [ "$miss" -eq 0 ]; then verdict="FIXED"; else verdict="STILL-BROKEN"; ALL_FIXED=0; fi
  printf '%-4s %-9s %-6s %-12s %s\n' "$id" contract unit "$verdict" "pass=$pass fail=$fail miss=$miss"
done

cp -f "$OUT" "$RESULTS/reverify-head.out" 2>/dev/null || true
echo
echo "Raw output: $RESULTS/reverify.out"
echo "Review delta this round: ${LAST_REVIEWED:-base}..$FIXED_REF"
echo "After reviewing that delta, advance the marker:"
echo "  echo $FIXED_REF > $LAST_REVIEWED_FILE && git add $LAST_REVIEWED_FILE && git commit -m 'review: advance last-reviewed'"
[ "$ALL_FIXED" -eq 1 ] && { echo "RESULT: all findings fixed."; exit 0; } || { echo "RESULT: not all findings fixed."; exit 1; }
