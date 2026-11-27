#!/usr/bin/env bash
# re-verify.sh for docs/verification/docs-en-samples (PR fluid-cloudnative/fluid#6296)
#
# With no argument: fetches the CURRENT PR head from the manifest's "pr" URL
# and grafts the harness onto it. With an argument: uses that ref instead.
# Prints the last-reviewed..head delta range, runs the L1 harness at head and
# the premise probe at the PR base, and prints a per-finding verdict.
set -u -o pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TOPIC_DIR="$(dirname "$SCRIPT_DIR")"
REPO_DIR="$(cd "$TOPIC_DIR/../../.." && pwd)"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
PR_URL=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['pr'])" "$MANIFEST")
PR_NUM=${PR_URL##*/}

cd "$REPO_DIR"

if [ -n "${1:-}" ]; then
  HEAD_REF="$1"
else
  git fetch https://github.com/fluid-cloudnative/fluid "pull/$PR_NUM/head" >/dev/null 2>&1
  HEAD_REF=FETCH_HEAD
fi
HEAD_SHA=$(git rev-parse "$HEAD_REF")
BASE_SHA=$(git merge-base "$HEAD_SHA" origin/master 2>/dev/null || echo b8ff60e309c341b58a9e26b085b76e6067e6bb60)
LAST_REVIEWED=$(cat "$TOPIC_DIR/.last-reviewed" 2>/dev/null || echo "$BASE_SHA")

echo "== PR: $PR_URL"
echo "== head: $HEAD_SHA"
echo "== incremental delta: $LAST_REVIEWED..$HEAD_SHA"
git log --oneline "$LAST_REVIEWED..$HEAD_SHA" | head -20

echo
echo "== L1 harness at head =="
HARNESS="docs/verification/docs-en-samples/scripts/check_docs.py"
git show "$HEAD_SHA" >/dev/null 2>&1 || { echo "bad ref $HEAD_SHA"; exit 2; }

FAIL=0
# run the harness from the harness checkout against the head sha
if python3 "$HARNESS" --repo . --ref "$HEAD_SHA" | tee "$TOPIC_DIR/results/head.out"; then
  echo "harness: PASS at head"
else
  echo "harness: FAIL at head"
  FAIL=1
fi

echo
echo "== premise probe at base ($BASE_SHA) =="
# At the original base (b8ff60e) this probe FAILS — that is the confirmed premise P0.
# After the PR merges, a new merge-base carries the fix and the probe passes.
if python3 "$HARNESS" --repo . --ref "$BASE_SHA" \
    --en docs/zh/samples/other_volumes.md --en2 docs/zh/samples/hdfs_configuration.md \
    | tee "$TOPIC_DIR/results/base-premise.out"; then
  echo "premise: merge-base already carries the fix (expected after the PR merges)"
else
  echo "premise: zh defects still present at this merge-base (expected for the original base b8ff60e)"
fi

echo
if [ "$FAIL" -eq 0 ]; then
  echo "VERDICT: all contract checks pass at head (D1-D5 green)."
else
  echo "VERDICT: still-broken — see results/head.out"
fi
exit "$FAIL"
