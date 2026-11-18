#!/usr/bin/env bash
# re-verify.sh — PR #6266 reviewer harness (Reviewer A / Claude).
# Usage: bash docs/verification/dataload-imagepullsecrets/scripts/re-verify.sh [<fixed-ref>]
# With no ref: fetches the current PR head from the manifest's "pr" URL
# (git fetch <repo> pull/<n>/head) and grafts this harness onto it.
# Runs the unit layer (go test) + the integration layer (helm chart render),
# then prints per finding Fixed / Still-broken / Partial / Harness-update.
# Exit 0 iff all findings are fixed (C3/f2 counts as unfixed until the legacy
# jindo engine gets the same treatment).
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFEST="$VERIFY_DIR/verify-manifest.json"
PR_URL=$(python3 -c "import json,sys;print(json.load(open('$MANIFEST'))['pr'])" 2>/dev/null)
PR_NUM=${PR_URL##*/}
REPO_URL=${PR_URL%/pull/*}
REPO_URL=${REPO_URL%/issues/*}
BASE_BRANCH=${BASE_BRANCH:-origin/master}

if [ $# -ge 1 ]; then
  TARGET_REF="$1"
else
  git fetch "$REPO_URL" "pull/$PR_NUM/head" 2>&1 | tail -1
  TARGET_REF="FETCH_HEAD"
fi
TARGET_SHA=$(git rev-parse "$TARGET_REF")

WORKTREE=$(mktemp -d /tmp/fluid-verify-XXXXXX)
git worktree add --detach "$WORKTREE" "$TARGET_SHA" >/dev/null 2>&1
# graft harness (test files + docs) onto the target ref
git -C "$(git rev-parse --show-toplevel)" archive "$(git rev-parse HEAD)" -- \
  'pkg/ddc/jindofsx/load_data_verify_test.go' \
  'pkg/ddc/jindocache/load_data_verify_test.go' \
  'pkg/ddc/jindo/load_data_verify_test.go' \
  'pkg/utils/posix_paths_verify_test.go' \
  'pkg/utils/validation/posix_verify_test.go' \
  'docs/verification/dataload-imagepullsecrets' \
  | tar -x -C "$WORKTREE"

cd "$WORKTREE" || exit 1
echo "=== harness grafted onto $TARGET_SHA (results below are on that ref) ==="
declare -A STATUS

run_go() { # pkg pattern
  go test "$1" -run "$2" -count=1 >/tmp/verify-gotest.log 2>&1
}

# F1/C1 — runtime secrets replace env default (contract; fail = still broken)
for pkg in ./pkg/ddc/jindofsx ./pkg/ddc/jindocache; do
  if run_go "$pkg" 'TestVerifyImagePullSecretsPriority|TestVerifyDataLoadValuesYAMLEncoding|TestDataLoadImagePullSecrets'; then
    STATUS[f1]="Fixed"
  else
    STATUS[f1]="Still-broken"
    tail -5 /tmp/verify-gotest.log
  fi
done

# F3/C2 — POSIX switch keeps Linux behavior (contract; fail = regression)
if run_go ./pkg/utils 'TestVerifyNormalizeSubPathPOSIXSemantics|TestVerifyUFSPathBuilderPOSIXSemantics' \
   && run_go ./pkg/utils/validation 'TestVerifyIsValidMountRootPOSIXSemantics'; then
  STATUS[f3]="Fixed (no Linux behavior change)"
else
  STATUS[f3]="Still-broken (behavior changed!)"
  tail -5 /tmp/verify-gotest.log
fi

# F2/C3 — legacy jindo engine coverage gap (contract; FAIL = gap still present)
if run_go ./pkg/ddc/jindo 'TestVerifyLegacyJindoImagePullSecrets'; then
  STATUS[f2]="Fixed"
else
  STATUS[f2]="Still-broken (legacy jindo engine still ignores runtime spec.imagePullSecrets)"
fi

# integration layer — helm chart consumes the values
bash docs/verification/dataload-imagepullsecrets/scripts/render_dataload_chart.sh >/tmp/verify-chart.log 2>&1
CHART_RC=$?
cat /tmp/verify-chart.log

cd - >/dev/null
git worktree remove --force "$WORKTREE" >/dev/null 2>&1

echo
echo "=== per-finding verdict (ref $TARGET_SHA) ==="
for k in f1 f2 f3; do echo "$k: ${STATUS[$k]:-Harness-update}"; done
[ "$CHART_RC" -eq 0 ] || echo "chart-render: FAIL (integration layer)"

[ "${STATUS[f1]}" = "Fixed" ] && [ "${STATUS[f2]}" = "Fixed" ] && [ "${STATUS[f3]:0:5}" = "Fixed" ] && [ "$CHART_RC" -eq 0 ]
