#!/usr/bin/env bash
# Supply-chain fidelity check for the zap bump (PR #6241, Reviewer A / Claude).
# Verifies that every file under vendor/go.uber.org/zap is byte-identical to the
# upstream go.uber.org/zap module version declared in go.mod, and that the
# go.sum entry matches the official checksum-DB hash.
#
# Usage: bash docs/verification/zap-bump/scripts/verify-vendor-fidelity.sh [<repo-root>]
# Requires: go (network for `go mod download` on first run; cached afterwards).
set -uo pipefail

ROOT="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)}"
cd "$ROOT"

VERSION=$(awk '/^[ \t]*go.uber.org\/zap /{print $2; exit}' go.mod)
if [ -z "$VERSION" ]; then
  echo "[fidelity] ERROR: could not read go.uber.org/zap version from go.mod"
  exit 2
fi
echo "[fidelity] go.mod declares go.uber.org/zap ${VERSION}"

MODDIR=$(go env GOMODCACHE)/go.uber.org/zap@${VERSION}
if [ ! -d "$MODDIR" ]; then
  GOFLAGS=-mod=mod go mod download go.uber.org/zap || { echo "[fidelity] ERROR: download failed"; exit 2; }
fi

# 1. go.sum integrity: official hash vs repo go.sum
ZIPSUM=$(GOFLAGS=-mod=mod go mod download -json go.uber.org/zap 2>/dev/null | python3 -c 'import json,sys;print(json.load(sys.stdin)["Sum"])')
GOSUMENTRY=$(grep "go.uber.org/zap ${VERSION} h1:" go.sum | awk '{print $NF}')
if [ "$ZIPSUM" = "$GOSUMENTRY" ]; then
  echo "[fidelity] go.sum h1 entry matches checksum DB: ${ZIPSUM}"
else
  echo "[fidelity] MISMATCH: go.sum has ${GOSUMENTRY}, official hash is ${ZIPSUM}"
  exit 1
fi

# 2. byte-identical vendor tree
DIFFS=0
while IFS= read -r f; do
  if [ -f "$MODDIR/$f" ]; then
    cmp -s "vendor/go.uber.org/zap/$f" "$MODDIR/$f" || { echo "[fidelity] DIFFERS: $f"; DIFFS=$((DIFFS+1)); }
  else
    echo "[fidelity] NOT-IN-UPSTREAM: $f"
    DIFFS=$((DIFFS+1))
  fi
done < <(cd vendor/go.uber.org/zap && find . -type f | sed 's|^\./||')

# 3. no missing source files inside vendored packages
while IFS= read -r f; do
  pkgdir=$(dirname "$f")
  if [ -d "vendor/go.uber.org/zap/$pkgdir" ] && [ ! -f "vendor/go.uber.org/zap/$f" ]; then
    echo "[fidelity] MISSING-IN-VENDOR: $f"
    DIFFS=$((DIFFS+1))
  fi
done < <(cd "$MODDIR" && find . -name "*.go" ! -name "*_test.go" | sed 's|^\./||')

if [ "$DIFFS" -ne 0 ]; then
  echo "[fidelity] verdict: FAIL (${DIFFS} problems)"
  exit 1
fi
echo "[fidelity] verdict: PASS (vendor tree byte-identical to upstream ${VERSION})"
