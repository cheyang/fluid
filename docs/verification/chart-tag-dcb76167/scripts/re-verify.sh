#!/usr/bin/env bash
# Re-verify PR #6278 (chart image tag) against the CURRENT PR head.
# No arguments: resolves the PR head from verify-manifest.json's "pr" URL.
# Optional arg: an explicit sha/ref to test instead of the PR head.
#
# Usage: bash docs/verification/chart-tag-dcb76167/scripts/re-verify.sh [<ref>]
set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"          # docs/verification/chart-tag-dcb76167
MANIFEST="$HERE/verify-manifest.json"
REPO_ROOT="$(git rev-parse --show-toplevel)"
PR_URL=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")
PR_NUM=${PR_URL##*/}
UPSTREAM=${PR_URL%/pull/$PR_NUM}

# resolve current PR head and the merge-base against upstream master
git fetch "$UPSTREAM" "pull/$PR_NUM/head"
HEAD_SHA=$(git rev-parse FETCH_HEAD)
git fetch "$UPSTREAM" master
BASE_SHA=$(git merge-base "$HEAD_SHA" FETCH_HEAD)

TARGET=${1:-$HEAD_SHA}
echo "PR:     $PR_URL"
echo "head:   $HEAD_SHA"
echo "base:   $BASE_SHA"
echo "target: $TARGET"
echo

# Contract test (asserts correct behavior; red on a chart that references a
# tag missing from the registry). Exit 0 iff every fluid image referenced by
# the chart at TARGET exists on Docker Hub.
python3 "$HERE/scripts/check_chart_images.py" \
    --repo "$REPO_ROOT" \
    --ref "$TARGET" \
    --tag-fragment "dcb76167" \
    --out "$HERE/results/re-verify-latest.json"

echo
echo "Polarity note: this is a CONTRACT test. Green = every image tag resolves;"
echo "red = the chart points at an unpublished tag (ImagePullBackOff precondition)."
