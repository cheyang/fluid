#!/usr/bin/env bash
# C2 — build & unit-test contract for PR #6255 (Go 1.26.10 -> 1.26.11).
#
# The project's `make test` contract, run with the PR's toolchain:
#   go build ./... ; go vet ./pkg/... ./cmd/... ./api/...
#   go list ./... (minus controller / e2etest) | FLUID_UNIT_TEST=true xargs go test
#
# IMPORTANT: FLUID_UNIT_TEST=true must prefix *xargs* (as the Makefile does),
# NOT `go list`. A pipeline env prefix only applies to the first command, and
# the env is what the spawned `go test` inherits. Prefixing go list instead
# makes test binaries run with the flag unset, which flips
# testutil.IsUnitTest() to false and fails pkg/ddc/{jindo,jindofsx}
# TestDestroyWorker with "jindoruntimes.data.fluid.io ... not found" — a
# harness artifact, NOT a product regression. Do not re-introduce it.
#
# Emits "CHECK C2-build pass|fail" / "CHECK C2-unit pass|fail"; exit 0 iff pass.
set -uo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

LOG="${1:-/dev/stdout}"

# Pin GOTOOLCHAIN to what go.mod declares (go1.26.11 on the PR head) so the
# suite runs on the toolchain this PR pins even where the local Go is newer
# (GOTOOLCHAIN=auto would silently prefer the newer local toolchain).
TC="$(awk '/^toolchain /{print $2}' go.mod | head -1)"
if [ -n "$TC" ]; then export GOTOOLCHAIN="$TC"; fi
export GOFLAGS=-mod=vendor CGO_ENABLED=0 KUBECONFIG=/dev/null

if go build ./... >"$LOG" 2>&1; then
  echo "CHECK C2-build pass  go build ./... with $(go env GOTOOLCHAIN 2>/dev/null || echo auto) -> $(go version | awk '{print $3}')"
else
  echo "CHECK C2-build fail  go build ./... failed (see $LOG)"
  exit 1
fi

if go vet ./pkg/... ./cmd/... ./api/... >>"$LOG" 2>&1; then
  echo "CHECK C2-vet pass  go vet ./pkg/... ./cmd/... ./api/..."
else
  echo "CHECK C2-vet fail  go vet failed (see $LOG)"
  exit 1
fi

out="$(mktemp)"
trap 'rm -f "$out"' EXIT
# Makefile-equivalent; FLUID_UNIT_TEST prefixes xargs on purpose (see header).
# No -p flag: match `make test` exactly. Forcing -p 4 changed package scheduling
# enough to trigger a pre-existing fixed-port collision (two test binaries both
# listen on 127.0.0.1:6060 via utils.NewPprofServer) — a test-suite flake, not a
# product regression; see README "Refuted during verification".
go list ./... | grep -v controller | grep -v e2etest | \
  FLUID_UNIT_TEST=true xargs go test -count=1 -gcflags="all=-N -l" >"$out" 2>&1 || true

ok_n="$(grep -c '^ok ' "$out")"
fail_n="$(grep -c '^FAIL' "$out")"
cat "$out" >>"$LOG"
if [ "$fail_n" -eq 0 ] && [ "$ok_n" -ge 65 ]; then
  echo "CHECK C2-unit pass  ${ok_n} packages ok, 0 FAIL"
  exit 0
else
  echo "CHECK C2-unit fail  ${ok_n} ok / ${fail_n} FAIL package lines:"
  grep -E '^--- FAIL|^FAIL' "$out" | head -10
  exit 1
fi
