#!/usr/bin/env bash
# Contract test for finding F2 of PR #6230 (reviewer A / Claude).
#
# Claim: "The minimum Go in go.mod must not exceed the Go version of every
# digest-pinned builder image (docker/Dockerfile*, FROM golang:<ver>-bookworm@sha256:...)."
#
# Polarity: CONTRACT. Green = every pinned builder image can compile the module
#           with its bundled toolchain (hermetic build; the digest pin is also
#           the toolchain pin).
# Red    = the image's Go is older than the module minimum, so image builds
#          only succeed via a *network* toolchain auto-download (GOTOOLCHAIN=auto)
#          of a toolchain that is NOT covered by the image digest pin. Builds in
#          air-gapped/registry-mirrored environments or with GOPROXY-only egress
#          fail. Note: docker is not needed to check this contract - the pinned
#          versions are declared in the Dockerfiles themselves.
#
# Bite-verified on 2026-11-06: red at PR head af817d9ad (go 1.26.0 vs 11x
# golang:1.25.12-bookworm), green at base 30d488bb (go 1.25.0).
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../../.." && pwd)"
OUT="${1:-/dev/stdout}"

{
  echo "# check_builder_image_go.sh"
  echo "# repo: $REPO"
} > "$OUT"

GO_MIN="$(awk '/^go[[:space:]]/{print $2; exit}' "$REPO/go.mod")"
echo "go.mod minimum go: $GO_MIN" | tee -a "$OUT"

RC=0
while IFS= read -r -d '' df; do
  # the builder stage image, e.g. golang:1.25.12-bookworm@sha256:...
  img="$(grep -m1 -oE 'FROM golang:[0-9]+\.[0-9]+(\.[0-9]+)?' "$df" | head -1)"
  [ -z "$img" ] && continue
  ver="${img#FROM golang:}"
  higher="$(printf '%s\n%s\n' "$ver" "$GO_MIN" | sort -V | tail -1)"
  if [ "$GO_MIN" != "$ver" ] && [ "$higher" = "$GO_MIN" ]; then
    echo "FAIL: $df pins $img but go.mod requires go >= $GO_MIN" | tee -a "$OUT"
    RC=1
  else
    echo "ok:   $df ($img satisfies go $GO_MIN)" | tee -a "$OUT"
  fi
done < <(find "$REPO/docker" -maxdepth 1 -name 'Dockerfile*' -print0)

if [ "$RC" -eq 0 ]; then
  echo "PASS: all pinned builder images satisfy the module's minimum Go" | tee -a "$OUT"
else
  echo "FAIL: at least one pinned builder image is below the module's minimum Go" | tee -a "$OUT"
fi
exit $RC
