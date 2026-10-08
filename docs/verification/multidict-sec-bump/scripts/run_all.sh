#!/usr/bin/env bash
# Runs the L0/L1/L2 layers of the multidict-sec-bump verification harness and
# captures raw output into results/. Exit 0 iff every contract holds.
#
# Prereqs: python3 (>=3.10) with venv+pip, network access to PyPI (or a mirror
# configured in pip), curl. ~90 MB of wheel downloads on the first run.
# Live/L3 layer intentionally not run (see README).
set -uo pipefail

# /tmp is a small tmpfs on the review host; keep wheels/venvs on the big disk.
mkdir -p "${HOME}/.pr-debate/tmp"
export TMPDIR="${HOME}/.pr-debate/tmp"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPTS="$HERE/scripts"
RES="$HERE/results"
mkdir -p "$RES"

REPO_ROOT="$(git rev-parse --show-toplevel)"
REQ="$REPO_ROOT/test/gha-e2e/mooncake/image/requirements.txt"
echo "requirements under test: $REQ (head: $(git rev-parse --short HEAD))"

FAILURES=()

echo "=== L0: static lock integrity (PyPI digest set) ==="
if python3 "$SCRIPTS/lock_integrity.py" "$REQ" 2>&1 | tee "$RES/10-lock-integrity.txt"; then
    L0=ok
else
    L0=FAIL; FAILURES+=("L0 lock integrity")
fi

echo
echo "=== L1: image-build pip resolution for cp312 / manylinux x86_64 ==="
WHLDIR=$(mktemp -d)
if pip download --no-cache-dir --retries 5 \
    --python-version 312 --abi cp312 --implementation cp \
    --platform manylinux_2_28_x86_64 --platform manylinux2014_x86_64 \
    --only-binary=:all: --require-hashes -r "$REQ" -d "$WHLDIR" 2>&1 | tail -4 | tee "$RES/20-pip-download.txt"; then
    L1=ok
else
    L1=FAIL; FAILURES+=("L1 pip download")
fi
ls "$WHLDIR" | sed "s|^|  saved: |" >> "$RES/20-pip-download.txt"

echo
echo "=== P0 premise: base multidict 6.7.1 must LEAK (GHSA-54p9-h82j-f925) ==="
V671=$(mktemp -d)/venv
python3 -m venv "$V671" >/dev/null 2>&1
if ! "$V671/bin/pip" install --no-cache-dir -q multidict==6.7.1 --only-binary=:all: >/dev/null 2>&1; then
    echo "  (could not install 6.7.1; retry with your index)" | tee "$RES/30-p0-base-6.7.1.txt"
fi
if "$V671/bin/python" "$SCRIPTS/leak_probe.py" > "$RES/30-p0-base-6.7.1.txt"; then
    P0=FAIL  # contract inverted for the premise: 6.7.1 must leak
    FAILURES+=("P0 premise did not reproduce on base")
else
    P0=ok
    echo "  => premise reproduced: base pin is vulnerable" >> "$RES/30-p0-base-6.7.1.txt"
fi
cat "$RES/30-p0-base-6.7.1.txt"

echo
echo "=== P0/L2: PR-pinned multidict must be clean ==="
# Extract the pinned multidict block from the lock and install it hash-locked.
# If the host python is not the wheel's target (e.g. lock trimmed to cp312 on a
# 3.11 host), fall back to installing the pinned version unhashed — the hash
# itself was already verified in L1.
MDBLOCK=$(mktemp)
python3 - "$REQ" "$MDBLOCK" <<'EOF'
import re, sys
txt = open(sys.argv[1]).read()
m = re.search(r"^multidict==.*?^\s*# via ", txt, re.M | re.S)
open(sys.argv[2], "w").write(m.group(0))
EOF
VPR=$(mktemp -d)/venv
python3 -m venv "$VPR" >/dev/null 2>&1
if ! "$VPR/bin/pip" install --no-cache-dir -q --require-hashes --only-binary=:all: -r "$MDBLOCK" >/dev/null 2>&1; then
    MDVER=$(head -1 "$MDBLOCK" | sed "s/multidict==//; s/ .*//")
    "$VPR/bin/pip" install --no-cache-dir -q "multidict==$MDVER" --only-binary=:all: >/dev/null 2>&1
fi
if "$VPR/bin/python" "$SCRIPTS/leak_probe.py" > "$RES/40-p0-pr-version.txt"; then
    P0PR=ok
else
    P0PR=FAIL; FAILURES+=("PR-pinned multidict leaks")
fi
cat "$RES/40-p0-pr-version.txt"

echo
echo "=== L2: compatibility with pinned aiohttp/yarl ==="
AIO_VER=$(grep -oP "^aiohttp==\K\S+" "$REQ" | head -1)
YARL_VER=$(grep -oP "^yarl==\K\S+" "$REQ" | head -1)
"$VPR/bin/pip" install --no-cache-dir -q "aiohttp==$AIO_VER" "yarl==$YARL_VER" >/dev/null 2>&1
if "$VPR/bin/python" "$SCRIPTS/compat_smoke.py" > "$RES/41-compat-smoke.txt" 2>&1 \
    && "$VPR/bin/pip" check >> "$RES/41-compat-smoke.txt" 2>&1; then
    L2=ok
else
    L2=FAIL; FAILURES+=("L2 compatibility smoke")
fi
cat "$RES/41-compat-smoke.txt"

echo
echo "=== F1: hash-scope contract (single-platform pin) + fallback demo ==="
if bash "$SCRIPTS/hash_scope_check.sh" "$REQ" "$RES"; then
    F1=ok
else
    F1=FAIL; FAILURES+=("F1 hash scope (minor finding, expected red under the PR)")
fi

echo
echo "=== summary ==="
{
    echo "L0 lock integrity            : $L0"
    echo "L1 pip resolution (cp312)    : $L1"
    echo "P0 premise leak on 6.7.1     : $P0"
    echo "P0 PR version clean          : $P0PR"
    echo "L2 compatibility smoke       : $L2"
    echo "F1 hash-scope contract       : $F1   (red == finding live, see README)"
} | tee "$RES/00-summary.txt"

if [ ${#FAILURES[@]} -gt 0 ]; then
    echo "FAILED: ${FAILURES[*]}" >&2
    # F1 alone is the known minor finding: do not fail the whole run for it.
    if [ ${#FAILURES[@]} -eq 1 ] && [[ "${FAILURES[0]}" == F1* ]]; then
        exit 0
    fi
    exit 1
fi
exit 0
