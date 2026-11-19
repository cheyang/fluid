#!/usr/bin/env bash
# Round-2 check: prove the PR's own rewritten namespace-determinism spec
# (mount_point_script_test.go @ head, 50x loop w/ fresh fake clients) is a real
# regression guard: graft the HEAD test file onto the BASE production code and
# confirm it goes RED; confirm GREEN on the HEAD production code.
# Usage: run from any checkout of the fluid repo with git + go available.
set -euo pipefail
BASE="${1:-dcb76167b0143a239addc6bed83ed50b45267631}"
HEAD="${2:-08c111a019c5f840fa076561eee2c2df17db92dc}"
TESTFILE="pkg/application/inject/fuse/mount_point_script_test.go"
FOCUS="deterministically select the first runtime info"

WT="$(mktemp -d)/wt"
git worktree add --force "$WT" "$BASE" >/dev/null 2>&1
trap 'git worktree remove --force "$WT" >/dev/null 2>&1 || true' EXIT
( cd "$WT" && git checkout "$HEAD" -- "$TESTFILE" )
echo "== base production + head test (expect FAIL) =="
if ( cd "$WT" && go test -count=1 -run TestFuse ./pkg/application/inject/fuse/ -args -ginkgo.focus="$FOCUS" >/dev/null 2>&1 ); then
  echo "UNEXPECTED PASS — the rewritten spec does not bite on base"
  exit 1
else
  echo "RED on base as expected — the rewritten spec is a real regression guard"
fi
echo "== head production + head test (expect PASS) =="
git worktree add --force "$WT" "$HEAD" >/dev/null 2>&1
( cd "$WT" && go test -count=1 -run TestFuse ./pkg/application/inject/fuse/ -args -ginkgo.focus="$FOCUS" >/dev/null 2>&1 ) \
  && echo "GREEN on head as expected" || { echo "UNEXPECTED FAIL on head"; exit 1; }
