#!/usr/bin/env bash
# Contract test (polarity: contract) for PR #6212 finding F1.
#
# Contract: test/gha-e2e/mooncake/image/requirements.txt must resolve under the
# exact install discipline of the image build
# (Dockerfile: pip install --only-binary=:all: --require-hashes -r requirements.txt,
# base image python:3.12.13-slim, linux/amd64), i.e. the pinned versions of all
# packages must be mutually consistent AND every pinned artifact hash must exist.
#
# This script simulates the image build's pip resolution without needing docker:
# it forces pip to resolve for CPython 3.12 / manylinux x86_64, which is the
# platform the Dockerfile builds on. It prints one result line:
#   MooncakeRequirementsResolve<TAB>pass|fail
# and exits 0 on pass, 1 on fail.
#
# Needs: python3 + pip, network access to a PyPI index that carries
# mooncake-transfer-engine-non-cuda (the default pypi.org does; some mirrors do
# not). Override the index with PIP_INDEX_URL.
set -uo pipefail

REQ="${1:-test/gha-e2e/mooncake/image/requirements.txt}"
INDEX="${PIP_INDEX_URL:-https://pypi.org/simple}"

if [ ! -f "$REQ" ]; then
  echo "check-requirements-resolution: requirements file not found: $REQ" >&2
  echo -e "MooncakeRequirementsResolve\tfail"
  exit 1
fi

pip3 install --dry-run --ignore-installed --no-cache-dir \
  --only-binary=:all: --require-hashes \
  -i "$INDEX" \
  --python-version 3.12 --implementation cp --abi cp312 \
  --platform manylinux_2_28_x86_64 --platform manylinux2014_x86_64 --platform manylinux_2_17_x86_64 \
  -r "$REQ"
rc=$?
if [ "$rc" -eq 0 ]; then
  echo -e "MooncakeRequirementsResolve\tpass"
  exit 0
else
  echo -e "MooncakeRequirementsResolve\tfail"
  exit 1
fi
