#!/usr/bin/env bash
# re-verify.sh for the scorecard-action bump harness (fluid PR #6207).
# Re-runs check-action-bump.sh against the CURRENT PR head (or a given ref).
# All harness checks are CONTRACT polarity: they must PASS on a good bump.
# Exit 0 iff every check passes. Prints the incremental review delta
# (.last-reviewed..head) for the next review round.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
FIXED_REF="${1:-}"

if [ -z "$FIXED_REF" ]; then
  PR_URL=$(sed -n 's/.*"pr":[[:space:]]*"\([^"]*\)".*/\1/p' "$MANIFEST" | head -1)
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"; repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  echo "re-verify: fetching PR head from https://$host/$owner/$repo.git pull/$num/head"
  git fetch --quiet "https://$host/$owner/$repo.git" "pull/$num/head" || exit 2
  FIXED_REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: target ref = $FIXED_REF"

LAST=""
[ -f "$TOPIC_DIR/.last-reviewed" ] && LAST=$(cat "$TOPIC_DIR/.last-reviewed")
if [ -n "$LAST" ] && [ "$LAST" != "$FIXED_REF" ]; then
  echo "re-verify: incremental review delta:"
  echo "  git log --oneline $LAST..$FIXED_REF"
  echo "  git diff $LAST...$FIXED_REF"
fi

bash "$HERE/check-action-bump.sh" "$FIXED_REF"
rc=$?
echo "re-verify: check-action-bump.sh exit=$rc (0 = all contract checks pass)"
exit "$rc"
