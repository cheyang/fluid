#!/usr/bin/env bash
# Re-verify PR #6294 (docs: fix broken relative links) on any ref.
#
# Usage: bash scripts/re-verify.sh [<ref>]
#   no ref -> fetch the current PR head from the PR URL in verify-manifest.json
#             (git fetch https://github.com/fluid-cloudnative/fluid.git pull/6294/head)
# Exit 0 iff finding L1 is fixed (zero broken relative links in the 8
# PR-touched files). Prints the last-reviewed..head delta range as well.
set -u

HERE="$(cd "$(dirname "$0")/.." && pwd)"   # docs/verification/docs-broken-links-claude
REPO_URL="https://github.com/fluid-cloudnative/fluid.git"
PR_NUM=6294

REV="${1:-}"
if [ -z "$REV" ]; then
  git fetch "$REPO_URL" "pull/$PR_NUM/head" || exit 2
  REV="FETCH_HEAD"
fi

# incremental-review delta info
LAST_REVIEWED=""
if [ -f "$HERE/.last-reviewed" ]; then LAST_REVIEWED="$(cat "$HERE/.last-reviewed")"; fi
if [ -n "$LAST_REVIEWED" ]; then
  echo "delta: $LAST_REVIEWED..$(git rev-parse "$REV")"
  git log --oneline "$LAST_REVIEWED..$REV" | sed 's/^/  /'
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
git archive "$REV" | tar -x -C "$TMP" || exit 2

python3 -I "$HERE/scripts/check_links.py" "$TMP" > "$TMP/all-links.txt"

# L1 contract: zero broken relative links in the 8 PR-touched files
grep -E '^(docs/(en|zh)/(core-concepts/introduction|userguide/overview)\.md|docs/(en|zh)/samples/vineyard/accelerate_kubeflow_pipelines\.md|docs/zh/samples/(application_controller|fuse_recover)\.md):' \
  "$TMP/all-links.txt" > "$TMP/broken-touched.txt" || true

n="$(wc -l < "$TMP/broken-touched.txt")"
if [ "$n" -eq 0 ]; then
  echo "L1: Fixed — 0 broken relative links in the 8 PR-touched files on $REV"
  exit 0
else
  echo "L1: Still-broken — $n broken relative link(s) in the PR-touched files on $REV:"
  sed 's/^/  /' "$TMP/broken-touched.txt"
  exit 1
fi
