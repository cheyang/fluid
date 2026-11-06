#!/usr/bin/env bash
# Contract test for finding F1 of PR #6230 (reviewer A / Claude).
#
# Claim: "The PR head must build with the Go version pinned by the repository's
# own CI (env.GO_VERSION in .github/workflows/*, where setup-go sets
# GOTOOLCHAIN=local)."
#
# Polarity: CONTRACT. Green = the repo honors its CI toolchain contract.
# Red    = go.mod's minimum Go is above the CI pin, so every CI job that runs
#          `go` (build, unittest, lint, staticcheck, make generate in the e2e
#          jobs) fails with:
#          "go: go.mod requires go >= X (running Y; GOTOOLCHAIN=local)".
#
# Bite-verified on 2026-11-06: red at PR head af817d9ad, green at base
# 30d488bb (results/ci-toolchain-contract-{head,base}.txt).
#
# Requires network on first run to download the pinned toolchain
# (GOTOOLCHAIN=go<CI_GO> triggers a one-time toolchain fetch).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../../.." && pwd)"
OUT="${1:-/dev/stdout}"

{
  echo "# check_ci_toolchain_contract.sh"
  echo "# repo: $REPO"
} > "$OUT"

# 1. The Go version CI pins (env.GO_VERSION, identical in all three workflows).
CI_GO="$(grep -rhoE '^[[:space:]]*GO_VERSION: [0-9]+\.[0-9]+(\.[0-9]+)?' \
  "$REPO/.github/workflows" | head -1 | awk '{print $2}')"
if [ -z "$CI_GO" ]; then
  echo "FAIL: could not read GO_VERSION from .github/workflows/" | tee -a "$OUT"
  exit 1
fi
echo "CI GO_VERSION pin: $CI_GO" | tee -a "$OUT"

# 2. The module's minimum Go (first `go` directive in go.mod).
GO_MIN="$(awk '/^go[[:space:]]/{print $2; exit}' "$REPO/go.mod")"
echo "go.mod minimum go:  $GO_MIN" | tee -a "$OUT"

# 3. Contract: GO_MIN must be <= CI_GO (version sort).
HIGHER="$(printf '%s\n%s\n' "$CI_GO" "$GO_MIN" | sort -V | tail -1)"
if [ "$GO_MIN" != "$CI_GO" ] && [ "$HIGHER" = "$GO_MIN" ]; then
  echo "FAIL: go.mod requires go >= $GO_MIN but CI pins $CI_GO with GOTOOLCHAIN=local" | tee -a "$OUT"
  echo "      -> every CI job that invokes go fails before compiling anything" | tee -a "$OUT"
  exit 1
fi

# 4. The pinned toolchain must actually build the tree (subset that pulls in
#    client_golang via controller-runtime metrics + fluid's own metrics pkg).
echo "building with GOTOOLCHAIN=go$CI_GO (as CI does with GOTOOLCHAIN=local)..." | tee -a "$OUT"
if ( cd "$REPO" && GOTOOLCHAIN="go$CI_GO" go build ./pkg/... ./cmd/... ) >> "$OUT" 2>&1; then
  echo "PASS: repo builds with the CI-pinned Go $CI_GO" | tee -a "$OUT"
  exit 0
else
  echo "FAIL: build with CI-pinned Go $CI_GO failed" | tee -a "$OUT"
  exit 1
fi
