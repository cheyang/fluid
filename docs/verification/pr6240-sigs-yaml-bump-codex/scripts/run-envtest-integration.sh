#!/usr/bin/env bash
# Grafts the additive sigsyamlverify package onto a worktree at <ref> and runs
# the envtest integration test (real kube-apiserver). Requires envtest assets;
# override with KUBEBUILDER_ASSETS. Captures CASE lines to
# results/envtest-<shortref>.txt.
set -euo pipefail
REF="${1:?usage: run-envtest-integration.sh <git-ref>}"
HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WT="$(cd "${HARNESS_DIR}/../../.." && pwd)"
REPO="$(cd "${WT}" && git rev-parse --git-common-dir | xargs dirname)"
export KUBEBUILDER_ASSETS="${KUBEBUILDER_ASSETS:-/home/ecs-user/envtest-assets/k8s/1.30.3-linux-amd64}"
SHORT="$(git -C "${REPO}" rev-parse --short "${REF}")"
TMP="$(mktemp -d)"
trap 'git -C "${REPO}" worktree remove --force "${TMP}" 2>/dev/null || rm -rf "${TMP}"' EXIT
git -C "${REPO}" worktree add --detach "${TMP}" "${REF}" >/dev/null
mkdir -p "${TMP}/pkg/utils/sigsyamlverify"
cp "${WT}/pkg/utils/sigsyamlverify/"*.go "${TMP}/pkg/utils/sigsyamlverify/"
cd "${TMP}"
go test -count=1 -v ./pkg/utils/sigsyamlverify/ -run TestEnvtestYamlRoundTrip 2>&1 \
  | grep -oE 'CASE [^ ]+ \| OK.*' > "${HARNESS_DIR}/results/envtest-${SHORT}.txt" || {
      echo "envtest integration FAILED on ${REF}" >&2; exit 1; }
cat "${HARNESS_DIR}/results/envtest-${SHORT}.txt"
