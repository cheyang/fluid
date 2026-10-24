#!/usr/bin/env bash
# re-verify.sh — re-run the mooncake-aiohttp-yarl harness against updated code.
#
# Adapted from the review-finding-verifier skill's re-verify.sh for a
# shell/python (pip lockfile) harness instead of go/ginkgo: layer kind "shell"
# runs a bash command that appends "name<TAB>pass|fail" lines to
# $STATUS_FILE (exported per layer); everything else (manifest discovery, PR
# head auto-fetch, harness grafting, polarity-aware verdicts, .last-reviewed
# delta, exit code) follows the original contract.
#
# Usage:   re-verify.sh [<fixed-ref>] [--manifest <path>] [--layers unit,integration]
# Default: no ref -> fetch the CURRENT head of the PR in manifest.pr.
# Exit 0 iff every finding is Fixed.
#
# Requires: git, jq, python3, network access to pypi.org. Set PIP_BIN to a
# modern pip (>= 24) if the host python's pip is too old to fetch PyPI.
set -euo pipefail

FIXED_REF=""; MANIFEST=""; LAYERS="unit,integration"
while [ $# -gt 0 ]; do
  case "$1" in
    --manifest) MANIFEST="$2"; shift 2;;
    --layers)   LAYERS="$2"; shift 2;;
    -h|--help)  sed -n '2,20p' "$0"; exit 0;;
    *) [ -z "$FIXED_REF" ] && FIXED_REF="$1" && shift || { echo "unexpected arg: $1" >&2; exit 2; };;
  esac
done
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
# `git checkout -f` below discards uncommitted changes to TRACKED files.
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi

# discover manifest
if [ -z "$MANIFEST" ]; then
  MANIFEST="$(ls docs/verification/*/verify-manifest.json 2>/dev/null | head -1 || true)"
  [ -n "$MANIFEST" ] || MANIFEST="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/verify-manifest.json"
fi
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found ($MANIFEST)" >&2; exit 2; }
echo "re-verify: manifest = $MANIFEST"

MANIFEST_ORIG="$MANIFEST"
RUNTIME_DIR="$(mktemp -d)"
cp "$MANIFEST" "$RUNTIME_DIR/manifest.json"
MANIFEST="$RUNTIME_DIR/manifest.json"

# resolve the PR head machine-independently when no fixed-ref was given
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  [ -n "$owner" ] && [ -n "$repo" ] && [ -n "$num" ] || { echo "re-verify: bad pr URL: $PR_URL" >&2; exit 2; }
  echo "re-verify: resolving PR head via git fetch https://$host/$owner/$repo.git pull/$num/head"
  git fetch --quiet "https://$host/$owner/$repo.git" "pull/$num/head" || { echo "re-verify: fetch failed" >&2; exit 2; }
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
  echo "re-verify: current head = $FIXED_REF"
fi

LAST_REVIEWED_FILE="$(dirname "$MANIFEST_ORIG")/.last-reviewed"
if [ -s "$LAST_REVIEWED_FILE" ]; then
  LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE")"
else
  LAST_REVIEWED="$(git merge-base origin/master "$FIXED_REF" 2>/dev/null || git merge-base master "$FIXED_REF" 2>/dev/null || echo '')"
fi
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (review delta = ${LAST_REVIEWED:-base}..$FIXED_REF)"

RESULTS_DIR="$RUNTIME_DIR/results"
mkdir -p "$RESULTS_DIR"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; rm -rf "$RUNTIME_DIR" 2>/dev/null || true; }
trap cleanup EXIT

echo "re-verify: fixed-ref=$FIXED_REF  harness-from=$ORIG_REF"
git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }

HPATHS=()
while IFS= read -r p; do [ -n "$p" ] && HPATHS+=("$p"); done < <(jq -r '.harnessPaths[]' "$MANIFEST")
git checkout "$HARNESS_SRC" -- "${HPATHS[@]}" 2>/dev/null || { echo "re-verify: failed to graft harness paths" >&2; exit 2; }

