#!/usr/bin/env bash
# re-verify.sh — re-run the setup-helm-bump harness against the current PR head
# (or an explicit ref) and report per-claim verdicts with polarity applied.
#
# PR #6270 is a CI-config-only change (two workflow YAML files), so this
# harness is script-based (python3) rather than go/ginkgo; no cluster needed.
#
# Usage:  bash docs/verification/setup-helm-bump/scripts/re-verify.sh [<fixed-ref>]
#   <fixed-ref>  defaults to the current PR head, resolved machine-independently
#                from manifest.pr via `git fetch <repo-url> pull/<n>/head`.
#
# Requires: git, python3 (with pyyaml), network for tag resolution
# (network-dependent checks self-skip as non-gating when offline).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found: $MANIFEST" >&2; exit 2; }
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  rest="${PR_URL#*://}"; owner="$(printf '%s' "${rest#*/}" | cut -d/ -f1)"
  repo="$(printf '%s' "${rest#*/}" | cut -d/ -f2)"
  num="$(printf '%s' "$PR_URL" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  git fetch --quiet "https://github.com/$owner/$repo.git" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: fixed-ref = $FIXED_REF"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
RESULTS_DIR="$TOPIC_DIR/results/reverify"
mkdir -p "$RESULTS_DIR"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
# graft harness onto the code under review (untracked on the PR branch)
HPATHS=()
while IFS= read -r p; do [ -n "$p" ] && HPATHS+=("$p"); done < <(jq -r '.harnessPaths[]' "$MANIFEST")
git checkout "$HARNESS_SRC" -- "${HPATHS[@]}" 2>/dev/null || true

python3 "$HERE/verify_checks.py" > "$RESULTS_DIR/layer-unit.out" 2> "$RESULTS_DIR/layer-unit.err" || true

# polarity table: contract -> pass is good; canary -> pass documents current
# behavior (unpinned), a flip to fail means the workflows changed shape.
printf '\n================  RE-VERIFY  ================\n'
printf '%-40s %-9s %-10s %s\n' CLAIM POLARITY VERDICT DETAIL
ALL_OK=1
N="$(jq '.findings|length' "$MANIFEST")"
for i in $(seq 0 $((N-1))); do
  id="$(jq -r ".findings[$i].id" "$MANIFEST")"
  pol="$(jq -r ".findings[$i].polarity" "$MANIFEST")"
  while IFS= read -r m; do
    [ -z "$m" ] && continue
    r="$(awk -F'\t' -v c="$m" '$1==c{print $2}' "$RESULTS_DIR/layer-unit.out" | tail -1)"
    [ -z "$r" ] && r="missing"
    if [ "$pol" = "contract" ]; then
      v="$([ "$r" = "pass" ] && echo FIXED || echo STILL-BROKEN)"
      [ "$r" = "pass" ] || ALL_OK=0
    else
      # canary: pass = behavior unchanged (still unpinned); fail = flipped
      v="$([ "$r" = "pass" ] && echo UNCHANGED || echo FLIPPED)"
      [ "$r" = "missing" ] && ALL_OK=0
    fi
    printf '%-40s %-9s %-10s %s\n' "$id ($m)" "$pol" "$v" "$r"
  done < <(jq -r ".findings[$i].match[]" "$MANIFEST")
done

LIVE="$(jq -r '.liveNote // empty' "$MANIFEST")"
[ -n "$LIVE" ] && printf '\nLive layer (skipped per round policy): %s\n' "$LIVE"
LAST_REVIEWED_FILE="$TOPIC_DIR/.last-reviewed"
printf '\nReview delta this round: %s..%s\n' "$(cat "$LAST_REVIEWED_FILE" 2>/dev/null || echo base)" "$FIXED_REF"
printf 'After reviewing the delta, advance the marker:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
       "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"
[ "$ALL_OK" -eq 1 ] && { echo "RESULT: all contract claims green."; exit 0; } \
                    || { echo "RESULT: some contract claims failing."; exit 1; }
