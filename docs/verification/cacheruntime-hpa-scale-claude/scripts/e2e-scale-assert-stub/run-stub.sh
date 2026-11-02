#!/usr/bin/env bash
# run-stub.sh — prove the e2e scale-GET assertion in test/gha-e2e/curvine/test.sh
# actually bites, without a cluster.
#
# It extracts the REAL verify_scale_subresource function verbatim from a given
# test.sh (default: the PR head's), serves a canned cluster state through a
# stubbed kubectl, and runs three scenarios:
#
#   S1  contract / green     — CR currentReplicas == selector-matched pods and the
#                              scale payload agrees: the function must pass.
#   S2  bites (the masked    — the controller reports a wrong-but-STABLE
#       bug class)             currentReplicas (2) while only 1 pod exists. The
#                              round-4 (circular) assertion PASSED this scenario;
#                              the fixed one must panic.
#   S3  bites                 — scale payload status.replicas disagrees with the
#                              selector-matched pod count: must panic.
#
# Differential control: pass --old to run the same S2 scenario against the
# function extracted from the PREVIOUS head's test.sh (99e35af0) — it passes
# there, which is exactly the bug class the round-4 circular check masked.
#
# Usage: bash run-stub.sh [--testsh <path-or-git-ref>] [--old]
#   --testsh  source of test.sh; a file path, or "git:<ref>" resolved in the
#             enclosing repo (default git:73ff9ba3, the current PR head)
#   --old     additionally run the S2 scenario against git:99e35af0 (previous head)
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# HERE = <repo>/docs/verification/cacheruntime-hpa-scale-claude/scripts/e2e-scale-assert-stub
REPO="$(cd "$HERE/../../../../.." && pwd)"

NEW_REF="HEAD"   # default: the worktree/checkout under test (re-verify.sh grafts the harness onto the fixed ref)
OLD_REF="99e35af0eef60f077bfefc519e328d1e31252401"  # pinned differential control: the round-4 head (circular assertion)

TESTSH=""
RUN_OLD=0
while [ $# -gt 0 ]; do
  case "$1" in
    --testsh) TESTSH="$2"; shift 2;;
    --old) RUN_OLD=1; shift;;
    -h|--help) sed -n '2,25p' "$0"; exit 0;;
    *) echo "unexpected arg: $1" >&2; exit 2;;
  esac
done
[ -n "$TESTSH" ] || TESTSH="git:$NEW_REF"

resolve_testsh() {
  local spec="$1"
  if [[ "$spec" == git:* ]]; then
    git -C "$REPO" show "${spec#git:}:test/gha-e2e/curvine/test.sh"
  else
    cat "$spec"
  fi
}

extract_fn() {  # extract_fn <testsh-content> -> prints verify_scale_subresource verbatim
  sed -n '/^function verify_scale_subresource() {/,/^}$/p'
}

# --- shared stub environment -------------------------------------------------
export SCALE_NAME="curvine-demo"
SELECTOR="cacheruntime.fluid.io/component-name=${SCALE_NAME}-worker,cacheruntime.fluid.io/name=${SCALE_NAME}"

BIN="$(mktemp -d)"
STATE="$(mktemp -d)"
cp "$HERE/kubectl-stub" "$BIN/kubectl"
chmod +x "$BIN/kubectl"
export PATH="$BIN:$PATH"
export STUB_STATE="$STATE"

write_state() {  # write_state <current_replicas> <spec_replicas> <scale_status_replicas> <pod_lines...>
  local cur="$1" spec="$2" scale_rep="$3"; shift 3
  printf '%s' "$cur"      > "$STATE/current_replicas"
  printf '%s' "$spec"     > "$STATE/spec_replicas"
  printf '%s' "$SELECTOR" > "$STATE/selector"
  {
    printf '{"kind":"Scale","apiVersion":"autoscaling/v1",'
    printf '"metadata":{"name":"%s","namespace":"default"},' "$SCALE_NAME"
    printf '"spec":{"replicas":%s},' "$spec"
    printf '"status":{"replicas":%s,"selector":"%s"}}' "$scale_rep" "$SELECTOR"
  } > "$STATE/scale.json"
  : > "$STATE/pods"
  local line
  for line in "$@"; do
    [ -n "$line" ] && printf '%s\n' "$line" >> "$STATE/pods"
  done
}

