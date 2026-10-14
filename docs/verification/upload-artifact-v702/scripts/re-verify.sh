#!/usr/bin/env bash
# re-verify.sh — re-run the upload-artifact-v702 harness against updated code.
#
# Usage (from a checkout of the branch that contains this harness):
#   bash docs/verification/upload-artifact-v702/scripts/re-verify.sh [<ref>]
#
# With no <ref> it resolves the current PR head from manifest .pr
# (git fetch <repo-url> pull/<n>/head — machine-independent). It then runs the
# contract script against that ref and prints per-finding Fixed / Still-broken.
# Exit 0 iff all contract checks pass.
#
# This variant of re-verify.sh reads workflow files via `git show <ref>:<path>`,
# so it does NOT need to graft/checkout anything onto the ref — no tree mutation,
# safe on a dirty-ish checkout (tracked modifications are never touched).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="$HERE/verify-manifest.json"
command -v jq >/dev/null || { echo "re-verify: jq is required" >&2; exit 2; }
git rev-parse --git-dir >/dev/null 2>&1 || { echo "re-verify: not a git repo" >&2; exit 2; }

REF="${1:-}"
if [ -z "$REF" ]; then
  PR_URL="$(jq -r '.pr // empty' "$MANIFEST")"
  rest="${PR_URL#*://}"; path="${rest#*/}"
  owner="$(printf '%s' "$path" | cut -d/ -f1)"
  repo="$(printf '%s' "$path" | cut -d/ -f2)"
  num="$(printf '%s' "$path" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
  cloneurl="https://github.com/$owner/$repo.git"
  echo "re-verify: resolving PR head via 'git fetch $cloneurl pull/$num/head'"
  git fetch --quiet "$cloneurl" "pull/$num/head" || { echo "fetch failed" >&2; exit 2; }
  REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: ref under test = $REF"

LAST_REVIEWED_FILE="$HERE/.last-reviewed"
[ -s "$LAST_REVIEWED_FILE" ] && echo "re-verify: last-reviewed = $(tr -d '[:space:]' < "$LAST_REVIEWED_FILE") (delta: $(cat "$LAST_REVIEWED_FILE")..$REF)"

OUT="$HERE/results/reverify.out"
mkdir -p "$HERE/results"
echo "=== re-verify $(date -u +%FT%TZ) ref=$REF ===" > "$OUT"

python3 -I "$HERE/scripts/verify_pins.py" --repo . --ref "$REF" 2>&1 | tee -a "$OUT"
RC="${PIPESTATUS[0]}"

echo
if [ "$RC" -eq 0 ]; then
  echo "VERDICT: all contract checks (C1..C4) PASS on $REF — Fixed / still-correct"
else
  echo "VERDICT: contract failures on $REF — see $OUT"
fi
exit "$RC"
