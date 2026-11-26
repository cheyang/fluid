#!/usr/bin/env bash
# re-verify.sh — re-run the codeql-action pin verification harness against the
# current (or a given) PR head for fluid-cloudnative/fluid#6289.
#
# Driven from a checkout of the branch that CONTAINS the harness
# (verify/codeql-action-bump-claude). With no argument it resolves the current
# PR head from the manifest's "pr" URL (machine-independent:
# git fetch https://github.com/fluid-cloudnative/fluid.git pull/6289/head).
#
# Usage:
#   bash scripts/re-verify.sh [<fixed-ref>]
#
# Requires: git, python3 (>=3.6), network for the first tag resolution
# (results/tag-map.cache.json then makes re-runs offline-capable).
#
# Per-finding verdicts (polarity honored):
#   contract checks should PASS when fixed (a red result IS the reproduction);
#   canary checks should FLIP to FAIL when the observed state changes.
#
# Exit code: 0 iff every *gating* finding is Fixed. Non-gating findings
# (nits / informational) are reported but do not fail the run — their status
# is tracked in the round ledger, not by this exit code.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(cd "$HERE/.." && pwd)"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
REPO="$(cd "$TOPIC_DIR/../../../../../.." && pwd)"

command -v python3 >/dev/null || { echo "re-verify: python3 required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
[ -f "$MANIFEST" ] || { echo "re-verify: manifest missing: $MANIFEST" >&2; exit 2; }

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["pr"])' "$MANIFEST")"
  num="${PR_URL##*/pull/}"
  echo "re-verify: resolving PR head via git fetch (PR #$num)"
  git fetch --quiet "https://github.com/fluid-cloudnative/fluid.git" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: fixed-ref = $FIXED_REF"

BASE_SHA="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["baseSha"])' "$MANIFEST")"
# fall back to merge-base if the pinned base is unknown in this clone
git cat-file -e "$BASE_SHA" 2>/dev/null || BASE_SHA="$(git merge-base origin/master "$FIXED_REF" 2>/dev/null || echo "$FIXED_REF")"
echo "re-verify: base = $BASE_SHA"

LAST_REVIEWED_FILE="$TOPIC_DIR/.last-reviewed"
if [ -s "$LAST_REVIEWED_FILE" ]; then
  LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE")"
else
  LAST_REVIEWED="$BASE_SHA"
fi
echo "re-verify: last-reviewed = $LAST_REVIEWED (review delta = $LAST_REVIEWED..$FIXED_REF)"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$ORIG_REF"
RESULTS_DIR="$TOPIC_DIR/results/reverify"
mkdir -p "$RESULTS_DIR"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
# graft the harness (docs/verification/<topic>) from the harness ref
git checkout "$HARNESS_SRC" -- "$TOPIC_DIR" 2>/dev/null || { echo "re-verify: graft failed" >&2; exit 2; }

python3 -I "$TOPIC_DIR/scripts/check_actions_pins.py" \
  --repo . --ref "$FIXED_REF" --base-ref "$BASE_SHA" \
  > "$RESULTS_DIR/unit.out" 2> "$RESULTS_DIR/unit.summary"

# map manifest findings to checks; honor polarity and the gating flag
VERDICTS="$RESULTS_DIR/verdicts.txt"
: > "$VERDICTS"
python3 - "$MANIFEST" "$RESULTS_DIR/unit.out" "$VERDICTS" <<'PYEOF'
import json, sys
manifest, outfile, verdictfile = sys.argv[1:4]
checks = {}
for line in open(outfile):
    parts = line.split()
    if len(parts) >= 3 and parts[0] == "CHECK":
        checks.setdefault(parts[1], []).append(parts[2])  # 'pass' | 'fail'
out = open(verdictfile, "w")
for f in json.load(open(manifest))["findings"]:
    fid, pol, gate = f["id"], f["polarity"], f.get("gating", True)
    results = [r for m in f["match"] for r in checks.get(m, ["missing"])]
    if "missing" in results:
        verdict, detail = "HARNESS-UPDATE", "checks not found: %s" % f["match"]
    elif pol == "contract":
        verdict = "FIXED" if "fail" not in results else ("STILL-BROKEN" if "pass" not in results else "PARTIAL")
        detail = "pass=%d fail=%d" % (results.count("pass"), results.count("fail"))
    else:  # canary: fixed only when it flips to fail
        verdict = "FIXED(flipped)" if "pass" not in results else ("STILL-PRESENT" if "fail" not in results else "PARTIAL")
        detail = "pass=%d fail=%d" % (results.count("pass"), results.count("fail"))
    if not gate and verdict not in ("FIXED", "FIXED(flipped)"):
        verdict += "(non-gating)"
    out.write("%s\t%s\t%s\t%s\t%s\n" % (fid, pol, "unit", verdict, detail))
PYEOF

printf '\n================  RE-VERIFY  ================\n'
printf '%-32s %-9s %-6s %-22s %s\n' ID POLARITY LAYER VERDICT DETAIL
cat "$VERDICTS" | while IFS=$'\t' read -r id pol layer verdict detail; do
  printf '%-32s %-9s %-6s %-22s %s\n' "$id" "$pol" "$layer" "$verdict" "$detail"
done

GATING_FAILED=0
while IFS=$'\t' read -r id pol layer verdict detail; do
  case "$verdict" in STILL-BROKEN|PARTIAL|STILL-PRESENT|HARNESS-UPDATE*) GATING_FAILED=1;; esac
done < "$VERDICTS"

printf '\nRaw output: %s\n' "$RESULTS_DIR/unit.out"
printf 'Review delta this round: %s..%s\n' "$LAST_REVIEWED" "$FIXED_REF"
printf 'After reviewing that delta, advance the marker:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
  "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"

if [ "$GATING_FAILED" -eq 0 ]; then
  echo "RESULT: all gating findings fixed (non-gating nits may remain; see table)."
  exit 0
else
  echo "RESULT: gating findings remain broken."
  exit 1
fi
