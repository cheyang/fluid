#!/usr/bin/env bash
# Re-verify the review findings for PR #6197 against the current PR head.
# No arguments: fetches the PR head from the manifest's "pr" URL.
# With a ref: grafts the harness onto that ref instead.
#
# Prints per finding: Fixed / Still-broken / Partial / Harness-update, and the
# incremental review delta (last-reviewed..head). Exit 0 iff everything is green
# (canaries count as still-broken findings, so exit 0 only once they are fixed
# and inverted — or after they are explicitly inverted in this branch).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="$HERE/verify-manifest.json"
REPO="$(cd "$HERE/../../.." && pwd)"
PR_URL="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])" 2>/dev/null)"
PR_NUM="${PR_URL##*/}"
ORIGIN="https://github.com/fluid-cloudnative/fluid.git"

REF="${1:-}"
if [ -z "$REF" ]; then
  echo "== fetching current PR head for $PR_URL =="
  git -C "$REPO" fetch "$ORIGIN" "pull/$PR_NUM/head" || exit 1
  REF="FETCH_HEAD"
fi
HEAD_SHA="$(git -C "$REPO" rev-parse "$REF")"
echo "PR head: $HEAD_SHA"

# graft harness onto the ref (worktree-free: checkout paths into the current tree)
echo "== grafting harness onto $REF =="
git -C "$REPO" checkout "$REF" -- . 2>/dev/null || true
git -C "$REPO" checkout verify/clientless-runtime-inject-claude -- \
  docs/verification/clientless-runtime-inject \
  pkg/webhook/plugins/runtimeconfiginjector/findings_claude_test.go 2>/dev/null || \
  echo "WARN: could not graft harness paths (already on the verify branch?)"

if [ -f "$HERE/.last-reviewed" ]; then
  LAST="$(cat "$HERE/.last-reviewed")"
  echo
  echo "== incremental review delta ($LAST..$HEAD_SHA) =="
  git -C "$REPO" log --oneline "$LAST..$HEAD_SHA" 2>/dev/null || echo "(no delta)"
  echo "advance the marker after the round:  git checkout $HEAD_SHA -- '$HERE/.last-reviewed'"
fi

echo
echo "== Layer 1 (unit): webhook + common + cache engine =="
( cd "$REPO" && go test -count=1 ./pkg/webhook/... ./pkg/common/... ) > "$HERE/results/go-webhook-common.txt" 2>&1
L1=$?
( cd "$REPO" && go test -count=1 -gcflags=all=-l ./pkg/ddc/cache/... ) > "$HERE/results/go-cache.txt" 2>&1
L1C=$?

echo "== Layer 2 (integration): admission handler chain =="
( cd "$REPO" && go test -count=1 ./pkg/webhook/handler/mutating/... ) > "$HERE/results/go-handler.txt" 2>&1
L2=$?

echo "== Chart layer: render + upgrade merge =="
( cd "$REPO" && helm template fluid charts/fluid/fluid 2>/dev/null | grep -E 'clientless\.fluid\.io|RuntimeConfigInjector|cacheruntimes|checksum/plugins-profile' ) > "$HERE/results/helm-render.txt" 2>&1
CH1=$?
python3 -I "$HERE/scripts/chart_merge_check.py" > "$HERE/results/chart-merge.txt" 2>&1
CH2=$?

echo
echo "== canaries (F1, F2) =="
( cd "$REPO" && go test -count=1 ./pkg/webhook/plugins/runtimeconfiginjector/ -args -ginkgo.focus "Reviewer findings" ) > "$HERE/results/canaries.txt" 2>&1
CAN=$?

echo
echo "================ per-finding verdict ================"
report() { printf '%-10s %-14s %s\n' "$1" "$2" "$3"; }
report ID status evidence
if [ $L1 -eq 0 ] && [ $L1C -eq 0 ] && [ $L2 -eq 0 ]; then
  report C-SUITE pass "unit+handler suites green (results/go-*.txt)"
else
  report C-SUITE fail "see results/go-webhook-common.txt / go-cache.txt / go-handler.txt"
fi
if [ $CH1 -eq 0 ] && [ $CH2 -eq 0 ]; then
  report C-CHART pass "rule+profile+rbac rendered, merge round-trips (results/helm-render.txt, chart-merge.txt)"
else
  report C-CHART fail "see results/helm-render.txt / chart-merge.txt"
fi
if [ $CAN -ne 0 ]; then
  report F1 "fixed?*" "canary suite RED — check which spec failed (results/canaries.txt); a red F1/F2 means the behavior changed: invert the canary"
  report F2 "fixed?*" "see above"
else
  report F1 still-broken "canary green: misleading not-ready-yet message still emitted"
  report F2 still-broken "canary green: invalid volume name still injected for dotted names"
fi

echo
echo "canary exit code: $CAN (0 = both findings still present; 1 = at least one flipped — see results/canaries.txt)"
[ $L1 -eq 0 ] && [ $L1C -eq 0 ] && [ $L2 -eq 0 ] && [ $CH1 -eq 0 ] && [ $CH2 -eq 0 ] || exit 1
# canaries green means the findings are still open -> non-zero exit
[ $CAN -ne 0 ] || exit 2
exit 0
