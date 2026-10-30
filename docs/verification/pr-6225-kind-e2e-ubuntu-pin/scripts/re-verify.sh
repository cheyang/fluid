#!/usr/bin/env bash
# re-verify.sh — re-run the PR #6225 harness against the current PR head.
#
# Same contract as the review-finding-verifier stock script, specialised for a
# CI/YAML-only change (no Go tests to graft): fetch the current PR head from the
# manifest's `pr` URL, run the static contract checks against that ref's
# .github/workflows/kind-e2e.yml, and report per-finding Fixed / Still-broken /
# Harness-update. Exit 0 iff every contract check passes.
#
# Usage: re-verify.sh [<ref>]   (default: current head of the manifest's PR)
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="$(dirname "$HERE")/verify-manifest.json"
[ -f "$MANIFEST" ] || { echo "re-verify: manifest not found ($MANIFEST)" >&2; exit 2; }

REF="${1:-}"
if [ -z "$REF" ]; then
  PR_URL="$(sed -n 's/.*"pr"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$MANIFEST" | head -1)"
  REPO="$(printf '%s' "$PR_URL" | sed 's|\(https://github.com/[^/]*/[^/]*\)/pull/.*|\1|')"
  NUM="$(printf '%s' "$PR_URL" | sed 's|.*/pull/||')"
  git fetch --quiet "$REPO" "pull/$NUM/head" || { echo "re-verify: fetch failed" >&2; exit 2; }
  REF="FETCH_HEAD"
fi
SHA="$(git rev-parse "$REF")"
echo "re-verify: checking .github/workflows/kind-e2e.yml at $SHA"

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
git show "$SHA:.github/workflows/kind-e2e.yml" > "$TMP/kind-e2e.yml" \
  || { echo "re-verify: workflow missing at $SHA" >&2; exit 2; }

out="$(bash "$HERE/check-pin.sh" "$TMP/kind-e2e.yml")"
printf '%s\n' "$out"

printf '\n================  RE-VERIFY  ================\n'
printf '%-42s %-9s %s\n' CHECK POLARITY VERDICT
ALL_OK=0
while IFS=$'\t' read -r name state; do
  [ "$state" = "pass" ] && verdict="FIXED/PRESENT" || { verdict="STILL-BROKEN/REGRESSED"; ALL_OK=1; }
  printf '%-42s %-9s %s\n' "$name" contract "$verdict"
done <<< "$out"

LAST="$(cat "$(dirname "$HERE")/.last-reviewed" 2>/dev/null || echo base)"
printf '\nReview delta this round: %s..%s\n' "$LAST" "$SHA"
printf 'After reviewing that delta, advance .last-reviewed to %s\n' "$SHA"
exit $ALL_OK
