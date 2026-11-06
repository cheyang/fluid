#!/usr/bin/env bash
# F1 contract test (polarity: CONTRACT — fails on buggy code, passes on fixed code).
#
# Claim: under PR #6021 head, `gofmt -l` (the exact check CI runs in
# .github/workflows/project-check.yml "Code fmt check") newly flags
# pkg/ddc/alluxio/operations/base_test.go. Correct behavior: the file is gofmt-clean.
#
# Version fidelity matters: gofmt formatting rules drift between Go minor versions
# (a go1.27 gofmt also flags pkg/ddc/thin/transform_fuse.go, which CI's go1.24.12
# gofmt does NOT — master CI is green). So this script resolves the gofmt binary
# from the GO_VERSION pinned in the workflow, and falls back to PATH gofmt with a
# warning if the toolchain cannot be resolved.
#
# Usage: bash check-gofmt.sh [repo-root]
set -u
ROOT="${1:-$(cd "$(dirname "$0")/../../../../.." && pwd)}"
TARGET="pkg/ddc/alluxio/operations/base_test.go"

GO_VERSION="$(sed -n 's/^.*GO_VERSION:[[:space:]]*//p' "${ROOT}/.github/workflows/project-check.yml" 2>/dev/null | head -1 | tr -d '[:space:]')"
GO_VERSION="${GO_VERSION:-1.24.12}"
case "${GO_VERSION}" in go*) ;; *) GO_VERSION="go${GO_VERSION}" ;; esac

GOFMT="$(GOTOOLCHAIN="${GO_VERSION}" go env GOROOT 2>/dev/null)/bin/gofmt"
if [ -x "${GOFMT}" ]; then
  echo "using ${GO_VERSION} gofmt (CI-pinned): ${GOFMT}"
else
  GOFMT="$(command -v gofmt || true)"
  [ -n "${GOFMT}" ] || { echo "FAIL: no gofmt binary found"; exit 2; }
  echo "WARNING: could not resolve ${GO_VERSION} toolchain; falling back to PATH gofmt" \
       "(version drift may cause false results: $(go version 2>/dev/null))"
fi

echo "== F1: ${GOFMT} -l ${TARGET} (contract: expect empty output) =="
OUT="$("${GOFMT}" -l "${ROOT}/${TARGET}")"
if [ -n "${OUT}" ]; then
  echo "FAIL: gofmt flags ${TARGET}:"
  echo "${OUT}"
  echo "Fix: remove the stray leading tab on the added comment lines and the func decl,"
  echo "or run: gofmt -w ${TARGET}  (equivalently 'make fmt')"
  exit 1
fi
echo "PASS: ${TARGET} is gofmt-clean"
exit 0
