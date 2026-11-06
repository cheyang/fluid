#!/usr/bin/env bash
# Contract test for PR #6230 (reviewer A / Claude), isolating F1's cause.
#
# Claim: "The dependency bump itself is sound: with a Go toolchain >= the new
# module minimum (1.26.0), the vendored tree builds, vets clean, fluid's own
# metrics package compiles against client_golang v1.25.0, and no fluid source
# imports the API surface that v1.25.0 broke (api/prometheus/v1)."
#
# Polarity: CONTRACT. Green = the failure is attributable to the toolchain pin
#           (F1/F2), not to the dependency change itself.
# Red    = the bumped dependencies actually break fluid's code - a different,
#          stronger finding than F1.
#
# Observed 2026-11-06 (local go1.27.0): PASS at PR head af817d9ad.
# Full unit suite differential: 66 packages ok at head; the 4 failing packages
# (pkg/ddc/alluxio, pkg/ddc/cache/engine, pkg/utils, pkg/utils/helm) fail
# identically at base 30d488bb -> pre-existing, not introduced by this PR.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../../.." && pwd)"
OUT="${1:-/dev/stdout}"

{
  echo "# check_dep_bump_soundness.sh"
  echo "# repo: $REPO"
  echo "# local go: $(go version)"
} > "$OUT"

GO_MIN="$(awk '/^go[[:space:]]/{print $2; exit}' "$REPO/go.mod")"
LOCAL="$(go env GOVERSION | sed 's/^go//')"
HIGHER="$(printf '%s\n%s\n' "$LOCAL" "$GO_MIN" | sort -V | tail -1)"
if [ "$GO_MIN" != "$LOCAL" ] && [ "$HIGHER" = "$GO_MIN" ]; then
  echo "SKIP: local go $LOCAL < module minimum $GO_MIN; cannot run this layer here" | tee -a "$OUT"
  exit 2
fi

# 1. no fluid source imports the v1.25.0 breaking API surface (api/prometheus/v1)
BROKE="$(grep -rn 'client_golang/api' "$REPO/pkg" "$REPO/cmd" "$REPO/tools" 2>/dev/null || true)"
if [ -n "$BROKE" ]; then
  echo "FAIL: fluid imports the API surface broken by client_golang v1.25.0:" | tee -a "$OUT"
  echo "$BROKE" | tee -a "$OUT"
  exit 1
fi
echo "ok: no non-vendor import of client_golang/api (v1.25.0 breaking surface unused)" | tee -a "$OUT"

# 2. vendored build + vet of the whole tree
if ( cd "$REPO" && go build ./... >> "$OUT" 2>&1 ); then
  echo "ok: go build ./... (vendored) passes with go $LOCAL" | tee -a "$OUT"
else
  echo "FAIL: go build ./... failed" | tee -a "$OUT"
  exit 1
fi
if ( cd "$REPO" && go vet ./pkg/... ./cmd/... >> "$OUT" 2>&1 ); then
  echo "ok: go vet ./pkg/... ./cmd/... passes" | tee -a "$OUT"
else
  echo "FAIL: go vet failed" | tee -a "$OUT"
  exit 1
fi

# 3. fluid's own metrics package (the only client_golang consumer) compiles
if ( cd "$REPO" && go test -count=1 -run xxxNoSuchTest ./pkg/metrics/... >> "$OUT" 2>&1 ); then
  echo "ok: pkg/metrics compiles and links against client_golang v1.25.0" | tee -a "$OUT"
  echo "PASS: dependency bump is sound; CI failure is the toolchain pin (F1/F2)" | tee -a "$OUT"
  exit 0
else
  echo "FAIL: pkg/metrics does not build/test against the new client_golang" | tee -a "$OUT"
  exit 1
fi
