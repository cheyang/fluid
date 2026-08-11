#!/usr/bin/env bash
# Live-layer setup for PR #6148 verification.
# Creates a scoped namespace + a Redis deployment that serves as the JuiceFS
# community metaurl backend. Idempotent. Honors $KUBECONFIG.
# Requires: none. (Scenario scripts 10/20 supply creds via env.)
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/config}"
export KUBECONFIG
NS="${VERIFY_NS:-jfs-verify}"

kubectl get ns "$NS" >/dev/null 2>&1 || kubectl create ns "$NS"
kubectl -n "$NS" apply -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata: {name: redis, labels: {app: redis}}
spec:
  replicas: 1
  selector: {matchLabels: {app: redis}}
  template: {metadata: {labels: {app: redis}}, spec: {containers: [{name: redis, image: redis:7-alpine, ports: [{containerPort: 6379}], readinessProbe: {exec: {command: [redis-cli, ping]}, initialDelaySeconds: 3, periodSeconds: 3}}]}}
---
apiVersion: v1
kind: Service
metadata: {name: redis}
spec: {selector: {app: redis}, ports: [{port: 6379, targetPort: 6379}]}
EOF
kubectl -n "$NS" rollout status deploy/redis --timeout=120s
REDIS_IP=$(kubectl -n "$NS" get svc redis -o jsonpath='{.spec.clusterIP}')
echo "REDIS_CLUSTERIP=$REDIS_IP  (metaurl=redis://${REDIS_IP}:6379/1)"
echo "$REDIS_IP" > /tmp/jfsr_redis_ip
