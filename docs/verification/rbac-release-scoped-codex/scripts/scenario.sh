#!/usr/bin/env bash
# Verification scenario for fluid PR #6256 (opt-in cluster RBAC name prefix).
#
# Round 2 (head da020a3): the PR was re-scoped per round-1 review option (b) —
# it no longer claims "Fixes #6134"; it adds an opt-in value
# rbac.clusterScopedNamePrefix (empty default = historical names preserved).
# This scenario therefore verifies the NEW contract and keeps round-1 checks
# as canaries documenting the explicitly out-of-scope remainder:
#
#   Render layer (no cluster):
#   R1  contract: head default render == base default render, byte-identical
#       (default upgrade is a no-op; no RBAC churn).
#   R2  contract: with distinct prefixes the 24 ClusterRole/ClusterRoleBinding
#       objects are fully disjoint and every binding's roleRef resolves to a
#       prefixed ClusterRole of the same render.
#   R3  canary (characterization): the full cluster-scoped set still contains
#       the fixed-name CSIDriver + MutatingWebhookConfiguration (documented in
#       the PR body as remaining shared). Flips when those become scoped too.
#
#   Integration layer (helm + envtest apiserver):
#   P0  premise (base): second install fails with the #6134 ownership conflict.
#   K1  canary: head second install with default values still fails
#       (out-of-scope remainder; documented in PR body).
#   K2  canary: head second install with --set namespace=test still fails on
#       CSIDriver/MWC fixed names.
#   C1  contract: two releases with DISTINCT prefixes install side by side once
#       the two fixed-name templates are removed (RBAC scope is complete).
#   U1  contract: helm upgrade base -> head with default values preserves the
#       historical object names (no delete/recreate churn).
#   U2  contract: opting an existing release into a prefix swaps old-name
#       non-hook RBAC for prefixed ones; bindings keep the same subjects.
#   U3  canary (N1): pre-upgrade hook RBAC left behind by a failed hooked
#       upgrade is NOT pruned when opting into a prefix (helm never prunes
#       hooks; before-hook-creation matches only the new name).
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

PASS=0; FAIL=0
declare -a SUMMARY
check() { # $1=id $2=expectation-description $3=0/1 (0 = expectation held)
  if [[ "$3" -eq 0 ]]; then PASS=$((PASS+1)); SUMMARY+=("PASS  $1  $2");
  else FAIL=$((FAIL+1)); SUMMARY+=("FAIL  $1  $2"); fi
}

# ============================== render layer =================================
log "=== R: render-level checks (helm template, no cluster) ==="
helm template fluid "$WORK/base-chart" --namespace fluid-system > "$WORK/base-default.yaml" 2>>"$LOG"
helm template fluid "$WORK/head-chart" --namespace fluid-system > "$WORK/head-default.yaml" 2>>"$LOG"
if diff -q "$WORK/base-default.yaml" "$WORK/head-default.yaml" >/dev/null; then
  check R1 "head default render is byte-identical to base (default upgrade = no churn)" 0
else
  check R1 "head default render is byte-identical to base (default upgrade = no churn)" 1
  diff "$WORK/base-default.yaml" "$WORK/head-default.yaml" | head -40 | tee -a "$LOG"
fi

helm template fluid "$WORK/head-chart" --namespace fluid-system --set rbac.clusterScopedNamePrefix=team-a > "$WORK/head-ta.yaml" 2>/dev/null
helm template fluid "$WORK/head-chart" --namespace fluid-system --set rbac.clusterScopedNamePrefix=team-b > "$WORK/head-tb.yaml" 2>/dev/null
python3 - "$WORK" > "$RESULTS/r2-r3-render.txt" <<'PYEOF'
import sys
def cluster_scoped(path):
    import yaml
    out=[]
    for d in open(path).read().split('---'):
        d=d.strip()
        if not d: continue
        try: o=yaml.safe_load(d)
        except Exception: continue
        if not isinstance(o,dict): continue
        k=o.get('kind'); n=(o.get('metadata') or {}).get('name')
        if k in ('ClusterRole','ClusterRoleBinding','CSIDriver','MutatingWebhookConfiguration',
                 'ValidatingWebhookConfiguration','CustomResourceDefinition','StorageClass'):
            out.append((k,n,o))
    return out
