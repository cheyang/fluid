#!/usr/bin/env bash
# Step 4 of review-finding-verifier: prove the harness actually BITES, plus a mutation
# check for the test-coverage finding (F4).
#
# (A) BITE CHECK -- temporarily apply the PROPOSED fix (no-target => a single "/" target
#     path, matching the chart's documented default) to pkg/ddc/jindo/load_data.go,
#     re-run L1, and confirm the F1 contract tests flip GREEN while the canaries stay
#     green. Then REVERT and confirm the production diff is empty again.
#
# (B) MUTATION CHECK (F4) -- replace the PR's
#     `utils.UFSPathBuilder{}.GenUFSPathInUnifiedNamespace(mount)` with a plain
#     `mount.Path`. If the PR's OWN added test still passes, then the helper's
#     name-derivation branch ("/{mount.Name}" for mounts without an explicit path) --
#     the behaviour the PR description explicitly claims -- is NOT covered by the test.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../../.." && pwd)"
OUT="$(cd "$HERE/.." && pwd)/results"
mkdir -p "$OUT"
TARGET="$REPO_ROOT/pkg/ddc/jindo/load_data.go"

cd "$REPO_ROOT"

restore() { git checkout -- "$TARGET" 2>/dev/null || true; }
trap restore EXIT

report() {
  echo
  echo "production diff vs PR head for load_data.go:"
  if git diff --quiet -- "$TARGET"; then echo "  (empty)"; else git diff --stat -- "$TARGET" | sed 's/^/  /'; fi
}

{
echo "############################################################"
echo "# (A) BITE CHECK -- apply the proposed fix, expect contracts to go GREEN"
echo "############################################################"
restore

python3 - "$TARGET" <<'PY'
import sys, re
p = sys.argv[1]
s = open(p).read()
old = '''	} else {
		// No explicit target is specified, fall back to loading all mount points of the dataset,
		// otherwise the generated targetPaths would be empty and the dataload would be a no-op.
		for _, mount := range targetDataset.Spec.Mounts {
			path := utils.UFSPathBuilder{}.GenUFSPathInUnifiedNamespace(mount)
			fluidNative := utils.IsTargetPathUnderFluidNativeMounts(path, *targetDataset)
			targetPaths = append(targetPaths, cdataload.TargetPath{
				Path:        path,
				Replicas:    1,
				FluidNative: fluidNative,
			})
		}
	}'''
new = '''	} else {
		// PROPOSED FIX (reviewer, temporary): JindoFS serves a single namespace over a
		// single UFS, and the chart documents the default as (path: "/", replicas: 1).
		targetPaths = append(targetPaths, cdataload.TargetPath{
			Path:        "/",
			Replicas:    1,
			FluidNative: false,
		})
	}'''
if old not in s:
    print("BITE-PATCH-FAILED: could not find the PR's else branch verbatim", file=sys.stderr)
    sys.exit(3)
open(p, 'w').write(s.replace(old, new, 1))
print("applied proposed fix")
PY
if [ $? -ne 0 ]; then echo "!! bite patch failed"; else
  go test ./pkg/ddc/jindo/ -run 'TestPR6156_L1' -v 2>&1 \
    | grep -E '^(--- (PASS|FAIL)|ok|FAIL|PASS)' | sed 's/^/  /'
fi
report

echo
echo "-- reverting the proposed fix --"
restore
report

echo
echo "############################################################"
echo "# (B) MUTATION CHECK (F4) -- is the name-derivation branch covered by the PR's test?"
echo "############################################################"
echo "-- baseline: the PR's own added test on unmodified PR code"
go test ./pkg/ddc/jindo/ -run 'Test_genDataLoadValue' 2>&1 \
  | grep -E '^(ok|FAIL|---)' | sed 's/^/  /'

echo
echo "-- mutant: GenUFSPathInUnifiedNamespace(mount) -> mount.Path"
python3 - "$TARGET" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
old = 'path := utils.UFSPathBuilder{}.GenUFSPathInUnifiedNamespace(mount)'
new = 'path := mount.Path // MUTANT: drop the name-derivation fallback'
if old not in s:
    print("MUTATION-FAILED: helper call not found", file=sys.stderr); sys.exit(3)
open(p,'w').write(s.replace(old, new, 1))
print("applied mutant")
PY
go test ./pkg/ddc/jindo/ -run 'Test_genDataLoadValue' 2>&1 \
  | grep -E '^(ok|FAIL|---)' | sed 's/^/  /'
echo
echo "  INTERPRETATION: if the mutant still passes, the PR's test never exercises the"
echo "  '/{mount.Name}' fallback that the PR description claims to handle."

restore
report
} 2>&1 | tee "$OUT/L1-bite-and-mutation.txt"

echo
echo "wrote $OUT/L1-bite-and-mutation.txt"
