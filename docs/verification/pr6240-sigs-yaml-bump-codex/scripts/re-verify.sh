#!/usr/bin/env bash
# re-verify.sh — re-run a verification harness against updated (fixed) code and
# report, per finding, whether the bug is Fixed / Still-broken / Partial, applying
# TEST POLARITY (contract tests should go green; bug-canaries should flip red).
#
# Part of the `review-finding-verifier` skill. Drive it from a checkout of the
# branch that CONTAINS the harness (e.g. verify/<topic>); it grafts the harness
# onto the fixed ref, runs the unit + integration layers, then restores your tree.
#
# Usage:
#   re-verify.sh <fixed-ref> [--manifest <path>] [--layers unit,integration]
#
#   <fixed-ref>   ref/branch/sha of the developer's updated code to test against.
#   --manifest    path to verify-manifest.json (default: auto-discover under
#                 docs/verification/*/verify-manifest.json, else next to this script).
#   --layers      comma list to run (default: unit,integration). Live layer (L3)
#                 needs a real environment and is only reminded about, not run.
#
# Requires: git, go, jq. Exit code 0 iff every finding is Fixed.
#
# Manifest shape (see verify-manifest.template.json):
# {
#   "harnessPaths": ["<paths to git-checkout from the harness ref>"],
#   "layers": {
#     "unit":        { "kind":"gotest",  "cmd":"go test ./pkg/... -run Foo -json" },
#     "integration": { "kind":"ginkgo",  "cmd":"KUBEBUILDER_ASSETS=$(...) go test ./test/... -ginkgo.json-report=$GINKGO_REPORT", "report":"$GINKGO_REPORT" }
#   },
#   "findings": [
#     { "id":"B1", "polarity":"contract", "layer":"unit", "match":["TestOverflow"] },
#     { "id":"B5", "polarity":"canary",   "layer":"unit", "match":["TestOffByOne"] }
#   ],
#   "liveNote": "L3: run scripts/10-*.sh; expect delay=base (not 2*base) at rc=1"
# }
set -euo pipefail

FIXED_REF=""; MANIFEST=""; LAYERS="unit,integration"
while [ $# -gt 0 ]; do
  case "$1" in
    --manifest) MANIFEST="$2"; shift 2;;
    --layers)   LAYERS="$2"; shift 2;;
    -h|--help)  sed -n '2,30p' "$0"; exit 0;;
    *) [ -z "$FIXED_REF" ] && FIXED_REF="$1" && shift || { echo "unexpected arg: $1" >&2; exit 2; };;
  esac
done
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }
# This script does `git checkout -f`, which discards uncommitted changes to TRACKED
# files. Refuse to run on a dirty tree so we never clobber unsaved work (untracked
# files like the manifest are fine).
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "re-verify: working tree has uncommitted tracked changes; commit or stash first" >&2
  echo "           (this script runs 'git checkout -f' and would discard them)" >&2
  exit 2
fi

# discover manifest
if [ -z "$MANIFEST" ]; then
  MANIFEST="$(ls docs/verification/*/verify-manifest.json 2>/dev/null | head -1 || true)"
  [ -n "$MANIFEST" ] || MANIFEST="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/verify-manifest.json"
fi
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found ($MANIFEST)" >&2; exit 2; }
echo "re-verify: manifest = $MANIFEST"

# The manifest and results live under the harness dir, which `git checkout -f`
# will wipe (the fixed ref has no harness). Copy them to a temp dir outside the
# repo so they survive the checkout; keep MANIFEST_ORIG for in-repo paths.
MANIFEST_ORIG="$MANIFEST"
RUNTIME_DIR="$(mktemp -d)"
cp "$MANIFEST" "$RUNTIME_DIR/manifest.json"
MANIFEST="$RUNTIME_DIR/manifest.json"