work=sys.argv[1]
ta=cluster_scoped(work+'/head-ta.yaml'); tb=cluster_scoped(work+'/head-tb.yaml')
hd=cluster_scoped(work+'/head-default.yaml')
ta_names={(k,n) for k,n,_ in ta}; tb_names={(k,n) for k,n,_ in tb}
rbac_ta={(k,n) for k,n in ta_names if k in ('ClusterRole','ClusterRoleBinding')}
rbac_tb={(k,n) for k,n in tb_names if k in ('ClusterRole','ClusterRoleBinding')}
print("RBAC count tenant-a:", len(rbac_ta), "tenant-b:", len(rbac_tb))
inter=rbac_ta & rbac_tb
print("RBAC intersection:", sorted(inter))
# roleRef resolution within each render
roles={n for k,n in rbac_ta if k=='ClusterRole'}
bad=[(n,o.get('roleRef',{}).get('name')) for k,n,o in ta if k=='ClusterRoleBinding' and o.get('roleRef',{}).get('name') not in roles]
print("bindings with unresolved roleRef (tenant-a):", bad)
unprefixed=[(k,n) for k,n in rbac_ta if not n.startswith('team-a-')]
print("unprefixed cluster RBAC under prefix (tenant-a):", unprefixed)
shared={ (k,n) for k,n in ta_names if k not in ('ClusterRole','ClusterRoleBinding') }
print("non-RBAC cluster-scoped still shared:", sorted(shared))
ok = (len(rbac_ta)==24 and len(rbac_tb)==24 and not inter and not bad and not unprefixed)
print("R2_OK" if ok else "R2_BAD")
canary = ('CSIDriver','fuse.csi.fluid.io') in shared and ('MutatingWebhookConfiguration','fluid-pod-admission-webhook') in shared
print("R3_CANARY_HOLDS" if canary else "R3_CANARY_FLIPPED")
PYEOF
grep -q "^R2_OK" "$RESULTS/r2-r3-render.txt"
check R2 "distinct prefixes render fully disjoint, self-resolving cluster RBAC (24+24)" $?
grep -q "^R3_CANARY_HOLDS" "$RESULTS/r2-r3-render.txt"
check R3 "canary: CSIDriver + MWC remain fixed-name shared singletons (documented remainder)" $?
cat "$RESULTS/r2-r3-render.txt" | tee -a "$LOG"

# ---- R4/R5: prefix validation (round-3 head 64d570d fixes the silent-truncation leg of F9)
P250="$(python3 -c 'print("p"*250)')"
if helm template t1 "$WORK/head-chart" --namespace fluid-system --set rbac.clusterScopedNamePrefix="$P250" > "$RESULTS/r4-overlong.log" 2>&1; then
  check R4.overlong "contract (F9 fixed): 250-char prefix rejected at render with clear message" 1
else
  grep -q "rbac.clusterScopedNamePrefix is too long" "$RESULTS/r4-overlong.log" \
    && grep -q "253-character" "$RESULTS/r4-overlong.log"
  check R4.overlong "contract (F9 fixed): 250-char prefix rejected at render with clear message" $?
fi
# boundary: longest cluster RBAC name is vineyardruntime-clusterrolebinding (34 chars)
P218="$(python3 -c 'print("p"*218)')"
P219="$(python3 -c 'print("p"*219)')"
helm template t1 "$WORK/head-chart" --namespace fluid-system --set rbac.clusterScopedNamePrefix="$P218" >/dev/null 2>&1
ok218=$?
helm template t1 "$WORK/head-chart" --namespace fluid-system --set rbac.clusterScopedNamePrefix="$P219" >/dev/null 2>&1
ok219=$?
if [[ $ok218 -eq 0 && $ok219 -ne 0 ]]; then
  check R4.boundary "contract: exact boundary - prefix 218 renders (max name 253), prefix 219 rejected" 0
else
  check R4.boundary "contract: exact boundary - prefix 218 renders (max name 253), prefix 219 rejected [rc218=$ok218 rc219=$ok219]" 1
fi
# R5 canaries (F9 residual legs, still open nit): pathological inputs unhandled
if helm template t1 "$WORK/head-chart" --namespace fluid-system --set rbac=null > "$RESULTS/r5-null.log" 2>&1; then
  check R5.null "canary (F9 residual): --set rbac=null dies with opaque nil-pointer error" 1
