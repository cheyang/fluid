#!/usr/bin/env bash
# Integration layer: prove the checked-out requirements.txt resolves and
# hash-verifies for the mooncake image platform (cp312 / manylinux x86_64,
# glibc>=2.28 as in python:3.12.13-slim) under the exact Dockerfile flags
# (--only-binary=:all: --require-hashes). Emits one go-test JSON line.
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
REQ="${REQUIREMENTS_FILE:-$ROOT/test/gha-e2e/mooncake/image/requirements.txt}"
OUT="${TESTJSON_OUT:-$TOPIC_DIR/results/integration-pip.jsonl}"
LOG="$TOPIC_DIR/results/pip-download.log"
DEST="$(mktemp -d /tmp/mpq-wheelhouse-XXXXXX)"
PIP_INDEX="${PIP_INDEX_URL:-https://pypi.org/simple}"
mkdir -p "$(dirname "$OUT")"

echo "[info] downloading $(grep -c '==' "$REQ") pins for cp312/manylinux_x86_64 from $PIP_INDEX" >&2
if python3 -m pip download --no-cache-dir --index-url "$PIP_INDEX" \
      --require-hashes --only-binary=:all: \
      --platform manylinux2014_x86_64 --platform manylinux_2_17_x86_64 --platform manylinux_2_28_x86_64 \
      --python-version 312 --implementation cp --abi cp312 \
      -r "$REQ" -d "$DEST" >"$LOG" 2>&1; then
  n=$(ls "$DEST" | wc -l)
  echo "{\"Test\":\"TestRequirementsResolveForImagePlatform\",\"Action\":\"pass\"}" > "$OUT"
  echo "[PASS] TestRequirementsResolveForImagePlatform: $n wheels downloaded and hash-verified" >&2
  rc=0
else
  echo "{\"Test\":\"TestRequirementsResolveForImagePlatform\",\"Action\":\"fail\"}" > "$OUT"
  echo "[FAIL] TestRequirementsResolveForImagePlatform: pip download failed; see $LOG" >&2
  tail -5 "$LOG" >&2
  rc=1
fi
rm -r "$DEST" 2>/dev/null || true
exit $rc
