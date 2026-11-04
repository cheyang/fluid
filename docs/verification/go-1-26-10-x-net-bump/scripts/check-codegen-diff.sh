#!/usr/bin/env bash
# C4 (contract): `make update-crd` and `make gen-openapi` produce no diff with
# the pinned toolchain (mirrors project-check.yml's CRD/OpenAPI validation).
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
TC="$(awk '/^toolchain /{print $2; exit}' go.mod)"
export GOTOOLCHAIN="$TC"
git checkout -f -- . >/dev/null 2>&1 || true
make update-crd
if [ -n "$(git status --porcelain)" ]; then
  echo "FAIL: make update-crd produced a diff:"; git status --porcelain
  git checkout -f -- .; exit 1
fi
make gen-openapi
if [ -n "$(git status --porcelain | grep generated || true)" ]; then
  echo "FAIL: make gen-openapi produced a diff:"; git status --porcelain
  git checkout -f -- .; exit 1
fi
git checkout -f -- .
echo "PASS: update-crd and gen-openapi are diff-free under $TC"
