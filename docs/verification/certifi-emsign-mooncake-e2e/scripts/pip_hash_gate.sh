#!/usr/bin/env bash
# C2 (contract): pip must ACCEPT the exact certifi entry from the mooncake e2e
# requirements.txt under the Dockerfile's install flags
# (`pip install --only-binary=:all: --require-hashes`), and must REJECT mutated
# entries (harness-bites controls).
#
# Usage: pip_hash_gate.sh [path-to-requirements.txt]
# Exit 0 iff the positive check passes AND all three negative controls fail.
# Network: pypi.org package index.
set -uo pipefail

REQS="${1:-test/gha-e2e/mooncake/image/requirements.txt}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

PY=python3
$PY -m venv "$WORK/venv"
PIP="$WORK/venv/bin/pip"

# extract the certifi entry (backslash-continued lines), keep the --hash flags
awk '/^certifi==/{f=1} f{print; if ($0 !~ /\\$/) {f=0}}' "$REQS" \
  | sed 's/[[:space:]]*#.*$//' > "$WORK/certifi-entry.txt"
grep -q '^certifi==' "$WORK/certifi-entry.txt" || { echo "FAIL: no certifi entry found in $REQS"; exit 1; }
echo "--- entry under test ---"; cat "$WORK/certifi-entry.txt"

run() { "$PIP" download --no-cache-dir --no-deps --only-binary=:all: --require-hashes -r "$1" -d "$2" >/dev/null 2>&1; }

echo "--- positive: exact entry must be accepted ---"
if ! run "$WORK/certifi-entry.txt" "$WORK/out-pos"; then
  echo "FAIL: pip rejected the shipped entry"; exit 1
fi
ls "$WORK/out-pos"
echo "PASS: pip accepted the entry under --only-binary=:all: --require-hashes"

echo "--- control 1: corrupted wheel hash must be rejected ---"
tac "$WORK/certifi-entry.txt" | sed '0,/--hash=sha256:/ s/--hash=sha256:....../--hash=sha256:000000/' | tac > "$WORK/bad1.txt"
grep -q 'sha256:000000' "$WORK/bad1.txt" || { echo "FAIL: control-1 mutation did not apply"; exit 1; }
run "$WORK/bad1.txt" "$WORK/out-b1" && { echo "FAIL: corrupted hash accepted"; exit 1; }
echo "PASS: corrupted hash rejected"

echo "--- control 2: sdist-only hash must be rejected under --only-binary ---"
WHEEL_HASH="$("$PY" - "$WORK/certifi-entry.txt" <<'PYEOF'
import json, re, sys, urllib.request
entry = open(sys.argv[1]).read()
ver = re.search(r'certifi==([0-9.]+)', entry).group(1)
d = json.load(urllib.request.urlopen("https://pypi.org/pypi/certifi/%s/json" % ver, timeout=30))
wheel = [u["digests"]["sha256"] for u in d["urls"] if u["filename"].endswith("py3-none-any.whl")][0]
print(wheel)
PYEOF
)"
grep -v "$WHEEL_HASH" "$WORK/certifi-entry.txt" | sed 's/\\$//; s/certifi==\([0-9.]*\)/certifi==\1 \\/' > "$WORK/bad2.txt"
run "$WORK/bad2.txt" "$WORK/out-b2" && { echo "FAIL: entry without wheel hash accepted under --only-binary"; exit 1; }
echo "PASS: sdist-hash-only entry rejected under --only-binary=:all:"

echo "--- control 3: same hashes, different version must be rejected ---"
sed 's/certifi==[0-9.]*/certifi==2026.7.22/' "$WORK/certifi-entry.txt" > "$WORK/bad3.txt"
grep -q 'certifi==2026.7.22' "$WORK/certifi-entry.txt" && sed -i 's/certifi==2026.7.22/certifi==2026.10.19/' "$WORK/bad3.txt"
run "$WORK/bad3.txt" "$WORK/out-b3" && { echo "FAIL: version/hash mismatch accepted"; exit 1; }
echo "PASS: version+hash mismatch rejected"
echo "C2 OK"
