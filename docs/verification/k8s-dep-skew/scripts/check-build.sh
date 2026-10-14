#!/usr/bin/env bash
# F1 contract gate: the module tree at the PR head must compile.
#
# Contract: `go build ./...` exits 0.
#   - On PR head 8534d282 this FAILS (repro of F1: sigs.k8s.io/structured-merge-diff/v4
#     v4.4.2 removed merge.Updater.IgnoredFields, which the replace-pinned
#     k8s.io/apimachinery v0.29.15 vendored code still sets).
#   - After a correct fix it PASSES.
#
# Usage: bash check-build.sh [results-dir]
set -uo pipefail
RESULTS="${1:-$(dirname "$0")/../results}"
mkdir -p "$RESULTS"

cd "$(git rev-parse --show-toplevel)"
go build ./... >"$RESULTS/go-build-all.txt" 2>&1
rc=$?
if [ $rc -eq 0 ]; then
  echo "F1: PASS (go build ./... exit 0)"
else
  echo "F1: FAIL (go build ./... exit $rc)"
  sed -n '1,10p' "$RESULTS/go-build-all.txt"
fi
exit $rc
