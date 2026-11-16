#!/usr/bin/env bash
# Verification scenario for fluid PR #6256 (release-scoped cluster RBAC names).
#
# Runs helm 3 against a throwaway envtest kube-apiserver and checks:
#   P0  premise: on the BASE chart, a second helm install fails with the
#       ownership conflict reported in #6134 (contract: it MUST fail on base).
#   F1  incomplete fix: on the HEAD chart, a second helm install with default
#       values STILL fails (contract for the "Fixes #6134" claim: it MUST NOT
#       fail; a failure here confirms F1).
#   F1b same with --set namespace=test (proper namespaced isolation).
#   F1c second install succeeds once csi/driver.yaml and
#       webhook/webhookconfiguration.yaml are removed (proves the RBAC rename
#       itself is complete and the remaining blockers are exactly those two
#       fixed-name cluster-scoped objects).
#   F2  upgrade: base release "fluid" upgraded to HEAD chart swaps the
#       ClusterRole(Binding) names (delete+create, not in-place rename) and
#       the new bindings still reference the same ServiceAccounts.
#
# Usage: scenario.sh [--head-ref <git ref>] [--base-ref <sha>] [--results <dir>]
# Exit code: 0 iff every expectation held.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(cd "$TOPIC_DIR/../../.." && pwd)"
BASE_SHA="f432bdc87ac35941763b74e8e87683c7a84de5d9"   # merge-base recorded at review time
HEAD_REF="HEAD"
RESULTS="$TOPIC_DIR/results"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --head-ref) HEAD_REF="$2"; shift 2;;
    --base-ref) BASE_SHA="$2"; shift 2;;
    --results)  RESULTS="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

HEAD_SHA="$(git -C "$REPO_ROOT" rev-parse "$HEAD_REF")"
mkdir -p "$RESULTS"
LOG="$RESULTS/scenario.log"
: > "$LOG"

log() { echo "$@" | tee -a "$LOG"; }

# --- locate envtest assets ---------------------------------------------------
if [[ -z "${KUBEBUILDER_ASSETS:-}" ]]; then
  KUBEBUILDER_ASSETS="$(ls -d "$HOME"/.local/share/kubebuilder-envtest/k8s/*-linux-amd64 2>/dev/null | sort -V | tail -1)"
fi
if [[ -z "${KUBEBUILDER_ASSETS:-}" || ! -x "$KUBEBUILDER_ASSETS/kube-apiserver" ]]; then
  log "FATAL: envtest assets not found (set KUBEBUILDER_ASSETS)"; exit 2
fi
export KUBEBUILDER_ASSETS
log "using KUBEBUILDER_ASSETS=$KUBEBUILDER_ASSETS"
log "helm: $(helm version --short 2>/dev/null || helm version)"

# --- materialize charts ------------------------------------------------------
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"; [[ -n "${ENVTEST_PID:-}" ]] && kill "$ENVTEST_PID" 2>/dev/null' EXIT
git -C "$REPO_ROOT" archive "$BASE_SHA" charts/fluid/fluid | tar -x -C "$WORK"
mv "$WORK/charts/fluid/fluid" "$WORK/base-chart"
rm -rf "$WORK/charts"
git -C "$REPO_ROOT" archive "$HEAD_SHA" charts/fluid/fluid | tar -x -C "$WORK"
mv "$WORK/charts/fluid/fluid" "$WORK/head-chart"
rm -rf "$WORK/charts"
# trimmed head chart: drop the two fixed-name cluster-scoped templates
cp -r "$WORK/head-chart" "$WORK/head-chart-trimmed"
rm "$WORK/head-chart-trimmed/templates/csi/driver.yaml" \
   "$WORK/head-chart-trimmed/templates/webhook/webhookconfiguration.yaml"

