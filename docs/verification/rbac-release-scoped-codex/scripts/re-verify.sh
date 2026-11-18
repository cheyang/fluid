#!/usr/bin/env bash
# Re-verify PR #6256 findings against the current (or given) PR head.
#
#   bash re-verify.sh [<git-ref>]     # default: current PR head fetched from GitHub
#
# Grafts nothing (the harness is chart-level and ref-parameterized): it runs
# the unit layer on the given ref's chart and the render+integration scenario
# against base vs head charts. Round-2 contract (head da020a3):
#   P0   premise     -> must keep failing on BASE (sanity that the repro still bites)
#   R1/R2 contract   -> default render identical to base; prefixes fully disjoint
#   R3/K1/K2 canary  -> documented out-of-scope remainder (CSIDriver/MWC/fixed
#                       namespaced SAs); FIXED only when these FLIP to fail
#   C1/U1/U2 contract-> the delivered scope keeps working
#   U3   canary (N1) -> orphaned hook RBAC not pruned on prefix opt-in; FIXED
#                       when it flips (hook objects pruned or never orphaned)
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
echo "== re-verify $PR_URL @ $HEAD_SHA =="

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

# Render + integration layers: full scenario (base vs head)
bash "$SCRIPT_DIR/scenario.sh" --head-ref "$HEAD_SHA"

cat <<'MAPPING_EOF'

Per-finding verdict mapping (read with scenario SUMMARY above; canaries are
FIXED only when they FLIP to fail — then invert them):
  P0           premise: holds while P0.install2/P0b keep FAILING on the base chart.
  F1   (round-1 blocker) fixed as a CLAIM: PR no longer says "Fixes #6134".
       Residual behavior tracked by canaries K1/K2 (documented limitation).
  F2   (round-1 same-name collision) superseded: release-name no longer feeds
       RBAC names; users pick unique prefixes (values.yaml comment).
  F3   (test gap) fixed: PR test now uses real `helm template` over the whole
       chart; R2 independently covers cross-release disjointness.
  F5   (upgrade churn) fixed by design: R1 byte-identical default render + U1.
  N1   (orphaned hook RBAC on prefix opt-in): canary U3.
MAPPING_EOF
