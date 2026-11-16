#!/usr/bin/env bash
# Re-verify PR #6256 findings against the current (or given) PR head.
#
#   bash re-verify.sh [<git-ref>]     # default: current PR head fetched from GitHub
#
# Grafts nothing (the harness is chart-level and ref-parameterized): it runs
# the unit layer on the given ref's chart and the integration scenario against
# base vs head charts. Prints per-finding status:
#   P0   premise     -> must keep failing on BASE (sanity that the repro still bites)
#   F1   incomplete  -> FIXED only when the second install SUCCEEDS with default
#                       values (or with only --set namespace=<other>)
#   F2   upgrade     -> upgrade succeeds, old names swapped, subjects preserved
set -u
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(cd "$TOPIC_DIR/../../.." && pwd)"
PR_URL="https://github.com/fluid-cloudnative/fluid/pull/6256"
PR_NUM=6256

REF="${1:-}"
if [[ -z "$REF" ]]; then
  git -C "$REPO_ROOT" fetch https://github.com/fluid-cloudnative/fluid.git "pull/${PR_NUM}/head" >/dev/null 2>&1
  REF="FETCH_HEAD"
fi
HEAD_SHA="$(git -C "$REPO_ROOT" rev-parse "$REF")"
echo "== re-verify $PR_URL @ $HEAD_SHA ==" 2>/dev/null || echo "== re-verify $PR_NUM @ $HEAD_SHA =="

LAST="$(cat "$TOPIC_DIR/.last-reviewed" 2>/dev/null || echo "")"
if [[ -n "$LAST" ]]; then
  echo "incremental review delta: $LAST..$HEAD_SHA"
  git -C "$REPO_ROOT" log --oneline "$LAST..$HEAD_SHA" | head -20 || true
fi

# Unit layer: chart package tests on the target ref (via temp worktree)
WT="$(mktemp -d)/wt"
git -C "$REPO_ROOT" worktree add "$WT" "$HEAD_SHA" >/dev/null 2>&1
( cd "$WT" && go test ./charts/fluid/fluid/ -count=1 ) && UNIT=pass || UNIT=fail
echo "unit layer (go test ./charts/fluid/fluid/ @ head): $UNIT"
git -C "$REPO_ROOT" worktree remove --force "$WT" >/dev/null 2>&1 || true

# Integration layer: full scenario (base vs head)
bash "$SCRIPT_DIR/scenario.sh" --head-ref "$HEAD_SHA"

cat <<EOF

Per-finding verdict mapping (read with scenario SUMMARY above):
  P0   Fixed-n/a     premise holds while P0/P0b keep FAILING on the base chart.
  F1   Fixed         when a second 'helm install' succeeds with defaults on the
                     head chart (F1a/F1b stop failing). Still-broken otherwise.
  F2   informational upgrade mechanics (name swap + subjects) must keep passing.
EOF
