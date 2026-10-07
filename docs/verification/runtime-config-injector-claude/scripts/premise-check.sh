#!/usr/bin/env bash
# Premise check (claim P0) for PR #6197, run against the BASE branch
# (merge-base e0fc4c189a6e45ee17a5e812abf3fcbecf566f09), i.e. WITHOUT the patch.
#
# Claimed problem (PR body / issue #6176 stage 2): app pods of a FUSE-less cache
# runtime (Mooncake) have no way to receive the runtime config - stage 1 (#6191,
# merged) generates runtime.sh inside the runtime ConfigMap, but nothing delivers
# it to app pods, so applications hardcode the master address.
#
# This script collects positive evidence for each half of that claim on the base
# tree. Run from a checkout of this branch (it uses `git show` on the base ref,
# so the working tree does not matter).
set -uo pipefail

BASE="${BASE_REF:-e0fc4c189a6e45ee17a5e812abf3fcbecf566f09}"
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

fail=0
check() { # name, expected(0=present,1=absent), actual_count
  local name="$1" expected="$2" count="$3"
  if [ "$expected" = absent ] && [ "$count" -gt 0 ]; then
    echo "  [FAIL] $name: expected absent, found $count occurrence(s)"; fail=1
  elif [ "$expected" = present ] && [ "$count" -eq 0 ]; then
    echo "  [FAIL] $name: expected present, found none"; fail=1
  else
    echo "  [PASS] $name ($([ "$expected" = absent ] && echo 'absent on base' || echo "present on base, $count hit(s)"))"
  fi
}

echo "P0 premise evidence on base branch $BASE:"

# 1. stage 1 is merged: the engine generates runtime.sh into the runtime ConfigMap
n=$(git show "$BASE":pkg/ddc/cache/engine/util.go | grep -c 'runtime\.sh')
check "runtime.sh generation exists (stage 1, #6191)" present "$n"

# 2. nothing on base delivers it to app pods: no webhook consumer of runtime.sh
n=$(git grep -c 'runtime\.sh\|RuntimeConfigShell' "$BASE" -- pkg/webhook/ | wc -l)
check "webhook consumes runtime.sh" absent "$n"

# 3. no fluid.io/datasets annotation consumer on base
n=$(git grep -c 'LabelAnnotationDatasets\b' "$BASE" -- pkg/webhook/ | wc -l)
check "webhook reads a fluid.io/datasets annotation" absent "$n"

# 4. the RBAC gap the third commit fixes is real on base: the webhook ClusterRole
#    has every other runtime type but no cacheruntimes, while base.GetRuntimeInfo
#    (pkg/ddc/base/runtime.go) already branches on common.CacheRuntime.
n=$(git show "$BASE":pkg/ddc/base/runtime.go | grep -c 'case common.CacheRuntime')
check "base.GetRuntimeInfo handles CacheRuntime on base" present "$n"
n=$(git show "$BASE":charts/fluid/fluid/templates/role/webhook/rabc.yaml | grep -c 'cacheruntimes')
check "webhook chart RBAC grants cacheruntimes" absent "$n"

# 5. and the app-pod-facing webhook plugins are all FUSE-related on base
echo "  webhook plugins registered on base:"
git show "$BASE":pkg/webhook/plugins/plugins_impl.go | grep -o 'registry\.Register([a-z]*\.Name' | sed 's/registry.Register(//;s/.Name//' | sed 's/^/    - /'

if [ "$fail" -ne 0 ]; then
  echo "PREMISE CHECK: one or more facts did not hold - inspect above"
  exit 1
fi
echo "PREMISE CHECK: all facts hold on base => premise CONFIRMED (the delivery gap is real)"
