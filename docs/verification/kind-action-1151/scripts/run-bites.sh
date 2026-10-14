#!/usr/bin/env bash
# Harness-bites check: prove the contract checks actually detect the problems
# they claim to detect. Two negative controls, both must FAIL the right checks:
#
#  1. BASE control — the workflows as they were on the merge-base e4a10bf4
#     (kind-action pinned to v1.15.0). Expect: pin-integrity FAIL (this is the
#     differential: the PR bump is exactly what turns it green).
#  2. FIXTURE control — a deliberately broken workflow that pins v1.15.0, uses
#     an input that does not exist in v1.15.1's action.yml, and omits
#     kubectl_version. Expect: pin-integrity, input-compat, explicit-overrides
#     all FAIL.
#
# Usage (from the repo root): bash docs/verification/kind-action-1151/scripts/run-bites.sh
set -uo pipefail
HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$HARNESS_DIR/scripts/verify_action_pin.py"
BASE_SHA="e4a10bf49a1e8068d901e6f146ee4e435db7e785"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail=0
echo "== bites 1: base $BASE_SHA workflows (expect pin-integrity FAIL) =="
mkdir -p "$TMP/base/.github/workflows"
for f in kind-e2e.yml backward-compatibility-e2e.yml; do
  git show "$BASE_SHA:.github/workflows/$f" > "$TMP/base/.github/workflows/$f"
done
python3 -I "$SCRIPT" --workflows-dir "$TMP/base/.github/workflows" --report | tee "$TMP/base.out"
if ! grep -q $'^pin-integrity\tfail$' "$TMP/base.out"; then
  echo "BITES-1 FAILED: pin-integrity did not fail on base workflows" >&2; fail=1
fi

echo
echo "== bites 2: broken fixture (expect pin-integrity + input-compat + explicit-overrides FAIL) =="
python3 -I "$SCRIPT" --workflows-dir "$HARNESS_DIR/scripts/bites-fixture/.github/workflows" --report | tee "$TMP/fixture.out"
for c in pin-integrity input-compat explicit-overrides; do
  if ! grep -q "^$c	fail$" "$TMP/fixture.out"; then
    echo "BITES-2 FAILED: $c did not fail on the broken fixture" >&2; fail=1
  fi
done

echo
[ "$fail" -eq 0 ] && echo "BITES OK: all negative controls failed as expected" \
                   || echo "BITES BROKEN: see messages above"
exit $fail
