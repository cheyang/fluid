#!/usr/bin/env bash
# L1 — direct reproduction of the PR #6230 CI failure (F1).
# Runs the project's PINNED toolchain (go1.25.12, GOTOOLCHAIN=local, exactly like
# GitHub Actions and the golang:1.25.12-bookworm docker image) against a ref.
#
#   bash scripts/l1-toolchain-gate.sh <ref> <expect: pass|fail>
#
#   <ref>     any git ref (e.g. af817d9 for the PR head, 30d488b for base)
#   <expect>  'pass' => ref must build under go1.25.12;  'fail' => must hit the gate
#
# Uses a temporary git worktree; never touches your checkout.
set -euo pipefail
REF="${1:?usage: l1-toolchain-gate.sh <ref> <pass|fail>}"
EXPECT="${2:?expect pass|fail}"
REPO="$(git rev-parse --show-toplevel)"

# Resolve a go1.25.12 toolchain binary (download via toolchain mechanism if needed)
TC_BIN="$(go env GOMODCACHE)/golang.org/toolchain@v0.0.1-go1.25.12.linux-amd64/bin/go"
if [ ! -x "$TC_BIN" ]; then
  echo "l1: go1.25.12 toolchain not cached; downloading via GOTOOLCHAIN=go1.25.12"
  GOTOOLCHAIN=go1.25.12 go version >/dev/null
fi
"$TC_BIN" version

WT="$(mktemp -d /tmp/pr6230-l1-XXXXXX)"
trap 'git -C "$REPO" worktree remove --force "$WT" >/dev/null 2>&1 || rm -rf "$WT"' EXIT
git -C "$REPO" worktree add --force "$WT" "$REF" >/dev/null 2>&1

echo "l1: ref=$REF expect=$EXPECT — running pinned go1.25.12 with GOTOOLCHAIN=local"
set +e
OUT="$(cd "$WT" && GOTOOLCHAIN=local "$TC_BIN" list ./pkg/metrics/ 2>&1)"
RC=$?
set -e
echo "$OUT"
case "$EXPECT" in
  fail)
    if [ $RC -ne 0 ] && echo "$OUT" | grep -q "go.mod requires go >= 1.26.0"; then
      echo "L1-RESULT: REPRODUCED (ref $REF fails under pinned go1.25.12, as CI does)"; exit 0
    fi
    echo "L1-RESULT: NOT REPRODUCED (rc=$RC)"; exit 1;;
  pass)
    if [ $RC -eq 0 ]; then
      echo "L1-RESULT: PASS (ref $REF accepted by pinned go1.25.12)"; exit 0
    fi
    echo "L1-RESULT: UNEXPECTED FAILURE (rc=$RC)"; exit 1;;
esac
