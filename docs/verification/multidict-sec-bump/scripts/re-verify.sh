#!/usr/bin/env bash
# Re-run the multidict-sec-bump harness against the current PR head (or a
# given ref). Usage: re-verify.sh [<fixed-ref>]
#
# With no argument, fetches the current head of the PR recorded in
# verify-manifest.json (works from any clone of fluid, no local remote name
# needed). Grafts the harness onto that ref in a temp worktree, runs the
# L0/L1/L2 layers, and prints per-finding verdicts:
#   P0     Fixed   when the lock's multidict pin no longer leaks
#   F1     Fixed   when the multidict entry pins the single platform hash
#   V1/V2  OK      while the lock resolves and the pin set stays compatible
# Exit 0 iff P0 and F1 are fixed and V1/V2 hold.
set -uo pipefail

# /tmp is a small tmpfs on the review host; the grafted worktree + wheels need
# the big disk.
mkdir -p "${HOME}/.pr-debate/tmp"
export TMPDIR="${HOME}/.pr-debate/tmp"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="$HERE/verify-manifest.json"
PR_URL=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")
PR_NUM=${PR_URL##*/}
UPSTREAM="https://github.com/fluid-cloudnative/fluid.git"

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
    git fetch "$UPSTREAM" "pull/${PR_NUM}/head"
    FIXED_REF=FETCH_HEAD
fi
echo "verifying against: $FIXED_REF ($(git rev-parse --short $FIXED_REF))"

WORK=$(mktemp -d)
git worktree add --detach "$WORK" "$FIXED_REF" >/dev/null 2>&1
# graft the harness onto the code under review
mkdir -p "$WORK/docs/verification"
cp -r "$HERE" "$WORK/docs/verification/"
(
    cd "$WORK"
    bash docs/verification/multidict-sec-bump/scripts/run_all.sh
) | tee "$HERE/results/re-verify-$(date +%Y%m%d%H%M%S).log"
RC=${PIPESTATUS[0]}

# incremental-review delta
LAST_REVIEWED="$HERE/.last-reviewed"
if [ -f "$LAST_REVIEWED" ]; then
    LR=$(cat "$LAST_REVIEWED")
    echo
    echo "incremental delta since last round ($LR):"
    git log --oneline "$LR..$FIXED_REF" | sed "s/^/  /"
else
    echo "(no .last-reviewed marker: this is the first round)"
fi
echo "after this round: git rev-parse $FIXED_REF > $HERE/.last-reviewed and commit"

git worktree remove --force "$WORK" >/dev/null 2>&1
exit $RC
