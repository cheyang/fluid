#!/usr/bin/env bash
# re-verify.sh — PR #6270 (azure/setup-helm 5.0.1 -> 5.1.0) harness re-runner.
# Custom variant of the review-finding-verifier re-verify: the layers here are
# shell scripts emitting "<check>\tpass|fail" lines, not go/ginkgo tests.
#
# Usage (from a checkout of THIS branch):
#   bash docs/verification/setup-helm-5-1-0/scripts/re-verify.sh [<ref>]
# With no <ref>, the current PR head is fetched from manifest.pr (machine-
# independent). Runs L1 + L2 against that ref and prints per-check
# Fixed/Still-broken (polarity applied: all checks here are 'contract').
# Exit 0 iff every contract check passes on the target ref.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="$HERE/../verify-manifest.json"
TARGET_REF="${1:-}"
if [ -z "$TARGET_REF" ]; then
  PR_URL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["pr"])' "$MANIFEST")"
  num="$(printf '%s' "$PR_URL" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  repo="$(printf '%s' "$PR_URL" | sed -E 's#(https://[^/]+/[^/]+/[^/]+)/pull/.*#\1#')"
  git fetch --quiet "$repo.git" "pull/$num/head"
  TARGET_REF="$(git rev-parse FETCH_HEAD)"
  echo "re-verify: resolved current PR head $TARGET_REF"
fi
LAST="$(cat "$HERE/../.last-reviewed" 2>/dev/null || true)"
echo "re-verify: last-reviewed = ${LAST:-<none>}  (incremental review delta = ${LAST:-base}..$TARGET_REF)"

WT="$(mktemp -d)"
git worktree add --detach "$WT" "$TARGET_REF" >/dev/null 2>&1 || { echo "re-verify: cannot add worktree at $TARGET_REF" >&2; exit 2; }
trap 'git worktree remove --force "$WT" >/dev/null 2>&1 || true' EXIT

RESULTS="$(mktemp)"
run_layer() { # $1 = script path; keep only "<id>\tpass|fail" lines
  bash "$1" "$WT" 2>&1 | grep -E '^[A-Za-z_0-9]+[[:space:]]+(pass|fail)$' || true
}
{
  run_layer "$HERE/check-pin.sh"
  run_layer "$HERE/helm-compat.sh"
} > "$RESULTS"
cat "$RESULTS"

N="$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["findings"]))' "$MANIFEST")"
VERDICTS="$(mktemp)"
printf '\n================  RE-VERIFY  ================\n'
printf '%-8s %-9s %-28s %-14s\n' ID POLARITY CHECK VERDICT
for i in $(seq 0 $((N-1))); do
  id="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['findings'][$i]['id'])")"
  pol="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['findings'][$i]['polarity'])")"
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    r="$(awk -F'\t' -v m="$m" '$1==m{print $2}' "$RESULTS" | tail -1)"
    if [ -z "$r" ]; then
      v="HARNESS-UPDATE"
    elif [ "$pol" = "contract" ]; then
      if [ "$r" = "pass" ]; then v="FIXED"; else v="STILL-BROKEN"; fi
    else
      if [ "$r" = "fail" ]; then v="FIXED(flipped)"; else v="STILL-PRESENT"; fi
    fi
    printf '%-8s %-9s %-28s %-14s\n' "$id" "$pol" "$m" "$v" | tee -a "$VERDICTS"
  done < <(python3 -c "import json;print('\n'.join(json.load(open('$MANIFEST'))['findings'][$i]['match']))")
done
rm -f "$RESULTS"
if grep -qE 'STILL-BROKEN|STILL-PRESENT|HARNESS-UPDATE' "$VERDICTS"; then rm -f "$VERDICTS"; exit 1; fi
rm -f "$VERDICTS"
exit 0
