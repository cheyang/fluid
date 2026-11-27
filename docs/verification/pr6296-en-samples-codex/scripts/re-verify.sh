#!/usr/bin/env bash
# re-verify.sh for PR #6296 (docs: en other_volumes + hdfs_configuration samples).
# Run from a checkout of THIS harness branch (verify/pr6296-en-samples-codex).
# Resolves the current PR head from manifest.pr, re-runs the contract suite
# against it, and prints per-finding Fixed / Still-broken plus the incremental
# review range from .last-reviewed.
#
# Usage: bash docs/verification/pr6296-en-samples-codex/scripts/re-verify.sh [ref]
#   [ref]  optional explicit ref to check; default = current PR head fetched via manifest.pr
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
CHECK="$HERE/check_docs.py"

command -v jq >/dev/null || { echo "jq required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "not a git repo" >&2; exit 2; }

PR_URL="$(jq -r .pr "$MANIFEST")"
BASE="$(jq -r .baseSha "$MANIFEST")"
PR_NUM="${PR_URL##*/}"

if [ $# -ge 1 ]; then
  HEAD="$1"
else
  git fetch -q https://github.com/fluid-cloudnative/fluid.git "pull/$PR_NUM/head"
  HEAD="$(git rev-parse FETCH_HEAD)"
fi
echo "== re-verify PR #$PR_NUM head=$HEAD base=$BASE =="

# Premise control (informational): base must still show both defects.
echo "-- premise control on base --"
python3 "$CHECK" --ref "$BASE" --expect-premise-fail || true

# Contract suite on the (possibly updated) head.
echo "-- contract suite on head --"
OUT="$(python3 "$CHECK" --ref "$HEAD")" && RC=0 || RC=$?
echo "$OUT"

# Per-finding verdict: every finding here is a CONTRACT check -> Fixed iff its
# check names all PASS on head.
echo "-- per-finding verdicts --"
FAIL_ALL=0
for id in $(jq -r '.findings[].id' "$MANIFEST"); do
  ok=1
  for m in $(jq -r --arg id "$id" '.findings[] | select(.id==$id) | .match[]' "$MANIFEST"); do
    echo "$OUT" | grep -q "^PASS $m" || ok=0
  done
  if [ "$ok" = 1 ]; then echo "$id: Fixed"; else echo "$id: Still-broken"; FAIL_ALL=1; fi
done

# Incremental-review delta hint.
LR="$TOPIC_DIR/.last-reviewed"
if [ -f "$LR" ]; then
  LAST="$(cat "$LR")"
  echo "-- incremental review range: $LAST..$HEAD --"
  git log --oneline "$LAST..$HEAD" || true
  echo "advance marker: echo $HEAD > $LR"
fi

exit "$FAIL_ALL"
