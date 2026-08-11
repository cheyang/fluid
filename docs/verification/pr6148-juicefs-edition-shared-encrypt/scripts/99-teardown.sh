#!/usr/bin/env bash
# Live-layer teardown: remove ONLY the scoped resources this harness created
# (namespace jfs-verify and everything in it). Restore the in-cluster
# juicefsruntime-controller deploy to its original replicas=0. Also stop a
# host-run PR controller if its PID is recorded in /tmp/jfsr_ctrl_pid.
# No cluster-wide destructive actions.
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/config}"; export KUBECONFIG
: "${VERIFY_NS:=jfs-verify}"
NS="$VERIFY_NS"

# stop a host-run local controller if present
if [ -f /tmp/jfsr_ctrl_pid ]; then
  PID="$(cat /tmp/jfsr_ctrl_pid 2>/dev/null || true)"
  [ -n "$PID" ] && kill "$PID" 2>/dev/null || true
  rm -f /tmp/jfsr_ctrl_pid
fi

# restore the shared Fluid install: the in-cluster juicefsruntime-controller is
# normally scaled to 0 in this environment; force it back to 0 regardless of state.
kubectl scale deploy -n fluid-system juicefsruntime-controller --replicas=0 2>/dev/null || true

# the controller (now 0) won't clear runtime finalizers, so patch them off
kubectl get juicefsruntime -n "$NS" -o name 2>/dev/null | while read -r rt; do
  kubectl patch "$rt" -n "$NS" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
  kubectl delete "$rt" -n "$NS" --now 2>/dev/null || true
done
kubectl get dataset -n "$NS" -o name 2>/dev/null | while read -r ds; do
  kubectl patch "$ds" -n "$NS" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
  kubectl delete "$ds" -n "$NS" --now 2>/dev/null || true
done
kubectl delete secret,deploy,svc -n "$NS" --all 2>/dev/null || true
kubectl delete ns "$NS" --timeout=60s 2>/dev/null || {
  kubectl patch ns "$NS" -p '{"metadata":{"finalizers":[]}}' --type=merge 2>/dev/null || true
  kubectl delete ns "$NS" --now --timeout=30s 2>/dev/null || true
}
echo "teardown done; namespace $NS removed, in-cluster controller restored to replicas=0"
