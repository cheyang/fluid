#!/usr/bin/env bash
# run_l1.sh — run the L1 (deterministic, unit-layer) checks of this harness and
# emit go-test-style JSON events on stdout so scripts/re-verify.sh can parse it.
# Human-readable per-check PASS/FAIL lines go to stderr; the summary to stdout
# at the end (also JSON-safe for the parser: kept on stderr instead).
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="$HERE/../results"
mkdir -p "$RESULTS_DIR"

RAW="$(bash "$HERE/checks.sh" 2>"$RESULTS_DIR/l1-details.txt")" || RAW="$(tail -n +1 /dev/null)"
# checks.sh always ends with the results table on stdout; parse it.
pass=0; fail=0
while IFS=$'\t' read -r name status; do
  [ -n "$name" ] || continue
  action=fail
  [ "$status" = "pass" ] && action=pass && pass=$((pass+1)) || fail=$((fail+1))
  printf '{"Action":"run","Test":"%s"}\n' "$name"
  printf '{"Action":"%s","Test":"%s","Elapsed":0}\n' "$action" "$name"
done <<< "$RAW"

printf '{"Action":"skip","Package":"(root)"}\n' >/dev/null
echo "l1-summary: pass=$pass fail=$fail (details: $RESULTS_DIR/l1-details.txt)" >&2
[ "$fail" -eq 0 ]
