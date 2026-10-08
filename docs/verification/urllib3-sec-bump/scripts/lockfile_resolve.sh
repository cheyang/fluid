#!/usr/bin/env bash
# T2 — lockfile resolution (integration layer).
#
# Claim under test: the hash-locked requirements.txt (with urllib3 bumped to
# 2.8.0) still resolves and passes hash verification for the exact environment
# the Docker build targets — Python 3.12 (image base python:3.12.13-slim),
# CPython, cp312 ABI, manylinux x86_64 — under `--require-hashes
# --only-binary=:all:`, which is precisely what
# test/gha-e2e/mooncake/image/Dockerfile runs at build time.
#
# This machine has no docker, so the Dockerfile's `pip install` is emulated
# with `pip download` cross-environment resolution (same resolver, same
# --require-hashes path, same wheel tags); the artifacts land in a temp dir
# instead of site-packages. The manylinux platform list covers every tag the
# pinned wheels actually carry (manylinux1/2014/2_5/2_17/2_28 x86_64).
#
# PASS (exit 0) = pip resolves the whole file and every hash verifies.
# FAIL (exit 1) = resolution or hash verification breaks (bad pin, missing
# transitive, constraint conflict) — i.e. the e2e image build would go red.
#
# Polarity: contract. Written 2026-10-08 by Reviewer A (Claude).
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
REQ="${1:-$REPO_ROOT/test/gha-e2e/mooncake/image/requirements.txt}"
LOG="${VERIFY_LOG_DIR:-$REPO_ROOT/docs/verification/urllib3-sec-bump/results}/lockfile_resolve.log"
mkdir -p "$(dirname "$LOG")"

DEST="$(mktemp -d)"
trap 'rm -rf "$DEST"' EXIT

echo "[T2] emulating the Dockerfile install for cp312 / manylinux x86_64 against: $REQ" | tee "$LOG"

pip3 download --no-cache-dir --require-hashes --only-binary=:all: \
    --python-version 3.12 --implementation cp --abi cp312 \
    --platform manylinux2014_x86_64 \
    --platform manylinux1_x86_64 \
    --platform manylinux_2_5_x86_64 \
    --platform manylinux_2_17_x86_64 \
    --platform manylinux_2_28_x86_64 \
    --dest "$DEST" -r "$REQ" >>"$LOG" 2>&1
rc=$?

if [ $rc -ne 0 ]; then
    echo "[T2] RESULT: FAIL — pip could not resolve/verify the lockfile (exit $rc); the image build would fail" | tee -a "$LOG"
    tail -20 "$LOG"
    exit 1
fi

if [ ! -f "$DEST/urllib3-2.8.0-py3-none-any.whl" ]; then
    echo "[T2] RESULT: FAIL — resolution succeeded but the urllib3 2.8.0 wheel was not selected" | tee -a "$LOG"
    exit 1
fi

count="$(ls "$DEST" | wc -l | tr -d ' ')"
echo "[T2] resolved and hash-verified $count wheels, including urllib3-2.8.0-py3-none-any.whl" | tee -a "$LOG"
echo "[T2] RESULT: PASS" | tee -a "$LOG"
exit 0
