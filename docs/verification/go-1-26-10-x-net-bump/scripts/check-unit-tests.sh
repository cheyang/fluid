#!/usr/bin/env bash
# C3 (contract): CI-equivalent unit suite passes with the pinned toolchain.
# Mirrors `make test` (project-check.yml): -race plus gomonkey's required
# -gcflags="all=-N -l". Long: ~10-20 min on 8 cores.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
TC="$(awk '/^toolchain /{print $2; exit}' go.mod)"
echo "using GOTOOLCHAIN=$TC"
GOTOOLCHAIN="$TC" go list ./... 2>/dev/null | grep -v controller | grep -v e2etest \
  | FLUID_UNIT_TEST=true GOTOOLCHAIN="$TC" xargs go test -race -covermode=atomic -gcflags="all=-N -l"
echo "PASS: unit suite green under $TC"
