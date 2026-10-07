#!/usr/bin/env bash
# Re-verification for PR #6197 reviewer harness (Reviewer A / Claude).
#
# Usage (from a checkout of the branch that holds this harness):
#   bash docs/verification/runtime-config-injector-claude/scripts/re-verify.sh [<ref>]
#
# With no <ref>, the current PR head is fetched from the manifest's `pr` URL
# (git fetch <repo> pull/<n>/head) - no local remote name needed, works on any
# clone/machine. The harness (test file + this docs/ tree) is grafted onto that
# ref in a temp worktree, and the unit layer is re-run.
#
# Polarity (see verify-manifest.json and README.md):
#   - CONTRACT/CONTROL specs: pass = correct behavior
#   - CANARY specs: pass = the reported bug is STILL present; fail = fixed
#
# Exit 0 iff every canary FAILED (bug fixed) and every contract passed.
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
DOCS="$(dirname "$HERE")"
TOPIC="$(basename "$DOCS")"
MANIFEST="$DOCS/verify-manifest.json"
REPO_URL=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")
PR_NUM=$(python3 -c "import re;print(re.search(r'/pull/(\d+)','''$REPO_URL''').group(1))")

REF="${1:-}"
REPO_ROOT="$(git rev-parse --show-toplevel)"
if [ -z "$REF" ]; then
  UPSTREAM=$(git -C "$REPO_ROOT" remote get-url origin 2>/dev/null || echo "https://github.com/fluid-cloudnative/fluid.git")
  git -C "$REPO_ROOT" fetch "$UPSTREAM" "pull/$PR_NUM/head" 2>/dev/null
  REF="FETCH_HEAD"
fi
[ -z "${REF:-}" ] && { echo "no ref given and PR head could not be fetched"; exit 2; }

WORKTREE=$(mktemp -d /tmp/fluid-reverify-XXXX)
git -C "$REPO_ROOT" worktree add --detach "$WORKTREE" "$REF" >/dev/null 2>&1 || { echo "worktree add failed"; exit 2; }
trap 'git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null' EXIT

# graft the harness onto the ref under test
mkdir -p "$WORKTREE/pkg/webhook/handler/mutating" "$WORKTREE/docs/verification"
cp "$REPO_ROOT/pkg/webhook/handler/mutating/clientless_harness_test.go" \
   "$WORKTREE/pkg/webhook/handler/mutating/" 2>/dev/null || true
cp -r "$DOCS" "$WORKTREE/docs/verification/$TOPIC"

LAST=$(cat "$DOCS/.last-reviewed" 2>/dev/null || echo "$REF")
echo "== re-verifying PR ${PR_NUM} at ${REF} =="
echo "delta since last review (${LAST}):"
git -C "$WORKTREE" log --oneline "$LAST"..HEAD | sed 's/^/  /'

cd "$WORKTREE"
REPORT=$(mktemp /tmp/ginkgo-report-XXXX.json)
OUT="$DOCS/results/re-verify-latest.txt"
{
  echo "re-verify at $(git rev-parse HEAD) on $(date -u +%FT%TZ)"
  go build ./... || echo "BUILD FAILED"
  go test -gcflags=all=-l ./pkg/webhook/handler/mutating/ \
    -ginkgo.focus="reviewer harness" -v -ginkgo.json-report="$REPORT"
} 2>&1 | tee "$OUT"

echo
echo "== verdict per spec (canary pass = STILL BROKEN) =="
python3 - "$REPORT" <<'EOF'
import json, sys
report = json.load(open(sys.argv[1]))
if isinstance(report, dict):
    suites = [report]
else:  # ginkgo emits a list of suite reports
    suites = report
specs = []
for suite in suites:
    specs.extend(suite.get("SpecReports", []) or suite.get("specReports", []))
canary_unfixed = contract_broken = 0
for spec in specs:
    title = " ".join(spec.get("ContainerHierarchyTexts", [])) + " " + spec.get("LeafNodeText", "")
    state = spec.get("State", "")
    failed = state in ("failed", "timedout", "panicked")
    skipped = state in ("skipped", "pending")
    if skipped:
        continue
    kind = "canary" if "CANARY" in title else ("contract" if ("CONTRACT" in title or "CONTROL" in title) else "other")
    if kind == "canary":
        if failed:
            print(f"  FIXED      (canary failed): {title.strip()}")
        else:
            print(f"  STILL-BROKEN (canary passed): {title.strip()}")
            canary_unfixed += 1
    else:
        if failed:
            print(f"  BROKEN     (contract failed): {title.strip()}")
            contract_broken += 1
        else:
            print(f"  ok         ({kind} passed): {title.strip()}")
sys.exit(1 if (canary_unfixed or contract_broken) else 0)
EOF
STATUS=$?
echo
[ $STATUS -eq 0 ] && echo "ALL FINDINGS FIXED (every canary flipped, every contract green)" \
                  || echo "NOT ALL FIXED (see above)"
exit $STATUS