# run one layer; "shell" kind: the cmd appends "name<TAB>pass|fail" lines to
# $STATUS_FILE (= $RESULTS_DIR/<layer>.parsed). Missing/empty -> harness error.
run_layer() {
  local layer="$1" kind cmd out
  kind="$(jq -r --arg l "$layer" '.layers[$l].kind // empty' "$MANIFEST")"
  cmd="$(jq -r --arg l "$layer" '.layers[$l].cmd // empty' "$MANIFEST")"
  [ -n "$cmd" ] || return 0
  out="$RESULTS_DIR/${layer}.out"
  STATUS_FILE="$RESULTS_DIR/$layer.parsed" LOG_FILE="$RESULTS_DIR/$layer.log" \
    bash -c "$cmd" >"$out" 2>&1 || true
  if [ ! -s "$RESULTS_DIR/$layer.parsed" ]; then
    echo "__BUILD_OR_NO_TESTS__" > "$RESULTS_DIR/$layer.parsed"
  fi
}

for L in ${LAYERS//,/ }; do
  run_layer "$L"
done

lookup() { # $1=layer $2=match-substring -> pass|fail|missing|build
  local f="$RESULTS_DIR/$1.parsed"
  [ -f "$f" ] || { echo "missing"; return; }
  grep -q '__BUILD_OR_NO_TESTS__' "$f" && { echo "build"; return; }
  local hit; hit="$(awk -F'\t' -v m="$2" 'index($1,m){print $2}' "$f" | tail -1)"
  [ -n "$hit" ] && echo "$hit" || echo "missing"
}

printf '\n================  RE-VERIFY  ================\n'
printf '%-6s %-9s %-12s %-14s %s\n' ID POLARITY LAYER VERDICT DETAIL
ALL_FIXED=1
N="$(jq '.findings|length' "$MANIFEST")"
for i in $(seq 0 $((N-1))); do
  id="$(jq -r ".findings[$i].id" "$MANIFEST")"
  pol="$(jq -r ".findings[$i].polarity" "$MANIFEST")"
  layer="$(jq -r ".findings[$i].layer" "$MANIFEST")"
  case ",$LAYERS," in *",$layer,"*) : ;; *)
    printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "SKIPPED" "layer not in --layers"
    ALL_FIXED=0; continue;; esac
  matches=()
  while IFS= read -r m; do [ -n "$m" ] && matches+=("$m"); done < <(jq -r ".findings[$i].match[]" "$MANIFEST")
  pass=0; fail=0; miss=0; build=0
  for m in "${matches[@]}"; do
    r="$(lookup "$layer" "$m")"
    case "$r" in pass) pass=$((pass+1));; fail) fail=$((fail+1));; build) build=$((build+1));; *) miss=$((miss+1));; esac
  done
  verdict=""; detail="pass=$pass fail=$fail miss=$miss"
  if [ "$build" -gt 0 ]; then
    verdict="HARNESS-UPDATE"; detail="layer did not run"; ALL_FIXED=0
  elif [ "$miss" -gt 0 ]; then
    verdict="HARNESS-UPDATE"; detail="checks not found: $detail"; ALL_FIXED=0
  elif [ "$pol" = "contract" ]; then
    if [ "$fail" -eq 0 ]; then verdict="FIXED";
    elif [ "$pass" -eq 0 ]; then verdict="STILL-BROKEN"; ALL_FIXED=0;
    else verdict="PARTIAL"; ALL_FIXED=0; fi
  else # canary: fixed only when it flips to fail
    if [ "$pass" -eq 0 ]; then verdict="FIXED(flipped)";
    elif [ "$fail" -eq 0 ]; then verdict="STILL-PRESENT"; ALL_FIXED=0;
    else verdict="PARTIAL"; ALL_FIXED=0; fi
  fi
  printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "$verdict" "$detail"
done

LIVE="$(jq -r '.liveNote // empty' "$MANIFEST")"
[ -n "$LIVE" ] && printf '\nLive layer (manual): %s\n' "$LIVE"
PERSIST="$(dirname "$MANIFEST_ORIG")/results/reverify"
mkdir -p "$PERSIST" 2>/dev/null && cp -f "$RESULTS_DIR"/*.parsed "$RESULTS_DIR"/*.log "$RESULTS_DIR"/*.out "$PERSIST"/ 2>/dev/null || true

printf '\nRaw output: %s\n' "$PERSIST"
printf '\nReview delta this round: %s..%s\n' "${LAST_REVIEWED:-base}" "$FIXED_REF"
printf 'After reviewing that delta, advance the marker:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
       "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"
[ "$ALL_FIXED" -eq 1 ] && { echo "RESULT: all findings fixed."; exit 0; } || { echo "RESULT: not all findings fixed."; exit 1; }
