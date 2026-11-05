#!/usr/bin/env bash
# F1 (contract): the PR diff on pkg/ddc/alluxio/ufs_internal.go is comment-only.
# Fails if any added line is not a Go line comment, or if any line is removed.
# Usage: check_comment_only_diff.sh <baseSha> <headSha>
set -euo pipefail
BASE="${1:?base sha}"; HEAD="${2:?head sha}"
DIFF="$(git diff "$BASE" "$HEAD" -- pkg/ddc/alluxio/ufs_internal.go)"
added=$(printf '%s\n' "$DIFF" | grep -E '^[+]' | grep -vE '^[+][+][+]' || true)
removed=$(printf '%s\n' "$DIFF" | grep -E '^[-]' | grep -vE '^[-][-][-]' || true)
if [ -z "$added" ]; then
  echo "FAIL: no added lines found - harness not exercising the PR diff"; exit 1
fi
if [ -n "$removed" ]; then
  echo "FAIL: diff removes lines:"; printf '%s\n' "$removed"; exit 1
fi
non_comment=$(printf '%s\n' "$added" | grep -vE '^[+][[:space:]]*//' || true)
if [ -n "$non_comment" ]; then
  echo "FAIL: added non-comment lines:"; printf '%s\n' "$non_comment"; exit 1
fi
echo "PASS: diff is comment-only ($(printf '%s\n' "$added" | grep -c .) added comment lines, 0 removed lines)"
