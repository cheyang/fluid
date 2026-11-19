#!/usr/bin/env bash
# mutation-check-t1.sh — reproducible evidence for finding T1 of the PR #6264 review
# (Reviewer A).
#
# T1 (round 1, head ef7ecf5f): the PR's data-operation routing tests only
# asserted the early type-mismatch error of each value-file generator, so a
# regression in the happy path of generateDataLoadValueFile left every new
# spec green. Round-1 evidence: results/mutation-t1.txt (section a: PR specs
# 23/23 green under this mutation).
#
# T1 was FIXED in round 2 (head ee7899ca5 "test(cache): cover the DataLoad
# value file happy path"): the PR now has its own happy-path spec, so under
# this same mutation the PR's specs must now go RED too. This script is
# therefore a FORWARD-LOOKING GUARD since round 2: it applies the mutation,
# runs (a) the PR's specs and (b) the reviewer harness tests, and expects BOTH
# to fail. If (a) stays green, the PR's happy-path coverage was lost again
# (T1 reopened) and the script exits non-zero.
#
# It then RESTORES the production file and refuses to leave the tree dirty.
#
# Usage: bash docs/verification/cache-engine-unit-tests/scripts/mutation-check-t1.sh
#        (from the repo root, on a clean tree)
set -euo pipefail

TARGET="pkg/ddc/cache/engine/dataload.go"
ANCHOR=$'\tdataload, ok := object.(*v1alpha1.DataLoad)\n\tif !ok {\n\t\treturn "", fmt.Errorf("object %v is not a DataLoad", object)\n\t}\n'
MUTATION=$'\tdataload, ok := object.(*v1alpha1.DataLoad)\n\tif !ok {\n\t\treturn "", fmt.Errorf("object %v is not a DataLoad", object)\n\t}\n\t_ = dataload\n\treturn "mutated-bogus.yaml", nil // MUTATION-EXPERIMENT-T1\n'

cd "$(git rev-parse --show-toplevel)"
if ! git diff --quiet -- "$TARGET"; then
  echo "mutation-check: $TARGET has uncommitted changes; aborting" >&2
  exit 2
fi
restore() { git checkout -- "$TARGET"; }
trap restore EXIT

python3 - "$TARGET" "$ANCHOR" "$MUTATION" <<'EOF'
import sys
p, anchor, mutation = sys.argv[1], sys.argv[2], sys.argv[3]
s = open(p).read()
if anchor not in s:
    sys.exit("anchor not found; production code changed shape -> HARNESS-UPDATE")
open(p, "w").write(s.replace(anchor, mutation, 1))
EOF
echo "== mutation applied to generateDataLoadValueFile happy path =="

echo
echo "== (a) PR's new specs under mutation (since round 2: expect RED -> gap closed) =="
# -run TestCacheEngine restricts to the Ginkgo suite so plain go tests don't mix in.
set +e
go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus="CacheEngine data operations|validateRuntimeClassTopology" -v 2>&1 \
  | sed 's/\x1b\[[0-9;]*m//g' | grep -E "Ran [0-9]+ of|SUCCESS|FAIL!|^ok |^FAIL"
rc_pr=${PIPESTATUS[0]}
set -e

echo
echo "== (b) reviewer harness under mutation (expect: FAIL -> harness bites) =="
set +e
go test ./pkg/ddc/cache/engine/ -run 'TestDataOperationValueFileHappyPath|TestDataLoadValueFileNotSupportedWhenSpecMissing' 2>&1 \
  | sed 's/\x1b\[[0-9;]*m//g' | grep -E -- '--- (PASS|FAIL)|^(ok|FAIL)'
rc=${PIPESTATUS[0]}
set -e
if [ "$rc" -ne 0 ]; then
  echo "harness caught the mutation (expected)"
else
  echo "UNEXPECTED: harness did not catch the mutation"
fi

# restore explicitly, then verify (the EXIT trap is only a safety net)
restore
echo
echo "== production file restored; diff status: =="
git status --short -- "$TARGET"
echo "(empty = clean)"

# Since round 2 the PR's own happy-path spec must catch this mutation too.
if [ "$rc_pr" -eq 0 ]; then
  echo "RESULT: T1 REOPENED — the PR's specs stayed green under the happy-path mutation."
  exit 1
fi
echo "RESULT: T1 closed — both the PR's specs and the harness went red under the mutation."
