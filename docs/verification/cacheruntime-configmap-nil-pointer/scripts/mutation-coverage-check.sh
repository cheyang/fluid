#!/usr/bin/env bash
# Evidence for findings C1/C2: does PR #6157's test suite actually catch the removal
# of each individual guard it adds?
#
# Method: revert one guard at a time in pkg/ddc/cache/engine/cm.go, then re-run the
# cm.go unit tests. If they stay green, that guard has no regression coverage.
#
# IMPORTANT: this measures ONLY the plain `go test -run TestGenerateRuntimeConfigData`
# signal. The package's Ginkgo suite (TestCacheEngine) has pre-existing failures on
# some bases; using the whole-package pass/fail as the signal produces a FALSE
# "everything is covered" result. Keep the signal narrow and green-at-baseline.
#
# Usage: bash mutation-coverage-check.sh [<repo-root>]
set -uo pipefail

ROOT="${1:-$(git rev-parse --show-toplevel)}"
cd "$ROOT" || exit 1
CM=pkg/ddc/cache/engine/cm.go
PKG=./pkg/ddc/cache/engine/
SEL='TestGenerateRuntimeConfigData'

[ -f "$CM" ] || { echo "cannot find $CM from $ROOT"; exit 2; }
BAK=$(mktemp); cp "$CM" "$BAK"
restore() { cp "$BAK" "$CM"; }
trap restore EXIT

baseline=$(go test "$PKG" -run "$SEL" 2>&1 | grep -cE '^ok')
if [ "$baseline" -ne 1 ]; then
  echo "ABORT: baseline '$SEL' is not green -- the signal is unusable." >&2
  go test "$PKG" -run "$SEL" 2>&1 | tail -20 >&2
  exit 3
fi
echo "baseline: $SEL is GREEN"
echo "---------------------------------------------------------------------------"
printf '%-46s %s\n' "MUTATION (guard reverted)" "RESULT"

mutate () {
  local name="$1" old="$2" new="$3"
  restore
  OLD="$old" NEW="$new" python3 - "$CM" <<'PY' || { printf '%-46s %s\n' "$name" "TARGET NOT FOUND (skipped)"; return; }
import io, os, sys
p = sys.argv[1]
s = io.open(p, encoding='utf-8').read()
old, new = os.environ['OLD'], os.environ['NEW']
if old not in s:
    sys.exit(1)
io.open(p, 'w', encoding='utf-8').write(s.replace(old, new, 1))
PY
  if go test "$PKG" -run "$SEL" 2>&1 | grep -qE '^ok'; then
    printf '%-46s %s\n' "$name" "STILL GREEN  <== guard has NO coverage"
  else
    printf '%-46s %s\n' "$name" "fails (guard is covered)"
  fi
}

mutate "drop Master component guard" \
  'if runtimeClass.Topology.Master != nil && !runtime.Spec.Master.Disabled {' \
  'if !runtime.Spec.Master.Disabled {'
mutate "drop Worker component guard" \
  'if runtimeClass.Topology.Worker != nil && !runtime.Spec.Worker.Disabled {' \
  'if !runtime.Spec.Worker.Disabled {'
mutate "drop Client component guard" \
  'if runtimeClass.Topology.Client != nil && !runtime.Spec.Client.Disabled {' \
  'if !runtime.Spec.Client.Disabled {'
mutate "drop all-components-nil clause" \
  'if runtimeClass.Topology == nil ||
		(runtimeClass.Topology.Master == nil && runtimeClass.Topology.Worker == nil && runtimeClass.Topology.Client == nil) {' \
  'if runtimeClass.Topology == nil {'

restore
echo "---------------------------------------------------------------------------"
echo "production code restored; git diff should show no change to $CM"
