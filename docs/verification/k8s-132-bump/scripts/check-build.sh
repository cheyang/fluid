#!/usr/bin/env bash
# check-build.sh — precise per-finding build check for PR #5392 findings.
# Runs `go build ./...` and reports which review findings are present.
# Exit 0 iff the tree compiles (F1 and F2 both absent).
set -uo pipefail

OUT="$(go build ./... 2>&1)"; RC=$?
echo "$OUT"
if [ $RC -eq 0 ]; then
  echo "CHECK-BUILD: OK — tree compiles (F1 fixed, F2 fixed)"
  exit 0
fi
F1=0; F2=0
echo "$OUT" | grep -q "unknown field IgnoredFields in struct literal of type merge.Updater" && F1=1
echo "$OUT" | grep -q "missing method ControllerModifyVolume" && F2=1
echo "CHECK-BUILD: FAIL (go build exit=$RC)"
echo "  F1 (structured-merge-diff v4.4.2 vs apimachinery v0.29.15, 'unknown field IgnoredFields'): $([ $F1 -eq 1 ] && echo PRESENT || echo not-observed-this-run)"
echo "  F2 (csi spec v1.9.0 ControllerServer needs ControllerModifyVolume): $([ $F2 -eq 1 ] && echo PRESENT || echo masked-or-fixed)"
[ $F1 -eq 1 ] && [ $F2 -eq 0 ] && echo "  note: F1 masks F2 — F1 must be fixed before F2's compile error is reachable (verified via probe)"
exit 1
