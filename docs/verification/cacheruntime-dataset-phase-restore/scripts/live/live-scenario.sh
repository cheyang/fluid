#!/usr/bin/env bash
# Live layer for PR #6162 verification.
#
# Runs one build of the cacheruntime manager out-of-cluster against a real
# cluster, drives a real worker outage, and reports:
#   1. whether the Dataset phase recovers Failed -> Bound  (the PR's claim)
#   2. how many reportSummary pod-execs the recovery cost  (finding F1)
#
# The exec counter is exact, not log-scraped: the runtime class's reportSummary
# command appends one timestamped line to /tmp/exec-count.log in the master
# container, so counting lines counts kubelet execs.
#
# Usage: live-scenario.sh <manager-binary> <label>
# Idempotent: re-running re-creates the fixture from scratch.
set -uo pipefail

BIN="${1:?usage: live-scenario.sh <manager-binary> <label>}"
LABEL="${2:?usage: live-scenario.sh <manager-binary> <label>}"
HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="${OUT_DIR:-/root/verify-results}/live-${LABEL}"
NS="${NS:-default}"
RT=verify6162
MASTER_POD="${RT}-master-0"
WORKER_POD="${RT}-worker-0"

mkdir -p "$OUT"

# Two concurrent runs would both reconcile the fixture and both trigger reportSummary
# execs, silently corrupting the exec count. Refuse rather than produce bad evidence.
exec 9>/tmp/verify6162-live.lock
if ! flock -n 9; then
  echo "FATAL: another live-scenario.sh already holds /tmp/verify6162-live.lock"
  echo "       (pgrep -af live-scenario.sh to find it)"
  exit 1
fi

exec > >(tee "${OUT}/scenario.log") 2>&1

say() { printf '\n=== %s ===\n' "$*"; }
phase() { kubectl -n "$NS" get dataset "$RT" -o jsonpath='{.status.phase}' 2>/dev/null; }
execs() { kubectl -n "$NS" exec "$MASTER_POD" -c master -- cat /tmp/exec-count.log 2>/dev/null | grep -c . || echo 0; }

# wait_phase <want> <timeout-seconds>
wait_phase() {
  local want="$1" timeout="$2" i=0
  while [ "$i" -lt "$timeout" ]; do
    [ "$(phase)" = "$want" ] && return 0
    sleep 2; i=$((i + 2))
  done
  return 1
}

say "STEP 0  reset fixture ($LABEL)"
kubectl -n "$NS" delete cacheruntime "$RT" --ignore-not-found --wait=true --timeout=120s
kubectl -n "$NS" delete dataset "$RT" --ignore-not-found --wait=true --timeout=120s
kubectl delete cacheruntimeclass "$RT" --ignore-not-found
kubectl apply -f "${HERE}/00-cacheruntimeclass.yaml"
kubectl -n "$NS" apply -f "${HERE}/01-dataset.yaml"

say "STEP 1  start the manager under test out-of-cluster"
echo "binary: $BIN"
"$BIN" start --development=true --enable-leader-election=false --metrics-addr=:0 \
  > "${OUT}/manager.log" 2>&1 &
MGR=$!
trap 'kill $MGR 2>/dev/null' EXIT
sleep 8
if ! kill -0 "$MGR" 2>/dev/null; then
  echo "FATAL: manager exited immediately"; tail -40 "${OUT}/manager.log"; exit 1
fi

kubectl -n "$NS" apply -f "${HERE}/02-cacheruntime.yaml"

say "STEP 2  wait for the initial Bound (Setup -> BindToDataset)"
if wait_phase Bound 300; then
  echo "OK: dataset reached Bound"
else
  echo "FATAL: never reached Bound (phase=$(phase))"
  kubectl -n "$NS" get cacheruntime "$RT" -o yaml | sed -n '/^status:/,$p' | head -40
  kubectl -n "$NS" get pods | grep "$RT"
  tail -40 "${OUT}/manager.log"
  exit 1
fi

say "STEP 3  zero the exec counter, then induce the outage"
kubectl -n "$NS" exec "$MASTER_POD" -c master -- sh -c ': > /tmp/exec-count.log'
echo "execs at baseline: $(execs)"
OUTAGE_START=$(date +%s)
kubectl -n "$NS" delete pod "$WORKER_POD" --wait=false
echo "deleted $WORKER_POD at $(date -Is)"

if wait_phase Failed 180; then
  echo "OK: dataset went Failed after $(( $(date +%s) - OUTAGE_START ))s"
else
  echo "WARN: never observed Failed (phase=$(phase)); outage may have been too short"
fi
EXEC_AT_FAILED=$(execs)
echo "execs while Failed: $EXEC_AT_FAILED"

say "STEP 4  wait for recovery (worker pod is recreated by the AdvancedStatefulSet)"
if wait_phase Bound 300; then
  RECOVER_S=$(( $(date +%s) - OUTAGE_START ))
  echo "OK: dataset recovered to Bound ${RECOVER_S}s after the outage started"
  RECOVERED=yes
else
  echo "NOT RECOVERED: phase is still '$(phase)' 300s after the runtime came back"
  RECOVERED=no
fi

say "STEP 5  let it settle, then read the exec counter"
# 60s of steady state: with the 5s permitSync window plus the ~90s periodic
# requeue, a correctly throttled build should add very few execs here.
sleep 60
EXEC_TOTAL=$(execs)

say "RESULT ($LABEL)"
kubectl -n "$NS" exec "$MASTER_POD" -c master -- cat /tmp/exec-count.log > "${OUT}/exec-timestamps.txt" 2>/dev/null
{
  echo "label:              $LABEL"
  echo "binary:             $BIN"
  echo "recovered_to_bound: $RECOVERED"
  echo "recovery_seconds:   ${RECOVER_S:-n/a}"
  echo "execs_while_failed: $EXEC_AT_FAILED"
  echo "execs_total:        $EXEC_TOTAL"
  echo "final_phase:        $(phase)"
} | tee "${OUT}/summary.txt"

say "exec inter-arrival (seconds between consecutive reportSummary execs)"
awk 'NR>1 { printf "%.2f\n", $1 - prev } { prev = $1 }' "${OUT}/exec-timestamps.txt" \
  | sort -n | uniq -c | tee "${OUT}/exec-gaps.txt"

kill "$MGR" 2>/dev/null
wait "$MGR" 2>/dev/null
grep -cE "restoring dataset phase from Failed to Bound" "${OUT}/manager.log" \
  | xargs -I{} echo "manager logged the restore branch {} time(s)"
echo "artifacts in ${OUT}/"
