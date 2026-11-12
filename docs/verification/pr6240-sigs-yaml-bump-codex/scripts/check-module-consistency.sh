#!/usr/bin/env bash
# Verifies the dependabot bump is mechanically consistent at <ref>:
#   go build ./... (proves vendor/modules.txt matches go.mod and everything compiles)
#   go mod verify  (module cache hashes match go.sum)
#   go mod tidy is a no-op for go.mod/go.sum (no missing/extra requirements)
set -uo pipefail
REF="${1:?usage: check-module-consistency.sh <git-ref>}"
HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO="$(cd "${HARNESS_DIR}/../../.." && git rev-parse --git-common-dir | xargs dirname)"
SHORT="$(git -C "${REPO}" rev-parse --short "${REF}")"
OUT="${HARNESS_DIR}/results/module-consistency-${SHORT}.txt"
TMP="$(mktemp -d)"
trap 'git -C "${REPO}" worktree remove --force "${TMP}" 2>/dev/null || rm -rf "${TMP}"' EXIT
git -C "${REPO}" worktree add --detach "${TMP}" "${REF}" >/dev/null
cd "${TMP}"
{
  echo "== ref ${REF} =="
  echo "--- go build ./..."
  go build ./... && echo "build: OK" || echo "build: FAIL"
  echo "--- go mod verify"
  go mod verify
  echo "--- go mod tidy no-op check"
  cp go.mod /tmp/tidy.go.mod; cp go.sum /tmp/tidy.go.sum
  go mod tidy >/dev/null 2>&1
  if diff -q /tmp/tidy.go.mod go.mod >/dev/null && diff -q /tmp/tidy.go.sum go.sum >/dev/null; then
    echo "tidy: NO-OP"
  else
    echo "tidy: DIRTY"
    diff /tmp/tidy.go.mod go.mod | head -20
    diff /tmp/tidy.go.sum go.sum | head -20
  fi
} | tee "${OUT}"
