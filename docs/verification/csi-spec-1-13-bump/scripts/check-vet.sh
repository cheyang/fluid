#!/usr/bin/env bash
# C2 (contract): go vet over the CSI packages must pass
# (subset of the repo's `make vet`, the exact step that failed in CI:
# build / lint / unittest / e2e jobs all died at Makefile:189 vet).
set -u
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
export GOTOOLCHAIN="${GOTOOLCHAIN:-$(awk '/^toolchain /{print $2; exit}' go.mod)}"
echo "using GOTOOLCHAIN=$GOTOOLCHAIN"
echo "== go vet ./pkg/csi/... =="
go vet ./pkg/csi/...
rc=$?
echo "vet exit=$rc"
exit $rc
