#!/usr/bin/env bash
# re-verify.sh — re-run the PR #6002 harness against an updated ref and report,
# per check, Fixed / Still-broken / Harness-update, applying TEST POLARITY.
#
# Same contract as the review-finding-verifier stock script, with one extension:
# layer kind "shell" — the layer command prints "name<TAB>pass|fail" lines.
#
# Drive from a checkout of the verify branch (the branch that CONTAINS the
# harness). With no <ref> argument it resolves the current PR head from
# manifest.pr (machine-independent). It grafts the harness paths onto the
# checked-out target, runs layers unit + integration, then restores your tree.
#
# Usage: re-verify.sh [ref] [--manifest path] [--layers unit,integration]
set -euo pipefail

FIXED_REF=""; MANIFEST=""; LAYERS="unit,integration"
while [ $# -gt 0 ]; do
  case "$1" in
    --manifest) MANIFEST="$2"; shift 2;;
    --layers)   LAYERS="$2"; shift 2;;
    -h|--help)  sed -n '2,14p' "$0"; exit 0;;
    *) [ -z "$FIXED_REF" ] && FIXED_REF="$1" && shift || { echo "unexpected arg: $1" >&2; exit 2; };;
  esac
done
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2; exit 2
fi

if [ -z "$MANIFEST" ]; then
  MANIFEST="$(ls docs/verification/*/verify-manifest.json 2>/dev/null | head -1 || true)"
fi
[ -n "$MANIFEST" ] && [ -f "$MANIFEST" ] || { echo "re-verify: manifest not found" >&2; exit 2; }
MANIFEST_DIR="$(dirname "$MANIFEST")"
echo "re-verify: manifest = $MANIFEST"

# resolve target ref
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  [ -n "$PR_URL" ] || { echo "re-verify: no ref given and manifest.pr missing" >&2; exit 2; }
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"; repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  echo "re-verify: resolving PR head via 'git fetch https://$host/$owner/$repo.git pull/$num/head'"
  git fetch --quiet "https://$host/$owner/$repo.git" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: target = $FIXED_REF"

LAST_REVIEWED_FILE="$MANIFEST_DIR/.last-reviewed"
if [ -s "$LAST_REVIEWED_FILE" ]; then LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE")"; else LAST_REVIEWED=""; fi
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (incremental review delta: ${LAST_REVIEWED:-base}..$FIXED_REF)"
if [ -n "$LAST_REVIEWED" ]; then
  git log --oneline "$LAST_REVIEWED..$FIXED_REF" 2>/dev/null | sed 's/^/re-verify: delta: /' || true
fi

RUNTIME_DIR="$(mktemp -d)"; RESULTS_DIR="$RUNTIME_DIR/results"; mkdir -p "$RESULTS_DIR"
ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; rm -rf "$RUNTIME_DIR" 2>/dev/null || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }
HPATHS=()
while IFS= read -r p; do [ -n "$p" ] && HPATHS+=("$p"); done < <(jq -r '.harnessPaths[]' "$MANIFEST")
git checkout "$HARNESS_SRC" -- "${HPATHS[@]}" 2>/dev/null || { echo "re-verify: failed to graft harness" >&2; exit 2; }

run_layer() {
  local layer="$1" kind cmd out
  kind="$(jq -r --arg l "$layer" '.layers[$l].kind // empty' "$MANIFEST")"
  cmd="$(jq -r --arg l "$layer" '.layers[$l].cmd // empty' "$MANIFEST")"
  [ -n "$cmd" ] || return 0
  out="$RESULTS_DIR/$layer.out"
  echo "re-verify: running layer '$layer' ($kind)" >&2
  if [ "$kind" = "shell" ]; then
    bash -c "$cmd" >"$out" 2>"$RESULTS_DIR/$layer.stderr" || true
    grep -E $'\t(pass|fail)$' "$out" || echo "__BUILD_OR_NO_TESTS__"
  else
    echo "re-verify: unsupported layer kind '$kind' in this harness copy" >&2
    echo "__BUILD_OR_NO_TESTS__"
  fi
}

for L in ${LAYERS//,/ }; do run_layer "$L" > "$RESULTS_DIR/$L.parsed"; done

lookup() {
  local f="$RESULTS_DIR/$1.parsed"
  [ -f "$f" ] || { echo "missing"; return; }
  grep -q '__BUILD_OR_NO_TESTS__' "$f" && { echo "build"; return; }
  local hit; hit="$(awk -F'\t' -v m="$2" 'index($1,m){print $2}' "$f" | tail -1)"
  [ -n "$hit" ] && echo "$hit" || echo "missing"
}

printf '\n================  RE-VERIFY  ================\n'
printf '%-28s %-9s %-12s %-14s %s\n' ID POLARITY LAYER VERDICT DETAIL
ALL_FIXED=1
N="$(jq '.findings|length' "$MANIFEST")"
for i in $(seq 0 $((N-1))); do
  id="$(jq -r ".findings[$i].id" "$MANIFEST")"
  pol="$(jq -r ".findings[$i].polarity" "$MANIFEST")"
  layer="$(jq -r ".findings[$i].layer" "$MANIFEST")"
  case ",$LAYERS," in *",$layer,"*) : ;; *)
    printf '%-28s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "SKIPPED" "layer not in --layers"; ALL_FIXED=0; continue;; esac
  pass=0; fail=0; miss=0; build=0
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    r="$(lookup "$layer" "$m")"
    case "$r" in pass) pass=$((pass+1));; fail) fail=$((fail+1));; build) build=$((build+1));; *) miss=$((miss+1));; esac
  done < <(jq -r ".findings[$i].match[]" "$MANIFEST")
  if [ "$build" -gt 0 ]; then verdict="Harness-update"; detail="layer did not produce results (env/build)"; ALL_FIXED=0
  elif [ "$miss" -gt 0 ]; then verdict="Harness-update"; detail="$miss match(es) missing"; ALL_FIXED=0
  elif [ "$pol" = "canary" ]; then
    if [ "$fail" -gt 0 ]; then verdict="Fixed"; detail="canary flipped to fail"
    else verdict="Still-broken"; detail="canary still passing"; ALL_FIXED=0; fi
  else
    if [ "$fail" -eq 0 ] && [ "$pass" -gt 0 ]; then verdict="Fixed"; detail="contract green"
    else verdict="Still-broken"; detail="contract failing ($fail)"; ALL_FIXED=0; fi
  fi
  printf '%-28s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "$verdict" "$detail"
done
echo "re-verify: results in $RESULTS_DIR (wiped after run; re-run to regenerate)"
[ "$ALL_FIXED" -eq 1 ]
