#!/usr/bin/env bash
# F1 — hash-scope check + silent-fallback demonstration for the mooncake lock.
#
# Contract part: the multidict entry must pin exactly the artifact(s) the image
# build can use on its documented platform (cp312 / manylinux x86_64), i.e. the
# single hash of multidict-<ver>-cp312-cp312-manylinux...x86_64.whl, matching
# the file's own regeneration procedure. Currently RED under the PR: Dependabot
# pinned all release artifacts. After a trim, this passes.
#
# Evidence part (mini-lock, fast and deterministic): shows WHY the full list
# weakens the documented fail-loudly guarantee of --require-hashes.
#   case A: PR-style entry, cp312 wheel hash invalidated -> pip silently falls
#           back to the pure-python multidict-<ver>-py3-none-any.whl (whose
#           hash is also in the list): build succeeds, no C extension.
#   case B: single-hash (trimmed) entry, that hash invalidated -> pip fails
#           hard with THESE PACKAGES DO NOT MATCH THE HASHES.
# Usage: hash_scope_check.sh <requirements.txt> [results-dir]
set -uo pipefail

REQ="${1:?usage: hash_scope_check.sh <requirements.txt> [results-dir]}"
RES="${2:-.}"
mkdir -p "$RES"
OUT="$RES/50-hash-scope.txt"
: > "$OUT"

VER=$(sed -n "s/^multidict==\([^ ]*\).*/\1/p" "$REQ" | head -1)
NHASH=$(sed -n "/^multidict==/,/# via /p" "$REQ" | grep -c -- "--hash=sha256:")

CP312_WHL="multidict-${VER}-cp312-cp312-manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64.whl"
CP312_HASH=$(curl -s --max-time 30 "https://pypi.org/pypi/multidict/${VER}/json" | \
    python3 -c "import json,sys
d=json.load(sys.stdin)
for f in d['urls']:
    if f['filename']=='$CP312_WHL': print(f['digests']['sha256'])")
IN_FILE=$(grep -c -- "--hash=sha256:${CP312_HASH}" "$REQ" || true)

echo "multidict==${VER}: ${NHASH} hashes pinned; cp312 manylinux x86_64 wheel hash present: ${IN_FILE}" | tee -a "$OUT"
if [ "$NHASH" -eq 1 ] && [ "$IN_FILE" -eq 1 ]; then
    echo "F1 CONTRACT: PASS (single-platform pin, matches file convention)" | tee -a "$OUT"
    F1=0
else
    echo "F1 CONTRACT: FAIL — ${NHASH} hashes pinned instead of the single documented platform hash" | tee -a "$OUT"
    F1=1
fi

ZERO=$(printf '0%.0s' $(seq 64))
PIPFLAGS=(--no-cache-dir --retries 5 --python-version 312 --abi cp312
    --implementation cp --platform manylinux_2_28_x86_64 --platform manylinux2014_x86_64
    --only-binary=:all: --require-hashes)

# case A: the PR-style entry with the cp312 hash invalidated
MINIA=$(mktemp); MINIB=$(mktemp)
sed -n "/^multidict==/,/# via /p" "$REQ" | sed "s/--hash=sha256:${CP312_HASH}/--hash=sha256:${ZERO}/" > "$MINIA"
DA=$(mktemp -d)
pip download -q "${PIPFLAGS[@]}" -r "$MINIA" -d "$DA" >/dev/null 2>&1
RCA=$?
PICKEDA=$(ls "$DA" 2>/dev/null | grep "^multidict" || echo "(none)")

# case B: the trimmed convention with its single hash invalidated
printf 'multidict==%s \\\n    --hash=sha256:%s\n' "$VER" "$ZERO" > "$MINIB"
DB=$(mktemp -d)
pip download -q "${PIPFLAGS[@]}" -r "$MINIB" -d "$DB" >/dev/null 2>&1
RCB=$?
PICKEDB=$(ls "$DB" 2>/dev/null | grep "^multidict" || echo "(none)")

{
    echo
    echo "Fallback demo (mini-lock, cp312/manylinux x86_64 resolution):"
    echo "  A) PR-style entry, cp312 wheel hash zeroed:"
    echo "       pip exit=${RCA}, multidict artifact chosen: ${PICKEDA}"
    if echo "$PICKEDA" | grep -q "py3-none-any"; then
        echo "       => SILENT FALLBACK to the pure-python wheel: build succeeds with no C extension."
    else
        echo "       => no fallback observed; see note below."
    fi
    echo "  B) trimmed single-hash entry, that hash zeroed:"
    echo "       pip exit=${RCB}, multidict artifact chosen: ${PICKEDB}"
    if [ "$RCB" -ne 0 ]; then
        echo "       => hard failure: the lock refuses the resolution (fail-loudly property)."
    else
        echo "       => unexpected success; inspect manually."
    fi
} | tee -a "$OUT"
exit $F1
