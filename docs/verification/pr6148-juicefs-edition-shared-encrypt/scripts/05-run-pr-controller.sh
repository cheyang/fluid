#!/usr/bin/env bash
# Build and run the PR-branch juicefsruntime-controller locally, out-of-cluster,
# against $KUBECONFIG. This is the "fixed" controller for the live layer: it
# reconciles JuiceFSRuntime objects in the cluster the same way the in-cluster
# deploy would, but with the PR's one-line fix.
#
# Keeps the in-cluster buggy controller at replicas=0 so it cannot race.
# Writes the controller log to results/controller.log and its PID to
# /tmp/jfsr_ctrl_pid (99-teardown.sh stops it).
#
# Env in: REPO (fluid checkout at PR head; default current dir), KUBECONFIG.
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/config}"; export KUBECONFIG
REPO="${REPO:-$(pwd)}"
DEST="$REPO/docs/verification/pr6148-juicefs-edition-shared-encrypt"
BIN="${JFSR_BIN:-/tmp/jfsr-pr-controller}"

# keep the in-cluster (buggy) controller from racing
kubectl scale deploy -n fluid-system juicefsruntime-controller --replicas=0 2>/dev/null || true

echo "[run-pr-controller] building binary from $REPO (cmd/juicefs)..."
( cd "$REPO" && go build -o "$BIN" ./cmd/juicefs/ )

JUICEFS_CE_IMAGE_ENV=juicedata/mount:ce-v1.3.0 \
JUICEFS_EE_IMAGE_ENV=juicedata/mount:ee-5.2.10-eb0a8b3 \
MOUNT_ROOT=/var/runtime-mnt \
KUBECONFIG="$KUBECONFIG" \
nohup "$BIN" start --development=false \
  --port-allocate-policy=bitmap --runtime-node-port-range=14000-15999 \
  > "$DEST/results/controller.log" 2>&1 &
echo $! > /tmp/jfsr_ctrl_pid
echo "[run-pr-controller] started PID=$(cat /tmp/jfsr_ctrl_pid); log=$DEST/results/controller.log"
sleep 6
kill -0 "$(cat /tmp/jfsr_ctrl_pid)" 2>/dev/null && echo "[run-pr-controller] running" \
  || { echo "[run-pr-controller] EXITED — see log:"; tail -20 "$DEST/results/controller.log"; exit 1; }
