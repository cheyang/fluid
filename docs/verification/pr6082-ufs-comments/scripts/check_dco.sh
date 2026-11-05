#!/usr/bin/env bash
# F2 (contract): every commit in base..head carries a Signed-off-by trailer (DCO).
# Fails (confirming the DCO check failure) when the trailer is absent.
# Usage: check_dco.sh <baseSha> <headSha>
set -euo pipefail
BASE="${1:?base sha}"; HEAD="${2:?head sha}"
rc=0
for c in $(git rev-list "$BASE..$HEAD"); do
  if git log -1 --format=%B "$c" | grep -q '^Signed-off-by: '; then
    echo "PASS: $c has Signed-off-by"
  else
    echo "FAIL: $c missing Signed-off-by trailer"; rc=1
  fi
done
exit $rc
