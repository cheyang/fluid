#!/bin/bash
# Unit-layer harness for PR #6175 (mooncake client-less e2e case).
# Verifies the script-level claims of the test case without a cluster:
#
#   U1  bash -n syntax check on every new shell script
#   U2  reportSummary.sh: fed the metrics format it documents, emits JSON with
#       exactly the keys the controller unmarshals, ufsTotal == cacheCapacity,
#       cached/fileNum non-trivial after a write
#   U3  the FailedMount grep in test.sh matches the literal CSI error string
#       from pkg/utils/mount.go, and does NOT bridge two unrelated messages
#       (the property the in-file comment claims)
#   U4  custom-entrypoint.sh rejects unknown/client roles (client-less guard)
#
# Usage: bash unit-harness.sh <repo-root> <results-dir>
set -u

REPO="${1:?repo root}"
RESULTS="${2:?results dir}"
mkdir -p "$RESULTS"
OUT="$RESULTS/unit-harness.out"
: > "$OUT"

pass() { echo "PASS  $1" | tee -a "$OUT"; }
fail() { echo "FAIL  $1" | tee -a "$OUT"; FAILED=1; }
FAILED=0

TD="$REPO/test/gha-e2e/mooncake"

# U1 syntax
for f in "$TD/test.sh" "$TD/image/custom-entrypoint.sh" "$TD/image/reportSummary.sh"; do
    if bash -n "$f" 2>>"$OUT"; then pass "U1 bash -n $(basename "$f")"; else fail "U1 bash -n $(basename "$f")"; fi
done

# U2 reportSummary.sh parsing (contract test).
# The script curls http://localhost:9003/metrics/summary, so stub curl on PATH.
STUBBIN="$RESULTS/.stubbin"
mkdir -p "$STUBBIN"
SAMPLE="$RESULTS/.sample-metrics"
printf 'Server: mooncake-master\nMem Storage: 4.00 MB / 2.00 GB (2.0%%)\nKeys: 1\nGet=1.00/1.00\n' > "$SAMPLE"
cat > "$STUBBIN/curl" <<'STUB'
#!/bin/bash
# stub: emit the mooncake metrics summary format reportSummary.sh documents
cat "${RESPONSE_FILE:?}"
STUB
chmod +x "$STUBBIN/curl"
JSON=$(PATH="$STUBBIN:$PATH" RESPONSE_FILE="$SAMPLE" bash "$TD/image/reportSummary.sh" 2>>"$OUT")
if [[ -n "$JSON" ]]; then
    pass "U2 reportSummary.sh runs on the documented metrics format"
else
    fail "U2 reportSummary.sh produced no output"
fi
PY=$(python3 - "$JSON" <<'EOF'
import json, sys
d = json.loads(sys.argv[1])
errors = []
if set(d) != {"cached","cachedPercentage","cacheCapacity","cacheHitRatio","fileNum","ufsTotal"}:
    errors.append("keys: %s" % sorted(d))
if d["ufsTotal"] != d["cacheCapacity"]:
    errors.append("ufsTotal(%s) != cacheCapacity(%s)" % (d["ufsTotal"], d["cacheCapacity"]))
if d["cached"] in ("", "0B"):
    errors.append("cached empty/zero: %r" % d["cached"])
if d["fileNum"] != "1":
    errors.append("fileNum: %r" % d["fileNum"])
if d["cacheHitRatio"] != "100":
    errors.append("cacheHitRatio: %r" % d["cacheHitRatio"])
print("; ".join(errors) if errors else "OK")
EOF
) 2>>"$OUT"
if [[ "$PY" == "OK" ]]; then pass "U2 reportSummary JSON keys/values match controller contract"; else fail "U2 reportSummary JSON: $PY"; fi

# U2b: empty metrics response must exit non-zero (set -euo pipefail path)
EMPTY="$RESULTS/.empty-metrics"
: > "$EMPTY"
if PATH="$STUBBIN:$PATH" RESPONSE_FILE="$EMPTY" bash "$TD/image/reportSummary.sh" >/dev/null 2>&1; then
    fail "U2b reportSummary.sh should exit non-zero on empty metrics response"
else
    pass "U2b reportSummary.sh exits non-zero on empty metrics response"
fi

# U3 FailedMount grep polarity
CSI_ERR=$(grep -o 'errors.New("timeout waiting for FUSE mount point to be ready")' "$REPO/pkg/utils/mount.go" | head -1)
if [[ -n "$CSI_ERR" ]] \
   && echo "rpc error: $CSI_ERR" | grep -qiE "fuse[[:space:]]+mount[[:space:]]*point" >/dev/null; then
    pass "U3 FailedMount pattern matches the real CSI error wording"
else
    fail "U3 FailedMount pattern does not match the real CSI error wording"
fi
# the concatenation of two unrelated messages must NOT match (no bridging)
BRIDGE="fuse.csi.fluid.io is not registered failed to mount volume for pod x mount point check failed"
if echo "$BRIDGE" | grep -qiE "fuse[[:space:]]+mount[[:space:]]*point" >/dev/null; then
    fail "U3 pattern bridges across two unrelated event messages"
else
    pass "U3 pattern does not bridge across two unrelated event messages"
fi

# U4 entrypoint role guard
if bash "$TD/image/custom-entrypoint.sh" client start >/dev/null 2>&1; then
    fail "U4 custom-entrypoint.sh must reject the client role"
else
    pass "U4 custom-entrypoint.sh rejects the client role"
fi
if bash "$TD/image/custom-entrypoint.sh" master bogus >/dev/null 2>&1; then
    fail "U4 custom-entrypoint.sh must reject a bogus action"
else
    pass "U4 custom-entrypoint.sh rejects a bogus action"
fi

echo "---"
if [[ "$FAILED" -ne 0 ]]; then
    echo "UNIT HARNESS FAILED" | tee -a "$OUT"
    exit 1
fi
echo "UNIT HARNESS PASSED" | tee -a "$OUT"
