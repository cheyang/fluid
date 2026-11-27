#!/usr/bin/env bash
# re-verify.sh for PR #6292 — diagnose-script runtime-name harness.
# Usage: bash scripts/re-verify.sh [<ref>]     (no ref => current PR head, auto-fetched)
# Grafts the harness onto the target ref (temp detached worktree), runs the
# unit layer, and prints per finding Fixed / Still-broken / Harness-update.
# Contract tests: Fixed iff PASS. Canary tests: Fixed iff FAIL (flip).
# Exit 0 iff all findings are Fixed.
set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"   # docs/verification/<topic>
BRANCH_ROOT="$(cd "$HERE/../.." && pwd)"                  # harness checkout root
MANIFEST="$HERE/verify-manifest.json"
RESULTS_OUT="$HERE/results/reverify-run.txt"

PR_URL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["pr"])' "$MANIFEST")"
PR_NUM="${PR_URL##*/}"
REPO_URL="https://github.com/fluid-cloudnative/fluid.git"

TARGET_REF="${1:-}"
if [ -z "$TARGET_REF" ]; then
  echo "==> fetching current PR head from $REPO_URL (pull/$PR_NUM/head)"
  git -C "$BRANCH_ROOT" fetch "$REPO_URL" "pull/$PR_NUM/head" || exit 2
  TARGET_REF="FETCH_HEAD"
fi
TARGET_SHA="$(git -C "$BRANCH_ROOT" rev-parse "$TARGET_REF")"
echo "==> verifying PR head: $TARGET_SHA"

LAST_REVIEWED=""
[ -f "$HERE/.last-reviewed" ] && LAST_REVIEWED="$(cat "$HERE/.last-reviewed")"
if [ -n "$LAST_REVIEWED" ]; then
  echo "==> incremental delta $LAST_REVIEWED..$TARGET_SHA:"
  git -C "$BRANCH_ROOT" log --oneline "$LAST_REVIEWED..$TARGET_SHA" | sed 's/^/    /'
  echo "    (advance the marker after this round: echo $TARGET_SHA > $HERE/.last-reviewed)"
else
  echo "==> no .last-reviewed marker; delta falls back to merge-base"
fi

TMPWT="$(mktemp -d)"
if ! git -C "$BRANCH_ROOT" worktree add --detach "$TMPWT" "$TARGET_SHA" >/dev/null 2>&1; then
  echo "ERROR: could not create worktree at $TARGET_SHA" >&2
  rm -rf "$TMPWT"
  exit 2
fi

mkdir -p "$HERE/results"
bash "$HERE/scripts/run_harness.sh" "$TMPWT" "$HERE/results/reverify-latest" | tee "$RESULTS_OUT"
RC="${PIPESTATUS[0]}"
git -C "$BRANCH_ROOT" worktree remove --force "$TMPWT" >/dev/null 2>&1

echo "==> per-finding verdicts (contract polarity: Fixed iff PASS)"
python3 - "$MANIFEST" "$RESULTS_OUT" <<'PY'
import json, re, sys
manifest = json.load(open(sys.argv[1]))
lines = open(sys.argv[2]).read().splitlines()
all_fixed = True
for f in manifest.get("findings", []):
    fid = f["id"]
    hits = [l for l in lines if re.match(r'^(PASS|FAIL): %s\b' % re.escape(fid), l)]
    passed = sum(1 for l in hits if l.startswith("PASS"))
    failed = sum(1 for l in hits if l.startswith("FAIL"))
    if not hits:
        verdict = "Harness-update"   # no test reported for this finding
        all_fixed = False
    elif f.get("polarity") == "canary":
        verdict = "Fixed" if failed else "Still-broken"
        all_fixed = all_fixed and failed
    else:  # contract
        verdict = "Fixed" if (passed and not failed) else "Still-broken"
        all_fixed = all_fixed and passed and not failed
    print("    %-6s %-16s (%d PASS / %d FAIL)" % (fid, verdict, passed, failed))
sys.exit(0 if all_fixed else 1)
PY
VERDICT_RC=$?

if [ "$RC" -ne 0 ]; then
  echo "==> harness itself reported failures (exit $RC)"
fi
exit $VERDICT_RC
