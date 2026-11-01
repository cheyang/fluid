#!/usr/bin/env bash
# Extracts scale_worker_and_verify from the PR-head test.sh and runs it against
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
echo "stub-harness: $pass passed, $((5-pass)) failed"
[[ $fail == 0 ]]
