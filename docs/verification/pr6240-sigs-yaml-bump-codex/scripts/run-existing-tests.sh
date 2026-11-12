#!/usr/bin/env bash
# Runs the existing unit tests of every first-party package that imports
# sigs.k8s.io/yaml, at the current checkout. Usage: run-existing-tests.sh
set -uo pipefail
HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WT="$(cd "${HARNESS_DIR}/../../.." && pwd)"
cd "${WT}"
PKGS="pkg/dataprocess pkg/ddc/jindofsx pkg/ddc/jindo pkg/ddc/juicefs pkg/ddc/thin pkg/ddc/alluxio pkg/ddc/vineyard pkg/ddc/jindocache pkg/ddc/cache/engine pkg/ddc/efc pkg/utils pkg/webhook/plugins/..."
ARGS="$(for p in $PKGS; do echo "github.com/fluid-cloudnative/fluid/$p"; done)"
go test -count=1 -timeout 20m $ARGS 2>&1 | tee "${HARNESS_DIR}/results/existing-tests-$(git rev-parse --short HEAD).txt"
