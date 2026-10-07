#!/bin/bash
# Bug-canary for finding F2: check_pvc_not_mountable in
# test/gha-e2e/mooncake/test.sh matches FailedMount events by
# involvedObject.name + reason only. Because events outlive the pod, a rerun
# on the same cluster can pass on stale evidence (proven against a real API
# server by TestStaleFailedMountEventsMatchPodNameSelector_F2Verify).
#
# CANARY polarity: exits 0 (tripped) while the query lacks involvedObject.uid
# scoping; exits 1 (fixed) once the query is scoped to the current pod's UID.
set -u
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"

query_line=$(grep -n "involvedObject.name=" "$REPO_ROOT/test/gha-e2e/mooncake/test.sh" | grep "FailedMount" || true)
if [[ -z "$query_line" ]]; then
    echo "CANARY-CLEAR: FailedMount event query no longer found in test.sh (restructured; re-review manually)"
    exit 1
fi
if echo "$query_line" | grep -q "involvedObject.uid"; then
    echo "CANARY-CLEAR: FailedMount query is scoped to the pod UID:"
    echo "$query_line"
    exit 1
fi
echo "CANARY-TRIPPED: FailedMount query is not scoped to the pod UID:"
echo "$query_line"
exit 0