# Auto-discover the current PR head when no <fixed-ref> was given. Prefer a
# machine-independent source so this works on any machine/clone:
#   1) manifest.pr = "https://<host>/<owner>/<repo>/pull/<n>"  -> fetch the PR
#      head directly from the repo URL (no local remote name needed; works for
#      fork PRs and enterprise hosts).
#   2) manifest.prHeadFetch = { "remote": "<local-remote>", "ref": "pull/<n>/head" }
#      -> fallback that depends on a remote named on THIS machine.
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  if [ -n "$PR_URL" ]; then
    rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
    owner="$(printf '%s' "$path" | cut -d/ -f1)"
    repo="$(printf '%s' "$path" | cut -d/ -f2)"
    num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
    if [ -n "$owner" ] && [ -n "$repo" ] && [ -n "$num" ]; then
      cloneurl="https://$host/$owner/$repo.git"
      echo "re-verify: resolving PR head via 'git fetch $cloneurl pull/$num/head' (machine-independent)"
      git fetch --quiet "$cloneurl" "pull/$num/head" || { echo "re-verify: fetch of PR head failed" >&2; exit 2; }
      FIXED_REF="$(git rev-parse FETCH_HEAD)"
    else
      echo "re-verify: could not parse owner/repo/number from pr URL: $PR_URL" >&2; exit 2
    fi
  else
    PREMOTE="$(jq -r '.prHeadFetch.remote // empty' "$MANIFEST")"
    PREF="$(jq -r '.prHeadFetch.ref // empty' "$MANIFEST")"
    if [ -n "$PREMOTE" ] && [ -n "$PREF" ]; then
      echo "re-verify: resolving PR head via 'git fetch $PREMOTE $PREF' (local remote fallback)"
      git fetch --quiet "$PREMOTE" "$PREF" || { echo "re-verify: fetch of PR head failed" >&2; exit 2; }
      FIXED_REF="$(git rev-parse FETCH_HEAD)"
    else
      echo "usage: re-verify.sh <fixed-ref> [--manifest p] [--layers ...]" >&2
      echo "       (or set manifest.pr to the PR URL — machine-independent — to auto-discover the head)" >&2
      exit 2
    fi
  fi
  echo "re-verify: current head = $FIXED_REF"
fi

# Resolve the last-reviewed marker (for the reviewer's incremental diff); not used
# by the layers themselves. Priority: .last-reviewed file next to the manifest,
# else merge-base with the default branch. Printed for the caller/agent to use.
LAST_REVIEWED_FILE="$(dirname "$MANIFEST_ORIG")/.last-reviewed"
if [ -s "$LAST_REVIEWED_FILE" ]; then
  LAST_REVIEWED="$(tr -d '[:space:]' < "$LAST_REVIEWED_FILE")"
else
  LAST_REVIEWED="$(git merge-base origin/main "$FIXED_REF" 2>/dev/null || git merge-base main "$FIXED_REF" 2>/dev/null || echo '')"
fi
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (review delta = ${LAST_REVIEWED:-base}..$FIXED_REF)"

# Results in the temp dir (absolute, outside the repo) so `git checkout -f` never
# touches them and the ginkgo report path resolves regardless of test cwd.
RESULTS_DIR="$RUNTIME_DIR/results"
mkdir -p "$RESULTS_DIR"
export GINKGO_REPORT="$RESULTS_DIR/ginkgo-report.json"

# capture current position (must hold the harness) and restore on exit
ORIG_REF="$(git symbolic-ref --quiet --short HEAD || git rev-parse HEAD)"
HARNESS_SRC="$(git rev-parse HEAD)"
cleanup() { git checkout -f "$ORIG_REF" >/dev/null 2>&1 || true; rm -rf "$RUNTIME_DIR" 2>/dev/null || true; }
trap cleanup EXIT

echo "re-verify: fixed-ref=$FIXED_REF  harness-from=$ORIG_REF"
git fetch --quiet --all 2>/dev/null || true
git checkout -f "$FIXED_REF" >/dev/null 2>&1 || { echo "re-verify: cannot checkout $FIXED_REF" >&2; exit 2; }

# graft the harness onto the fixed code (paths from manifest)
HPATHS=()
while IFS= read -r p; do [ -n "$p" ] && HPATHS+=("$p"); done < <(jq -r '.harnessPaths[]' "$MANIFEST")
git checkout "$HARNESS_SRC" -- "${HPATHS[@]}" 2>/dev/null || { echo "re-verify: failed to graft harness paths" >&2; exit 2; }

