#!/usr/bin/env bash
# C1 (contract): the fluid CSI plugin chain must compile.
# Fails on PR #6229 head (csi spec v1.13.0 + archived csi-common v1.0.2);
# passes on the merge-base (csi spec v1.8.0).
set -u
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
export GOTOOLCHAIN="${GOTOOLCHAIN:-$(awk '/^toolchain /{print $2; exit}' go.mod)}"
echo "using GOTOOLCHAIN=$GOTOOLCHAIN"
echo "== go build ./pkg/csi/... ./cmd/csi/... =="
go build ./pkg/csi/... ./cmd/csi/...
rc=$?
echo "build exit=$rc"
exit $rc
