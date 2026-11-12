#!/usr/bin/env bash
# Re-verification harness for PR #6241 (zap 1.27.0 -> 1.28.0 bump), Reviewer A (Claude).
# Usage: bash docs/verification/zap-bump/scripts/re-verify.sh [<ref>]
# Without <ref>, fetches the current PR head from upstream (refs/pull/6241/head).
# Exit 0 iff all harness contract tests pass on the target ref.
set -uo pipefail

DOCS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRANCH_DIR="$(cd "$DOCS_DIR/../../.." && pwd)"
MANIFEST="$DOCS_DIR/verify-manifest.json"
PR_URL=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["pr"])' "$MANIFEST" 2>/dev/null)
PR_NUM="${PR_URL##*/}"
REPO_URL="${PR_URL%/pull/*}"
TARGET="${1:-}"

if [ -z "$TARGET" ]; then
  echo "[re-verify] resolving current PR head: $PR_URL"
  git -C "$BRANCH_DIR" fetch "$REPO_URL" "pull/${PR_NUM}/head" 2>/dev/null || git -C "$BRANCH_DIR" fetch origin "pull/${PR_NUM}/head"
  TARGET="FETCH_HEAD"
fi

echo "[re-verify] target ref: $TARGET"
git -C "$BRANCH_DIR" rev-parse "$TARGET^{commit}"

# Incremental-review delta
LAST_REVIEWED=""
if [ -f "$DOCS_DIR/.last-reviewed" ]; then
  LAST_REVIEWED=$(cat "$DOCS_DIR/.last-reviewed")
fi
if [ -n "$LAST_REVIEWED" ]; then
  echo "[re-verify] last-reviewed: $LAST_REVIEWED"
  echo "[re-verify] incremental delta ${LAST_REVIEWED}..${TARGET}:"
  git -C "$BRANCH_DIR" log --oneline "$LAST_REVIEWED..${TARGET}" 2>/dev/null | head -20
else
  echo "[re-verify] no .last-reviewed marker; full review required"
fi

echo "[re-verify] grafting harness onto $TARGET in a temp worktree"
WT=$(mktemp -d /tmp/fluid-zap-bump-XXXX)
git -C "$BRANCH_DIR" worktree add -q --detach "$WT" "$TARGET" || { echo "worktree add failed"; exit 2; }
# copy harness files (additive; production code untouched)
mkdir -p "$WT/pkg/utils/zapcompat"
cp "$BRANCH_DIR/pkg/utils/zapcompat/doc.go" "$WT/pkg/utils/zapcompat/"
cp "$BRANCH_DIR/pkg/utils/zapcompat/zapcompat_harness_test.go" "$WT/pkg/utils/zapcompat/"

cd "$WT" || exit 2
FAIL=0

echo "[re-verify] unit layer: zapcompat harness"
OUT1=$(go test -count=1 -v ./pkg/utils/zapcompat/ 2>&1) || FAIL=1
echo "$OUT1" | grep -E '^(=== RUN|--- PASS|--- FAIL|ok|FAIL)'
# a pass with zero executed tests is a harness error, never a green result
echo "$OUT1" | grep -q -- '--- PASS' || { echo "[re-verify] harness error: no tests executed"; FAIL=1; }

echo "[re-verify] supply-chain layer: vendor fidelity vs upstream module"
cp "$BRANCH_DIR/docs/verification/zap-bump/scripts/verify-vendor-fidelity.sh" "$WT/" 2>/dev/null
if command -v go >/dev/null; then
  bash ./verify-vendor-fidelity.sh "$WT" || FAIL=1
else
  echo "[re-verify] go missing; fidelity check skipped (counts as Harness-update)"
fi

echo "[re-verify] compile layer: build the repo at the target ref"
go build ./... || { echo "[re-verify] go build ./... FAILED"; FAIL=1; }

if [ "$FAIL" -eq 0 ]; then
  echo "[re-verify] verdict: all contract checks green on $TARGET"
else
  echo "[re-verify] verdict: FAIL (see output above)"
fi

git -C "$BRANCH_DIR" worktree remove --force "$WT"
exit $FAIL