else
  grep -q "nil pointer" "$RESULTS/r5-null.log"
  check R5.null "canary (F9 residual): --set rbac=null dies with opaque nil-pointer error" $?
fi
if helm template t1 "$WORK/head-chart" --namespace fluid-system --set rbac.clusterScopedNamePrefix=foo_bar > "$RESULTS/r5-charset.yaml" 2>&1 \
   && grep -q "foo_bar-alluxioruntime-controller" "$RESULTS/r5-charset.yaml"; then
  check R5.charset "canary (F9 residual): invalid-charset prefix foo_bar renders; fails only at API apply" 0
else
  check R5.charset "canary (F9 residual): invalid-charset prefix foo_bar renders; fails only at API apply" 1
fi

# --- start envtest apiserver -------------------------------------------------
( cd "$REPO_ROOT" && go build -o "$WORK/envtest-kubeconfig" ./docs/verification/rbac-release-scoped-codex/harness/envtest-kubeconfig ) || { log "FATAL: helper build failed"; exit 2; }
"$WORK/envtest-kubeconfig" "$WORK/kubeconfig" > "$WORK/envtest.out" 2>&1 &
ENVTEST_PID=$!
export KUBECONFIG="$WORK/kubeconfig"
for i in $(seq 1 90); do [[ -s "$WORK/kubeconfig" ]] && grep -q ENVTEST_READY "$WORK/envtest.out" && break; sleep 1; done
kubectl --request-timeout=10s get --raw=/healthz > /dev/null || { log "FATAL: apiserver not healthy"; cat "$WORK/envtest.out"; exit 2; }
log "envtest apiserver up"

cleanup_release() { helm uninstall "$1" -n "$2" --wait --timeout 60s >>"$LOG" 2>&1 || true; }

# ============================== P0 (premise, BASE chart) =====================
log "=== P0: premise on BASE chart ($BASE_SHA) ==="
helm install fluid "$WORK/base-chart" -n fluid-system --create-namespace > "$RESULTS/p0-install1.log" 2>&1
check P0.install1 "base: first release installs" $?

helm install fluid-1 "$WORK/base-chart" -n test --create-namespace > "$RESULTS/p0-install2.log" 2>&1
rc=$?
if [[ $rc -ne 0 ]] && grep -q "cannot be imported into the current release" "$RESULTS/p0-install2.log"; then
  check P0.install2 "base: second release FAILS with ownership conflict (premise holds)" 0
else
  check P0.install2 "base: second release FAILS with ownership conflict (premise holds)" 1
fi
cleanup_release fluid-1 test

helm install fluid-1 "$WORK/base-chart" -n test --create-namespace --set namespace=test > "$RESULTS/p0b-install2.log" 2>&1
rc=$?
if [[ $rc -ne 0 ]] && grep -q 'ClusterRole "alluxioruntime-controller"' "$RESULTS/p0b-install2.log"; then
  check P0b "base with --set namespace=test: second release FAILS on ClusterRole alluxioruntime-controller (matches issue #6134 exactly)" 0
else
  check P0b "base with --set namespace=test: second release FAILS on ClusterRole alluxioruntime-controller (matches issue #6134 exactly)" 1
fi
cleanup_release fluid-1 test
cleanup_release fluid fluid-system

# ============================== K1/K2 (head, known limitation) ===============
log "=== K1/K2: second install on HEAD chart ($HEAD_SHA) — documented out-of-scope remainder ==="
helm install fluid "$WORK/head-chart" -n fluid-system --create-namespace > "$RESULTS/k1-install1.log" 2>&1
check K1.install1 "head: first release installs (default names preserved)" $?
kubectl get clusterrole alluxioruntime-controller >>"$LOG" 2>&1
check K1.defaultname "head default: historical ClusterRole name alluxioruntime-controller used (no prefix)" $?

helm install fluid-1 "$WORK/head-chart" -n test --create-namespace > "$RESULTS/k1-install2.log" 2>&1
rc=$?
if [[ $rc -ne 0 ]] && grep -q "cannot be imported into the current release" "$RESULTS/k1-install2.log"; then
  check K1 "canary: head second install with default values still fails (documented limitation)" 0
else
  check K1 "canary: head second install with default values still fails (documented limitation)" 1
fi
grep -m1 "cannot be imported" "$RESULTS/k1-install2.log" | tee -a "$LOG" || true
cleanup_release fluid-1 test