# The real test.sh helpers the extracted function depends on. panic exits 1 so a
# scenario's failure is observable as the subshell's exit code.
testname="stub"
dataset_name="$SCALE_NAME"
function syslog() { echo ">>> $1"; }
function panic() { echo ">>> test \"stub\" failed: $1" >&2; exit 1; }

run_scenario() {  # run_scenario <label> <fn-source-file> <cur> <spec> <scale_rep> <pod_lines...>
  local label="$1" fns="$2"; shift 2
  write_state "$@"
  (
    # shellcheck disable=SC1090
    source "$fns"
    verify_scale_subresource
  )
  local rc=$?
  echo "scenario $label -> exit $rc"
  return $rc
}

# --- extract the functions ---------------------------------------------------
FNS_NEW="$(mktemp)"
extract_fn < <(resolve_testsh "$TESTSH") > "$FNS_NEW"
if ! grep -q 'verify_scale_subresource' "$FNS_NEW"; then
  echo "FATAL: could not extract verify_scale_subresource from $TESTSH" >&2
  exit 2
fi

PASS=0; FAIL=0
record() {  # record <ok|bad> <label>
  if [ "$1" = ok ]; then PASS=$((PASS+1)); else FAIL=$((FAIL+1)); fi
  printf 'STUB\t%s\t%s\n' "$2" "$1"
}

echo "== extracted verify_scale_subresource from $TESTSH =="
echo "(function body sourced verbatim: $(wc -l < "$FNS_NEW") lines)"

# S1: green — everything consistent: 1 pod, CR says 1, scale says 1
if run_scenario "S1-green (1 pod, CR=1, scale=1)" "$FNS_NEW" 1 1 1 "pod/${SCALE_NAME}-worker-0"; then
  record ok "S1-green"
else
  record bad "S1-green"
  echo "  UNEXPECTED: green path must pass"
fi

# S2: the masked bug class — controller reports a WRONG-but-stable currentReplicas (2)
# with only 1 real pod; the scale payload (2) mirrors the lying CR field, exactly as
# the real API server would (status.replicas is filled from statusReplicasPath).
rc=0
run_scenario "S2-controller-lies (CR=2, scale=2, pods=1)" "$FNS_NEW" 2 1 2 "pod/${SCALE_NAME}-worker-0" || rc=$?
if [ $rc -ne 0 ]; then record ok "S2-controller-lies"; else record bad "S2-controller-lies"; echo "  UNEXPECTED: the fixed assertion must catch a lying controller"; fi

# S3: scale payload disagrees with the selector-matched pod count
rc=0
run_scenario "S3-scale-payload-lies (scale status=3, pods=1)" "$FNS_NEW" 1 1 3 "pod/${SCALE_NAME}-worker-0" || rc=$?
if [ $rc -ne 0 ]; then record ok "S3-scale-payload-lies"; else record bad "S3-scale-payload-lies"; echo "  UNEXPECTED: the fixed assertion must catch a lying scale payload"; fi

# Differential control: previous head's (circular) function must PASS S2
if [ "$RUN_OLD" = 1 ]; then
  FNS_OLD="$(mktemp)"
  extract_fn < <(resolve_testsh "git:$OLD_REF") > "$FNS_OLD"
  if ! grep -q 'verify_scale_subresource' "$FNS_OLD"; then
    echo "FATAL: could not extract verify_scale_subresource from git:$OLD_REF" >&2
    exit 2
  fi
  rc=0
  echo "== differential control: previous head $OLD_REF (circular assertion) =="
  run_scenario "S2-controller-lies on PREVIOUS head" "$FNS_OLD" 2 1 2 "pod/${SCALE_NAME}-worker-0" || rc=$?
  if [ $rc -eq 0 ]; then
    echo "  (as expected: the round-4 circular assertion passed a lying controller — that is the masked bug class)"
    record ok "S2-on-previous-head-99e35af0"
  else
    echo "  UNEXPECTED: old function was expected to pass S2"
    record bad "S2-on-previous-head-99e35af0"
  fi
fi

echo
echo "RESULT: pass=$PASS fail=$FAIL"
[ "$FAIL" -eq 0 ] || exit 1