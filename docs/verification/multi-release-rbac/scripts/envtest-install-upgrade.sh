#!/usr/bin/env bash
# envtest-install-upgrade.sh — integration layer for PR #6256 round 2.
#
# Runs real `helm install` / `helm upgrade` against a throwaway kube-apiserver
# (kubebuilder envtest assets) to prove, at the API-server level:
#
#   D (contract, default no-op, run on a FRESH cluster): upgrading a release
#      from BASE chart to HEAD chart with DEFAULT values performs ZERO
#      cluster RBAC creates/deletes (no rename, no churn) and leaves the
#      ClusterRole set unchanged. Run first because it needs the only clean
#      cluster this script ever gets (any fluid install claims the fixed
#      names). Afterwards the control release is uninstalled and its
#      cluster-scoped RBAC removed.
#
#   A (setup): install release 'fluid' from the BASE chart, default values —
#      the pre-PR state of a real deployment.
#
#   B (canary, F1 residual): a SECOND release 'fluid-b' from the HEAD chart
#      with an opt-in prefix still cannot be installed: a fixed-name
#      cluster-scoped singleton (CSIDriver fuse.csi.fluid.io) is owned by the
#      first release and Helm refuses to import it — the exact #6134 error.
#      Documents that the opt-in prefix is an RBAC-only mitigation, exactly as
#      the PR body declares ("not complete multi-release support").
#
#   C (contract, upgrade mechanics): upgrade 'fluid' BASE -> HEAD with an
#      opt-in prefix. Uses --no-hooks: the chart's pre-upgrade hook is a Job
#      that needs a kubelet to run, which a bare envtest apiserver does not
#      have; the claim under test (RBAC rename transition) does not involve
#      hooks (hook RBAC is hook-scoped and short-lived). Expected: upgrade
#      succeeds, old fixed-name ClusterRole/ClusterRoleBindings are deleted,
#      prefixed ones created, ServiceAccount subjects unchanged — and the
#      apiserver audit log shows ALL new-RBAC CREATEs happen BEFORE ANY
#      old-RBAC DELETE (helm v3 pkg/kube/client.go Update() visits target
#      resources first, then deletes original.Difference(target)), i.e. the
#      old and new bindings overlap and there is no RBAC permission gap.
#
# Requirements: helm, kubectl, jq, git, openssl, and envtest assets
# (kube-apiserver + etcd). Override assets dir with KUBEBUILDER_ASSETS. Must
# run from a checkout whose charts/fluid/fluid is the PR head (the verify
# branch is exactly that: PR head + harness-only files).
#
# Results land in ../results/envtest/. Idempotent; cleans up its own
# apiserver/etcd; touches nothing outside 127.0.0.1 and its temp dirs.
set -euo pipefail

BASE_SHA="f432bdc87ac35941763b74e8e87683c7a84de5d9"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS="$SCRIPT_DIR/../results/envtest"
mkdir -p "$RESULTS"
REPO_ROOT="$(git rev-parse --show-toplevel)"
HEAD_CHART="$REPO_ROOT/charts/fluid/fluid"

# The historical fixed-name fluid cluster RBAC objects (for scoped cleanup of
# this script's own throwaway apiserver between scenarios).
FLUID_RBAC_NAMES="alluxioruntime-controller cacheruntime-controller fluid-csi-plugin dataset-controller efcruntime-controller fluidapp-controller jindoruntime-controller juicefsruntime-controller thinruntime-controller vineyardruntime-controller fluid-webhook fluid-crds-upgrade alluxioruntime-clusterrolebinding cacheruntime-clusterrolebinding dataset-clusterrolebinding efcruntime-clusterrolebinding fluidapp-clusterrolebinding jindoruntime-clusterrolebinding juicefsruntime-clusterrolebinding thinruntime-clusterrolebinding vineyardruntime-clusterrolebinding fluid-webhook-clusterrolebinding"

