#!/usr/bin/env bash
# Undo everything live-setup.sh + live-scenario.sh did: delete the synthetic
# fixture and restore the in-cluster cacheruntime-controller to its recorded
# replica count. Safe to run repeatedly.
set -uo pipefail

NS="${NS:-default}"
NS_SYS=fluid-system
DEPLOY=cacheruntime-controller
STATE="${STATE_FILE:-/root/verify-results/live-original-replicas}"
RT=verify6162

echo "=== stop any stray out-of-cluster manager ==="
pkill -f 'cacheruntime-controller-(pr|master) start' && echo "killed" || echo "none running"

echo "=== delete the synthetic fixture ==="
kubectl -n "$NS" delete cacheruntime "$RT" --ignore-not-found --wait=true --timeout=120s
kubectl -n "$NS" delete dataset "$RT" --ignore-not-found --wait=true --timeout=120s
kubectl delete cacheruntimeclass "$RT" --ignore-not-found
# The engine creates a config ConfigMap and a PV/PVC per runtime; sweep leftovers.
kubectl -n "$NS" delete configmap "fluid-runtime-config-${RT}" --ignore-not-found
kubectl -n "$NS" delete pvc "$RT" --ignore-not-found
kubectl delete pv "${NS}-${RT}" --ignore-not-found

echo "=== restore in-cluster $DEPLOY ==="
want=1
[ -f "$STATE" ] && want="$(cat "$STATE")"
kubectl -n "$NS_SYS" scale deploy "$DEPLOY" --replicas="$want"
kubectl -n "$NS_SYS" rollout status deploy "$DEPLOY" --timeout=180s
kubectl -n "$NS_SYS" get deploy "$DEPLOY"

echo "=== residual harness objects (should be empty) ==="
kubectl get cacheruntime,dataset,cacheruntimeclass -A --no-headers 2>/dev/null | grep "$RT" || echo "none"