# --- start envtest apiserver -------------------------------------------------
( cd "$REPO_ROOT" && go build -o "$WORK/envtest-kubeconfig" ./docs/verification/rbac-release-scoped-codex/harness/envtest-kubeconfig ) || { log "FATAL: helper build failed"; exit 2; }
"$WORK/envtest-kubeconfig" "$WORK/kubeconfig" > "$WORK/envtest.out" 2>&1 &
ENVTEST_PID=$!
export KUBECONFIG="$WORK/kubeconfig"
for i in $(seq 1 90); do [[ -s "$WORK/kubeconfig" ]] && grep -q ENVTEST_READY "$WORK/envtest.out" && break; sleep 1; done
kubectl --request-timeout=10s get --raw=/healthz > /dev/null || { log "FATAL: apiserver not healthy"; cat "$WORK/envtest.out"; exit 2; }
log "envtest apiserver up"

PASS=0; FAIL=0
declare -a SUMMARY
check() { # $1=id $2=expectation-description $3=0/1 (0 = expectation held)
  if [[ "$3" -eq 0 ]]; then PASS=$((PASS+1)); SUMMARY+=("PASS  $1  $2");
  else FAIL=$((FAIL+1)); SUMMARY+=("FAIL  $1  $2"); fi
}

cleanup_release() { # best-effort
  helm uninstall "$1" -n "$2" --wait --timeout 60s >>"$LOG" 2>&1 || true
}

# ============================== P0 (premise, BASE chart) =====================
log "=== P0: premise on BASE chart ($BASE_SHA) ==="
helm install fluid "$WORK/base-chart" -n fluid-system --create-namespace > "$RESULTS/p0-install1.log" 2>&1
check P0.install1 "base: first release installs" $?

helm install fluid-1 "$WORK/base-chart" -n test --create-namespace > "$RESULTS/p0-install2.log" 2>&1
rc=$?
grep -q "cannot be imported into the current release" "$RESULTS/p0-install2.log"
grep_hit=$?
if [[ $rc -ne 0 && $grep_hit -eq 0 ]]; then
  check P0.install2 "base: second release FAILS with ownership conflict (premise holds)" 0
else
  check P0.install2 "base: second release FAILS with ownership conflict (premise holds)" 1
fi
grep -m1 -E "ClusterRole|cannot be imported" "$RESULTS/p0-install2.log" | tee -a "$LOG" || true
cleanup_release fluid-1 test

# P0b: base chart with per-release namespace isolation -> must fail on the
# exact object the issue reported (ClusterRole alluxioruntime-controller)
helm install fluid-1 "$WORK/base-chart" -n test --create-namespace --set namespace=test > "$RESULTS/p0b-install2.log" 2>&1
rc=$?
if [[ $rc -ne 0 ]] && grep -q 'ClusterRole "alluxioruntime-controller"' "$RESULTS/p0b-install2.log"; then
  check P0b "base with --set namespace=test: second release FAILS on ClusterRole alluxioruntime-controller (matches issue #6134 exactly)" 0
else
  check P0b "base with --set namespace=test: second release FAILS on ClusterRole alluxioruntime-controller (matches issue #6134 exactly)" 1
fi
grep -m1 "cannot be imported" "$RESULTS/p0b-install2.log" | tee -a "$LOG" || true
cleanup_release fluid-1 test
cleanup_release fluid fluid-system

# ============================== F1 (HEAD chart) ==============================
log "=== F1: second install on HEAD chart ($HEAD_SHA) ==="
helm install fluid "$WORK/head-chart" -n fluid-system --create-namespace > "$RESULTS/f1-install1.log" 2>&1
check F1.install1 "head: first release installs" $?

# F1a: exact issue repro (no --set namespace; namespaced objects default to fluid-system)
helm install fluid-1 "$WORK/head-chart" -n test --create-namespace > "$RESULTS/f1a-install2.log" 2>&1
rc=$?
if [[ $rc -ne 0 ]] && grep -q "cannot be imported into the current release" "$RESULTS/f1a-install2.log"; then
  check F1a "head: issue repro (no values override) still FAILS (fix incomplete)" 0
else
  check F1a "head: issue repro (no values override) still FAILS (fix incomplete)" 1
