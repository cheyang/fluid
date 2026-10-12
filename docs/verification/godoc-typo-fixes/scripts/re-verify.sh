#!/usr/bin/env bash
# Re-verify harness for PR https://github.com/fluid-cloudnative/fluid/pull/6127
# Usage: bash scripts/re-verify.sh [<ref>]   (no ref => fetch current PR head)
# Exit 0 iff all code findings (F1, F2) are fixed. F3 (DCO) is inspection-only.
set -u
BASE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$BASE_DIR/../../.." && pwd)"
PR_URL=$(python3 -c "import json;print(json.load(open('$BASE_DIR/verify-manifest.json'))['pr'])")
PR_NUM=$(echo "$PR_URL" | grep -o '[0-9]*$')
REPO_URL=$(echo "$PR_URL" | sed 's|/pull/[0-9]*$||')

cd "$REPO_ROOT"
REF="${1:-}"
if [ -z "$REF" ]; then
  echo ">> fetching current PR head for $PR_URL"
  git fetch "$REPO_URL" pull/"$PR_NUM"/head || { echo "FAIL: could not fetch PR head"; exit 2; }
  REF=FETCH_HEAD
fi
echo ">> re-verifying against $REF"

STATUS=0
echo ">> L1 contract test (godoc consistency)"
if git show "$REF":pkg/utils/errors.go > /tmp/_gc_errors.go 2>/dev/null && \
   git show "$REF":pkg/utils/quantity.go > /tmp/_gc_quantity.go 2>/dev/null; then
  if python3 "$BASE_DIR/scripts/check_godoc_consistency.py" /tmp/_gc_errors.go /tmp/_gc_quantity.go; then
    echo "F1: Fixed | F2: Fixed"
  else
    echo "F1/F2: Still-broken (see violations above)"
    STATUS=1
  fi
else
  echo "F1/F2: Harness-update (files moved or ref not fetchable)"
  STATUS=2
fi

echo ">> F3 (DCO): inspect commit trailers of $REF"
git log -1 --format="trailers: %(trailers)" "$REF" || true
echo "   (fixed iff trailer is 'Signed-off-by: Name <email>' and the GitHub DCO check is green)"

echo ">> Focused unit tests (graft not needed; run from a worktree at $REF if desired)"
echo "   go test ./pkg/utils/ -run 'TestIgnoreNoKindMatchError|TestTranformQuantityToUnits' -count=1"

# Incremental review delta
LAST=""
[ -f "$BASE_DIR/.last-reviewed" ] && LAST=$(cat "$BASE_DIR/.last-reviewed")
if [ -n "$LAST" ]; then
  echo ">> incremental delta: $LAST..$REF"
  git log --oneline "$LAST..$REF" 2>/dev/null | head -20
fi
echo ">> after the round, advance the marker: git rev-parse $REF > $BASE_DIR/.last-reviewed"
exit $STATUS
