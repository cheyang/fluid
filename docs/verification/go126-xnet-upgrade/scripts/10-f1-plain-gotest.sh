#!/usr/bin/env bash
# F1 repro: plain `go test` (default build flags, NO -gcflags="all=-N -l")
# on pkg/utils/helm. Polarity: contract — the intended behavior is that the
# package's unit tests pass when invoked directly. On the PR head (go1.26.10)
# they panic; on the base (go1.25.12) they pass. The project's canonical path
# (make test / CI) adds -gcflags="all=-N -l", which masks this.
#
# Usage: 10-f1-plain-gotest.sh <path-to-worktree-at-head> [path-to-worktree-at-base]
set -u
HEAD_WT="${1:?usage: $0 <head-worktree> [base-worktree]}"
BASE_WT="${2:-}"

run_one() {
  local wt="$1" tag="$2" out="$3"
  ( cd "$wt" && FLUID_UNIT_TEST=true go test -count=1 -timeout 5m ./pkg/utils/helm/ ) \
    > "$out" 2>&1
  local rc=$?
  if grep -q "^ok" "$out"; then echo "[$tag] PASS (go test ok)"; else echo "[$tag] FAIL (rc=$rc)"; fi
}

GOTOOLCHAIN=go1.26.10 run_one "$HEAD_WT" "head@go1.26.10" "${RESULTS_DIR:-$(dirname "$0")/../results}/f1-head.log"
if [ -n "$BASE_WT" ]; then
  GOTOOLCHAIN=go1.25.12 run_one "$BASE_WT" "base@go1.25.12" "${RESULTS_DIR:-$(dirname "$0")/../results}/f1-base.log"
fi
# Control: same head code, inlining disabled -> must PASS, proving the
# mechanism is inlining of exec.LookPath at pkg/utils/helm/utils.go:46.
( cd "$HEAD_WT" && GOTOOLCHAIN=go1.26.10 FLUID_UNIT_TEST=true \
    go test -count=1 -timeout 5m -gcflags="all=-N -l" ./pkg/utils/helm/ ) \
  > "${RESULTS_DIR:-$(dirname "$0")/../results}/f1-head-noinline.log" 2>&1
if grep -q "^ok" "${RESULTS_DIR:-$(dirname "$0")/../results}/f1-head-noinline.log"; then
  echo "[head@go1.26.10 + -N -l] PASS (control: inlining is the trigger)"
else
  echo "[head@go1.26.10 + -N -l] FAIL (control failed — mechanism refuted)"
fi
