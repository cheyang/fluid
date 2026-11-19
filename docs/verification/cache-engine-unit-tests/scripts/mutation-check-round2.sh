#!/usr/bin/env bash
# mutation-check-round2.sh — bites-check for the specs the PR added in round 2
# (commit ee7899ca5 "test(cache): cover the DataLoad value file happy path").
#
# The PR now carries its own happy-path spec ("writes a DataLoad value file built
# from the runtime class DataOperationSpecs") plus two worker-RuntimePhaseNone
# table rows. This script applies four production mutations and asserts the
# suite goes red for each; if any mutation leaves the suite green, the
# corresponding PR spec is vacuous and this script fails.
#
#   M5  generateDataLoadValueFile returns a bogus file name right after the
#       type assertion  -> expect the happy-path spec RED
#   M6  genDataLoadValue drops `dataloadInfo.Image = opSpec.Image`
#       -> expect the happy-path spec RED (content assertion on the image)
#   M7  getRuntimeConfigDir returns /etc/fluid/mutated
#       -> expect the happy-path spec RED (runtime-config volume wiring)
#   M8  master-disabled branch of CheckRuntimeReady also accepts
#       Worker.Phase == RuntimePhaseNone
#       -> expect "master disabled, worker not set up yet" RED
#
# Each mutation is reverted before the next; the script refuses to run on a
# dirty tree and refuses to leave one behind.
#
# Usage: bash docs/verification/cache-engine-unit-tests/scripts/mutation-check-round2.sh
#        (from the repo root, on a clean tree)
set -uo pipefail

cd "$(git rev-parse --show-toplevel)"

DATALOAD="pkg/ddc/cache/engine/dataload.go"
UTIL="pkg/ddc/cache/engine/util.go"
DATAOP="pkg/ddc/cache/engine/data_operation.go"

for T in "$DATALOAD" "$UTIL" "$DATAOP"; do
  if ! git diff --quiet -- "$T"; then
    echo "mutation-check: $T has uncommitted changes; aborting" >&2
    exit 2
  fi
done
restore_all() { git checkout -- "$DATALOAD" "$UTIL" "$DATAOP"; }
trap restore_all EXIT

run_specs() { # prints the ginkgo summary lines (Ran / SUCCESS! / FAIL! / ok)
  FLUID_UNIT_TEST=true go test -gcflags=all=-l ./pkg/ddc/cache/engine/ \
    -run TestCacheEngine 2>&1 \
    | sed 's/\x1b\[[0-9;]*m//g' | grep -E "^Ran [0-9]+ of|SUCCESS!|FAIL!|^ok |^FAIL" || true
}

FAILED=0

check_mutation() { # $1 = tag, $2 = description; runs the suite and judges
  local tag="$1" desc="$2" summary
  echo "== $tag: $desc =="
  summary="$(run_specs)"
  echo "$summary"
  if echo "$summary" | grep -q "FAIL!"; then
    echo "-- $tag caught (suite went red, as required)"
  else
    echo "!! $tag left the suite green — the PR spec supposed to bite is vacuous"
    FAILED=1
  fi
  echo
}

# ---- M5: bogus value file ----
python3 - "$DATALOAD" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
anchor = '\tdataload, ok := object.(*v1alpha1.DataLoad)\n\tif !ok {\n\t\treturn "", fmt.Errorf("object %v is not a DataLoad", object)\n\t}\n'
mut = anchor + '\treturn "mutated-bogus.yaml", nil // MUTATION-EXPERIMENT-M5\n'
if anchor not in s:
    sys.exit("anchor not found; production code changed shape -> HARNESS-UPDATE")
open(p, "w").write(s.replace(anchor, mut, 1))
EOF
check_mutation M5 "generateDataLoadValueFile returns mutated-bogus.yaml (happy path)"
restore_all

# ---- M6: image not injected ----
python3 - "$DATALOAD" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
anchor = '\tdataloadInfo.Image = opSpec.Image\n'
if anchor not in s:
    sys.exit("anchor not found; production code changed shape -> HARNESS-UPDATE")
open(p, "w").write(s.replace(anchor, '', 1))
EOF
check_mutation M6 "genDataLoadValue drops the DataOperationSpecs image"
restore_all

# ---- M7: runtime-config mount dir ----
python3 - "$UTIL" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
anchor = '\treturn "/etc/fluid/config"\n'
if anchor not in s:
    sys.exit("anchor not found; production code changed shape -> HARNESS-UPDATE")
open(p, "w").write(s.replace(anchor, '\treturn "/etc/fluid/mutated"\n', 1))
EOF
check_mutation M7 "getRuntimeConfigDir returns /etc/fluid/mutated"
restore_all

# ---- M8: worker None accepted in master-disabled branch ----
python3 - "$DATAOP" <<'EOF'
import sys
p = sys.argv[1]
s = open(p).read()
anchor = '''\tif runtime.Spec.Master.Disabled {
\t\treturn runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhasePartialReady
\t}'''
mut = '''\tif runtime.Spec.Master.Disabled {
\t\treturn runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhasePartialReady || runtime.Status.Worker.Phase == v1alpha1.RuntimePhaseNone // MUTATION-EXPERIMENT-M8
\t}'''
if anchor not in s:
    sys.exit("anchor not found; production code changed shape -> HARNESS-UPDATE")
open(p, "w").write(s.replace(anchor, mut, 1))
EOF
check_mutation M8 "master-disabled worker gate accepts RuntimePhaseNone"
restore_all

echo "== production files restored; diff status: =="
git status --short -- "$DATALOAD" "$UTIL" "$DATAOP"
echo "(empty = clean)"

if [ "$FAILED" -ne 0 ]; then
  echo "RESULT: at least one mutation left the suite green — a PR spec is vacuous."
  exit 1
fi
echo "RESULT: every mutation turned the suite red — the round-2 PR specs bite."
