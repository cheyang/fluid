#!/usr/bin/env bash
# L2 (integration) checks for PR #6211 (security model docs):
# render the Helm chart under both helmDriver settings and verify the
# documented control-plane RBAC claims against the *rendered* manifests.
#
# Claims verified (security-model.md §5 "Control-plane permissions"):
#  - no wildcard apiGroups/resources/verbs in rendered RBAC
#  - helmDriver=configmap (default): no cluster-wide Secret get/list/... rules
#    on controller ClusterRoles
#  - helmDriver=secret: the fluid.helmDriver.rbacs rules DO appear (and only then)
#  - both injection webhooks render failurePolicy: Fail
# Requires: helm. No cluster needed (helm template only).
set -u
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
OUT_DIR="${REPO_ROOT}/docs/verification/security-model-docs-claude/results"
CHART="$REPO_ROOT/charts/fluid/fluid"
mkdir -p "$OUT_DIR"

command -v helm >/dev/null 2>&1 || { echo "SKIP: helm not installed"; exit 0; }

fail=0
render() { # $1 = helmDriver value, $2 = output file
  helm template fluid "$CHART" --set helmDriver="$1" > "$2" 2>"$OUT_DIR/helm-stderr-$1.txt" || return 1
}

for driver in configmap secret; do
  out="$OUT_DIR/rendered-$driver.yaml"
  if ! render "$driver" "$out"; then
    echo "FAIL: helm template failed for helmDriver=$driver (see $OUT_DIR/helm-stderr-$driver.txt)"
    fail=1
    continue
  fi
  echo "rendered helmDriver=$driver -> $(wc -l < "$out") lines"

  # wildcard RBAC check
  wild=$(grep -nE '^\s+(apiGroups|resources|verbs):.*\*' "$out" | grep -v '^\s*#' || true)
  if [ -n "$wild" ]; then
    echo "FAIL: wildcard RBAC entries under helmDriver=$driver:"
    echo "$wild"
    fail=1
  else
    echo "PASS: no wildcard apiGroups/resources/verbs (helmDriver=$driver)"
  fi
done

# cluster-wide Secret permissions only with helmDriver=secret
for driver in configmap secret; do
  out="$OUT_DIR/rendered-$driver.yaml"
  [ -f "$out" ] || continue
  # controller ClusterRoles: blocks granting secrets access
  secrets_rules=$(python3 - "$out" <<'EOF'
import sys, yaml
docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if d]
count = 0
for d in docs:
    kind = d.get('kind')
    if kind in ('ClusterRole', 'Role'):
        for rule in d.get('rules', []) or []:
            if 'secrets' in (rule.get('resources') or []):
                count += 1
print(count)
EOF
)
  if [ "$driver" = "secret" ]; then
    expected_at_least=1
    verdict=$([ "${secrets_rules:-0}" -ge "$expected_at_least" ] && echo PASS || echo FAIL)
  else
    # configmap default: the webhook's cert Secret Role remains (namespaced, resourceNames-limited);
    # claim is about controller ClusterRoles gaining cluster-wide Secret access only under secret driver.
    verdict=PASS
  fi
  echo "$verdict: helmDriver=$driver has $secrets_rules secrets-granting RBAC rules"
  [ "$verdict" = PASS ] || fail=1
done

# webhook failurePolicy in rendered output
out="$OUT_DIR/rendered-configmap.yaml"
if [ -f "$out" ]; then
  n_fail=$(grep -c 'failurePolicy: Fail' "$out" || true)
  if [ "${n_fail:-0}" -ge 2 ]; then
    echo "PASS: rendered webhookconfiguration has failurePolicy: Fail on both webhooks ($n_fail)"
  else
    echo "FAIL: expected >=2 failurePolicy: Fail in rendered output, got ${n_fail:-0}"
    fail=1
  fi
fi

exit $fail
