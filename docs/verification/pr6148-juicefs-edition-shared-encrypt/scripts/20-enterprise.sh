#!/usr/bin/env bash
# Enterprise regression scenario. Dataset declares the JuiceFS enterprise token
# in SharedEncryptOptions. The PR fix must NOT change this path: it stays
# enterprise and genFormatCmd emits the ee `juicefs auth --token=\${TOKEN}` cmd.
#
# Env in (required): JFS_TOKEN (enterprise token)
# Env in (optional): ACCESS_KEY, SECRET_KEY, STORAGE (default oss), BUCKET
# Env in (control):  VERIFY_NS (default jfs-verify), VERIFY_NAME (default jfs-ee)
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/config}"; export KUBECONFIG
: "${JFS_TOKEN:?JFS_TOKEN is required}"
: "${VERIFY_NS:=jfs-verify}"; export VERIFY_NS
: "${STORAGE:=oss}"; : "${BUCKET:=https://fluid-verify-oss-cn-hongkong.aliyuncs.com}"
NS="$VERIFY_NS"; NAME="${VERIFY_NAME:-jfs-ee}"; SECRET="${NAME}-secret"

kubectl -n "$NS" delete juicefsruntime "$NAME" 2>/dev/null || true
kubectl -n "$NS" delete dataset "$NAME" 2>/dev/null || true
kubectl -n "$NS" delete secret "$SECRET" 2>/dev/null || true

kubectl -n "$NS" create secret generic "$SECRET" \
  --from-literal=token="$JFS_TOKEN" \
  ${ACCESS_KEY:+--from-literal=access-key="$ACCESS_KEY"} \
  ${SECRET_KEY:+--from-literal=secret-key="$SECRET_KEY"}

ENC="{name: token, valueFrom: {secretKeyRef: {name: $SECRET, key: token}}}"
[ -n "${ACCESS_KEY:-}" ] && ENC="$ENC, {name: access-key, valueFrom: {secretKeyRef: {name: $SECRET, key: access-key}}}"
[ -n "${SECRET_KEY:-}" ] && ENC="$ENC, {name: secret-key, valueFrom: {secretKeyRef: {name: $SECRET, key: secret-key}}}"

cat <<EOF | kubectl -n "$NS" apply -f -
apiVersion: data.fluid.io/v1alpha1
kind: Dataset
metadata: {name: $NAME}
spec:
  sharedEncryptOptions: [$ENC]
  mounts:
    - name: ${NAME}-m
      mountPoint: "juicefs:///demo"
      options: {storage: $STORAGE, bucket: "$BUCKET"}
---
apiVersion: data.fluid.io/v1alpha1
kind: JuiceFSRuntime
metadata: {name: $NAME}
spec:
  replicas: 1
  tieredstore:
    levels: [{mediumtype: MEM, path: /dev/shm, quota: 1024, low: "0.1"}]
EOF

CM="${NAME}-juicefs-values"
for i in $(seq 1 25); do
  kubectl -n "$NS" get cm "$CM" >/dev/null 2>&1 && break
  sleep 3
done
echo "=== $NAME values configmap ==="
kubectl -n "$NS" get cm "$CM" -o jsonpath='{.data.data}' 2>/dev/null | python3 -c "
import sys,yaml
d=yaml.safe_load(sys.stdin.read()) or {}
print('edition    =', d.get('edition'))
print('source     =', d.get('source'))
print('formatCmd  =', repr((d.get('configs') or {}).get('formatCmd')))
print('fuseImage  =', (d.get('fuse') or {}).get('image'))
"
