#!/usr/bin/env bash
# re-verify.sh for PR #6211 (security model docs), topic security-model-docs.
# No ref argument: resolves the CURRENT PR head from the manifest's "pr" URL.
# Usage: bash docs/verification/security-model-docs-claude/scripts/re-verify.sh [<ref>]
#
# Prints per finding: Fixed / Still-broken / Partial / Harness-update, and the
# incremental review range (last-reviewed..head). Exit 0 iff all findings are
# in a passing state.
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"      # docs/verification/<topic>
REPO_ROOT="$(cd "$HERE/../../.." && pwd)"
MANIFEST="$HERE/verify-manifest.json"
MARKER="$HERE/.last-reviewed"

PR_URL=$(python3 -c "import json,sys; print(json.load(open('$MANIFEST'))['pr'])")
PR_NUM=$(basename "${PR_URL%%/files}" )
REPO_URL=$(dirname "$(dirname "$PR_URL")")   # https://github.com/owner/repo
REPO_URL="${REPO_URL/pull\//}"                # strip any pull/ suffix slop

if [ $# -ge 1 ]; then
  TARGET_REF="$1"
else
  echo "Resolving current PR head from $PR_URL"
  git -C "$REPO_ROOT" fetch "$REPO_URL" "pull/$PR_NUM/head" || { echo "FAIL: cannot fetch PR head"; exit 2; }
  TARGET_REF="$(git -C "$REPO_ROOT" rev-parse FETCH_HEAD)"
fi
echo "Verifying against: $TARGET_REF"

# Graft the harness onto the target ref in a temp worktree (production tree untouched)
WORKTREE=$(mktemp -d /tmp/pr6211-verify.XXXXXX)
git -C "$REPO_ROOT" worktree add -q --detach "$WORKTREE" "$TARGET_REF" || { echo "FAIL: worktree"; exit 2; }
HARNESS_DIR_REL="docs/verification/security-model-docs-claude"
mkdir -p "$WORKTREE/$HARNESS_DIR_REL"
cp -r "$HERE/." "$WORKTREE/$HARNESS_DIR_REL/" 2>/dev/null
rm -rf "$WORKTREE/$HARNESS_DIR_REL/results"

cd "$WORKTREE" || exit 2

echo
echo "=== L1: doc claim checks ==="
python3 -I "$HARNESS_DIR_REL/scripts/check_doc_claims.py" > "$HARNESS_DIR_REL/results-l1.txt" 2>&1
L1_RC=$?
tail -2 "$HARNESS_DIR_REL/results-l1.txt"

echo
echo "=== L2: helm render checks ==="
bash "$HARNESS_DIR_REL/scripts/render_chart_checks.sh" > "$HARNESS_DIR_REL/results-l2.txt" 2>&1
L2_RC=$?
grep -E "PASS|FAIL" "$HARNESS_DIR_REL/results-l2.txt" | grep -v "^WARNING"

echo
echo "=== Per-finding verdicts ==="
FAILED_IDS=""
add_fail() { FAILED_IDS="$FAILED_IDS $1"; }
if [ $L1_RC -eq 0 ] && [ $L2_RC -eq 0 ]; then
  echo "DOC-4: PASS (all code-path/CRD/chart claims hold at $TARGET_REF)"
else
  echo "DOC-4: Still-broken / Harness-update (see results-l1.txt / results-l2.txt)"
  add_fail DOC-4
fi
# DOC-1/2/3 are prose-level findings; the scripts assert the underlying claims,
# so a passing run means "not regressed"; the prose itself needs a human/agent read.
echo "DOC-1: $([ $L1_RC -eq 0 ] && echo 'claims hold; re-read prose for the local:// Jindo wording' || echo 'check failed')"
echo "DOC-2: re-read prose (inbound links) — script asserts outbound links only"
echo "DOC-3: re-read prose (kubelet fallback) — script asserts the documented default"

# Incremental review range
if [ -f "$MARKER" ]; then
  LAST=$(cat "$MARKER")
  echo
  echo "=== Incremental review range: $LAST..$TARGET_REF ==="
  git -C "$REPO_ROOT" log --oneline "$LAST..$TARGET_REF" 2>/dev/null | head -20
  echo "Advance the marker after the round:  echo $TARGET_REF > $MARKER"
else
  echo "(no .last-reviewed marker — first round)"
fi

git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" >/dev/null 2>&1

echo
if [ -z "$FAILED_IDS" ]; then
  echo "RESULT: all findings in passing state"
  exit 0
else
  echo "RESULT: failures:$FAILED_IDS"
  exit 1
fi
