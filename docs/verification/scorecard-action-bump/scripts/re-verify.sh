#!/usr/bin/env bash
# Re-run the scorecard-action pin harness against the current PR head.
# Usage: bash docs/verification/scorecard-action-bump/scripts/re-verify.sh [<fixed-ref>]
#   - no ref: fetches the current PR head from verify-manifest.json's "pr" URL
#     (works from any clone, fork PRs included).
#   - prints the last-reviewed..head delta and a per-check verdict table.
# Exit 0 iff all FAIL-level checks pass on the grafted head.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TOPIC_DIR="$(cd "$HERE/.." && pwd)"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
MARKER="$TOPIC_DIR/.last-reviewed"

PR_URL=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")
PR_NUM=${PR_URL##*/}
REPO_URL=${PR_URL%/pull/*}

REF="${1:-}"
if [ -z "$REF" ]; then
  git fetch "$REPO_URL" "pull/$PR_NUM/head"
  REF=$(git rev-parse FETCH_HEAD)
fi
echo "PR head under test: $REF"

LAST=$(cat "$MARKER" 2>/dev/null || echo "")
if [ -n "$LAST" ] && [ "$LAST" != "$REF" ]; then
  echo "incremental delta: $LAST..$REF"
  git log --oneline "$LAST..$REF" 2>/dev/null || true
elif [ "$LAST" = "$REF" ]; then
  echo "head unchanged since last round ($LAST)"
fi

# merge-base with master gives the PR base -> the PREVIOUS action pin
git fetch "$REPO_URL" master
MASTER=$(git rev-parse FETCH_HEAD)
MB=$(git merge-base "$REF" "$MASTER")
PREV_PIN=$(git show "$MB":.github/workflows/sonarcloud.yaml 2>/dev/null \
  | grep -o "ossf/scorecard-action@[0-9a-f]\{40\}" | head -1 | cut -d@ -f2 || true)
echo "base merge-base: $MB (prev action pin: ${PREV_PIN:-unknown})"

# graft the harness onto the ref under test in a throwaway worktree
WT=$(mktemp -d)
git worktree add --detach "$WT" "$REF" >/dev/null 2>&1
trap 'git worktree remove --force "$WT" >/dev/null 2>&1; rm -rf "$WT"' EXIT
rm -rf "$WT/docs/verification/scorecard-action-bump"
mkdir -p "$WT/docs/verification"
cp -r "$TOPIC_DIR" "$WT/docs/verification/scorecard-action-bump"

cd "$WT"
if [ -n "$PREV_PIN" ] && [ "$PREV_PIN" != "unknown" ]; then
  python3 docs/verification/scorecard-action-bump/scripts/check_action_pin.py \
    --workflow .github/workflows/sonarcloud.yaml \
    --prev-pin "$PREV_PIN" \
    --report docs/verification/scorecard-action-bump/results/l1-report.json
else
  python3 docs/verification/scorecard-action-bump/scripts/check_action_pin.py \
    --workflow .github/workflows/sonarcloud.yaml \
    --report docs/verification/scorecard-action-bump/results/l1-report.json
fi

# per-check verdict table
python3 - <<'PYEOF'
import json
r = json.load(open('docs/verification/scorecard-action-bump/results/l1-report.json'))
print("\nper-check verdicts (Fixed = pass):")
for c in r['checks']:
    print("  %-3s %-9s %s" % (c['id'], c['status'].upper(), c['name']))
print("summary:", r['summary'])
PYEOF

echo
echo "NOTE: check C3 (inert exclude_checks/exclude_paths inputs) is a pre-existing"
echo "WARN-level canary; it stays 'warn' until the inputs are removed or upstream"
echo "declares them. All other checks are contract tests: red = regression."
echo "Advance the marker after the round:"
echo "  echo $REF > $MARKER"
