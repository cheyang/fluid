#!/usr/bin/env bash
# F1 (contract): the commit under review carries a `Signed-off-by:` trailer,
# which is what the repo's DCO bot (probot/dco) enforces.
# PASS  -> commit is signed off (DCO would pass)
# FAIL  -> commit lacks the trailer (DCO check fails, tide blocks merge)
# Usage: check-dco-trailer.sh <commit-ish>
set -u
commit="${1:?usage: check-dco-trailer.sh <commit-ish>}"
trailers=$(git log -1 --format='%(trailers)' "$commit")
if printf '%s' "$trailers" | grep -q '^Signed-off-by:'; then
  echo "PASS: $commit carries Signed-off-by"
  exit 0
else
  echo "FAIL: $commit has no Signed-off-by trailer (DCO will fail)"
  echo "trailers: [${trailers}]"
  exit 1
fi
