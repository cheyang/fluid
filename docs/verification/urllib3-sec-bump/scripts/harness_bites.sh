#!/usr/bin/env bash
# Harness-bites check — prove T2 can actually detect a bad lockfile.
#
# A green test that cannot go red proves nothing. This script plants two
# defects in a COPY of the requirements file (the repo file is never touched)
# and asserts that the T2 resolution goes red for each:
#
#   bite 1: corrupt the urllib3 wheel hash (simulates a tampered/mistyped pin)
#   bite 2: keep the 2.8.0 hashes but pin the version back to 2.7.0
#           (simulates a version/hash mismatch)
#
# Both must FAIL resolution (pip --require-hashes rejects the file). If either
# unexpectedly succeeds, the harness does not bite and its green result on the
# real file is worthless.
#
# Polarity: these are canaries for the harness itself, not findings.
# Written 2026-10-08 by Reviewer A (Claude).
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG="${VERIFY_LOG_DIR:-$REPO_ROOT/docs/verification/urllib3-sec-bump/results}/harness_bites.log"
mkdir -p "$(dirname "$LOG")"
: > "$LOG"

REQ="$REPO_ROOT/test/gha-e2e/mooncake/image/requirements.txt"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

run_resolve() { # $1 = req file -> exit code of the T2 resolution
    local f="$1"
    pip3 download --no-cache-dir --require-hashes --only-binary=:all: \
        --python-version 3.12 --implementation cp --abi cp312 \
        --platform manylinux2014_x86_64 --platform manylinux1_x86_64 \
        --platform manylinux_2_5_x86_64 --platform manylinux_2_17_x86_64 \
        --platform manylinux_2_28_x86_64 \
        --dest "$WORK/dl" -r "$f" >/dev/null 2>&1
}

echo "[bites] bite 1: corrupting the urllib3 wheel hash in a copy" | tee -a "$LOG"
cp "$REQ" "$WORK/corrupt.txt"
python3 -I - "$WORK/corrupt.txt" <<'EOF' >>"$LOG" 2>&1
import sys, re
p = sys.argv[1]
s = open(p).read()
# flip the first listed urllib3 hash (the wheel's)
new = re.sub(r"(urllib3==2\.8\.0 \\\n    --hash=sha256:)0cf3cae5",
             r"\g<1>0cf3cae6", s, count=1)
assert new != s, "pattern not found — test setup bug"
open(p, "w").write(new)
EOF
run_resolve "$WORK/corrupt.txt"
rc1=$?
echo "[bites]   exit=$rc1 (expected non-zero)" | tee -a "$LOG"

echo "[bites] bite 2: pin 2.7.0 but keep the 2.8.0 hashes" | tee -a "$LOG"
cp "$REQ" "$WORK/mismatch.txt"
sed -i 's/^urllib3==2\.8\.0/urllib3==2.7.0/' "$WORK/mismatch.txt"
run_resolve "$WORK/mismatch.txt"
rc2=$?
echo "[bites]   exit=$rc2 (expected non-zero)" | tee -a "$LOG"

if [ $rc1 -ne 0 ] && [ $rc2 -ne 0 ]; then
    echo "[bites] RESULT: PASS — T2 goes red on both planted defects; its green on the real file is meaningful" | tee -a "$LOG"
    exit 0
fi
echo "[bites] RESULT: FAIL — a planted defect did NOT break resolution; harness does not bite" | tee -a "$LOG"
exit 1
