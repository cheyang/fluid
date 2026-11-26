#!/usr/bin/env bash
# re-verify.sh — re-run the PR #6290 verification harness against the current PR
# head (or a given ref) and report per-claim status. Works from any clone of the
# fluid repo; resolves the PR head machine-independently via the manifest's `pr`.
#
# Usage:  re-verify.sh [ref]     (default: current PR head, fetched from the PR URL)
# Exit:   0 iff all gating contract checks pass on the checked ref.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="${MANIFEST:-$HERE/../verify-manifest.json}"
REF="${1:-}"

if [ -z "$REF" ]; then
  PR_URL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["pr"])' "$MANIFEST")"
  rest="${PR_URL#*://}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  git fetch --quiet "https://${rest%%/*}/$owner/$repo.git" "pull/$num/head"
  REF="$(git rev-parse FETCH_HEAD)"
  echo "re-verify: resolved current PR head = $REF"
fi

LAST="$HERE/../.last-reviewed"
[ -s "$LAST" ] && echo "re-verify: last-reviewed = $(tr -d '[:space:]' < "$LAST")  (incremental review delta = $(tr -d '[:space:]' < "$LAST")..$REF)"

echo "re-verify: running harness against $REF"
bash "$HERE/verify-bump.sh" "$REF"
