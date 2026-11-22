#!/usr/bin/env bash
# P0 premise check for https://github.com/fluid-cloudnative/fluid/pull/6284
# Runs against the BASE (merge-base 253f6bb) branch, without the patch, because
# the question is whether the problem exists today.
#
# It grafts the PR's own new test cases (test-only diff) onto the base code and
# runs them. Expected on base: panic "index out of range" in MasterPodName.
# This also serves as the harness-bites check for the fix: base == fix absent,
# so the same tests going red here proves they exercise the patched path.
#
# Usage: bash docs/verification/masterpodname-oob-claude/scripts/premise-check.sh
#   REPO_DIR (optional) — path to a fluid git checkout (defaults to two dirs up)
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="${REPO_DIR:-$(cd "$HERE/../../../.." && pwd)}"
PR_HEAD="2796b3521ad37c7fd29837ec11e5ee22dfdcef1f"
BASE_SHA="253f6bb84f26a906fba301550b5c81ebdd73af35"
WORKTREE="$(mktemp -d /tmp/fluid-p0-6284.XXXXXX)"
trap 'git -C "$REPO_DIR" worktree remove --force "$WORKTREE" >/dev/null 2>&1; true' EXIT

git -C "$REPO_DIR" worktree add --quiet "$WORKTREE" "$BASE_SHA" || { echo "FAIL: cannot check out base $BASE_SHA"; exit 2; }

# Graft only the test-file part of the PR onto base.
git -C "$REPO_DIR" diff "$BASE_SHA" "$PR_HEAD" -- pkg/ddc/alluxio/operations/base_test.go \
  | git -C "$WORKTREE" apply - || { echo "FAIL: cannot graft test diff onto base"; exit 2; }

cd "$WORKTREE"
echo "== base=$BASE_SHA + PR test cases; expect panic: index out of range =="
go test -count=1 -gcflags="all=-N -l" ./pkg/ddc/alluxio/operations/ -run 'TestAlluxioFileUtils_MasterPodName' 2>&1 | tail -20
status=${PIPESTATUS[0]}

if [ "$status" -ne 0 ]; then
  echo "PREMISE CONFIRMED: tests fail on base (exit $status) — the panic MasterPodName() causes exists without the patch."
  exit 0
fi
echo "PREMISE NOT REPRODUCED: tests pass on base — investigate before trusting the PR."
exit 1
