#!/usr/bin/env bash
# mutation-bites.sh — prove the specs added by PR #6265 actually catch regressions.
# Applies five temporary mutations to the production functions under test, runs the
# new ginkgo specs after each, and requires every mutation to turn the suite RED.
# Restores the production files afterwards (git checkout). Exit 0 iff all mutations bite.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
FOCUS='CacheEngine data operation helpers|validateRuntimeClassTopology|CacheEngine.Shutdown'
run() { FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine -count=1 -run TestCacheEngine -args -ginkgo.focus="$FOCUS" >/tmp/mutation-run.txt 2>&1; }
cleanup() { git checkout -- pkg/ddc/cache/engine/data_operation.go pkg/ddc/cache/engine/validate.go pkg/ddc/cache/engine/shutdown.go; }
trap cleanup EXIT

expect_red() { # $1=mutation name
  if run; then echo "MUTATION-NOT-DETECTED: $1 (suite stayed green)"; return 1
  else echo "bit ok: $1 -> $(grep -c '\[FAILED\]' /tmp/mutation-run.txt) failed spec lines"; fi
}

DO=pkg/ddc/cache/engine/data_operation.go
VA=pkg/ddc/cache/engine/validate.go
SH=pkg/ddc/cache/engine/shutdown.go
rc=0

# M1: CheckRuntimeReady drops PartialReady from master-worker branch
python3 - "$DO" <<'PY'
import sys;p=sys.argv[1];s=open(p).read()
s=s.replace("""	return runtime.Status.Master.Phase == v1alpha1.RuntimePhaseReady &&
		(runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhasePartialReady)""",
"	return runtime.Status.Master.Phase == v1alpha1.RuntimePhaseReady && runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady")
open(p,'w').write(s)
PY
expect_red "M1 CheckRuntimeReady PartialReady" || rc=1; cleanup

# M2: findDataOperationSpec never matches
python3 - "$DO" <<'PY'
import sys;p=sys.argv[1];s=open(p).read()
s=s.replace('if op.Name == string(opName) {','if op.Name == string(opName)+"x" {')
open(p,'w').write(s)
PY
expect_red "M2 findDataOperationSpec no match" || rc=1; cleanup

# M3: GetDataOperationValueFile misroutes DataLoad
python3 - "$DO" <<'PY'
import sys;p=sys.argv[1];s=open(p).read()
s=s.replace('case dataoperation.DataLoadType:','case dataoperation.OperationType("DataLoadX"):')
open(p,'w').write(s)
PY
expect_red "M3 GetDataOperationValueFile misroute" || rc=1; cleanup

# M4: validateRuntimeClassTopology accepts empty topology
python3 - "$VA" <<'PY'
import sys;p=sys.argv[1];s=open(p).read()
s=s.replace("""	if runtimeClass.Topology == nil ||
		(runtimeClass.Topology.Master == nil && runtimeClass.Topology.Worker == nil && runtimeClass.Topology.Client == nil) {""",
"""	if runtimeClass.Topology == nil {""")
open(p,'w').write(s)
PY
expect_red "M4 validateRuntimeClassTopology empty topology" || rc=1; cleanup

# M5: Shutdown skips TearDownWorkers
python3 - "$SH" <<'PY'
import sys;p=sys.argv[1];s=open(p).read()
s=s.replace("""	err = helper.TearDownWorkers(info)
	if err != nil {
		return err
	}
	return nil""","""	_ = helper
	return nil""")
open(p,'w').write(s)
PY
expect_red "M5 Shutdown skips TearDownWorkers" || rc=1; cleanup

trap - EXIT; cleanup
if [ $rc -eq 0 ]; then echo "ALL 5 MUTATIONS DETECTED"; else echo "SOME MUTATIONS ESCAPED"; fi
exit $rc
