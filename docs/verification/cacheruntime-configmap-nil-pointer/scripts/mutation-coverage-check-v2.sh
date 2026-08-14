#!/usr/bin/env bash
# Evidence for findings C1/C2 (round 2, PR head 61c71c0c): does the revised test
# suite actually catch the removal of each guard the PR now relies on?
#
# Guard inventory on the revised head:
#   cm.go       per-component guards: runtimeClass.Topology.{Master,Worker,Client} != nil
#   runtime.go  loader guard: validateRuntimeClassTopology(&runtimeClass) in getRuntimeClass
#   validate.go validateRuntimeClassTopology: clause A (Topology == nil),
#               clause B (all three components nil)
#
# Method: revert one guard at a time, re-run the PR's own scoped tests. If they stay
# green, that guard has no regression coverage.
#
# IMPORTANT: this measures ONLY the scoped `-run` signal below. The package's Ginkgo
# suite (TestCacheEngine) has 12 pre-existing spec failures unrelated to this PR;
# using whole-package pass/fail as the mutation signal produces a FALSE "everything
# is covered" result. Keep the signal narrow and green-at-baseline.
#
# Run against a checkout of the PR head (no harness graft needed: the signal is the
# PR's own tests in cm_test.go).
#
# Usage: bash mutation-coverage-check-v2.sh [<repo-root>]
set -uo pipefail

ROOT="${1:-$(git rev-parse --show-toplevel)}"
cd "$ROOT" || exit 1
CM=pkg/ddc/cache/engine/cm.go
RT=pkg/ddc/cache/engine/runtime.go
VA=pkg/ddc/cache/engine/validate.go
PKG=./pkg/ddc/cache/engine/
SEL='TestGenerateRuntimeConfigData|TestGenerateDataLoadValueFile'

for f in "$CM" "$RT" "$VA"; do
  [ -f "$f" ] || { echo "cannot find $f from $ROOT"; exit 2; }
done
BAK_DIR=$(mktemp -d)
cp "$CM" "$BAK_DIR/cm.go"; cp "$RT" "$BAK_DIR/runtime.go"; cp "$VA" "$BAK_DIR/validate.go"
restore() { cp "$BAK_DIR/cm.go" "$CM"; cp "$BAK_DIR/runtime.go" "$RT"; cp "$BAK_DIR/validate.go" "$VA"; }
trap 'restore; rm -rf "$BAK_DIR"' EXIT

if ! go test "$PKG" -run "$SEL" >/dev/null 2>&1; then
  echo "ABORT: baseline '$SEL' is not green -- the signal is unusable." >&2
  go test "$PKG" -run "$SEL" 2>&1 | tail -20 >&2
  exit 3
fi
echo "baseline: $SEL is GREEN"
echo "---------------------------------------------------------------------------"
printf '%-46s %s\n' "MUTATION (guard reverted)" "RESULT"

# mutate <name> <file> <old> <new>
mutate () {
  local name="$1" file="$2" old="$3" new="$4"
  restore
  OLD="$old" NEW="$new" python3 - "$file" <<'PY' || { printf '%-46s %s\n' "$name" "TARGET NOT FOUND (skipped)"; return; }
import io, os, sys
p = sys.argv[1]
s = io.open(p, encoding='utf-8').read()
old, new = os.environ['OLD'], os.environ['NEW']
if old not in s:
    sys.exit(1)
io.open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PY
  if go test "$PKG" -run "$SEL" >/dev/null 2>&1; then
    printf '%-46s %s\n' "$name" "STILL GREEN  <== guard has NO coverage"
  else
    printf '%-46s %s\n' "$name" "fails (guard is covered)"
  fi
}

mutate "drop Master component guard (cm.go)" \
  "$CM" \
  'if runtimeClass.Topology.Master != nil && !runtime.Spec.Master.Disabled {' \
  'if !runtime.Spec.Master.Disabled {'
mutate "drop Worker component guard (cm.go)" \
  "$CM" \
  'if runtimeClass.Topology.Worker != nil && !runtime.Spec.Worker.Disabled {' \
  'if !runtime.Spec.Worker.Disabled {'
mutate "drop Client component guard (cm.go)" \
  "$CM" \
  'if runtimeClass.Topology.Client != nil && !runtime.Spec.Client.Disabled {' \
  'if !runtime.Spec.Client.Disabled {'
mutate "drop loader validation call (runtime.go)" \
  "$RT" \
  '	if err := validateRuntimeClassTopology(&runtimeClass); err != nil {
		return nil, err
	}
' \
  ''
mutate "drop all-components-nil clause (validate.go)" \
  "$VA" \
  '	if runtimeClass.Topology == nil ||
		(runtimeClass.Topology.Master == nil && runtimeClass.Topology.Worker == nil && runtimeClass.Topology.Client == nil) {' \
  '	if runtimeClass.Topology == nil {'

restore
echo "---------------------------------------------------------------------------"
echo "production code restored; git diff should show no change to cm.go/runtime.go/validate.go"