ASSETS="${KUBEBUILDER_ASSETS:-$HOME/.local/share/kubebuilder-envtest/k8s/1.36.2-linux-amd64}"
[ -x "$ASSETS/kube-apiserver" ] || { echo "envtest assets not found at $ASSETS (set KUBEBUILDER_ASSETS)"; exit 2; }
for bin in helm kubectl jq git openssl; do command -v $bin >/dev/null || { echo "$bin is required"; exit 2; }; done

WORK="$(mktemp -d /tmp/fluid-6256-envtest.XXXXXX)"
# random high port: avoid any chance of colliding with another listener
API_PORT="$(shuf -i 20000-50000 -n 1)"
cleanup() {
  [ -n "${APISERVER_PID:-}" ] && kill "$APISERVER_PID" 2>/dev/null || true
  [ -n "${ETCD_PID:-}" ] && kill "$ETCD_PID" 2>/dev/null || true
  [ -z "${KEEP_WORK:-}" ] || { echo "KEEP_WORK set; preserving $WORK (incl. apiserver.log, audit.log)"; return; }
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "== materializing base chart ($BASE_SHA) =="
mkdir -p "$WORK/base-chart"
git -C "$REPO_ROOT" archive "$BASE_SHA" charts/fluid/fluid | tar -x -C "$WORK/base-chart" --strip-components=3
[ -f "$WORK/base-chart/Chart.yaml" ] || { echo "base chart materialization failed"; exit 2; }

echo "== starting etcd + kube-apiserver =="
"$ASSETS/etcd" --data-dir "$WORK/etcd" \
  --listen-client-urls "http://127.0.0.1:2379" \
  --advertise-client-urls "http://127.0.0.1:2379" \
  --listen-peer-urls "http://127.0.0.1:2380" >/dev/null 2>&1 &
ETCD_PID=$!

openssl genrsa -out "$WORK/sa.key" 2048 2>/dev/null
printf 'token,admin,admin,"system:masters"\n' > "$WORK/tokens.csv"
cat > "$WORK/audit-policy.yaml" <<'EOF'
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
  - level: RequestResponse
    resources:
      - group: rbac.authorization.k8s.io
EOF

"$ASSETS/kube-apiserver" \
  --etcd-servers=http://127.0.0.1:2379 \
  --secure-port="$API_PORT" \
  --bind-address=127.0.0.1 \
  --advertise-address=127.0.0.1 \
  --cert-dir="$WORK/pki" \
  --token-auth-file="$WORK/tokens.csv" \
  --authorization-mode=RBAC \
  --service-cluster-ip-range=10.0.0.0/24 \
  --allow-privileged=true \
  --service-account-key-file="$WORK/sa.key" \
  --service-account-signing-key-file="$WORK/sa.key" \
  --service-account-issuer=https://test \
  --endpoint-reconciler-type=none \
  --audit-policy-file="$WORK/audit-policy.yaml" \
  --audit-log-path="$WORK/audit.log" >"$WORK/apiserver.log" 2>&1 &
APISERVER_PID=$!

KCFG="$WORK/kubeconfig"
cat > "$KCFG" <<EOF
apiVersion: v1
kind: Config
clusters:
- name: envtest
  cluster:
    server: https://127.0.0.1:$API_PORT
    insecure-skip-tls-verify: true
users:
- name: envtest
  user:
    token: token
contexts:
- name: envtest
  context:
    cluster: envtest
    user: envtest
current-context: envtest
EOF

for i in $(seq 1 60); do
  if kubectl --kubeconfig "$KCFG" get ns -o name >/dev/null 2>&1; then break; fi
  sleep 1
done
kubectl --kubeconfig "$KCFG" get ns -o name >/dev/null 2>&1 || { echo "kube-apiserver did not become ready"; cat "$WORK/apiserver.log" | tail -5; exit 2; }
# /healthz may keep two poststart bootstrap hooks (rbac/bootstrap-roles,
# scheduling/bootstrap-system-priority-classes) red on a bare envtest
# apiserver; they do not affect object CRUD, so discovery readiness suffices.
echo "apiserver ready"

kc() { kubectl --kubeconfig "$KCFG" "$@"; }
helmc() { helm --kubeconfig "$KCFG" "$@"; }
# ordered, deduped (verb, resource, name) -> first timestamp, non-empty names only
rbac_writes_since() { # $1 = byte offset
  tail -c +"$1" "$WORK/audit.log" 2>/dev/null | jq -r '
    select(.objectRef.resource=="clusterroles" or .objectRef.resource=="clusterrolebindings")
    | select(.verb=="create" or .verb=="delete")
    | select(.objectRef.name != null and .objectRef.name != "")
    | [.requestReceivedTimestamp, .verb, .objectRef.resource, .objectRef.name] | @tsv' 2>/dev/null \
  | sort -k2,4 -u | sort
}

echo "== scenario D (control, fresh cluster): default upgrade renames nothing =="
helmc install fluid-ctl "$WORK/base-chart" --namespace ns-ctl --create-namespace --set namespace=ns-ctl \
  >"$RESULTS/D-install-base.log" 2>&1 && echo "D: base install ok" || { echo "D: base install failed"; tail -5 "$RESULTS/D-install-base.log"; }
AUDIT_OFFSET_D=$(wc -c < "$WORK/audit.log" 2>/dev/null || echo 0)
kc get clusterroles -o json | jq -r '.items[].metadata.name' | sort > "$RESULTS/D-clusterroles-before.txt"
helmc upgrade fluid-ctl "$HEAD_CHART" --namespace ns-ctl --set namespace=ns-ctl --no-hooks \
  >"$RESULTS/D-upgrade-head-default.log" 2>&1 && echo "D: default upgrade succeeded" || { echo "D: upgrade failed"; tail -8 "$RESULTS/D-upgrade-head-default.log"; }
rbac_writes_since "$AUDIT_OFFSET_D" > "$RESULTS/D-upgrade-rbac-write-order.txt" || true
if [ -s "$RESULTS/D-upgrade-rbac-write-order.txt" ]; then
  echo "D: FAIL — default upgrade touched cluster RBAC:"; cat "$RESULTS/D-upgrade-rbac-write-order.txt"
else
  echo "D: PASS — default upgrade performed zero cluster RBAC creates/deletes (no churn)"
fi
kc get clusterroles -o json | jq -r '.items[].metadata.name' | sort > "$RESULTS/D-clusterroles-after.txt"
diff "$RESULTS/D-clusterroles-before.txt" "$RESULTS/D-clusterroles-after.txt" >/dev/null \
  && echo "D: PASS — ClusterRole set unchanged by the default upgrade" \
  || { echo "D: FAIL — ClusterRole set changed under default values"; diff "$RESULTS/D-clusterroles-before.txt" "$RESULTS/D-clusterroles-after.txt"; }

echo "-- cleanup: remove control release and its cluster RBAC --"
helmc uninstall fluid-ctl -n ns-ctl >"$RESULTS/D-uninstall.log" 2>&1 || true
for n in $FLUID_RBAC_NAMES; do kc delete clusterrole "$n" --ignore-not-found >/dev/null 2>&1 || true; kc delete clusterrolebinding "$n" --ignore-not-found >/dev/null 2>&1 || true; done

echo "== scenario A: install release 'fluid' from BASE chart, default values =="
helmc install fluid "$WORK/base-chart" --namespace fluid-system --create-namespace \
  >"$RESULTS/A-install-base.log" 2>&1 && echo "A: PASS (base install deployed)" || { echo "A: FAIL"; tail -5 "$RESULTS/A-install-base.log"; }

echo "== scenario B: install SECOND release 'fluid-b' from HEAD chart with opt-in prefix =="
helmc install fluid-b "$HEAD_CHART" --namespace ns-b --create-namespace \
  --set namespace=ns-b --set rbac.clusterScopedNamePrefix=tenant-b \
  >"$RESULTS/B-install2-with-prefix.log" 2>&1 \
  && { echo "B: UNEXPECTED PASS — second release installed (limitation lifted? update this canary)"; } \
  || { echo "B: expected failure observed (the #6134 ownership error, now on a shared singleton):"; grep -m2 "cannot be imported\|INSTALLATION FAILED" "$RESULTS/B-install2-with-prefix.log" || tail -3 "$RESULTS/B-install2-with-prefix.log"; }

echo "== scenario C: upgrade 'fluid' BASE -> HEAD with opt-in prefix (rename path, --no-hooks) =="
AUDIT_OFFSET_C=$(wc -c < "$WORK/audit.log" 2>/dev/null || echo 0)
helmc upgrade fluid "$HEAD_CHART" --namespace fluid-system \
  --set rbac.clusterScopedNamePrefix=tenant-a --no-hooks \
  >"$RESULTS/C-upgrade-with-prefix.log" 2>&1 \
  && echo "C: upgrade succeeded" || { echo "C: FAIL"; tail -8 "$RESULTS/C-upgrade-with-prefix.log"; }

echo "-- post-upgrade cluster RBAC state --"
kc get clusterroles -o json | jq -r '.items[].metadata.name' | sort > "$RESULTS/C-clusterroles-after.txt"
kc get clusterrolebindings -o json | jq -r '.items[].metadata.name' | sort > "$RESULTS/C-clusterrolebindings-after.txt"
echo "prefixed ClusterRoles: $(grep -c '^tenant-a-' "$RESULTS/C-clusterroles-after.txt" || true)"
echo "old fixed-name ClusterRoles still present: $(grep -cE '^(alluxioruntime-controller|dataset-controller|fluid-webhook|fluid-csi-plugin|fluid-crds-upgrade|cacheruntime-controller|efcruntime-controller|fluidapp-controller|jindoruntime-controller|juicefsruntime-controller|thinruntime-controller|vineyardruntime-controller)$' "$RESULTS/C-clusterroles-after.txt" || true)"
kc get clusterrolebinding tenant-a-alluxioruntime-clusterrolebinding -o json 2>/dev/null \
  | jq '.subjects' > "$RESULTS/C-binding-subjects.json" && echo "C: new binding subjects: $(jq -c '.subjects' < "$RESULTS/C-binding-subjects.json" 2>/dev/null || echo see-file)" || echo "(tenant-a-alluxioruntime-clusterrolebinding not found)"

echo "-- upgrade-window RBAC write ordering (from apiserver audit log) --"
rbac_writes_since "$AUDIT_OFFSET_C" > "$RESULTS/C-upgrade-rbac-write-order.txt"
cat "$RESULTS/C-upgrade-rbac-write-order.txt"
FIRST_DELETE_TS=$(awk -F'\t' '$2=="delete"{print $1; exit}' "$RESULTS/C-upgrade-rbac-write-order.txt" || true)
LAST_CREATE_TS=$(awk -F'\t' '$2=="create"{print $1}' "$RESULTS/C-upgrade-rbac-write-order.txt" | tail -1 || true)
if [ -n "$FIRST_DELETE_TS" ] && [ -n "$LAST_CREATE_TS" ]; then
  if [ "$LAST_CREATE_TS" \< "$FIRST_DELETE_TS" ] || [ "$LAST_CREATE_TS" = "$FIRST_DELETE_TS" ]; then
    echo "C: ORDERING OK — last new-RBAC CREATE ($LAST_CREATE_TS) precedes first old-RBAC DELETE ($FIRST_DELETE_TS): bindings overlap, no permission gap"
  else
    echo "C: ORDERING VIOLATION — first DELETE ($FIRST_DELETE_TS) happened before last CREATE ($LAST_CREATE_TS)"
  fi
else
  echo "C: could not determine ordering from audit log (no creates or deletes captured)"
fi

echo "== done; results in $RESULTS =="
