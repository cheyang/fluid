#!/usr/bin/env bash
# Take the in-cluster cacheruntime-controller out of the way so the manager under
# test is the only reconciler for CacheRuntime objects. Two managers would both
# reconcile and both issue reportSummary execs, which would corrupt the exec count.
#
# Records the original replica count so live-teardown.sh restores exactly it.
set -euo pipefail

NS_SYS=fluid-system
DEPLOY=cacheruntime-controller
STATE="${STATE_FILE:-/root/verify-results/live-original-replicas}"

mkdir -p "$(dirname "$STATE")"

echo "=== pre-flight: this cluster must have no CacheRuntime/Dataset of its own ==="
existing=$(kubectl get cacheruntime,dataset -A --no-headers 2>/dev/null | grep -v verify6162 | grep -c . || true)
if [ "$existing" -ne 0 ]; then
  echo "REFUSING: found $existing pre-existing CacheRuntime/Dataset object(s) not owned by this harness:"
  kubectl get cacheruntime,dataset -A --no-headers | grep -v verify6162
  echo "Scaling the shared controller down would stall them. Aborting."
  exit 1
fi
echo "OK: no pre-existing objects would be affected"

if [ ! -f "$STATE" ]; then
  kubectl -n "$NS_SYS" get deploy "$DEPLOY" -o jsonpath='{.spec.replicas}' > "$STATE"
fi
echo "=== recorded original replicas: $(cat "$STATE") (saved to $STATE) ==="

kubectl -n "$NS_SYS" scale deploy "$DEPLOY" --replicas=0
kubectl -n "$NS_SYS" rollout status deploy "$DEPLOY" --timeout=120s || true
echo "=== in-cluster $DEPLOY scaled to 0 ==="
kubectl -n "$NS_SYS" get deploy "$DEPLOY"