# run one layer, emit "name<TAB>pass|fail" lines to stdout
run_layer() {
  local layer="$1" kind cmd report out
  kind="$(jq -r --arg l "$layer" '.layers[$l].kind // empty' "$MANIFEST")"
  cmd="$(jq -r --arg l "$layer" '.layers[$l].cmd // empty' "$MANIFEST")"
  [ -n "$cmd" ] || return 0
  out="$RESULTS_DIR/${layer}.out"
  echo "re-verify: running layer '$layer' ($kind)" >&2
  if [ "$kind" = "gotest" ]; then
    bash -c "$cmd" >"$out" 2>&1 || true
    # compile failure: package builds are reported as build-fail / [build failed]
    if grep -q '"Action":"build-fail"' "$out" || grep -q 'build failed' "$out"; then
      echo "__BUILD_OR_NO_TESTS__"; return 0
    fi
    if ! grep -q '"Action":"\(pass\|fail\)"' "$out"; then
      echo "__BUILD_OR_NO_TESTS__"; return 0
    fi
    jq -rs '[.[]|select(.Test!=null and (.Action=="pass" or .Action=="fail"))]
            | .[] | "\(.Test)\t\(.Action)"' "$out" 2>/dev/null || echo "__PARSE_ERROR__"
  elif [ "$kind" = "ginkgo" ]; then
    report="$(jq -r --arg l "$layer" '.layers[$l].report // empty' "$MANIFEST")"
    report="$(eval echo "$report")"; [ -n "$report" ] || report="$GINKGO_REPORT"
    bash -c "$cmd" >"$out" 2>&1 || true
    if [ ! -s "$report" ]; then echo "__BUILD_OR_NO_TESTS__"; return 0; fi
    jq -r '.[].SpecReports[]? | select((.LeafNodeText//"")!="" )
           | "\(.LeafNodeText)\t\(.State)"' "$report" 2>/dev/null \
      | sed 's/\tpassed/\tpass/; s/\tfailed/\tfail/' || echo "__PARSE_ERROR__"
  fi
}

# parse each layer once into a per-layer file (bash 3.2: no associative arrays)
for L in ${LAYERS//,/ }; do
  run_layer "$L" > "$RESULTS_DIR/$L.parsed"
done

# look up a match string in a layer's result lines -> pass|fail|missing|build
lookup() { # $1=layer $2=match-substring
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
    verdict="HARNESS-UPDATE"; detail="did not compile/run against fixed code"; ALL_FIXED=0
  elif [ "$miss" -gt 0 ]; then
    verdict="HARNESS-UPDATE"; detail="tests not found: $detail"; ALL_FIXED=0
  elif [ "$pol" = "contract" ]; then
    if [ "$fail" -eq 0 ]; then verdict="FIXED";
    elif [ "$pass" -eq 0 ]; then verdict="STILL-BROKEN"; ALL_FIXED=0;
    else verdict="PARTIAL"; ALL_FIXED=0; fi
  else # canary: correct fix flips it to fail
    if [ "$pass" -eq 0 ]; then verdict="FIXED(flipped)";
    elif [ "$fail" -eq 0 ]; then verdict="STILL-PRESENT"; ALL_FIXED=0;
    else verdict="PARTIAL"; ALL_FIXED=0; fi
  fi
  printf '%-6s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "$verdict" "$detail"
done

LIVE="$(jq -r '.liveNote // empty' "$MANIFEST")"
[ -n "$LIVE" ] && printf '\nLive layer (manual): %s\n' "$LIVE"
# persist raw output into the (restored-after-cleanup) in-repo results dir for inspection
PERSIST="$(dirname "$MANIFEST_ORIG")/results/reverify"
mkdir -p "$PERSIST" 2>/dev/null && cp -f "$RESULTS_DIR"/*.out "$RESULTS_DIR"/*.parsed "$RESULTS_DIR"/*.json "$PERSIST"/ 2>/dev/null || true

printf '\nReminder: a "canary" finding is FIXED only when it FLIPS to fail; then invert its\n'
printf 'assertion (or promote the new behavior to a contract test). Raw output: %s\n' "$PERSIST"
printf '\nReview delta this round: %s..%s\n' "${LAST_REVIEWED:-base}" "$FIXED_REF"
printf 'After reviewing that delta, advance the marker for the next round:\n'
printf '  echo %s > %s && git add %s && git commit -m "review: advance last-reviewed"\n' \
       "$FIXED_REF" "$LAST_REVIEWED_FILE" "$LAST_REVIEWED_FILE"
[ "$ALL_FIXED" -eq 1 ] && { echo "RESULT: all findings fixed."; exit 0; } || { echo "RESULT: not all findings fixed."; exit 1; }
