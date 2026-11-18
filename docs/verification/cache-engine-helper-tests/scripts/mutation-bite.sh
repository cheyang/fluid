#!/usr/bin/env bash
# mutation-bite.sh — proves the 23 new specs added by PR #6265 actually detect
# regressions in the production code they cover (a test-only PR's harness-bites check).
#
# Each mutation temporarily breaks one behavior in the production code
# (CheckRuntimeReady branches, GetDataOperationValueFile routing + NotSupported,
# findDataOperationSpec, validateRuntimeClassTopology, Shutdown cleanup legs),
# runs the focused specs, and expects FAILURE. Every mutation must be CAUGHT.
# After each mutation the file is restored with git checkout.
#
# Run from the repository root (or anywhere; the script locates the repo root).
# Exit 0 iff baseline and final baseline pass and every mutation is caught.
set -u
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
cd "$REPO_ROOT"

FOCUS='CacheEngine data operation helpers|validateRuntimeClassTopology|CacheEngine.Shutdown'
STATUS=0

run_specs() {
  FLUID_UNIT_TEST=true go test -gcflags=all=-l ./pkg/ddc/cache/engine/ \
    -run TestCacheEngine -ginkgo.focus="$FOCUS" -count=1 2>&1 | tail -1
}

mutate() {
  local file="$1" old="$2" new="$3" name="$4"
  git checkout -- "$file"
  if ! grep -qF "$old" "$file"; then
    echo "MUTATION $name: PATTERN-NOT-FOUND (harness problem — update the pattern for the current code)"
    STATUS=1
    return
  fi
  python3 - "$file" "$old" "$new" <<'EOF'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(path).read()
assert s.count(old) >= 1, f"pattern not found: {old[:60]}"
open(path, "w").write(s.replace(old, new, 1))
EOF
  result="$(run_specs)"
  if echo "$result" | grep -q "^ok"; then
    echo "MUTATION $name: SURVIVED (tests did NOT catch it) -> $result"
    STATUS=1
  else
    echo "MUTATION $name: CAUGHT (tests fail as they should) -> $result"
  fi
  git checkout -- "$file"
}

echo "=== baseline (unmutated) ==="
baseline="$(run_specs)"
echo "$baseline"
echo "$baseline" | grep -q "^ok" || STATUS=1

DO=pkg/ddc/cache/engine/data_operation.go
VA=pkg/ddc/cache/engine/validate.go
SH=pkg/ddc/cache/engine/shutdown.go

# M1: CheckRuntimeReady no longer accepts PartialReady workers
mutate "$DO" \
  'return runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhasePartialReady' \
  'return runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady' \
  'M1-worker-partialready-dropped'

# M2: master-worker arch ignores master phase
mutate "$DO" \
  'return runtime.Status.Master.Phase == v1alpha1.RuntimePhaseReady &&
		(runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhasePartialReady)' \
  'return runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhasePartialReady' \
  'M2-master-phase-ignored'

# M3: master-disabled branch returns true unconditionally
mutate "$DO" \
  'if runtime.Spec.Master.Disabled {
		return runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhasePartialReady
	}' \
  'if runtime.Spec.Master.Disabled {
		return true
	}' \
  'M3-master-disabled-always-ready'

# M4: getRuntime error no longer reported as not-ready
mutate "$DO" \
  'runtime, err := e.getRuntime()
	if err != nil {
		return false
	}' \
  'runtime, err := e.getRuntime()
	if err != nil {
		return true
	}' \
  'M4-missing-runtime-ready'

# M5: DataLoad routing no longer reaches the DataLoad generator
mutate "$DO" \
  'case dataoperation.DataLoadType:
		valueFileName, err = e.generateDataLoadValueFile(ctx, object)' \
  'case dataoperation.DataLoadType:
		return "", fluiderrors.NewNotSupported(schema.GroupResource{}, "x")' \
  'M5-dataload-route-broken'

# M6: default branch no longer returns NotSupported
mutate "$DO" \
  'default:
		return "", fluiderrors.NewNotSupported(' \
  'default:
		return "", nil' \
  'M6-unknown-type-no-error'

# M7: findDataOperationSpec never matches
mutate "$DO" \
  'if op.Name == string(opName) {
			return &op
		}' \
  'if false {
			return &op
		}' \
  'M7-findopspec-never-matches'

# M8: validateRuntimeClassTopology accepts empty topologies
mutate "$VA" \
  'if runtimeClass.Topology == nil ||
		(runtimeClass.Topology.Master == nil && runtimeClass.Topology.Worker == nil && runtimeClass.Topology.Client == nil) {' \
  'if false {' \
  'M8-validate-accepts-empty'

# M9: Shutdown skips fuse label cleanup
mutate "$SH" \
  'count, err := helper.CleanUpFuse()' \
  'count := 0
	_ = count' \
  'M9-shutdown-skips-fuse-cleanup'

# M10: Shutdown skips worker label teardown
mutate "$SH" \
  'err = helper.TearDownWorkers(info)' \
  '_ = info' \
  'M10-shutdown-skips-worker-teardown'

echo "=== final baseline (all restored) ==="
final="$(run_specs)"
echo "$final"
echo "$final" | grep -q "^ok" || STATUS=1
if [ "$STATUS" -eq 0 ]; then
  echo "MUTATION-BITE: PASS (all mutations caught, baselines green)"
else
  echo "MUTATION-BITE: FAIL (see above)"
fi
exit "$STATUS"
