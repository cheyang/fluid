#!/usr/bin/env bash
# C4 (contract, integration-ish): the existing unit tests of the CSI plugin
# package must compile and pass (CI `unittest` job equivalent for the
# affected package).
set -u
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
export GOTOOLCHAIN="${GOTOOLCHAIN:-$(awk '/^toolchain /{print $2; exit}' go.mod)}"
echo "using GOTOOLCHAIN=$GOTOOLCHAIN"
echo "== go test -count=1 ./pkg/csi/... =="
go test -count=1 ./pkg/csi/... 2>&1 | sed 's/\[build failed\]/[compile error]/g'
rc=${PIPESTATUS[0]}
echo "unit exit=$rc"
exit $rc
