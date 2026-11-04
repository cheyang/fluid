#!/usr/bin/env bash
# Acceptance checks for the dependency/toolchain bump (all offline, no cluster):
#  A1 build:   go build ./... with the pinned toolchain go1.26.10
#  A2 module:  go mod verify; go mod tidy and go mod vendor are no-ops
#  A3 digest:  pinned Docker digest resolves to a multi-arch index whose
#              tag is golang:1.26.10-bookworm (queries registry-1.docker.io)
#  A4 tests:   full unit suite in the canonical make-test mode (-gcflags="all=-N -l")
set -u
WT="${1:?usage: $0 <head-worktree>}"
RESULTS="${RESULTS_DIR:-$(dirname "$0")/../results}"

( cd "$WT" && GOTOOLCHAIN=go1.26.10 go build ./... ) > "$RESULTS/a1-build.log" 2>&1 \
  && echo "[A1] PASS go build ./... (go1.26.10)" || echo "[A1] FAIL"

( cd "$WT" && go mod verify && cp go.mod /tmp/gm.bak && go mod tidy && go mod vendor && \
  git diff --quiet -- go.mod go.sum vendor && echo ok ) > "$RESULTS/a2-module.log" 2>&1 \
  && echo "[A2] PASS module tidy/vendor no-op" || echo "[A2] FAIL (see a2-module.log)"

DIGEST=$(grep -rhoP '(?<=bookworm@)sha256:[0-9a-f]{64}' "$WT/docker" "$WT/test/gha-e2e/jindo/oss-emulator/Dockerfile" | sort -u)
if [ "$(echo "$DIGEST" | wc -l)" -eq 1 ]; then
  TOKEN=$(curl -s "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/golang:pull" \
    | python3 -c "import json,sys;print(json.load(sys.stdin)['token'])")
  TAGDIG=$(curl -s -I -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/vnd.oci.image.index.v1+json" \
    "https://registry-1.docker.io/v2/library/golang/manifests/1.26.10-bookworm" \
    | grep -i docker-content-digest | tr -d '\r' | awk '{print $2}')
  INDEX=$(curl -s -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/vnd.oci.image.index.v1+json" \
    "https://registry-1.docker.io/v2/library/golang/manifests/$DIGEST")
  ARCHES=$(echo "$INDEX" | python3 -c "import json,sys;d=json.load(sys.stdin);print(','.join(sorted(m['platform'].get('architecture','') for m in d['manifests'] if m['platform'].get('os')=='linux')))")
  echo "[A3] pinned=$DIGEST tag=$TAGDIG linux-arches=$ARCHES" | tee "$RESULTS/a3-digest.log"
  [ "$DIGEST" = "$TAGDIG" ] && echo "$ARCHES" | grep -q "amd64" && echo "$ARCHES" | grep -q "arm64" \
    && echo "[A3] PASS single multi-arch digest, matches current tag" || echo "[A3] FAIL"
else
  echo "[A3] FAIL: inconsistent or missing digests across Dockerfiles" | tee "$RESULTS/a3-digest.log"
fi

( cd "$WT" && FLUID_UNIT_TEST=true bash -c \
  'go list ./... | grep -v controller | grep -v e2etest | xargs go test -count=1 -timeout 15m -gcflags="all=-N -l"' ) \
  > "$RESULTS/a4-unit-canonical.log" 2>&1
  if grep -q "^FAIL" "$RESULTS/a4-unit-canonical.log"; then echo "[A4] FAIL (see a4-unit-canonical.log)"; else echo "[A4] PASS full unit suite (canonical flags)"; fi
