#!/usr/bin/env bash
# C2 (contract): the tree builds and vets clean with the toolchain pinned in
# go.mod, in vendored mode. This also proves vendor/ is consistent with go.mod
# (the Go tool refuses inconsistent vendoring).
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
TC="$(awk '/^toolchain /{print $2; exit}' go.mod)"
echo "using GOTOOLCHAIN=$TC"
GOTOOLCHAIN="$TC" GOFLAGS=-mod=vendor go build ./...
GOTOOLCHAIN="$TC" go vet ./pkg/... ./cmd/... ./api/...
GOTOOLCHAIN="$TC" go mod verify
echo "PASS: build + vet + mod verify clean under $TC"
