#!/bin/bash
# Bug-canary for finding F4: wait_runtime_deleted in
# test/gha-e2e/mooncake/test.sh (and the same pattern in curvine/test.sh)
# pipes kubectl through 2>/dev/null, so a kubectl call that fails for any
# reason (API outage, missing CRD) yields an empty result and the GC assertion
# passes vacuously.
#
# This harness extracts the real function bodies from the PR's test.sh and
# runs them with a kubectl stub that fails every call. CANARY polarity: it
# passes (exit 0) while the vacuous-pass behavior exists and flips to red once
# the scripts distinguish "no resources left" from "kubectl failed".
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"

cat > "$TMP/bin/kubectl" <<'STUB'
#!/bin/bash
# simulate a hard API failure for every call
echo "error: the server is currently unable to handle the request" >&2
exit 1
STUB
chmod +x "$TMP/bin/kubectl"

# Extract the exact functions under test plus their helpers from the PR file.
{
    echo 'dataset_name="mooncake-demo"'
    echo 'testname="canary"'
    sed -n '/^function syslog() {/,/^}/p' "$REPO_ROOT/test/gha-e2e/mooncake/test.sh"
    sed -n '/^function panic() {/,/^}/p' "$REPO_ROOT/test/gha-e2e/mooncake/test.sh"
    sed -n '/^function wait_runtime_deleted() {/,/^}/p' "$REPO_ROOT/test/gha-e2e/mooncake/test.sh"
    echo 'wait_runtime_deleted'
} > "$TMP/harness.sh"

PATH="$TMP/bin:$PATH" timeout 60 bash "$TMP/harness.sh" > "$TMP/out" 2>&1
rc=$?
cat "$TMP/out"

if [[ $rc -eq 0 ]] && grep -q "garbage collected" "$TMP/out"; then
    echo "CANARY-TRIPPED: wait_runtime_deleted reported success while every kubectl call was failing"
    exit 0
fi
echo "CANARY-CLEAR: wait_runtime_deleted no longer passes vacuously (rc=$rc)"
exit 1
