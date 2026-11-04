#!/usr/bin/env bash
# re-verify.sh — re-run this harness against the current PR head (or an
# explicit ref). Prints per finding Fixed / Still-broken / Partial /
# Harness-update and the incremental review range.
# Usage: bash scripts/re-verify.sh [<fixed-ref>]
set -u
HERE="$(cd "$(dirname "$0")/.." && pwd)"          # docs/verification/go126-xnet-upgrade
TOPIC_DIR="$HERE"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
PR_URL=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")
REPO_URL="https://github.com/fluid-cloudnative/fluid.git"

REF="${1:-}"
if [ -z "$REF" ]; then
  echo ">> fetching current PR head from $PR_URL"
  REF=$(git ls-remote "$REPO_URL" "refs/pull/$(basename $(dirname "$PR_URL"))/head" | awk '{print $1}') \
    || { echo "could not resolve PR head"; exit 2; }
fi
echo ">> verifying against ref: $REF"

WORK=$(mktemp -d /tmp/go126-xnet-verify.XXXXXX)
git clone -q --depth 50 "$REPO_URL" "$WORK/src" 2>/dev/null
git -C "$WORK/src" fetch -q --depth 50 origin "$REF" 2>/dev/null
git -C "$WORK/src" checkout -q FETCH_HEAD 2>/dev/null
# graft the harness (this docs/ tree) onto the code under review
git -C "$WORK/src" checkout -q "$VERIFY_BRANCH_REF" -- docs/verification/go126-xnet-upgrade 2>/dev/null || true

# base worktree for the premise/F1 differential
git -C "$WORK/src" fetch -q --depth 50 origin 7f27b7e2a9cc6343e8905abd37a236e4c735dc24 2>/dev/null
git -C "$WORK/src" worktree add -q "$WORK/base" 7f27b7e2a9cc6343e8905abd37a236e4c735dc24 2>/dev/null

echo ">> [F1] plain go test differential"
bash "$TOPIC_DIR/scripts/10-f1-plain-gotest.sh" "$WORK/src" "$WORK/base"
echo ">> [A] acceptance (build / module / digest / canonical unit suite)"
RESULTS_DIR="$TOPIC_DIR/results-rerun" bash "$TOPIC_DIR/scripts/30-acceptance.sh" "$WORK/src"

echo ">> findings status"
echo "  F1 (plain-go-test gomonkey/inlining panic): Still-broken if [head@go1.26.10] FAIL above and [base@go1.25.12] PASS; fixed only when the head invocation passes without -N -l."
echo "  P0/A1..A4: PASS markers above mean the acceptance contract still holds."

LAST=$(cat "$TOPIC_DIR/.last-reviewed" 2>/dev/null)
if [ -n "$LAST" ]; then
  echo ">> incremental review delta: $LAST..$REF"
  git -C "$WORK/src" log --oneline "$LAST..$REF" 2>/dev/null | head -20
fi
echo ">> after the round: echo $REF > $TOPIC_DIR/.last-reviewed && git commit"

rm -rf "$WORK"
