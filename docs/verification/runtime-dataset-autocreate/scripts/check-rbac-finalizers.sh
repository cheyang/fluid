#!/usr/bin/env bash
# F2 static tripwire: the auto-created Dataset carries a controller ownerReference with
# blockOwnerDeletion=true. On clusters running the OwnerReferencesPermissionEnforcement
# admission plugin (default-off upstream; enabled on e.g. OpenShift), the CREATE is
# rejected unless the caller holds `update` on the OWNER's <kind>/finalizers subresource.
# This script fails if any engine controller ClusterRole can create datasets but lacks
# its own <engine>runtimes/finalizers update permission.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
fail=0
for f in "$ROOT"/charts/fluid/fluid/templates/role/*/rbac.yaml; do
  name="$(basename "$(dirname "$f")")"
  case "$name" in csi|fluidapp|webhook|cache|dataset) continue;; esac
  if grep -q -- "- datasets" "$f"; then
    if ! grep -q "finalizers" "$f"; then
      echo "MISSING finalizers subresource permission: $f"
      fail=1
    fi
  fi
done
if [ "$fail" = 0 ]; then echo "OK: all engine roles with datasets access also hold finalizers permissions"; fi
exit "$fail"
