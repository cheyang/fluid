#!/usr/bin/env bash
# Contract harness for PR #6292 (tools/diagnose-fluid-*.sh kubectl_resource fix).
#
# Layer: L1 (deterministic, PATH-stubbed kubectl/helm — no cluster needed).
# Polarity: CONTRACT tests. On the buggy (base) code they FAIL; on the fixed
# (head) code they PASS. A red result *is* the reproduction of the bug.
#
# Usage: run_harness.sh <repo-checkout-dir> [<results-dir>]
# Exit 0 iff every check passes.
set -u

REPO_DIR="${1:?usage: run_harness.sh <repo-checkout-dir> [<results-dir>]}"
HARNESS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STUB_DIR="$HARNESS_ROOT/scripts/stubs"
RESULTS_DIR="${2:-$(mktemp -d)}"
mkdir -p "$RESULTS_DIR"

WORK="$(mktemp -d)"
export STUB_LOG="$WORK/kubectl-stub.log"
: >"$STUB_LOG"

RUNTIME_NAME="demo"
RUNTIME_NS="ns1"

# script-suffix -> kubectl resource kind for the runtime describe call
SCRIPTS=(alluxio curvine jindo juicefs)
declare -A KINDS=(
  [alluxio]=alluxioruntime
  [curvine]=cacheruntime
  [jindo]=jindoruntime
  [juicefs]=juicefsruntime
)

cleanup() {
  rm -rf "$WORK" 2>/dev/null || true
  # remove only diagnose dirs this harness created (newest first, guard the name shape)
  for d in $(ls -t -d /tmp/diagnose_fluid_[0-9]* 2>/dev/null); do
    [ -d "$d" ] && rm -rf "$d"
  done
}
trap cleanup EXIT

failures=0
report() { # <status> <message>
  printf '%s: %s\n' "$1" "$2"
  [ "$1" = "PASS" ] || failures=$((failures + 1))
}

# ---------------------------------------------------------------- AC2: syntax
for s in "${SCRIPTS[@]}"; do
  if bash -n "$REPO_DIR/tools/diagnose-fluid-$s.sh"; then
    report PASS "AC2 bash -n diagnose-fluid-$s.sh"
  else
    report FAIL "AC2 bash -n diagnose-fluid-$s.sh"
  fi
done

# ---------------------------------------------- AC3: no ${name} left in tools
if git -C "$REPO_DIR" grep -nF '${name}' -- 'tools/*.sh' >/dev/null 2>&1; then
  report FAIL "AC3 no unset \${name} expansion left under tools/"
else
  report PASS "AC3 no unset \${name} expansion left under tools/"
fi

# ---------------------------------------------- F1: contract on the 4 scripts
for s in "${SCRIPTS[@]}"; do
  kind="${KINDS[$s]}"
  out_tar="$WORK/out-$s.tar.gz"
  (
    cd "$WORK"
    PATH="$STUB_DIR:$PATH" \
      bash "$REPO_DIR/tools/diagnose-fluid-$s.sh" collect \
      -r "$RUNTIME_NAME" -n "$RUNTIME_NS" --collect-path "$out_tar" \
      >"$WORK/collect-$s.log" 2>&1
  )
  rc=$?

  # (a) the runtime describe call carries the runtime name
  if grep -qx "kubectl: describe $kind --namespace $RUNTIME_NS $RUNTIME_NAME" "$STUB_LOG"; then
    report PASS "F1 $kind describe carries runtime name '$RUNTIME_NAME'"
  else
    report FAIL "F1 $kind describe carries runtime name '$RUNTIME_NAME' (log: $(grep "describe $kind " "$STUB_LOG" | tr '\n' ';'))"
  fi

  # (b) the buggy form (describe without a resource name) must NOT appear
  if grep -qx "kubectl: describe $kind --namespace $RUNTIME_NS" "$STUB_LOG"; then
    report FAIL "F1 $kind describe is still called without a resource name"
  else
    report PASS "F1 $kind describe is never called without a resource name"
  fi

  # (c) the archive holds <kind>-<runtime_name>.yaml produced by the named describe
  extract="$WORK/extract-$s"
  mkdir -p "$extract"
  if tar -xzf "$out_tar" -C "$extract" 2>/dev/null; then
    got="$(find "$extract" -name "$kind-$RUNTIME_NAME.yaml" | head -1)"
    if [ -n "$got" ] && grep -q "stub-describe: describe $kind --namespace $RUNTIME_NS $RUNTIME_NAME" "$got"; then
      report PASS "F1 archive contains $kind-$RUNTIME_NAME.yaml from the named describe"
    else
      report FAIL "F1 archive contains $kind-$RUNTIME_NAME.yaml from the named describe (got: '${got:-nothing}')"
    fi
  else
    report FAIL "F1 archive for $s could be extracted (rc=$rc, tar missing?)"
  fi

  # reset the shared log so per-script greps stay unambiguous
  : >"$STUB_LOG"
  cp "$WORK/collect-$s.log" "$RESULTS_DIR/collect-$s.log" 2>/dev/null || true

  # the scripts reuse /tmp/diagnose_fluid_<epoch>; avoid same-second collisions
  sleep 1
done

printf 'harness result: %s (%d failure(s))\n' \
  "$([ $failures -eq 0 ] && echo ALL-PASS || echo FAILURES)" "$failures"
exit $([ $failures -eq 0 ] && echo 0 || echo 1)
