#!/bin/bash
# re-verify.sh for PR #6175 verification harness (topic: mooncake-clientless-e2e).
#
# Usage: bash scripts/re-verify.sh [<ref>]
#   <ref> defaults to the current PR head, fetched from the manifest's "pr" URL
#   (git fetch <upstream> pull/<n>/head), so no sha needs to be typed.
#
# Grafts this harness (docs/verification/mooncake-clientless-e2e + the additive
# Go test) onto that ref in a temp worktree, runs the unit + integration layers,
# and prints per-claim Fixed / Still-broken / Harness-update. All harness tests
# are contract tests (green == claim holds), so "fixed" == green.
#
# Exit 0 iff all claims hold.

set -u

HARNESS_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$HARNESS_DIR/../../.." && pwd)"
MANIFEST="$HARNESS_DIR/verify-manifest.json"

PR_URL=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["pr"])' "$MANIFEST")
PR_NUM=${PR_URL##*/}

if [[ $# -ge 1 ]]; then
    REF="$1"
else
    UPSTREAM="https://github.com/fluid-cloudnative/fluid.git"
    echo ">>> fetching PR head for $PR_URL"
    git -C "$REPO_ROOT" fetch "$UPSTREAM" "pull/$PR_NUM/head" || exit 1
    REF="FETCH_HEAD"
fi
REF_SHA=$(git -C "$REPO_ROOT" rev-parse --short "$REF")
echo ">>> verifying against $REF ($REF_SHA)"

WORKTREE=$(mktemp -d /tmp/mooncake-verify.XXXXXX)
git -C "$REPO_ROOT" worktree add --detach "$WORKTREE" "$REF" >/dev/null || exit 1
trap 'git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null; rm -rf "$WORKTREE"' EXIT

# graft the harness (from the current checkout) onto the ref under test
git -C "$REPO_ROOT" archive HEAD -- \
    docs/verification/mooncake-clientless-e2e \
    pkg/ddc/cache/component/gc_labels_verify_test.go \
    | tar -x -C "$WORKTREE"

RESULTS="$HARNESS_DIR/results"
mkdir -p "$RESULTS"

declare -A STATUS

echo "=== L1 unit (shell harness) ==="
if bash "$HARNESS_DIR/scripts/unit-harness.sh" "$WORKTREE" "$RESULTS" 2>&1 | tee "$RESULTS/unit-harness.out"; then
    STATUS[V2]=ok; STATUS[V3]=ok; STATUS[V5]=ok
else
    STATUS[V2]=broken; STATUS[V3]=broken; STATUS[V5]=broken
fi

echo "=== L1 unit (go test, includes GC selector labels) ==="
if (cd "$WORKTREE" && go test ./pkg/ddc/cache/component/... 2>&1 | tail -3 | tee "$RESULTS/go-unit.out") \
    && grep -q "^ok" "$RESULTS/go-unit.out"; then
    STATUS[V1]=ok
else
    STATUS[V1]=broken
fi

echo "=== L2 integration (static cross-checks) ==="
if python3 -I "$HARNESS_DIR/scripts/manifest_checks.py" "$WORKTREE" 2>&1 | tee "$RESULTS/manifest-checks.out" | tail -2; then
    STATUS[V4]=ok
else
    STATUS[V4]=broken
fi

echo
echo "=== per-claim verdict (contract polarity: green == holds) ==="
rc=0
for id in V1 V2 V3 V4 V5; do
    if [[ "${STATUS[$id]:-missing}" == ok ]]; then
        echo "  $id: OK (claim holds on $REF_SHA)"
    else
        echo "  $id: ${STATUS[$id]:-missing} on $REF_SHA"
        rc=1
    fi
done

# incremental review delta support
LAST=$(cat "$HARNESS_DIR/.last-reviewed" 2>/dev/null)
if [[ -n "$LAST" ]]; then
    echo
    echo ">>> incremental review delta: git log --oneline $LAST..$REF_SHA"
    echo "    (after reviewing it, advance the marker:)"
    echo "    echo $REF_SHA > $HARNESS_DIR/.last-reviewed && git commit -am 'advance last-reviewed to $REF_SHA'"
fi

exit $rc
