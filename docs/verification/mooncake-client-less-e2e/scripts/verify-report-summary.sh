#!/bin/bash
# Unit-level harness for test/gha-e2e/mooncake/image/reportSummary.sh.
# Stubs `curl` with fabricated master-metrics payloads and asserts the JSON
# that Fluid writes into Dataset status.cacheStates.
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"
REPORT_SUMMARY="$REPO_ROOT/test/gha-e2e/mooncake/image/reportSummary.sh"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"

failures=0

run_case() {
    local name="$1" payload="$2" curl_rc="${3:-0}"
    cat > "$TMP/bin/curl" <<STUB
#!/bin/bash
if [[ $curl_rc -ne 0 ]]; then exit $curl_rc; fi
cat <<'PAYLOAD'
$payload
PAYLOAD
STUB
    chmod +x "$TMP/bin/curl"
    PATH="$TMP/bin:$PATH" bash "$REPORT_SUMMARY"
}

assert_json_field() {
    local json="$1" field="$2" want="$3" name="$4"
    local got
    got=$(echo "$json" | jq -r ".$field")
    if [[ "$got" == "$want" ]]; then
        echo "PASS: $name: .$field == $want"
    else
        echo "FAIL: $name: .$field: got '$got', want '$want'"
        failures=$((failures + 1))
    fi
}

# Case 1: normal payload after a 4MiB write into a 1Gi quota
out=$(run_case "normal" 'Mooncake Master Metrics Summary
Mem Storage: 4.19 MB / 1.00 GB (0.4%) | File Storage: 0 B / 0 B (0.0%)
Keys: 3
Get=95.00/100.00 Put=3.00/3.00')
echo "$out" | jq . >/dev/null || { echo "FAIL: normal: output is not valid JSON"; failures=$((failures + 1)); }
assert_json_field "$out" cached "4.19MiB" normal
assert_json_field "$out" cacheCapacity "1.00GiB" normal
assert_json_field "$out" ufsTotal "1.00GiB" normal
assert_json_field "$out" fileNum "3" normal
assert_json_field "$out" cachedPercentage "0.4" normal
assert_json_field "$out" cacheHitRatio "95" normal

# Case 2: zero usage; Keys line missing entirely -> fallbacks
out=$(run_case "zero" 'Mooncake Master Metrics Summary
Mem Storage: 0 B / 1.00 GB (0.0%)')
assert_json_field "$out" cached "0B" zero
assert_json_field "$out" fileNum "0" zero
assert_json_field "$out" cacheHitRatio "0" zero
assert_json_field "$out" cachedPercentage "0.0" zero

# Case 3: percentage parens missing -> pipefail || fallback must kick in
out=$(run_case "nopercent" 'Mem Storage: 1.00 MB / 1.00 GB')
assert_json_field "$out" cachedPercentage "0.0" nopercent

# Case 4: metrics endpoint down (curl connection refused). Document the actual
# behavior: script must exit non-zero so the controller logs a failure.
run_case "down" "" 7 >/dev/null 2>&1
rc=$?
if [[ $rc -ne 0 ]]; then
    echo "PASS: down: exits non-zero (rc=$rc) when the metrics endpoint is unreachable"
else
    echo "FAIL: down: exited 0 despite unreachable metrics endpoint"
    failures=$((failures + 1))
fi

if [[ $failures -gt 0 ]]; then
    echo "reportSummary harness: $failures failure(s)"
    exit 1
fi
echo "reportSummary harness: all cases passed"
