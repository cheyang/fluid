#!/usr/bin/env bash
# Re-verify the review findings for PR #6197 against the current PR head.
# No arguments: fetches the PR head from the manifest's "pr" URL.
# With a ref: grafts the harness onto that ref instead.
#
# Round 3 (head 268012a): the round-2 canaries (A-F1, A-F2) FLIPPED at this
# head and were inverted into contract tests; B-F5, A-N1 and B-F6 got their
# own contracts. The "canaries" run below now executes the full reviewer
# findings suites (contract polarity): green means the fixes hold, red means
# a regression re-opened a finding.
#
# Prints per finding: Fixed / Still-broken / Partial / Harness-update, and the
# incremental review delta (last-reviewed..head). Exit 0 iff everything is green.
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
  pkg/webhook/plugins/runtimeconfiginjector/findings_claude_test.go \
  pkg/webhook/utils/findings_claude_runtime_info_test.go 2>/dev/null || \
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

echo "== Chart layer: render + upgrade merge + checksum stability =="
( cd "$REPO" && helm template fluid charts/fluid/fluid 2>/dev/null | grep -E 'clientless\.fluid\.io|RuntimeConfigInjector|cacheruntimes|checksum/plugins-profile' ) > "$HERE/results/helm-render.txt" 2>&1
CH1=$?
python3 -I "$HERE/scripts/chart_merge_check.py" > "$HERE/results/chart-merge.txt" 2>&1
CH2=$?
python3 -I "$HERE/scripts/checksum_churn_check.py" --show-prefix > "$HERE/results/checksum-churn.txt" 2>&1
CH3=$?

echo
echo "== reviewer findings contracts (A-F1, A-F2, A-N1, B-F5) =="
( cd "$REPO" && go test -count=1 ./pkg/webhook/plugins/runtimeconfiginjector/ ./pkg/webhook/utils/ -args -ginkgo.focus "Reviewer findings" ) > "$HERE/results/canaries.txt" 2>&1
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
if [ $CH3 -eq 0 ]; then
  report B-F6 fixed "second upgrade renders byte-identical -> checksum stable (results/checksum-churn.txt)"
else
  report B-F6 broken "checksum churn on second upgrade — see results/checksum-churn.txt"
fi
if [ $CAN -eq 0 ]; then
  report A-F1 fixed "contract green: non-cache dataset rejected naming the runtime type (results/canaries.txt)"
  report A-F2 fixed "contract green: PVC-resolved name that cannot form a volume name rejected (results/canaries.txt)"
  report A-N1 fixed "contract green: only fluid-fuse / fluid-fuse-<idx> are skipped (results/canaries.txt)"
  report B-F5 fixed "contract green: annotation name denials carry the reason (results/canaries.txt)"
else
  report A-F1 "regressed?" "contract suite RED — check results/canaries.txt"
  report A-F2 "regressed?" "see above"
  report A-N1 "regressed?" "see above"
  report B-F5 "regressed?" "see above"
fi

echo
echo "findings contracts exit code: $CAN (0 = all fixed and holding; 1 = a fix regressed — see results/canaries.txt)"
[ $L1 -eq 0 ] && [ $L1C -eq 0 ] && [ $L2 -eq 0 ] && [ $CH1 -eq 0 ] && [ $CH2 -eq 0 ] && [ $CH3 -eq 0 ] || exit 1
[ $CAN -eq 0 ] || exit 2
exit 0
