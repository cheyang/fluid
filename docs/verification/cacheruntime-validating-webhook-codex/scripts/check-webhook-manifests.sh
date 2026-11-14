#!/usr/bin/env bash
# F2 check (PR #6243): the kubebuilder webhook marker added in
# pkg/webhook/handler/validating/webhook.go must be reflected in the committed
# config/webhook/manifests.yaml. This mirrors the CI lint gate, which runs
# `make update-crd` and fails on a dirty tree ("CRD validation failed").
# Exit 0 = manifests up to date; exit 1 = stale (F2 present).
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
manifest="$repo_root/config/webhook/manifests.yaml"
if grep -q "update.cacheruntime.fluid.io" "$manifest"; then
  echo "PASS: config/webhook/manifests.yaml contains update.cacheruntime.fluid.io"
  exit 0
fi
echo "FAIL: config/webhook/manifests.yaml is stale (missing update.cacheruntime.fluid.io)."
echo "      The PR adds a +kubebuilder:webhook marker but did not commit the"
echo "      regenerated manifests; 'make update-crd' leaves a dirty tree and the"
echo "      lint CI job fails. Fix: run 'make update-crd' and commit the result."
exit 1
