#!/usr/bin/env bash
# re-verify.sh — re-run the PR #6253 doc-claims harness against the current PR head
# (or an explicit fixed-ref) and report per finding: Fixed / Still-broken / Partial /
# Not-run, applying TEST POLARITY (contract checks should PASS; canaries flip).
#
# Adapted from the review-finding-verifier stock re-verify.sh for "script"-kind
# layers (this PR is docs-only; the layers are python checkers, not go/ginkgo).
#
# Usage: re-verify.sh [<fixed-ref>]
#   no ref -> the current PR head is fetched via manifest.pr (machine-independent).
# Requires: git, jq, python3. The integration layer needs network (Docker Hub,
# raw.githubusercontent.com); pass --layers unit to skip it.
set -euo pipefail

FIXED_REF=""; LAYERS="unit,integration"
while [ $# -gt 0 ]; do
  case "$1" in
    --layers) LAYERS="$2"; shift 2;;
    -h|--help) sed -n '2,16p' "$0"; exit 0;;
    *) [ -z "$FIXED_REF" ] && FIXED_REF="$1" && shift || { echo "unexpected arg: $1" >&2; exit 2; };;
  esac
done
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
command -v python3 >/dev/null || { echo "re-verify: python3 is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  exit 2
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="$(cd "$HERE/.." && pwd)/verify-manifest.json"
[ -f "$MANIFEST" ] || MANIFEST="$(ls docs/verification/*/verify-manifest.json 2>/dev/null | head -1 || true)"
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found" >&2; exit 2; }
echo "re-verify: manifest = $MANIFEST"

RUNTIME_DIR="$(mktemp -d)"
cp "$MANIFEST" "$RUNTIME_DIR/manifest.json"
RESULTS_DIR="$RUNTIME_DIR/results"; mkdir -p "$RESULTS_DIR"
trap 'rm -rf "$RUNTIME_DIR" 2>/dev/null || true' EXIT

# resolve PR head when no fixed-ref given (machine-independent: fetch from the PR URL)
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$RUNTIME_DIR/manifest.json")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  [ -n "$owner" ] && [ -n "$repo" ] && [ -n "$num" ] || { echo "re-verify: bad pr URL $PR_URL" >&2; exit 2; }
  echo "re-verify: resolving PR head via git fetch https://$host/$owner/$repo.git pull/$num/head"
  git fetch --quiet "https://$host/$owner/$repo.git" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
  echo "re-verify: current head = $FIXED_REF"
fi

LAST_REVIEWED_FILE="$(dirname "$MANIFEST")/.last-reviewed"
if [ -s "$LAST_REVIEWED_FILE" ]; then
  LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE")"
else
  LAST_REVIEWED="$(git merge-base origin/master "$FIXED_REF" 2>/dev/null || git merge-base master "$FIXED_REF" 2>/dev/null || echo '')"
fi
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (review delta = ${LAST_REVIEWED:-base}..$FIXED_REF)"

ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; }
trap 'cleanup; rm -rf "$RUNTIME_DIR" 2>/dev/null || true' EXIT

echo "re-verify: fixed-ref=$FIXED_REF  harness-from=$ORIG_REF"
git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }

# graft the harness onto the fixed code
HPATHS=()
while IFS= read -r p; do [ -n "$p" ] && HPATHS+=("$p"); done < <(jq -r '.harnessPaths[]' "$RUNTIME_DIR/manifest.json")
git checkout "$HARNESS_SRC" -- "${HPATHS[@]}" 2>/dev/null || { echo "re-verify: failed to graft harness paths" >&2; exit 2; }

# run one script layer; emit "name<TAB>pass|fail" lines
run_layer() {
  local layer="$1" cmd out
  cmd="$(jq -r --arg l "$layer" '.layers[$l].cmd // empty' "$RUNTIME_DIR/manifest.json")"
  [ -n "$cmd" ] || return 0
  out="$RESULTS_DIR/${layer}.out"
  echo "re-verify: running layer '$layer'" >&2
  if bash -c "$cmd" >"$out" 2>"$RESULTS_DIR/${layer}.err"; then :; fi
  grep -E '^[A-Za-z0-9_]+\t(pass|fail)\t' "$out" || echo "__NO_CHECKS__"
}

for L in ${LAYERS//,/ }; do
  run_layer "$L" > "$RESULTS_DIR/$L.parsed"
done

lookup() {
  local f="$RESULTS_DIR/$1.parsed"
  [ -f "$f" ] || { echo "missing"; return; }
  grep -q '__NO_CHECKS__' "$f" && { echo "missing"; return; }
  local hit; hit="$(awk -F'\t' -v m="$2" '$1==m{print $2}' "$f" | tail -1)"
  [ -n "$hit" ] && echo "$hit" || echo "missing"
}

printf '\n================  RE-VERIFY  ================\n'
printf '%-6s %-9s %-12s %-14s %s\n' ID POLARITY LAYER VERDICT DETAIL
ALL_FIXED=1
N="$(jq '.findings|length' "$RUNTIME_DIR/manifest.json")"
for i in $(seq 0 $((N-1))); do
  id="$(jq -r ".findings[$i].id" "$RUNTIME_DIR/manifest.json")"
  pol="$(jq -r ".findings[$i].polarity" "$RUNTIME_DIR/manifest.json")"
  layer="$(jq -r ".findings[$i].layer" "$RUNTIME_DIR/manifest.json")"
  case ",$LAYERS," in *",$layer,"*) : ;; *)
    printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "SKIPPED" "layer not run"; continue;; esac
  pass=0; fail=0; miss=0
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    r="$(lookup "$layer" "$m")"
    case "$r" in pass) pass=$((pass+1));; fail) fail=$((fail+1));; *) miss=$((miss+1));; esac
  done < <(jq -r ".findings[$i].match[]" "$RUNTIME_DIR/manifest.json")
  if [ "$miss" -gt 0 ]; then
    printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "NOT-RUN" "checks missing (pass=$pass fail=$fail miss=$miss)"
    ALL_FIXED=0
  elif [ "$pol" = "contract" ]; then
    if [ "$fail" -eq 0 ]; then printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "FIXED" "pass=$pass fail=$fail";
    elif [ "$pass" -eq 0 ]; then printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "STILL-BROKEN" "pass=$pass fail=$fail"; ALL_FIXED=0;
    else printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "PARTIAL" "pass=$pass fail=$fail"; ALL_FIXED=0; fi
  else
    if [ "$pass" -eq 0 ]; then printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "FIXED(flipped)" "pass=$pass fail=$fail";
    elif [ "$fail" -eq 0 ]; then printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "STILL-PRESENT" "pass=$pass fail=$fail"; ALL_FIXED=0;
    else printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "PARTIAL" "pass=$pass fail=$fail"; ALL_FIXED=0; fi
  fi
done

PERSIST="$(cd "$(dirname "$MANIFEST")" && pwd)/results/reverify"
mkdir -p "$PERSIST" 2>/dev/null && cp -f "$RESULTS_DIR"/*.out "$RESULTS_DIR"/*.err "$RESULTS_DIR"/*.parsed "$PERSIST"/ 2>/dev/null || true

printf '\nRaw output: %s\n' "$PERSIST"
printf 'Review delta this round: %s..%s\n' "${LAST_REVIEWED:-base}" "$FIXED_REF"
printf 'After reviewing that delta, advance the marker:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
       "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"
[ "$ALL_FIXED" -eq 1 ] && { echo "RESULT: all findings fixed."; exit 0; } || { echo "RESULT: not all findings fixed."; exit 1; }
