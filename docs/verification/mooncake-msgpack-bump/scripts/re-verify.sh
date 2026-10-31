#!/usr/bin/env bash
# re-verify.sh — re-run the mooncake-msgpack-bump verification harness against the
# current PR head (or a given ref) and report per-claim verdicts with polarity.
#
# Usage:  re-verify.sh [<fixed-ref>]     (no ref -> fetch current PR head via manifest.pr)
# Exit 0 iff every contract claim passes on the tested ref.
#
# Claims (all CONTRACT polarity — they assert the intended correct lock and FAIL
# on a tampered/inconsistent one; there are no bug-canaries in this harness):
#   C1  lock integrity:  msgpack entry's hash set == PyPI's published digests
#   C2  installability:  pip --require-hashes accepts the entry for the image platform
#   P0  premise (evidence-only, captured under results/; decided against the BASE ref)
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
MANIFEST="$TOPIC_DIR/verify-manifest.json"
PR_URL="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])")"
REPO_URL="${PR_URL%\.git}"; REPO_URL="${PR_URL%/pull/*}.git"
PR_NUM="${PR_URL##*/}"

REF="${1:-}"
if [ -z "$REF" ]; then
  echo "re-verify: resolving PR head via git fetch $REPO_URL pull/$PR_NUM/head"
  git fetch --quiet "$REPO_URL" "pull/$PR_NUM/head" || { echo "fetch failed" >&2; exit 2; }
  REF="$(git rev-parse FETCH_HEAD)"
fi
echo "re-verify: testing ref $REF"

# Extract the requirements.txt at the tested ref into a scratch dir (no checkout
# of tracked files needed — the harness is read-only against the repo).
SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT
REQ_PATH="test/gha-e2e/mooncake/image/requirements.txt"
git show "$REF:$REQ_PATH" > "$SCRATCH/requirements.txt" 2>/dev/null || {
  echo "re-verify: $REQ_PATH missing at $REF" >&2; exit 2; }
echo "re-verify: extracted $REQ_PATH @ $REF ($(grep -m1 '^msgpack==' "$SCRATCH/requirements.txt"))"

# run the harness (its own results land in the in-repo results dir)
bash "$HERE/verify_msgpack_lock.sh" "$SCRATCH/requirements.txt" "$TOPIC_DIR/results" \
  > "$SCRATCH/verdicts.tsv"
RC=$?

echo
echo "================  RE-VERIFY  ================"
printf '%-8s %-14s %s\n' CLAIM POLARITY VERDICT
while IFS=$'\t' read -r id verdict; do
  printf '%-8s %-14s %s\n' "$id" contract "$verdict"
done < "$SCRATCH/verdicts.tsv"
grep -h 'msgpack==' "$TOPIC_DIR/results/msgpack-entry.txt" | sed 's/^/  entry: /'

# harness-bites reminder: if a claim unexpectedly fails, first confirm the harness
# still bites (results/harness-bites/SUMMARY.md) before trusting the verdict.
LAST_REVIEWED_FILE="$TOPIC_DIR/.last-reviewed"
[ -s "$LAST_REVIEWED_FILE" ] && echo "review delta: $(cat "$LAST_REVIEWED_FILE")..$REF"
echo "after reviewing the delta, advance the marker:"
echo "  echo $REF > $LAST_REVIEWED_FILE && git add $LAST_REVIEWED_FILE && git commit -m 'review: advance last-reviewed'"

if [ $RC -eq 0 ]; then echo "RESULT: all contract claims pass."; exit 0
else echo "RESULT: FAILURES above — lock broken or PyPI metadata changed."; exit 1; fi
