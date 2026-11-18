#!/usr/bin/env bash
# re-verify.sh — re-run the sklearn-150-bump verification harness against the
# current (or a given) ref of PR #6260 and report per finding:
#   FIXED / STILL-BROKEN / PARTIAL / HARNESS-UPDATE
# applying test polarity (contract tests go green when fixed; canaries flip).
#
# Usage (from a checkout of the branch that contains this harness):
#   scripts/re-verify.sh [fixed-ref]
# With no ref, the current PR head is resolved machine-independently via
# `git fetch <repo-url> pull/6260/head` using manifest.pr.
#
# Layers: unit (pypi.org metadata checks) + integration (pinned venv build +
# sample-path contract test). No live/KUBECONFIG layer for this topic.
# Exit 0 iff every contract finding passes (F1 is expected to fail until the
# header recipe is updated — see the review findings).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
FIXED_REF="${1:-}"

command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: tracked changes present; commit or stash first (git checkout -f below)" >&2
  exit 2
fi

if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  cloneurl="https://$host/$owner/$repo.git"
  echo "re-verify: resolving PR head via git fetch $cloneurl pull/$num/head"
  git fetch --quiet "$cloneurl" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: fixed-ref = $FIXED_REF"

LAST_REVIEWED_FILE="$TOPIC_DIR/.last-reviewed"
LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE" 2>/dev/null || true)"
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (review delta = ${LAST_REVIEWED:-base}..$FIXED_REF)"

RUNTIME_DIR="$(mktemp -d)"
RESULTS_DIR="$TOPIC_DIR/results/reverify"
mkdir -p "$RESULTS_DIR"
# copy the manifest outside the repo: `git checkout -f <fixed-ref>` removes the
# in-repo harness (it exists only on the harness branch) before re-grafting
cp "$MANIFEST" "$RUNTIME_DIR/manifest.json"
MANIFEST="$RUNTIME_DIR/manifest.json"
ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; rm -rf "$RUNTIME_DIR" 2>/dev/null || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
HPATHS=()
while IFS= read -r p; do [ -n "$p" ] && HPATHS+=("$p"); done < <(jq -r '.harnessPaths[]' "$MANIFEST")
[ "${#HPATHS[@]}" -gt 0 ] || { echo "re-verify: no harnessPaths in manifest" >&2; exit 2; }
git checkout "$HARNESS_SRC" -- "${HPATHS[@]}" 2>/dev/null || { echo "re-verify: failed to graft harness paths" >&2; exit 2; }

echo "re-verify: running unit + integration layers"
bash "$HERE/run-unit-layer.sh" samples/vineyard/requirements.txt \
  | tee "$RESULTS_DIR/unit-layer.out" | grep -E $'\tpass$|\tfail$' > "$RESULTS_DIR/unit.parsed" || true

VENV="$RUNTIME_DIR/venv"
bash "$HERE/run-integration-layer.sh" samples/vineyard/requirements.txt "$VENV" \
  | tee "$RESULTS_DIR/integration-layer.out" | grep -E $'\tpass$|\tfail$' > "$RESULTS_DIR/integration.parsed" || true

lookup() { # $1=layer $2=match
  local hit
  hit="$(awk -F'\t' -v m="$2" 'index($1,m){print $2}' "$RESULTS_DIR/$1.parsed" 2>/dev/null | tail -1)"
  [ -n "$hit" ] && echo "$hit" || echo "missing"
}

printf '\n================  RE-VERIFY  ================\n'
printf '%-6s %-9s %-12s %-14s %s\n' ID POLARITY LAYER VERDICT DETAIL
ANY_OPEN=0
N="$(jq '.findings|length' "$MANIFEST")"
for i in $(seq 0 $((N-1))); do
  id="$(jq -r ".findings[$i].id" "$MANIFEST")"
  pol="$(jq -r ".findings[$i].polarity" "$MANIFEST")"
  layer="$(jq -r ".findings[$i].layer" "$MANIFEST")"
  matches=()
  while IFS= read -r m; do [ -n "$m" ] && matches+=("$m"); done < <(jq -r ".findings[$i].match[]" "$MANIFEST")
  pass=0; fail=0; miss=0
  for m in "${matches[@]}"; do
    r="$(lookup "$layer" "$m")"
    case "$r" in pass) pass=$((pass+1));; fail) fail=$((fail+1));; *) miss=$((miss+1));; esac
  done
  detail="pass=$pass fail=$fail miss=$miss"
  verdict=""
  if [ "$miss" -gt 0 ]; then
    verdict="HARNESS-UPDATE"; detail="checks not found: $detail"; ANY_OPEN=1
  elif [ "$pol" = "contract" ]; then
    if [ "$fail" -eq 0 ]; then verdict="FIXED";
    elif [ "$pass" -eq 0 ]; then verdict="STILL-BROKEN"; ANY_OPEN=1;
    else verdict="PARTIAL"; ANY_OPEN=1; fi
  else
    if [ "$pass" -eq 0 ]; then verdict="FIXED(flipped)"; ANY_OPEN=1;
    elif [ "$fail" -eq 0 ]; then verdict="STILL-PRESENT"; ANY_OPEN=1;
    else verdict="PARTIAL"; ANY_OPEN=1; fi
  fi
  printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "$verdict" "$detail"
done

printf '\nRaw output: %s\n' "$RESULTS_DIR"
printf 'Review delta this round: %s..%s\n' "${LAST_REVIEWED:-base}" "$FIXED_REF"
printf 'After reviewing that delta, advance the marker:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
       "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"
if [ "$ANY_OPEN" -eq 0 ]; then echo "RESULT: all contract checks green."; exit 0
else echo "RESULT: at least one contract check still red (F1 is open until the header recipe is updated)."; exit 1; fi
