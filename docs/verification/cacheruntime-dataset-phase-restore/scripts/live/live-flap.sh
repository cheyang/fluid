#!/usr/bin/env bash
# live-flap.sh — the decisive live measurement for finding F1.
#
# The single-outage scenario (live-scenario.sh) showed the restore branch firing
# once, ~16s after the previous cache-state exec, i.e. with the 5s permitSync()
# window already OPEN — so the bypass cost nothing there. This script drives the
# case the bypass actually changes: repeated not-ready -> ready flaps close
# together, so several ready-reconciles land inside one limiter window.
#
# The outage switch is the worker's readiness probe (test -f /tmp/ready), so an
# outage starts and ends on command and its length is controlled, unlike deleting
# the pod.
#
# Counter: the manager's own V(1) "Exec command start" line (fileutils.go:55).
# That counts exec ATTEMPTS, which is exactly what F1 is about, and is unaffected
# by whether the exec itself succeeds.
#
# Usage: live-flap.sh <manager-binary> <label> [flaps] [dwell-seconds]
set -uo pipefail

BIN="${1:?usage: live-flap.sh <manager-binary> <label> [flaps] [dwell]}"
LABEL="${2:?usage: live-flap.sh <manager-binary> <label> [flaps] [dwell]}"
FLAPS="${3:-5}"
DWELL="${4:-3}"

HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="${OUT_DIR:-/root/verify-results}/flap-${LABEL}"
NS="${NS:-default}"
RT=verify6162
WORKER_POD="${RT}-worker-0"

mkdir -p "$OUT"
exec 9>/tmp/verify6162-live.lock
flock -n 9 || { echo "FATAL: another live run holds the lock"; exit 1; }
exec > >(tee "${OUT}/flap.log") 2>&1

MGRLOG="${OUT}/manager.log"
say()   { printf '\n=== %s ===\n' "$*"; }
phase() { kubectl -n "$NS" get dataset "$RT" -o jsonpath='{.status.phase}' 2>/dev/null; }
# exec attempts the manager has issued so far
execs() { grep -ac "Exec command start" "$MGRLOG" 2>/dev/null || echo 0; }
wready() { kubectl -n "$NS" get pod "$WORKER_POD" -o jsonpath='{.status.containerStatuses[0].ready}' 2>/dev/null; }

wait_for() { # wait_for <fn> <want> <timeout>
  local f="$1" want="$2" t="$3" i=0
  while [ "$i" -lt "$t" ]; do [ "$($f)" = "$want" ] && return 0; sleep 1; i=$((i+1)); done
  return 1
}

say "STEP 0  reset fixture ($LABEL)"
kubectl -n "$NS" delete cacheruntime "$RT" --ignore-not-found --wait=true --timeout=120s
kubectl -n "$NS" delete dataset "$RT" --ignore-not-found --wait=true --timeout=120s
kubectl delete cacheruntimeclass "$RT" --ignore-not-found
kubectl apply -f "${HERE}/00-cacheruntimeclass.yaml"
kubectl -n "$NS" apply -f "${HERE}/01-dataset.yaml"

say "STEP 1  start the manager under test"
echo "binary: $BIN"
"$BIN" start --development=true --enable-leader-election=false --metrics-addr=:0 > "$MGRLOG" 2>&1 &
MGR=$!
trap 'kill $MGR 2>/dev/null' EXIT
sleep 8
kill -0 "$MGR" 2>/dev/null || { echo "FATAL: manager died"; tail -30 "$MGRLOG"; exit 1; }

kubectl -n "$NS" apply -f "${HERE}/02-cacheruntime.yaml"

say "STEP 2  wait for the initial Bound"
if wait_for phase Bound 300; then
  echo "OK: reached Bound"
else
  echo "FATAL: never reached Bound (phase=$(phase))"; tail -30 "$MGRLOG"; exit 1
fi
# Confirm the reportSummary exec actually succeeds now (cmdguard-clean command),
# otherwise the latency story below would be unrealistic.
echo -n "reportSummary execs completing: "
grep -ac "Exec command finished" "$MGRLOG" 2>/dev/null || echo 0

say "STEP 3  drive $FLAPS flaps, ${DWELL}s dwell each"
BASE=$(execs)
echo "exec attempts before flapping: $BASE"
T0=$(date +%s)
for i in $(seq 1 "$FLAPS"); do
  kubectl -n "$NS" exec "$WORKER_POD" -c worker -- rm -f /tmp/ready >/dev/null 2>&1
  wait_for wready false 20 || echo "  flap $i: worker never went NotReady"
  sleep "$DWELL"
  p_down=$(phase)
  kubectl -n "$NS" exec "$WORKER_POD" -c worker -- touch /tmp/ready >/dev/null 2>&1
  wait_for wready true 20 || echo "  flap $i: worker never came back Ready"
  sleep "$DWELL"
  printf '  flap %d: phase-while-down=%s phase-after=%s exec-attempts=%s\n' \
    "$i" "$p_down" "$(phase)" "$(( $(execs) - BASE ))"
done
T1=$(date +%s)
ELAPSED=$(( T1 - T0 ))

say "STEP 4  settle and report"
sleep 20
TOTAL=$(( $(execs) - BASE ))
RESTORES=$(grep -ac "restoring dataset phase from Failed to Bound" "$MGRLOG" 2>/dev/null || echo 0)
WINDOWS=$(( ELAPSED / 5 + 1 ))   # 5s == defaultSyncRetryDuration

say "RESULT ($LABEL)"
{
  echo "label:                    $LABEL"
  echo "binary:                   $BIN"
  echo "flaps:                    $FLAPS (dwell ${DWELL}s each)"
  echo "flap_window_seconds:      $ELAPSED"
  echo "limiter_windows_elapsed:  ~$WINDOWS  (at defaultSyncRetryDuration=5s)"
  echo "exec_attempts:            $TOTAL"
  echo "restore_branch_hits:      $RESTORES"
  echo "final_phase:              $(phase)"
  echo
  echo "A throttled build cannot exceed ~limiter_windows_elapsed exec attempts."
  echo "exec_attempts >> that bound is the F1 amplification, measured live."
} | tee "${OUT}/summary.txt"

say "exec attempt timestamps"
grep -a "Exec command start" "$MGRLOG" | awk '{print $1}' | tee "${OUT}/exec-times.txt" | tail -20

kill "$MGR" 2>/dev/null; wait "$MGR" 2>/dev/null
echo "artifacts in ${OUT}/"