helm install fluid-1 "$WORK/head-chart" -n test --create-namespace --set namespace=test --set rbac.clusterScopedNamePrefix=team-b > "$RESULTS/k2-install2.log" 2>&1
rc=$?
if [[ $rc -ne 0 ]] && grep -qE "CSIDriver|fuse.csi.fluid.io|fluid-pod-admission-webhook" "$RESULTS/k2-install2.log"; then
  check K2 "canary: even with a distinct prefix, second install still fails on CSIDriver/MWC (documented limitation)" 0
else
  check K2 "canary: even with a distinct prefix, second install still fails on CSIDriver/MWC (documented limitation)" 1
fi
grep -m1 -E "cannot be imported|Error:" "$RESULTS/k2-install2.log" | tee -a "$LOG" || true
cleanup_release fluid-1 test
cleanup_release fluid fluid-system

# ============================== C1 (two prefixed releases, trimmed) ==========
log "=== C1: two releases with distinct prefixes (trimmed chart: no CSIDriver/MWC) ==="
helm install fluid "$WORK/head-chart-trimmed" -n fluid-system --create-namespace --set rbac.clusterScopedNamePrefix=team-a > "$RESULTS/c1-install1.log" 2>&1
check C1.install1 "trimmed head: release fluid (prefix team-a) installs" $?
helm install fluid-1 "$WORK/head-chart-trimmed" -n test --create-namespace --set namespace=test --set rbac.clusterScopedNamePrefix=team-b > "$RESULTS/c1-install2.log" 2>&1
check C1.install2 "trimmed head: release fluid-1 (prefix team-b, own namespace) installs" $?
kubectl get clusterrole team-a-alluxioruntime-controller >>"$LOG" 2>&1 \
  && kubectl get clusterrole team-b-alluxioruntime-controller >>"$LOG" 2>&1
check C1.coexist "both releases' prefixed ClusterRoles coexist" $?
kubectl get clusterrolebinding team-b-alluxioruntime-clusterrolebinding -o jsonpath='{.roleRef.name}{" "}{.subjects[0].kind}/{.subjects[0].name}/{.subjects[0].namespace}' > "$RESULTS/c1-binding.txt" 2>>"$LOG"
grep -q "team-b-alluxioruntime-controller ServiceAccount/alluxioruntime-controller/test" "$RESULTS/c1-binding.txt"
check C1.binding "team-b binding roleRef+subject resolve within release fluid-1" $?
cleanup_release fluid-1 test
cleanup_release fluid fluid-system

# ============================== U1 (upgrade base -> head, default) ===========
log "=== U1: upgrade base -> head with default values (no churn) ==="
helm install fluid "$WORK/base-chart" -n fluid-system --create-namespace > "$RESULTS/u1-install.log" 2>&1
check U1.install "base chart installs as release 'fluid'" $?
helm upgrade fluid "$WORK/head-chart" -n fluid-system --no-hooks > "$RESULTS/u1-upgrade.log" 2>&1
check U1.upgrade "helm upgrade base->head (defaults) succeeds" $?
kubectl get clusterrole alluxioruntime-controller >>"$LOG" 2>&1
check U1.names "historical ClusterRole name preserved after upgrade (no rename)" $?
kubectl get clusterrolebinding dataset-clusterrolebinding -o jsonpath='{.subjects[0].name}/{.subjects[0].namespace}' > "$RESULTS/u1-subjects.txt" 2>>"$LOG"
grep -q "dataset-controller/fluid-system" "$RESULTS/u1-subjects.txt"
check U1.subjects "bindings unchanged (SA dataset-controller/fluid-system)" $?
cleanup_release fluid fluid-system

# ============================== U2 (opt-in prefix upgrade) ===================
log "=== U2: opt an existing release into a prefix ==="
helm install fluid "$WORK/head-chart" -n fluid-system --create-namespace > "$RESULTS/u2-install.log" 2>&1
check U2.install "head chart (default) installs as release 'fluid'" $?
helm upgrade fluid "$WORK/head-chart" -n fluid-system --no-hooks --set rbac.clusterScopedNamePrefix=t1 > "$RESULTS/u2-upgrade.log" 2>&1
check U2.upgrade "helm upgrade opting into prefix t1 succeeds" $?
if kubectl get clusterrole alluxioruntime-controller >>"$LOG" 2>&1; then
  check U2.oldpruned "old-name non-hook ClusterRole alluxioruntime-controller pruned" 1
