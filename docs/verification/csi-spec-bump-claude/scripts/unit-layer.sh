#!/usr/bin/env bash
# unit-layer.sh — unit layer for the PR #6229 verification harness (reviewer: claude).
#
# Finding F1's contract is "the module compiles and the CSI server interfaces
# remain satisfied". A plain `go test -json` run reports a package-level build
# failure with no test-level JSON events, which the generic re-verify.sh parser
# would misclassify as HARNESS-UPDATE ("tests not found"). This wrapper makes
# the outcome machine-readable either way:
#
#   - `go build ./...` fails  -> emit a synthetic go-test JSON *fail* event for
#     TestCSISpecCompatHarness, carrying the compiler errors as "output" events
#     (re-verify.sh then reports F1 as STILL-BROKEN, which is the truth).
#   - `go build ./...` passes -> pass through the real `go test -json` output
#     for the harness test.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"

PKG="github.com/fluid-cloudnative/fluid/pkg/csi/plugins"
TEST="TestCSISpecCompatHarness"
BUILD_LOG="$(mktemp)"
trap 'rm -f "$BUILD_LOG"' EXIT

if go build ./... >"$BUILD_LOG" 2>&1; then
  exec go test ./pkg/csi/plugins/ -run "^${TEST}$" -count=1 -json
else
  # NOTE: emit ONLY go-test JSON on stdout (stderr is captured into the same
  # output file by re-verify.sh, and any non-JSON line breaks its jq parser).
  # Compiler errors are embedded as go-test "output" events (valid JSON,
  # ignored by the parser's pass/fail select, but kept for human inspection).
  if [ -s "$BUILD_LOG" ]; then
    jq -Rs 'split("\n") | map(select(length > 0)) | .[]
            | {Action: "output", Test: "TestCSISpecCompatHarness",
               Package: "github.com/fluid-cloudnative/fluid/pkg/csi/plugins",
               Output: (. + "\n")}' "$BUILD_LOG"
  fi
  printf '{"Action":"fail","Test":"%s","Package":"%s","Elapsed":0}\n' "$TEST" "$PKG"
  printf '{"Action":"fail","Package":"%s","Elapsed":0}\n' "$PKG"
fi
