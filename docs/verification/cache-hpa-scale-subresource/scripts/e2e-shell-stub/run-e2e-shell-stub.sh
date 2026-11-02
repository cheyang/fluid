#!/usr/bin/env bash
# Extracts scale_worker_and_verify and verify_scale_subresource from the PR-head
# test.sh and runs them against
# the stubbed kubectl. Validates: (1) write assertions pass when the write lands,
# for both methods, with pod checks off; (2) a missing write panics; (3) a wrong
# scale-GET spec.replicas panics; (4) the full pod-verify path still converges.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
SRC="${TEST_SH:?path to head test.sh}"
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT

# extract helpers + function under test
{
  echo 'testname="stub"'; echo 'dataset_name="curvine-demo"'
  sed -n '/^function syslog()/,/^}/p' "$SRC"
  sed -n '/^function panic()/,/^}/p' "$SRC"
  sed -n '/^function scale_worker_and_verify()/,/^}/p' "$SRC"
  sed -n '/^function verify_scale_subresource()/,/^}/p' "$SRC"
} > "$WORK/fn.sh"

mkdir -p "$WORK/bin" "$WORK/state"
cp "$HERE/kubectl-stub.sh" "$WORK/bin/kubectl"; chmod +x "$WORK/bin/kubectl"
export PATH="$WORK/bin:$PATH" STUB_STATE="$WORK/state" STUB_NODES=1
pass=0; fail=0
check(){ # name, expect_rc, expect_log_substr, cmd...
  local name="$1" want_rc="$2" want_log="$3"; shift 3
  local out rc
  out="$(bash -c "source '$WORK/fn.sh'; $*" 2>&1)"; rc=$?
  if [[ "$rc" == "$want_rc" && "$out" == *"$want_log"* ]]; then
    echo "PASS: $name"; pass=$((pass+1))
  else
    echo "FAIL: $name (rc=$rc want=$want_rc)"; echo "$out" | tail -3; fail=$((fail+1))
  fi
}

echo 1 > "$STUB_STATE/spec_replicas"
check "scale write asserted on CR, pods skipped" 0 "Worker scale write to 2 verified on the CR" \
  'scale_worker_and_verify 2 scale false'
check "patch write asserted on CR, pods skipped" 0 "Worker scale write to 1 verified on the CR" \
  'scale_worker_and_verify 1 patch false'
echo 1 > "$STUB_STATE/spec_replicas"
check "full pod-verify path converges" 0 "Worker scaled to 2 replicas successfully" \
  'scale_worker_and_verify 2 scale true'
# fault injection: scale-GET reports wrong spec.replicas -> panic
echo 2 > "$STUB_STATE/spec_replicas"
check "scale-GET mismatch panics" 1 "scale subresource spec.replicas mismatch: expected 2, got 7" \
  'STUB_SCALE_SPEC_REPLICAS=7; export STUB_SCALE_SPEC_REPLICAS; scale_worker_and_verify 2 patch false'
# fault injection: write never lands (state frozen at 9) -> panic
check "spec write mismatch panics" 1 "cacheruntime spec.worker.replicas mismatch: expected 3, got 9" \
  'kubectl(){ if [[ "$*" == get* ]]; then echo 9; fi; }; scale_worker_and_verify 3 patch false'
# --- verify_scale_subresource (round-5 delta: status.replicas asserted against
# --- selector-matched pod count instead of the CR's own field) ---------------
# happy path: CR currentReplicas, selector-matched pods, and scale status all agree
echo 1 > "$STUB_STATE/spec_replicas"; echo 1 > "$STUB_STATE/current_replicas"; echo 1 > "$STUB_STATE/pod_count"
check "GET-side contract agrees with selector-matched pods" 0 "status.replicas=1 selector-matched pods"   'verify_scale_subresource'
# anti-circularity: a controller publishing a wrong-but-stable count must be caught
echo 3 > "$STUB_STATE/current_replicas"
check "stale currentReplicas caught (non-circular)" 1 "status.worker.currentReplicas (3) != selector-matched pod count (1)"   'verify_scale_subresource'
# plumbing break: API server serves a status.replicas that matches neither pods nor CR
echo 1 > "$STUB_STATE/current_replicas"
check "scale-GET status.replicas mismatch panics" 1 "scale subresource status.replicas != selector-matched pod count 1"   'STUB_SCALE_STATUS_REPLICAS=7; export STUB_SCALE_STATUS_REPLICAS; verify_scale_subresource'
# zero-pod edge: pod_count falls back to 0 and a consistent 0 passes
echo 0 > "$STUB_STATE/current_replicas"; echo 0 > "$STUB_STATE/pod_count"
check "zero selector-matched pods handled" 0 "status.replicas=0 selector-matched pods"   'verify_scale_subresource'
echo "stub-harness: $pass passed, $((9-pass)) failed"
[[ $fail == 0 ]]
