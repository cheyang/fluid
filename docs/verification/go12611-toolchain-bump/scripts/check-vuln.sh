#!/usr/bin/env bash
# C1 — vulnerability contract for PR #6255 (Go 1.26.10 -> 1.26.11).
#
# Contract (against the checked-out code, i.e. the PR head when grafted):
#   govulncheck ./cmd/... ./pkg/... ./api/... with the repo toolchain must NOT
#   report GO-2026-6859 (net/http MaxBytesReader header race) and must not
#   report any Go standard-library advisory at symbol level.
#
# Premise reproduction (P0) runs against the BASE branch instead:
#   scripts/check-vuln.sh --premise <base-sha>   (see re-verify.sh)
#
# Requires: go, govulncheck in PATH (or $GOVULNCHECK), network for vuln DB.
# Emits "CHECK C1-vuln pass|fail"; exit 0 iff pass.
set -uo pipefail

GOVULNCHECK="${GOVULNCHECK:-govulncheck}"
[ -x "$GOVULNCHECK" ] || GOVULNCHECK="$(command -v govulncheck || true)"
if [ -z "$GOVULNCHECK" ]; then
  echo "CHECK C1-vuln fail  govulncheck not found in PATH (install: go install golang.org/x/vuln/cmd/govulncheck@latest)"
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

out="$(mktemp)"
trap 'rm -f "$out"' EXIT

# Pin GOTOOLCHAIN to the toolchain go.mod declares (go1.26.11 on the PR head),
# so the check tests the toolchain this PR pins even on machines whose local Go
# is newer (GOTOOLCHAIN=auto would silently prefer the newer local toolchain).
TC="$(awk '/^toolchain /{print $2}' go.mod | head -1)"
if [ -n "$TC" ]; then export GOTOOLCHAIN="$TC"; fi
echo "  (GOTOOLCHAIN=${GOTOOLCHAIN:-auto})"
GOFLAGS=-mod=vendor "$GOVULNCHECK" ./cmd/... ./pkg/... ./api/... >"$out" 2>&1 || true

if grep -q "GO-2026-6859" "$out"; then
  echo "CHECK C1-vuln fail  GO-2026-6859 still reported at symbol level"
  grep -E "Vulnerability #|Found in:|Fixed in:|Your code is affected" "$out" | head -12
  exit 1
fi
if grep -qE "Your code is affected by .* the Go standard library" "$out"; then
  echo "CHECK C1-vuln fail  stdlib advisories remain reachable:"
  grep -E "Vulnerability #|Standard library" "$out" | head -12
  exit 1
fi

summary="$(grep -E "Your code is affected" "$out" | tail -1)"
if [ -z "$summary" ]; then
  echo "CHECK C1-vuln fail  govulncheck produced no summary (tool error?):"
  tail -5 "$out"
  exit 1
fi
echo "CHECK C1-vuln pass  ${summary}"
echo "  (remaining advisories are module-level k8s.io/kubernetes ones, out of scope for this PR)"
exit 0
