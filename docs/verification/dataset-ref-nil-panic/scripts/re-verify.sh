#!/usr/bin/env bash
# re-verify.sh — one-line re-verification for PR #6272 findings (Reviewer A / Claude).
# Usage: bash docs/verification/dataset-ref-nil-panic/scripts/re-verify.sh [<fixed-ref>]
# With no ref, fetches the current PR head from the manifest's `pr` URL.
# Runs the unit + integration layers and prints per-finding
# Fixed / Still-broken / Partial / Harness-update (contract polarity:
# pass = fixed). Exit 0 iff all findings are fixed.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFEST="$MANIFEST_DIR/verify-manifest.json"
TOPIC="dataset-ref-nil-panic"
HARNESS_PATHS=(
  "docs/verification/$TOPIC"
  "pkg/utils/dataset_reference_panic_verify_test.go"
  "pkg/controllers/v1alpha1/dataset/dataset_deletion_referror_test.go"
)

# Resolve PR head
PR_URL=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])" 2>/dev/null || true)
PR_NUM=""
if [ -n "${PR_URL:-}" ]; then
  PR_NUM=$(echo "$PR_URL" | grep -o 'pull/[0-9]*' | grep -o '[0-9]*')
fi

REF="${1:-}"
if [ -z "$REF" ]; then
  if [ -z "$PR_NUM" ]; then
    echo "ERROR: no ref given and no pr URL in manifest" >&2
    exit 2
  fi
  git fetch "https://github.com/fluid-cloudnative/fluid" "pull/$PR_NUM/head" || exit 2
  REF=FETCH_HEAD
fi

REPO_ROOT="$(git rev-parse --show-toplevel)"
WORKTREE=$(mktemp -d /tmp/fluid-6272-reverify.XXXXXX)
echo "Grafting harness onto $REF in $WORKTREE"
git worktree add --detach "$WORKTREE" "$REF" || exit 2
for p in "${HARNESS_PATHS[@]}"; do
  mkdir -p "$WORKTREE/$(dirname "$p")"
  cp -r "$REPO_ROOT/$p" "$WORKTREE/$p"
done

cleanup() { git worktree remove --force "$WORKTREE" 2>/dev/null; }
trap cleanup EXIT

cd "$WORKTREE" || exit 2

fail=0
echo
echo "== Unit layer (contract: pass = fixed) =="
if go test ./pkg/utils/ -run 'TestVerifyRemoveNotFoundDatasetRef|TestRemoveNotFoundDatasetRef' -count=1; then
  echo "AC1-ref-safety: Fixed"
  echo "AC1-nil-client-safety: Fixed"
  echo "AC3-existing-paths-unchanged: Fixed"
else
  echo "AC1-ref-safety / AC1-nil-client-safety / AC3: Still-broken (or panic)"
  fail=1
fi

echo
echo "== Integration layer (contract: pass = fixed) =="
if go test ./pkg/controllers/v1alpha1/dataset/ -count=1 -ginkgo.no-color; then
  echo "AC2-controller-requeue: Fixed"
else
  echo "AC2-controller-requeue: Still-broken (or panic)"
  fail=1
fi

echo
if [ "$fail" -eq 0 ]; then
  echo "ALL FINDINGS FIXED"
else
  echo "ONE OR MORE FINDINGS STILL BROKEN"
fi
exit $fail
