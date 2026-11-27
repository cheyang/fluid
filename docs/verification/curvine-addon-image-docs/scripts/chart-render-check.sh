#!/usr/bin/env bash
# Integration-layer check for fluid PR #6293 (reviewer: codex).
# Renders the shipped thin runtime helm chart with the fuse values that
# addons/curvine/runtime-profile.yaml produces, and asserts the fuse container
# of the DaemonSet actually receives the image, command and args.
set -euo pipefail

CHART="${1:-charts/thin}"
VALUES="$(mktemp /tmp/pr6293-values.XXXXXX.yaml)"
trap 'rm -f "$VALUES"' EXIT

# These values are exactly what ThinEngine.parseFromProfileFuse produces from
# addons/curvine/runtime-profile.yaml (image/imageTag/imagePullPolicy/command/args).
cat >"$VALUES" <<'YAML'
fuse:
  enabled: true
  image: ghcr.io/curvineio/curvine
  imageTag: latest
  imagePullPolicy: IfNotPresent
  command:
    - /entrypoint.sh
  args:
    - fluid-thin-runtime
worker:
  enabled: true
owner:
  enabled: false
YAML

rendered="$(helm template pr6293 "$CHART" -f "$VALUES")"

fail=0
check() { # $1 = description, $2 = grep -q pattern
  if printf '%s' "$rendered" | grep -q -- "$2"; then
    echo "PASS: $1"
  else
    echo "FAIL: $1 (pattern not found: $2)"
    fail=1
  fi
}

check "fuse image is ghcr.io/curvineio/curvine:latest" 'image: ghcr.io/curvineio/curvine:latest'
check "fuse imagePullPolicy is IfNotPresent" 'imagePullPolicy: IfNotPresent'
check "fuse container gets command from profile" '^\s*command:\s*$'
check "fuse container command is /entrypoint.sh" '\- /entrypoint.sh'
check "fuse container args is fluid-thin-runtime" '\- fluid-thin-runtime'

if [ "$fail" -ne 0 ]; then
  echo "---- rendered fuse container spec ----"
  printf '%s\n' "$rendered" | sed -n '/name: thin-fuse/,/volumeMounts/p' | head -30
  exit 1
fi
echo "ALL CHART CHECKS PASSED"
