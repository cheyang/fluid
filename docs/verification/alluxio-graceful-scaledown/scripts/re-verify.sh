#!/bin/bash
# Re-verification for PR #6061 findings (Codex harness).
# Usage: bash docs/verification/alluxio-graceful-scaledown/scripts/re-verify.sh [ref]
# With no ref, fetches the current PR head. Grafts nothing: harness files are
# self-contained scripts + the PR's own unit tests. Exit 0 iff all contract
# findings are fixed.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../../.." && pwd)"
cd "$ROOT"

if [ "${1:-}" != "" ]; then
  echo "note: ref argument '$1' ignored by this harness; run from a checkout of the code under review"
fi

fails=0
report() { # id status
  printf "%-4s %s\n" "$1" "$2"
}

echo "== unit regression layer =="
if go test -gcflags="all=-N -l" ./pkg/ddc/alluxio/operations/ >/dev/null 2>&1 \
   && go test -gcflags="all=-N -l" ./pkg/ddc/alluxio/ -args -ginkgo.focus="drainScalingDownWorkers|getWorkerWebPort|workerWebPortFromPod|decommission deadline" >/dev/null 2>&1; then
  report U0 "pass (PR's own drain unit tests green)"
else
  report U0 "FAIL (PR's own unit tests broken)"; fails=$((fails+1))
fi

echo "== contract findings =="
check() { # id script
  if bash "$HERE/$2" >/dev/null 2>&1; then report "$1" "Fixed"; else report "$1" "Still-broken"; fails=$((fails+1)); fi
}
check F1 check-default-alluxio-image.sh
check F2 check-e2e-fixture-coverage.sh
check F3 check-decommission-wait-claim.sh
check F4 check-host-only-matching.sh

echo "== incremental review range =="
LAST=$(cat "$(dirname "$HERE")/.last-reviewed" 2>/dev/null || true)
echo "last-reviewed: ${LAST:-<none>}  (advance with: git rev-parse HEAD > docs/verification/alluxio-graceful-scaledown/.last-reviewed)"

[ "$fails" -eq 0 ] && echo "ALL FIXED" || echo "$fails finding(s) still open"
exit "$fails"
