#!/usr/bin/env bash
# F2 repro: the CSI ControllerServer interface break is masked on the PR head by F1
# (everything below apimachinery fails to compile first). To surface it, copy the
# tree to a throwaway directory, apply a MINIMAL shim that only restores the smd
# field removed in v4.4.2 (diagnostic-only, never committed), and rebuild.
#
# Contract: after the shim, `go build ./...` still fails in pkg/csi/plugins with
#   "*controllerServer does not implement csi.ControllerServer (missing method
#    ControllerModifyVolume)"
#   - On PR head 8534d282 this error IS observed (repro of F2, caused by
#     github.com/container-storage-interface/spec v1.8.0 -> v1.9.0).
#   - After a correct fix (implement the method / embed UnimplementedControllerServer)
#     the probed build succeeds and the grep finds nothing.
#
# Usage: bash probe-csi-interface.sh [results-dir]
set -uo pipefail
RESULTS="${1:-$(dirname "$0")/../results}"
mkdir -p "$RESULTS"
RESULTS="$(cd "$RESULTS" && pwd)"

SRC="$(git rev-parse --show-toplevel)"
PROBE="$(mktemp -d /tmp/pr5392-f2-probe.XXXXXX)"
trap 'rm -rf "$PROBE"' EXIT
cp -a "$SRC" "$PROBE/tree"
cd "$PROBE/tree"

# diagnostic-only shim: restore the field removed by smd v4.4.2 so the build
# proceeds past F1. The field is dead code in v4.4.2 (nothing reads it), so this
# cannot mask any further error.
python3 - <<'EOF'
p = 'vendor/sigs.k8s.io/structured-merge-diff/v4/merge/update.go'
s = open(p).read()
needle = """type Updater struct {
	// Deprecated: This will eventually become private.
	Converter Converter

	// Deprecated: This will eventually become private.
	IgnoreFilter map[fieldpath.APIVersion]fieldpath.Filter

	returnInputOnNoop bool
}"""
shim = """type Updater struct {
	// Deprecated: This will eventually become private.
	Converter Converter

	// Deprecated: This will eventually become private.
	IgnoreFilter map[fieldpath.APIVersion]fieldpath.Filter

	// DIAGNOSTIC-ONLY shim (verification harness): restore field removed in v4.4.2
	IgnoredFields map[fieldpath.APIVersion]*fieldpath.Set

	returnInputOnNoop bool
}"""
assert needle in s, "shim anchor not found - smd source changed, update probe"
open(p, 'w').write(s.replace(needle, shim))
EOF
if [ $? -ne 0 ]; then
  echo "F2: HARNESS-UPDATE (probe shim anchor not found; smd vendored source changed)"
  exit 3
fi

go build ./... >"$RESULTS/probe-build.txt" 2>&1
if grep -q "missing method ControllerModifyVolume" "$RESULTS/probe-build.txt"; then
  echo "F2: FAIL (csi.ControllerServer is missing ControllerModifyVolume; csi spec v1.9.0)"
  grep "ControllerModifyVolume" "$RESULTS/probe-build.txt" | head -3
  exit 1
elif [ -s "$RESULTS/probe-build.txt" ]; then
  echo "F2: probe build still fails for another reason (check results/probe-build.txt):"
  head -5 "$RESULTS/probe-build.txt"
  exit 2
else
  echo "F2: PASS (controllerServer satisfies csi.ControllerServer after F1 shim)"
  exit 0
fi
