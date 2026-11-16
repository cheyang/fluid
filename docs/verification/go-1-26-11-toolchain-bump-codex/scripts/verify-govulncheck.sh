#!/usr/bin/env bash
# verify-govulncheck.sh — symbol-level govulncheck differential for
# GO-2026-6859: the PR head toolchain (go1.26.11) must report no stdlib
# findings, while the baseline toolchain (go1.26.10) must report GO-2026-6859
# as reachable. Requires govulncheck in PATH and network (toolchain download).
set -euo pipefail
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
PKGS="./cmd/... ./pkg/... ./api/..."
run() { # $1=toolchain $2=label
  echo "=== govulncheck with GOTOOLCHAIN=$1 ($2) ==="
  GOTOOLCHAIN="$1" GOFLAGS=-mod=vendor govulncheck $PKGS 2>&1 | grep -E "^(Vulnerability|Your code|This scan|  (Module|Found in|Fixed in|More info))" || true
}
run go1.26.11 head
run go1.26.10 baseline
echo
echo "Expected: head reports NO 'GO-2026-6859'; baseline reports 'Vulnerability #1: GO-2026-6859'."
