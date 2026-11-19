#!/usr/bin/env bash
# Re-verification for PR https://github.com/fluid-cloudnative/fluid/pull/6269.
# Usage: bash scripts/re-verify.sh [<ref>]   (no ref -> current PR head)
#
# Grafts this harness (docs/verification/fluid-6269-security-alerts +
# hack/sdk/post_gen_path_test.py) onto the given ref in a temp worktree and
# runs the unit + integration layers:
#   L1 post-gen.py path-validation contract tests      (expect 10/10 PASS)
#   L2 lock-file checks: mooncake (cp3.12), vineyard (cp3.10)   (expect exit 0)
#      canary: aiohttp-only bump (#6212 shape)         (expect exit 1 = premise holds)
#   L2 submodule bump identity check                   (expect exit 0)
# Prints per-claim verdicts; exit 0 iff all expectations match.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"          # docs/verification/fluid-6269-security-alerts
BRANCH_DIR="$(cd "$HERE/../../.." && pwd)"        # checkout holding this harness branch
UPSTREAM="https://github.com/fluid-cloudnative/fluid.git"
PR=6269

if [ $# -ge 1 ]; then
    REF="$1"
else
    git fetch "$UPSTREAM" "refs/pull/$PR/head" >/dev/null 2>&1 || { echo "fetch failed"; exit 2; }
    REF="$(git rev-parse FETCH_HEAD)"
fi
echo "Verifying PR #$PR head: $REF"

WT="$(mktemp -d)/wt"
git -C "$BRANCH_DIR" worktree add --detach "$WT" "$REF" >/dev/null 2>&1 || { echo "worktree failed"; exit 2; }
trap 'git -C "$BRANCH_DIR" worktree remove --force "$WT" >/dev/null 2>&1; rm -rf "$(dirname "$WT")"' EXIT
# Graft harness from this branch onto the ref under test
git -C "$BRANCH_DIR" archive HEAD -- docs/verification/fluid-6269-security-alerts hack/sdk/post_gen_path_test.py | tar -x -C "$WT"

cd "$WT"
FAIL=0

echo "--- L1: post-gen.py path validation (contract tests) ---"
if python3 hack/sdk/post_gen_path_test.py | tee /tmp/rv-postgen.txt; then
    echo "L1 post-gen: PASS"
else
    echo "L1 post-gen: FAIL"; FAIL=1
fi

echo "--- L2: mooncake lock (cp3.12, hash + constraint check) ---"
if python3 -P -S docs/verification/fluid-6269-security-alerts/scripts/check_lock.py \
        test/gha-e2e/mooncake/image/requirements.txt 3.12 /tmp/rv-mooncake.json; then
    echo "L2 mooncake lock: PASS"
else
    echo "L2 mooncake lock: FAIL"; FAIL=1
fi

echo "--- L2: vineyard lock (cp3.10, hash + constraint check) ---"
if python3 -P -S docs/verification/fluid-6269-security-alerts/scripts/check_lock.py \
        samples/vineyard/requirements.txt 3.10 /tmp/rv-vineyard.json; then
    echo "L2 vineyard lock: PASS"
else
    echo "L2 vineyard lock: FAIL"; FAIL=1
fi

echo "--- L2: canary — Dependabot #6212 shape must remain unresolvable (premise) ---"
if python3 -P -S docs/verification/fluid-6269-security-alerts/scripts/check_lock.py \
        docs/verification/fluid-6269-security-alerts/fixtures/mooncake-aiohttp-only.txt 3.12 /tmp/rv-canary.json; then
    echo "L2 canary: UNEXPECTED PASS (aiohttp-only bump now resolves — premise invalidated?)"; FAIL=1
else
    if grep -q "requires yarl" /tmp/rv-canary.json 2>/dev/null || grep -rq "requires yarl" /tmp/rv-canary.json; then
        echo "L2 canary: PASS (aiohttp-only bump still violates yarl>=1.25.1)"
    else
        echo "L2 canary: FAIL for unexpected reason:"; cat /tmp/rv-canary.json; FAIL=1
    fi
fi

echo "--- L2: submodule bump identity ---"
if bash docs/verification/fluid-6269-security-alerts/scripts/check-submodule.sh >/tmp/rv-subm.txt 2>&1; then
    echo "L2 submodule: PASS"; tail -2 /tmp/rv-subm.txt
else
    echo "L2 submodule: FAIL"; cat /tmp/rv-subm.txt; FAIL=1
fi

echo
if [ "$FAIL" = 0 ]; then
    echo "ALL VERIFICATIONS PASS"
else
    echo "SOME VERIFICATIONS FAILED"
fi
# Incremental review hint
LAST="$(cat "$HERE/.last-reviewed" 2>/dev/null || true)"
if [ -n "$LAST" ]; then
    echo
    echo "Delta since last reviewed round: $LAST..$REF"
    echo "git -C \"$WT\" log --oneline $LAST..$REF"
    echo "After this round: echo $REF > docs/verification/fluid-6269-security-alerts/.last-reviewed"
fi
exit $FAIL
