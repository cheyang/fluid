#!/usr/bin/env bash
# integration-layer.sh — runs the two controller test packages tracked by the
# gomega-bump verify manifest and emits go-test-json-shaped events on stdout
# so re-verify.sh (gotest kind) can score them per finding.
#
# Why a wrapper: these two suites fail inside ginkgo's BeforeSuite (broken
# decorators), and in that mode `go test -json` emits only package-level
# events with no Test-level events, so re-verify cannot match them by name.
# The synthetic events below keep one stable, distinct test name per package:
#   TestAPIs/alluxio      <- pkg/controllers/v1alpha1/alluxio
#   TestAPIs/dataprocess  <- pkg/controllers/v1alpha1/dataprocess
#
# envtest assets are machine-local; override with KUBEBUILDER_ASSETS.
set -uo pipefail
export KUBEBUILDER_ASSETS="${KUBEBUILDER_ASSETS:-$HOME/.local/share/kubebuilder-envtest/k8s/1.31.0-linux-amd64}"

run_pkg() { # $1 = import path arg, $2 = synthetic test name
  if go test -count=1 -timeout 15m "$1" >/dev/null 2>&1; then
    printf '{"Action":"pass","Package":"%s","Test":"%s","Elapsed":0}\n' "$1" "$2"
  else
    printf '{"Action":"fail","Package":"%s","Test":"%s","Elapsed":0}\n' "$1" "$2"
  fi
}

run_pkg ./pkg/controllers/v1alpha1/alluxio/     "TestAPIs/alluxio"
run_pkg ./pkg/controllers/v1alpha1/dataprocess/ "TestAPIs/dataprocess"
