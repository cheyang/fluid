#!/usr/bin/env bash
# re-verify.sh — re-run the certifi/mooncake verification harness against the
# current (or given) PR head. This PR changes one hash-locked requirements file,
# so instead of grafting Go test packages we extract the file at the target ref
# with `git show` and run the two contract checks plus the premise check on it.
#
# Usage: re-verify.sh [<ref>]   (default: current head of manifest.pr)
# Exit 0 iff every contract check passes on the target ref.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="$HERE/../verify-manifest.json"
command -v jq >/dev/null || { echo "jq required" >&2; exit 2; }

REF="${1:-}"
if [ -z "$REF" ]; then
  PR_URL="$(jq -r '.pr' "$MANIFEST")"
  rest="${PR_URL#*://}"; host="${rest%%/*}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"; repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  git fetch --quiet "https://$host/$owner/$repo.git" "pull/$num/head"
  REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: target ref = $REF"
LAST="$(cat "$HERE/../.last-reviewed" 2>/dev/null || true)"
echo "re-verify: last-reviewed = ${LAST:-<none>} (review delta: ${LAST:-base}..$REF)"

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
REQS_PATH="test/gha-e2e/mooncake/image/requirements.txt"
git show "$REF:$REQS_PATH" > "$WORK/requirements.txt"

rc=0
echo "== P0 premise (emSign ECC Root CA - G3 must be ABSENT on the target pin) =="
python3 "$HERE/check_bundle_delta.py" "$WORK/requirements.txt" || { echo "P0: FAIL"; rc=1; }
echo "== C1 pin/hash/GHSA-range/dependency-constraint check =="
python3 "$HERE/check_certifi_pin.py" "$WORK/requirements.txt" || { echo "C1: FAIL"; rc=1; }
echo "== C2 pip --require-hashes acceptance + negative controls =="
bash "$HERE/pip_hash_gate.sh" "$WORK/requirements.txt" || { echo "C2: FAIL"; rc=1; }

if [ $rc -eq 0 ]; then echo "RE-VERIFY: all checks passed on $REF"; else echo "RE-VERIFY: FAILURES on $REF"; fi
exit $rc
