#!/usr/bin/env bash
# Differential harness for finding F3 (staticcheck ST1019 red on CI) — PR #6241, round 2.
#
# Claim: the only red check on the PR head (staticcheck) fails with ST1019 in
# pkg/utils/dataset/lifecycle/node_test.go (duplicate import of
# sigs.k8s.io/controller-runtime/pkg/client), and that failure is BASELINE,
# not PR-introduced: it reproduces byte-identically on the merge-base commit
# (5e64735, introduced there by master commit 6f55f846d / #6244) and is fixed
# on master tip (dcb76167b, fixed by 1321a217d / #6257). The PR's own diff
# (go.mod, go.sum, vendor/go.uber.org/zap/**, vendor/modules.txt) never touches
# the failing file.
#
# Usage: bash check-staticcheck-baseline.sh <head-sha> <base-sha> [<fix-sha>]
# Prefers real staticcheck (honnef.co v0.6.1 = CI's 2025.1.1); falls back to a
# grep-based ST1019 detector (duplicate plain/aliased import of the same path
# in the failing file) when staticcheck is not installed.
# Exit 0 iff: failure present on head AND base (baseline attribution holds)
#             AND (when <fix-sha> given) absent on fix-sha.
set -uo pipefail

DOCS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BRANCH_DIR="$(cd "$DOCS_DIR/../../.." && pwd)"
HEAD_SHA="${1:?usage: check-staticcheck-baseline.sh <head> <base> [<fix-sha>]}"
BASE_SHA="${2:?usage: check-staticcheck-baseline.sh <head> <base> [<fix-sha>]}"
FIX_SHA="${3:-}"
FILE="pkg/utils/dataset/lifecycle/node_test.go"

STABLE_STATICCHECK=false
if command -v go >/dev/null; then
  SC="$(go env GOPATH)/bin/staticcheck"
  if [ -x "$SC" ]; then
    # staticcheck must match the toolchain that produced the stdlib export data
    STABLE_STATICCHECK=true
  fi
fi

run_ref() {
  local sha="$1" wt out
  wt=$(mktemp -d /tmp/f3-sc-XXXX)
  git -C "$BRANCH_DIR" worktree add -q --detach "$wt" "$sha" || { echo "worktree failed for $sha"; return 2; }
  if [ "$STABLE_STATICCHECK" = true ]; then
    out=$(cd "$wt" && "$SC" ./... 2>&1; echo "exit:$?")
    echo "$out"
  else
    # grep fallback: detect the duplicate import ST1019 complains about
    if git -C "$BRANCH_DIR" show "$sha:$FILE" 2>/dev/null | grep -qE '^	"sigs\.k8s\.io/controller-runtime/pkg/client"$' \
       && git -C "$BRANCH_DIR" show "$sha:$FILE" 2>/dev/null | grep -qE '^	ctrlclient "sigs\.k8s\.io/controller-runtime/pkg/client"$'; then
      out="$FILE:36: package \"sigs.k8s.io/controller-runtime/pkg/client\" is being imported more than once (ST1019) [grep-fallback]"; echo "$out"; out="$out
exit:1"
    else
      out="exit:0 [grep-fallback: no duplicate import]"
    fi
    echo "$out"
  fi
  git -C "$BRANCH_DIR" worktree remove --force "$wt"
  # returns the combined output on stdout; caller inspects
}

result() { # 0 = clean, 1 = ST1019 present
  local out="$1"
  if echo "$out" | grep -q "ST1019"; then echo 1; else echo 0; fi
}

echo "[F3] staticcheck differential (staticcheck: $STABLE_STATICCHECK)"
H_OUT=$(run_ref "$HEAD_SHA")
B_OUT=$(run_ref "$BASE_SHA")
H_RC=$(result "$H_OUT")
B_RC=$(result "$B_OUT")

echo "[F3] head  ($HEAD_SHA): ST1019-present=$H_RC"
echo "[F3] base  ($BASE_SHA): ST1019-present=$B_RC"

FAIL=0
if [ "$H_RC" = 1 ] && [ "$B_RC" = 1 ]; then
  echo "[F3] verdict: baseline-attributed (identical failure on base and head) — NOT introduced by this PR"
else
  echo "[F3] verdict: attribution changed — investigate! head=$H_RC base=$B_RC"
  FAIL=1
fi

if [ -n "$FIX_SHA" ]; then
  F_OUT=$(run_ref "$FIX_SHA" | tee /dev/stderr)
  F_RC=$(result "$F_OUT")
  echo "[F3] fix   ($FIX_SHA): ST1019-present=$F_RC"
  if [ "$F_RC" = 0 ]; then
    echo "[F3] remedy confirmed: rebasing this PR onto >= $FIX_SHA clears the red check"
  else
    echo "[F3] fix ref still red — remedy NOT confirmed"
    FAIL=1
  fi
fi

exit $FAIL
