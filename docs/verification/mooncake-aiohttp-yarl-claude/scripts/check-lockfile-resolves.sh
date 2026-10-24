#!/usr/bin/env bash
# Integration-layer contract test for the mooncake e2e image dependency lock.
#
# Reproduces, without docker, the exact pip step of
# test/gha-e2e/mooncake/image/Dockerfile:
#
#   RUN pip install --no-cache-dir --only-binary=:all: --require-hashes \
#           -r /tmp/requirements.txt
#
# The image is python:3.12.13-slim on linux/amd64 (glibc >= 2.36), so the
# target interpreter is CPython 3.12 and the platform is manylinux x86_64.
# The pinned wheels span the whole glibc range the real image's pip accepts:
# mooncake-transfer-engine-non-cuda ships only a manylinux_2_28 cp312 wheel
# while nvidia-cuda-runtime-cu12 ships only manylinux2014 (= manylinux_2_17),
# so both platforms are declared. `pip download` with
# --python-version/--abi/--platform then runs the identical resolver + hash
# verification for that target on any host python. (pip's wheel *cache* is
# kept so re-runs on the same machine are fast; it does not affect resolution.)
#
# Contract (this is a CONTRACT test, not a canary):
#   exit 0 / status "lockfile-resolves  pass"  -> the image build's pip step
#                                                 would succeed on linux/amd64
#   exit != 0 / "fail"                        -> the image build fails
#
# Usage: check-lockfile-resolves.sh <requirements.txt>
# Env:   STATUS_FILE   optional file to append "lockfile-resolves<TAB>pass|fail"
#        LOG_FILE      optional path for the pip log (default: alongside status)
set -uo pipefail

REQ="${1:?usage: $0 <requirements.txt> [see header for env]}"
NAME="lockfile-resolves"
STATUS_FILE="${STATUS_FILE:-}"
LOG_FILE="${LOG_FILE:-}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

if [ -n "$STATUS_FILE" ] && [ -z "$LOG_FILE" ]; then
    LOG_FILE="$(dirname "$STATUS_FILE")/pip-download.log"
fi

PIP=()
if [ -n "${PIP_BIN:-}" ]; then
    PIP=("$PIP_BIN")
else
    if python3 -m pip --version >/dev/null 2>&1; then PIP=(python3 -m pip); else PIP=(pip3); fi
    # pip < 24 can crash fetching PyPI's index (vendored cachecontrol bug); if
    # the host pip is that old, build a small venv with a current pip instead.
    major="$("${PIP[@]}" --version 2>/dev/null | awk '{print $2}' | cut -d. -f1)"
    if [ -n "$major" ] && [ "$major" -lt 24 ] && python3 -m venv --help >/dev/null 2>&1; then
        VENV="${MOONCAKE_VERIFY_VENV:-${TMPDIR:-/tmp}/mooncake-verify-venv}"
        if [ ! -x "$VENV/bin/pip" ]; then
            python3 -m venv "$VENV" >/dev/null 2>&1 || true
        fi
        if [ -x "$VENV/bin/pip" ] && [ -z "$($VENV/bin/pip --version 2>/dev/null | awk '{print $2}' | awk -F. '$1>=24{print}')" ]; then
            "$VENV/bin/pip" install -q --index-url https://pypi.org/simple \
                --upgrade pip >/dev/null 2>&1 || true
        fi
        if [ -x "$VENV/bin/pip" ] && [ -n "$($VENV/bin/pip --version 2>/dev/null | awk '{print $2}' | awk -F. '$1>=24{print}')" ]; then
            PIP=("$VENV/bin/pip")
        fi
    fi
fi
"${PIP[@]}" --version >/dev/null 2>&1 || { echo "no usable pip found" >&2; exit 3; }

rc=0
"${PIP[@]}" download \
    --disable-pip-version-check \
    --index-url https://pypi.org/simple \
    --only-binary=:all: --require-hashes \
    --implementation cp --python-version 3.12 --abi cp312 \
    --platform manylinux_2_28_x86_64 \
    --platform manylinux2014_x86_64 \
    --dest "$TMP/wheels" \
    -r "$REQ" >"$TMP/pip.log" 2>&1 || rc=$?

if [ "$rc" -eq 0 ]; then verdict=pass; else verdict=fail; fi
echo "$NAME: $verdict (pip exit $rc)"

if [ -n "$STATUS_FILE" ]; then
    printf '%s\t%s\n' "$NAME" "$verdict" >>"$STATUS_FILE"
fi
if [ -n "$LOG_FILE" ]; then
    cp "$TMP/pip.log" "$LOG_FILE"
fi

# show the interesting part of the log either way
grep -E "^(ERROR|INFO: pip is looking|The conflict|    [A-Za-z].*depends on|The user requested)" \
    "$TMP/pip.log" | head -20 || true

exit "$rc"
