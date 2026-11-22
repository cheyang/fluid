#!/usr/bin/env bash
# Re-verify harness for https://github.com/fluid-cloudnative/fluid/pull/6284
# Findings: P0 (premise, runs on BASE) + F1 (MasterPodName panic on malformed
# fsadmin-report output — fixed by the PR; contract tests are the PR's own cases).
#
# With no argument: fetches the CURRENT PR head from the manifest 'pr' URL and
# grafts the harness onto it. With an argument: uses that ref instead.
# Exit 0 iff F1's contract tests pass on the target (i.e. still fixed).
set -u

HERE="$(cd "$(dirname "$0")" && pwd)"
MANIFEST="$HERE/../verify-manifest.json"
PR_URL="$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['pr'])" "$MANIFEST")"
PR_NUM="${PR_URL##*/}"
REPO_URL="${PR_URL%\/pull\/*}"
TARGET_REF="${1:-}"
WORKTREE="$(mktemp -d /tmp/fluid-reverify-6284.XXXXXX)"
REPO_DIR="$(cd "$HERE/../../../.." && pwd)"
trap 'git -C "$REPO_DIR" worktree remove --force "$WORKTREE" >/dev/null 2>&1; true' EXIT

# Resolve target ref (PR head fetch is machine-independent, no local remote needed).
if [ -z "$TARGET_REF" ]; then
  echo "== fetching current PR head for $PR_URL =="
  git -C "$REPO_DIR" fetch -q "$REPO_URL" "pull/$PR_NUM/head" || { echo "FAIL: fetch"; exit 2; }
  TARGET_REF="FETCH_HEAD"
fi
TARGET_SHA="$(git -C "$REPO_DIR" rev-parse --short "$TARGET_REF")"
echo "== target: $TARGET_SHA =="

git -C "$REPO_DIR" worktree add --quiet "$WORKTREE" "$TARGET_REF" || { echo "FAIL: worktree"; exit 2; }

# Graft the harness (this docs tree) onto the target — additive only.
mkdir -p "$WORKTREE/docs/verification"
cp -r "$HERE/.." "$WORKTREE/docs/verification/" || true

cd "$WORKTREE"
echo "== L1 unit: MasterPodName contract tests (the PR's own cases) =="
go test -count=1 -gcflags="all=-N -l" ./pkg/ddc/alluxio/operations/ -run 'TestAlluxioFileUtils_MasterPodName' -v 2>&1 | tail -6
UNIT_STATUS="${PIPESTATUS[0]}"

echo
echo "== full package =="
go test -count=1 -gcflags="all=-N -l" ./pkg/ddc/alluxio/operations/ 2>&1 | tail -3
PKG_STATUS="${PIPESTATUS[0]}"

echo
echo "== verdict =="
if [ "$UNIT_STATUS" -eq 0 ] && [ "$PKG_STATUS" -eq 0 ]; then
  echo "F1: Fixed (contract tests green on $TARGET_SHA)"
  exit 0
else
  echo "F1: Still-broken / Harness-update (unit=$UNIT_STATUS pkg=$PKG_STATUS on $TARGET_SHA)"
  exit 1
fi
