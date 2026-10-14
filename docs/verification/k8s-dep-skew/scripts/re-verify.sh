#!/usr/bin/env bash
# re-verify.sh — re-run this PR's verification harness against the current PR head
# (or an explicit <fixed-ref>) and report per finding Fixed / Still-broken / Masked.
#
# Adapted from the review-finding-verifier skill's re-verify.sh for a harness whose
# gates are build-level (go build exit codes), not go-test JSON events: this PR is a
# dependency bump whose findings are compile breaks, so the unit layer here is
# "the tree builds", and the regression sweep only runs once it does.
#
# Usage: bash scripts/re-verify.sh [<fixed-ref>]
#   No <fixed-ref> -> resolves the current PR head from manifest.pr via
#   `git fetch <pr-url-repo> pull/<n>/head` (machine-independent).
#
# Exit 0 iff F1 and F2 are both fixed.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="$HERE/../verify-manifest.json"
LAST_REVIEWED_FILE="$HERE/../.last-reviewed"
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found ($MANIFEST)" >&2; exit 2; }

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  [ -n "$PR_URL" ] || { echo "re-verify: manifest has no .pr and no ref given" >&2; exit 2; }
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  cloneurl="https://$host/$owner/$repo.git"
  echo "re-verify: resolving PR head via 'git fetch $cloneurl pull/$num/head'"
  git fetch --quiet "$cloneurl" "pull/$num/head" || { echo "re-verify: fetch failed" >&2; exit 2; }
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: fixed-ref = $FIXED_REF"

LAST_REVIEWED=""
[ -s "$LAST_REVIEWED_FILE" ] && LAST_REVIEWED="$(tr -d '[:space:]' <"$LAST_REVIEWED_FILE")"
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (review delta = ${LAST_REVIEWED:-base}..$FIXED_REF)"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
RESULTS_TMP="$(mktemp -d)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; rm -rf "$RESULTS_TMP"; }
trap cleanup EXIT

# copy the manifest out of the repo: `git checkout -f` below switches to the fixed
# ref, which does not contain the harness, and the in-repo path would vanish.
cp "$MANIFEST" "$RESULTS_TMP/manifest.json"

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
# graft the harness onto the code under review
HPATHS="$(jq -r '.harnessPaths|join(" ")' "$RESULTS_TMP/manifest.json")"
# shellcheck disable=SC2086
git checkout "$HARNESS_SRC" -- $HPATHS || { echo "re-verify: failed to graft harness paths" >&2; exit 2; }
[ -n "$HPATHS" ] && [ -f "$MANIFEST" ] || { echo "re-verify: graft produced no harness at $MANIFEST" >&2; exit 2; }

echo
printf '================  RE-VERIFY (PR 5392: k8s.io/kubernetes 1.29.15 -> 1.32.10)  ================\n'

# ---- F1: whole-tree compile gate (contract) ----
bash "$HERE/check-build.sh" "$RESULTS_TMP" | tee "$RESULTS_TMP/f1.txt"
F1_RC="${PIPESTATUS[0]}"

# ---- F2: csi.ControllerServer interface gate (contract; masked by F1 while F1 fails) ----
bash "$HERE/probe-csi-interface.sh" "$RESULTS_TMP" | tee "$RESULTS_TMP/f2.txt"
F2_RC="${PIPESTATUS[0]}"

# ---- regression sweep (only meaningful once the tree compiles) ----
bash "$HERE/run-affected-tests.sh" "$RESULTS_TMP" | tee "$RESULTS_TMP/sweep.txt"

# persist raw output into the in-repo results dir (survives checkout restore)
PERSIST="$HERE/../results/reverify-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$PERSIST"
cp -f "$RESULTS_TMP"/* "$PERSIST"/ 2>/dev/null || true

echo
echo "---- verdicts ----"
ALL_FIXED=1
if [ "$F1_RC" -eq 0 ]; then echo "F1 (smd v4.4.2 vs pinned apimachinery 0.29.15): FIXED"; else echo "F1: STILL-BROKEN"; ALL_FIXED=0; fi
case "$F2_RC" in
  0) echo "F2 (csi spec 1.9.0 ControllerServer interface): FIXED";;
  1) echo "F2: STILL-BROKEN"; ALL_FIXED=0;;
  2) echo "F2: STILL-BROKEN (different error after F1 shim - inspect probe-build.txt)"; ALL_FIXED=0;;
  *) echo "F2: HARNESS-UPDATE (probe shim anchor no longer matches vendored smd)"; ALL_FIXED=0;;
esac
if [ "$F1_RC" -eq 0 ]; then
  grep -q "REGRESSION SWEEP: PASS" "$RESULTS_TMP/sweep.txt" || ALL_FIXED=0
fi
echo
echo "Review delta this round: ${LAST_REVIEWED:-base}..$FIXED_REF"
echo "After reviewing that delta, advance the marker:"
echo "  echo $FIXED_REF > $LAST_REVIEWED_FILE && git add $LAST_REVIEWED_FILE && git commit -m 'review: advance last-reviewed'"
[ "$ALL_FIXED" -eq 1 ] && { echo "RESULT: all findings fixed."; exit 0; } || { echo "RESULT: not all findings fixed."; exit 1; }
