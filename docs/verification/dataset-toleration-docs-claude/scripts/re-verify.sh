#!/usr/bin/env bash
# re-verify.sh — re-run the docs verification harness for PR
# fluid-cloudnative/fluid#6300 against the current PR head (or a given ref),
# and report per finding Fixed / Still-broken with test polarity applied.
#
# Adapted from the review-finding-verifier skill template for a docs-only
# harness: the checks read the tree via `git show <ref>:<path>`, so no
# checkout/grafting is needed and the working tree is never touched.
#
# Usage:
#   scripts/re-verify.sh [<fixed-ref>]
#     (no ref -> fetch the current PR head via manifest.pr, machine-independent)
#
# Requires: git, python3 (PyYAML for the integration layer).
# Exit 0 iff every finding's contract checks pass at the tested ref.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
REPO="$(git rev-parse --show-toplevel 2>/dev/null || echo .)"

[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found: $MANIFEST" >&2; exit 2; }

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
  PR_URL="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path_="${rest#*/}"
  owner="$(printf '%s' "$path_" | cut -d/ -f1)"
  repo="$(printf '%s' "$path_" | cut -d/ -f2)"
  num="$(printf '%s' "$path_" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  cloneurl="https://$host/$owner/$repo.git"
  echo "re-verify: resolving PR head via git fetch $cloneurl pull/$num/head"
  git fetch --quiet "$cloneurl" "pull/$num/head"
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: testing ref = $FIXED_REF"

# review-delta marker (informational)
LAST_REVIEWED=""
[ -s "$TOPIC_DIR/.last-reviewed" ] && LAST_REVIEWED="$(tr -d '[:space:]' < "$TOPIC_DIR/.last-reviewed")"
echo "re-verify: last-reviewed = ${LAST_REVIEWED:-<none>}  (delta = ${LAST_REVIEWED:-base}..$FIXED_REF)"

RESULTS_DIR="$TOPIC_DIR/results"
mkdir -p "$RESULTS_DIR"

# run both layers against the tested ref
UNIT_OUT="$RESULTS_DIR/reverify-unit.txt"
INT_OUT="$RESULTS_DIR/reverify-integration.txt"
python3 "$HERE/check_docs.py" "$FIXED_REF" --repo "$REPO" --layer unit >"$UNIT_OUT" 2>&1 || true
python3 "$HERE/check_docs.py" "$FIXED_REF" --repo "$REPO" --layer integration >"$INT_OUT" 2>&1 || true

# per-finding verdict: contract polarity -> FIXED iff all matched checks pass
printf '\n================  RE-VERIFY (docs harness)  ================\n'
printf '%-26s %-9s %-12s %-14s %s\n' ID POLARITY LAYER VERDICT CHECKS
ALL_FIXED=1
while IFS=$'\t' read -r id pol layer checks; do
  [ -n "$id" ] || continue
  pass=0; fail=0; missing=0
  for c in $checks; do
    case "$layer" in
      integration) f="$INT_OUT";;
      *) f="$UNIT_OUT";;
    esac
    line="$(grep -m1 "^CHECK	$c	" "$f" || true)"
    if [ -z "$line" ]; then missing=$((missing+1));
    elif echo "$line" | grep -q "	pass	"; then pass=$((pass+1));
    else fail=$((fail+1)); fi
  done
  if [ "$missing" -gt 0 ]; then
    verdict="HARNESS-UPDATE"; ALL_FIXED=0; detail="checks not found: pass=$pass fail=$fail miss=$missing"
  elif [ "$pol" = "contract" ]; then
    if [ "$fail" -eq 0 ]; then verdict="FIXED"; detail="pass=$pass fail=$fail";
    elif [ "$pass" -eq 0 ]; then verdict="STILL-BROKEN"; ALL_FIXED=0; detail="pass=$pass fail=$fail";
    else verdict="PARTIAL"; ALL_FIXED=0; detail="pass=$pass fail=$fail"; fi
  else
    if [ "$pass" -eq 0 ]; then verdict="FIXED(flipped)"; detail="pass=$pass fail=$fail";
    elif [ "$fail" -eq 0 ]; then verdict="STILL-PRESENT"; ALL_FIXED=0; detail="pass=$pass fail=$fail";
    else verdict="PARTIAL"; ALL_FIXED=0; detail="pass=$pass fail=$fail"; fi
  fi
  printf '%-26s %-9s %-12s %-14s %s\n' "$id" "$pol" "$layer" "$verdict" "$detail"
done < <(python3 - "$MANIFEST" <<'EOF'
import json, sys
m = json.load(open(sys.argv[1]))
for f in m.get("findings", []):
    print("%s\t%s\t%s\t%s" % (f["id"], f.get("polarity", "contract"), f.get("layer", "unit"), " ".join(f.get("match", []))))
EOF
)

printf '\nRaw output: %s/{reverify-unit,reverify-integration}.txt\n' "$RESULTS_DIR"
printf 'Review delta this round: %s..%s\n' "${LAST_REVIEWED:-base}" "$FIXED_REF"
if [ "$ALL_FIXED" -eq 1 ]; then echo "RESULT: all findings fixed."; exit 0; else echo "RESULT: not all findings fixed."; exit 1; fi
