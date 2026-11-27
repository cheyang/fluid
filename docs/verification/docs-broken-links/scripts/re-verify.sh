#!/usr/bin/env bash
# re-verify.sh — PR #6294 docs-broken-links harness, adapted from the
# review-finding-verifier skill's stock re-verify.sh.
#
# Differences from stock: the layer kind is "script" (this is a docs PR; there
# are no Go tests). The cmd must print "name<TAB>pass|fail" lines. The graft
# target runs in a TEMPORARY git worktree so the caller's checkout is never
# clobbered. Polarity handling is identical to stock: contract tests should go
# green when fixed; canaries should flip red.
#
# Usage: re-verify.sh [fixed-ref] [--manifest <path>]
#   No fixed-ref: fetch the current PR head via manifest.pr (works on any clone).
#   Exit 0 iff every finding is Fixed.
set -euo pipefail

FIXED_REF=""; MANIFEST=""
while [ $# -gt 0 ]; do
  case "$1" in
    --manifest) MANIFEST="$2"; shift 2;;
    *) [ -z "$FIXED_REF" ] && FIXED_REF="$1" && shift || { echo "unexpected arg: $1" >&2; exit 2; };;
  esac
done
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
command -v python3 >/dev/null || { echo "re-verify: python3 is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }

HARNESS_SRC="$(git rev-parse HEAD)"
if [ -z "$MANIFEST" ]; then
  MANIFEST="$(ls docs/verification/*/verify-manifest.json 2>/dev/null | head -1 || true)"
fi
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found" >&2; exit 2; }
echo "re-verify: manifest = $MANIFEST (harness src = $HARNESS_SRC)"

if [ -z "$FIXED_REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  [ -n "$PR_URL" ] || { echo "re-verify: no fixed-ref and no manifest.pr" >&2; exit 2; }
  REPO_URL="${PR_URL%/pull/*}"
  PR_NUM="${PR_URL##*/pull/}"
  git fetch --quiet "$REPO_URL" "pull/$PR_NUM/head"
  FIXED_REF="FETCH_HEAD"
  echo "re-verify: resolved PR head from $PR_URL -> $(git rev-parse --short "$FIXED_REF")"
fi

# incremental-review delta hint
TOPIC_DIR="$(dirname "$MANIFEST")"
if [ -f "$TOPIC_DIR/.last-reviewed" ]; then
  LAST="$(cat "$TOPIC_DIR/.last-reviewed")"
  echo "re-verify: last-reviewed = $LAST"
  echo "re-verify: incremental delta to review: git log --oneline $LAST..$FIXED_REF"
  echo "re-verify: advance marker after the round: git rev-parse $FIXED_REF > $TOPIC_DIR/.last-reviewed"
fi

WT="$(mktemp -d /tmp/pr6294-reverify.XXXXXX)"
cleanup() { git worktree remove --force "$WT" >/dev/null 2>&1 || rm -rf "$WT"; }
trap cleanup EXIT
git worktree add --quiet --force "$WT" "$FIXED_REF"
mapfile -t HPATHS < <(jq -r '.harnessPaths[]' "$MANIFEST")
( cd "$WT" && git checkout --quiet "$HARNESS_SRC" -- "${HPATHS[@]}" ) || { echo "re-verify: failed to graft harness paths" >&2; exit 2; }

RESULTS_DIR="$TOPIC_DIR/results/reverify-$(git rev-parse --short "$FIXED_REF")"
mkdir -p "$RESULTS_DIR"

N="$(jq '.findings|length' "$MANIFEST")"
printf '\n================  RE-VERIFY  ================\n'
printf '%-24s %-9s %-14s %s\n' ID POLARITY VERDICT DETAIL
ALL_FIXED=1
for i in $(seq 0 $((N-1))); do
  id="$(jq -r ".findings[$i].id" "$MANIFEST")"
  pol="$(jq -r ".findings[$i].polarity" "$MANIFEST")"
  layer="$(jq -r ".findings[$i].layer" "$MANIFEST")"
  kind="$(jq -r --arg l "$layer" '.layers[$l].kind // empty' "$MANIFEST")"
  cmd="$(jq -r --arg l "$layer" '.layers[$l].cmd // empty' "$MANIFEST")"
  [ "$kind" = "script" ] || { echo "$id: unknown layer kind $kind" >&2; exit 2; }
  out="$RESULTS_DIR/$id.out"
  ( cd "$WT" && bash -c "$cmd" ) >"$out" 2>&1 || true
  pass=0; fail=0; miss=0
  while IFS= read -r m; do
    [ -n "$m" ] || continue
    r="$(awk -F'\t' -v m="$m" 'index($1,m){print $2}' "$out" | tail -1)"
    case "$r" in pass) pass=$((pass+1));; fail) fail=$((fail+1));; *) miss=$((miss+1));; esac
  done < <(jq -r ".findings[$i].match[]" "$MANIFEST")
  detail="pass=$pass fail=$fail miss=$miss"
  if [ "$miss" -gt 0 ]; then verdict="HARNESS-UPDATE"; ALL_FIXED=0
  elif [ "$pol" = "contract" ]; then
    if [ "$fail" -eq 0 ]; then verdict="FIXED"; elif [ "$pass" -eq 0 ]; then verdict="STILL-BROKEN"; ALL_FIXED=0; else verdict="PARTIAL"; ALL_FIXED=0; fi
  else
    if [ "$pass" -eq 0 ]; then verdict="FIXED(flipped)"; elif [ "$fail" -eq 0 ]; then verdict="STILL-PRESENT"; ALL_FIXED=0; else verdict="PARTIAL"; ALL_FIXED=0; fi
  fi
  printf '%-24s %-9s %-14s %s\n' "$id" "$pol" "$verdict" "$detail"
done
[ "$ALL_FIXED" -eq 1 ]
