#!/bin/bash
# L0 premise (polarity: canary — MUST fail/panic on pre-#6157 Fluid).
# Applies the client-less manifests against a Fluid WITHOUT the #6157 fix and
# expects: cacheruntime-controller nil-pointer panic + Dataset stuck NotBound.
# Run only against a pre-fix controller (e.g. fluidcloudnative/*:v1.1.0-36f0467).
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
TD=test/gha-e2e/mooncake
before=$(kubectl get pod -n fluid-system -l control-plane=cacheruntime-controller \
  -ojsonpath="{.items[0].status.containerStatuses[0].restartCount}")
kubectl create -f $TD/cacheruntimeclass.yaml -f $TD/dataset.yaml -f $TD/cacheruntime.yaml
sleep 40
phase=$(kubectl get dataset mooncake-demo -ojsonpath="{.status.phase}" 2>/dev/null)
after=$(kubectl get pod -n fluid-system -l control-plane=cacheruntime-controller \
  -ojsonpath="{.items[0].status.containerStatuses[0].restartCount}")
echo "dataset.phase=$phase  controller restarts $before -> $after"
kubectl logs -n fluid-system -l control-plane=cacheruntime-controller -c manager --previous 2>/dev/null \
  | grep -A12 -iE "Observed a panic|nil pointer dereference" | head -20
kubectl delete -f $TD/cacheruntime.yaml -f $TD/dataset.yaml --ignore-not-found
kubectl delete cacheruntimeclass mooncake-demo --ignore-not-found
if [[ "$phase" != "Bound" && "$after" -gt "$before" ]]; then
  echo "PREMISE CONFIRMED: client-less topology panics pre-fix controller (Dataset NotBound)"
else
  echo "PREMISE NOT REPRODUCED (controller may already carry the #6157 fix)"
fi
