#!/usr/bin/env bash
# C3 (contract): compile-time interface satisfaction assertions in
# pkg/csi/plugins/zz_verify_pr6229_interface_contract_test.go must hold,
# i.e. csi-common's Default servers and fluid's own servers satisfy the
# vendored csi.IdentityServer/ControllerServer/NodeServer interfaces.
set -u
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
export GOTOOLCHAIN="${GOTOOLCHAIN:-$(awk '/^toolchain /{print $2; exit}' go.mod)}"
echo "using GOTOOLCHAIN=$GOTOOLCHAIN"
echo "== go test -count=1 -run TestVerifyPR6229CSIInterfaceContracts ./pkg/csi/plugins/ =="
go test -count=1 -run TestVerifyPR6229CSIInterfaceContracts ./pkg/csi/plugins/ 2>&1 | sed 's/\[build failed\]/[compile error]/g'
rc=${PIPESTATUS[0]}
echo "interface-contract exit=$rc"
exit $rc