fi
grep -m1 "cannot be imported" "$RESULTS/f1a-install2.log" | tee -a "$LOG" || true
cleanup_release fluid-1 test

# F1b: with per-release namespace isolation
helm install fluid-1 "$WORK/head-chart" -n test --create-namespace --set namespace=test > "$RESULTS/f1b-install2.log" 2>&1
rc=$?
if [[ $rc -ne 0 ]] && grep -qE "csidrivers.storage.k8s.io|CSIDriver|fuse.csi.fluid.io|mutatingwebhookconfiguration|fluid-pod-admission-webhook" "$RESULTS/f1b-install2.log"; then
  check F1b "head: second release with --set namespace=test still FAILS on fixed-name cluster-scoped object (CSIDriver/MWC)" 0
else
  check F1b "head: second release with --set namespace=test still FAILS on fixed-name cluster-scoped object (CSIDriver/MWC)" 1
fi
grep -m1 -E "cannot be imported|Error:" "$RESULTS/f1b-install2.log" | tee -a "$LOG" || true
cleanup_release fluid-1 test

# F1c: trimmed chart -> second install must succeed (isolates RBAC rename correctness)
helm install fluid-1 "$WORK/head-chart-trimmed" -n test --create-namespace --set namespace=test > "$RESULTS/f1c-install2.log" 2>&1
check F1c "head trimmed (no CSIDriver/MWC templates): second release with --set namespace=test installs OK (RBAC rename itself works)" $?

# assert release-scoped names actually materialized for release fluid vs fluid-1
kubectl get clusterrole fluid-alluxioruntime-controller >>"$LOG" 2>&1 \
  && kubectl get clusterrole fluid-1-alluxioruntime-controller >>"$LOG" 2>&1
check F1.names "both releases' release-scoped ClusterRoles coexist" $?
cleanup_release fluid-1 test
cleanup_release fluid fluid-system

# ============================== F2 (upgrade base -> head) ====================
log "=== F2: upgrade base -> head for release 'fluid' ==="
helm install fluid "$WORK/base-chart" -n fluid-system --create-namespace > "$RESULTS/f2-install.log" 2>&1
check F2.install "base chart installs as release 'fluid'" $?
kubectl get clusterrole alluxioruntime-controller >>"$LOG" 2>&1
check F2.before "old-name ClusterRole alluxioruntime-controller exists before upgrade" $?

helm upgrade fluid "$WORK/head-chart" -n fluid-system --no-hooks > "$RESULTS/f2-upgrade.log" 2>&1
check F2.upgrade "helm upgrade to head chart succeeds" $?

if kubectl get clusterrole alluxioruntime-controller >>"$LOG" 2>&1; then
  check F2.oldgone "old-name ClusterRole alluxioruntime-controller deleted by upgrade" 1
else
  check F2.oldgone "old-name ClusterRole alluxioruntime-controller deleted by upgrade" 0
fi
kubectl get clusterrole fluid-alluxioruntime-controller >>"$LOG" 2>&1
check F2.newexists "new-name ClusterRole fluid-alluxioruntime-controller created by upgrade" $?
kubectl get clusterrolebinding fluid-alluxioruntime-clusterrolebinding -o jsonpath='{.subjects[0].kind}/{.subjects[0].name}/{.subjects[0].namespace}' > "$RESULTS/f2-binding-subjects.txt" 2>>"$LOG"
grep -q "ServiceAccount/alluxioruntime-controller/fluid-system" "$RESULTS/f2-binding-subjects.txt"
check F2.subjects "new ClusterRoleBinding still binds SA alluxioruntime-controller/fluid-system" $?
cleanup_release fluid fluid-system

# ============================== summary ======================================
log ""
log "================ SUMMARY (base=$BASE_SHA head=$HEAD_SHA) ================"
for line in "${SUMMARY[@]}"; do log "$line"; done
log "pass=$PASS fail=$FAIL"
[[ $FAIL -eq 0 ]]
