#!/usr/bin/env bash
# Regression sweep: unit tests of the packages whose behavior depends on the
# vendored k8s.io/kubernetes / csi spec code that this PR bumped:
#   - pkg/ddc/base/portallocator (uses k8s.io/kubernetes/pkg/registry/core/service/{allocator,portallocator})
#   - pkg/utils/kubeclient, pkg/ddc/{juicefs,thin,efc} (use k8s.io/kubernetes/pkg/api/v1/pod)
#   - pkg/csi/... (csi spec v1.9.0 ControllerServer surface)
# Blocked entirely by F1 on the PR head; must pass once the tree compiles.
#
# Usage: bash run-affected-tests.sh [results-dir]
set -uo pipefail
RESULTS="${1:-$(dirname "$0")/../results}"
mkdir -p "$RESULTS"

cd "$(git rev-parse --show-toplevel)"
if ! go build ./... >/dev/null 2>&1; then
  echo "REGRESSION SWEEP: BLOCKED (tree does not compile - see F1)"
  exit 2
fi
go test \
  ./pkg/ddc/base/portallocator/... \
  ./pkg/utils/kubeclient/... \
  ./pkg/csi/... \
  ./pkg/ddc/juicefs/... \
  ./pkg/ddc/thin/... \
  ./pkg/ddc/efc/... \
  >"$RESULTS/affected-unit-tests.txt" 2>&1
rc=$?
if [ $rc -eq 0 ]; then
  echo "REGRESSION SWEEP: PASS"
else
  echo "REGRESSION SWEEP: FAIL (exit $rc)"
  grep -E "^(FAIL|ok  .*(FAIL|---))" "$RESULTS/affected-unit-tests.txt" | head -10
fi
exit $rc
