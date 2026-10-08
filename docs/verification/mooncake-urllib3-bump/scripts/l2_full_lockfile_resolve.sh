#!/usr/bin/env bash
# L2 integration: prove the merged lock file (with the bumped urllib3 entry)
# still resolves end-to-end against the real PyPI for the image's platform
# (cp312 / manylinux x86_64), with --require-hashes so every artifact's digest
# is verified — i.e. simulate exactly what the Dockerfile's pip install does,
# without needing docker.
#
# Contract polarity: exit 0 = lock file installs as written; non-zero = broken.
set -euo pipefail
REQ="${1:?usage: l2_full_lockfile_resolve.sh <requirements.txt> [dest-dir]}"
DEST="${2:-/tmp/mooncake-lock-download}"
VENV="${VENV:-/tmp/verify-venv}"
PIP="$VENV/bin/pip"
[ -x "$PIP" ] || { python3 -m venv "$VENV"; "$VENV/bin/pip" install -q --no-cache-dir --upgrade pip; }

rm -rf "$DEST"; mkdir -p "$DEST"
"$PIP" download --no-cache-dir --require-hashes --only-binary=:all: \
    --implementation cp --python-version 312 --abi cp312 \
    --platform manylinux2014_x86_64 \
    --platform manylinux_2_17_x86_64 \
    --platform manylinux_2_28_x86_64 \
    --platform manylinux_2_35_x86_64 \
    --platform manylinux_2_36_x86_64 \
    -r "$REQ" -d "$DEST"
echo "L2a PASS: $(ls "$DEST" | wc -l) artifacts downloaded and hash-verified"
ls "$DEST"
