#!/usr/bin/env bash
# Grafts the additive sigsyamlverify test package onto a worktree at <ref> and
# runs the YAML behavior matrix, writing normalized "CASE ..." lines to
# results/behavior-<shortref>.txt. Diffing two result files proves whether the
# sigs.k8s.io/yaml bump changed any observable behavior.
set -euo pipefail

REF="${1:?usage: run-behavior-matrix.sh <git-ref>}"
HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"          # docs/verification/<topic>
WT="$(cd "${HARNESS_DIR}/../../.." && pwd)"                                # repo worktree (verify branch checkout)
REPO="$(cd "${WT}" && git rev-parse --git-common-dir | xargs dirname)"  # main repo
OUT_DIR="${HARNESS_DIR}/results"
mkdir -p "${OUT_DIR}"

SHORT="$(git -C "${REPO}" rev-parse --short "${REF}")"
TMP="$(mktemp -d)"
trap 'git -C "${REPO}" worktree remove --force "${TMP}" 2>/dev/null || rm -rf "${TMP}"' EXIT

git -C "${REPO}" worktree add --detach "${TMP}" "${REF}" >/dev/null
mkdir -p "${TMP}/pkg/utils/sigsyamlverify"
cp "${WT}/pkg/utils/sigsyamlverify/"*.go "${TMP}/pkg/utils/sigsyamlverify/"

cd "${TMP}"
go test -count=1 -v ./pkg/utils/sigsyamlverify/ -run TestYamlBehaviorMatrix 2>&1 \
  | grep '^CASE ' > "${OUT_DIR}/behavior-${SHORT}.txt" || {
      echo "behavior matrix FAILED on ${REF}" >&2; exit 1; }
echo "wrote ${OUT_DIR}/behavior-${SHORT}.txt ($(wc -l < "${OUT_DIR}/behavior-${SHORT}.txt") cases)"
