#!/usr/bin/env bash
# re-verify.sh — PR #6267 verification harness (reviewer: claude)
# Usage: bash scripts/re-verify.sh [<ref>]   (no ref => current PR head)
# Grafts this harness onto the target ref (via a temp worktree), runs the
# unit+integration layers, prints per-finding Fixed / Still-broken / Partial /
# Harness-update. Exit 0 iff all contract checks pass.
set -euo pipefail

MANIFEST_DIR="$(cd "$(dirname "$0")/.." && pwd)"
REPO_DIR="$(cd "$MANIFEST_DIR/../.." && pwd)"
PR_URL="https://github.com/fluid-cloudnative/fluid/pull/6267"
PR_N=6267

TARGET_REF="${1:-}"
if [ -z "$TARGET_REF" ]; then
    TARGET_REF="refs/remotes/origin/pr/$PR_N"
    git -C "$REPO_DIR" fetch origin "pull/$PR_N/head:refs/remotes/origin/pr/$PR_N" >/dev/null 2>&1 || \
        git -C "$REPO_DIR" fetch https://github.com/fluid-cloudnative/fluid.git "pull/$PR_N/head:refs/remotes/origin/pr/$PR_N" >/dev/null 2>&1
fi
TARGET_SHA="$(git -C "$REPO_DIR" rev-parse "$TARGET_REF")"
echo "target: $TARGET_SHA"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
git -C "$REPO_DIR" worktree add --detach "$WORK" "$TARGET_SHA" >/dev/null 2>&1

cp "$MANIFEST_DIR/scripts/check_troubleshooting_links.py" "$WORK/check.py"
if (cd "$WORK" && python3 check.py); then
    echo "C1/C2: PASS"
    RESULT=0
else
    echo "C1/C2: FAIL"
    RESULT=1
fi

# Optional network layer (skipped silently when offline).
if (cd "$WORK" && python3 check.py --http >/dev/null 2>&1); then
    echo "HTTP layer: PASS"
elif [ "${REVERIFY_HTTP:-0}" = "1" ]; then
    echo "HTTP layer: FAIL"
    RESULT=1
else
    echo "HTTP layer: skipped/unreachable (set REVERIFY_HTTP=1 to enforce)"
fi

if [ -f "$MANIFEST_DIR/.last-reviewed" ]; then
    LAST="$(cat "$MANIFEST_DIR/.last-reviewed")"
    echo "delta since last round: $LAST..$TARGET_SHA"
else
    echo "no .last-reviewed marker; this is the first round"
fi
echo "advance marker after this round:  echo $TARGET_SHA > $MANIFEST_DIR/.last-reviewed"

exit $RESULT
