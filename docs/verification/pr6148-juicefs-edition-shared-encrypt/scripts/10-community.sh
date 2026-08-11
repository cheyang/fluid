#!/usr/bin/env bash
# Community scenario — the fix target. Dataset declares metaurl in
# SharedEncryptOptions (dataset-wide), so the PR fix classifies it as
# community; without the fix it is misclassified as enterprise (empty FormatCmd).
#
# Env in (required): METAURL  (e.g. redis://<svc>:6379/1)
# Env in (optional): ACCESS_KEY, SECRET_KEY, STORAGE (default oss), BUCKET
# Env in (control):  VERIFY_NS (default jfs-verify), VERIFY_NAME (default jfs-comm)
# Prerequisite: a juicefsruntime-controller reconciling this namespace — either
#   the local PR controller (built from this branch) running out-of-cluster, or
#   the in-cluster deploy patched to the PR image. For the bug-repro contrast,
#   instead run the installed buggy v1.1.0 controller (scale deploy to 1).
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/config}"; export KUBECONFIG
: "${METAURL:?METAURL is required (e.g. redis://1.2.3.4:6379/1)}"
: "${VERIFY_NS:=jfs-verify}"; export VERIFY_NS
: "${STORAGE:=oss}"; : "${BUCKET:=https://fluid-verify-oss-cn-hongkong.aliyuncs.com}"
NS="$VERIFY_NS"; NAME="${VERIFY_NAME:-jfs-comm}"; SECRET="${NAME}-secret"

kubectl -n "$NS" delete juicefsruntime "$NAME" 2>/dev/null || true
kubectl -n "$NS" delete dataset "$NAME" 2>/dev/null || true
kubectl -n "$NS" delete secret "$SECRET" 2>/dev/null || true

kubectl -n "$NS" create secret generic "$SECRET" \
  --from-literal=metaurl="$METAURL" \
  ${ACCESS_KEY:+--from-literal=access-key="$ACCESS_KEY"} \
  ${SECRET_KEY:+--from-literal=secret-key="$SECRET_KEY"}

# Build the sharedEncryptOptions block conditionally, then emit Dataset+Runtime.
ENC="{name: metaurl, valueFrom: {secretKeyRef: {name: $SECRET, key: metaurl}}}"
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