else
  check U2.oldpruned "old-name non-hook ClusterRole alluxioruntime-controller pruned" 0
fi
kubectl get clusterrole t1-alluxioruntime-controller >>"$LOG" 2>&1
check U2.newexists "prefixed ClusterRole t1-alluxioruntime-controller created" $?
kubectl get clusterrolebinding t1-alluxioruntime-clusterrolebinding -o jsonpath='{.roleRef.name}{" "}{.subjects[0].kind}/{.subjects[0].name}/{.subjects[0].namespace}' > "$RESULTS/u2-subjects.txt" 2>>"$LOG"
grep -q "t1-alluxioruntime-controller ServiceAccount/alluxioruntime-controller/fluid-system" "$RESULTS/u2-subjects.txt"
check U2.subjects "prefixed binding roleRef+subject resolve to same SA" $?
cleanup_release fluid fluid-system

# ============================== U3 (N1: orphaned hook RBAC) ==================
log "=== U3: failed hooked upgrade leaves hook RBAC; prefix opt-in does not prune it ==="
helm install fluidup "$WORK/base-chart" -n fluid-system --create-namespace > "$RESULTS/u3-install.log" 2>&1
check U3.install "base chart installs as release 'fluidup'" $?
# hooked upgrade; envtest has no kubelet so the hook Job never completes -> timeout leaves hook objects
helm upgrade fluidup "$WORK/base-chart" -n fluid-system --timeout 25s > "$RESULTS/u3-hooked-upgrade.log" 2>&1
rc=$?
kubectl get clusterrole fluid-crds-upgrade >>"$LOG" 2>&1
leftover=$?
if [[ $rc -ne 0 && $leftover -eq 0 ]]; then
  check U3.setup "hooked upgrade times out leaving hook ClusterRole fluid-crds-upgrade behind (precondition)" 0
else
  check U3.setup "hooked upgrade times out leaving hook ClusterRole fluid-crds-upgrade behind (precondition) [rc=$rc leftover=$leftover]" 1
fi
helm upgrade fluidup "$WORK/head-chart" -n fluid-system --no-hooks --set rbac.clusterScopedNamePrefix=t1 > "$RESULTS/u3-prefix-upgrade.log" 2>&1
check U3.upgrade "prefix opt-in upgrade (--no-hooks) of failed release succeeds" $?
if kubectl get clusterrole alluxioruntime-controller >>"$LOG" 2>&1; then
  check U3.mainpruned "old-name non-hook ClusterRole pruned by prefix opt-in" 1
else
  check U3.mainpruned "old-name non-hook ClusterRole pruned by prefix opt-in" 0
fi
kubectl get clusterrole t1-alluxioruntime-controller >>"$LOG" 2>&1
check U3.newexists "prefixed ClusterRole t1-alluxioruntime-controller created" $?
if kubectl get clusterrole fluid-crds-upgrade >>"$LOG" 2>&1; then
  check U3.canary "canary: orphaned hook ClusterRole fluid-crds-upgrade NOT pruned by prefix opt-in" 0
else
  check U3.canary "canary: orphaned hook ClusterRole fluid-crds-upgrade NOT pruned by prefix opt-in" 1
fi
kubectl get clusterrolebinding fluid-crds-upgrade >>"$LOG" 2>&1 && \
  kubectl get clusterrolebinding fluid-crds-upgrade -o jsonpath='{.subjects[0].name}/{.subjects[0].namespace}' > "$RESULTS/u3-orphan-binding.txt" 2>>"$LOG"
kubectl delete clusterrole fluid-crds-upgrade >>"$LOG" 2>&1 || true
kubectl get clusterrolebinding fluid-crds-upgrade >>"$LOG" 2>&1 && kubectl delete clusterrolebinding fluid-crds-upgrade >>"$LOG" 2>&1 || true
cleanup_release fluidup fluid-system

# ============================== summary ======================================
log ""
log "================ SUMMARY (base=$BASE_SHA head=$HEAD_SHA) ================"
for line in "${SUMMARY[@]}"; do log "$line"; done
log "pass=$PASS fail=$FAIL"
[[ $FAIL -eq 0 ]]
