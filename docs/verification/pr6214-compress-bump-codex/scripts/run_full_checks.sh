#!/usr/bin/env bash
# Full verification for PR #6214 (klauspost/compress 1.17.11 -> 1.18.7).
# Run from the repo root of a checkout that contains the harness
# (branch verify/pr6214-compress-bump-codex, or a graft onto a newer head).
set -euo pipefail

echo "== [1/4] go build ./... (vendor/go.mod consistency + toolchain compat)"
go build ./...

echo "== [2/4] module hashes vs go.sum"
go mod download github.com/klauspost/compress
go mod verify

echo "== [3/4] vendor integrity: regenerate and require zero diff"
go mod vendor
rc=0
if git diff --quiet -- vendor go.mod go.sum; then
  echo "vendor integrity: OK (regenerated tree identical to committed)"
else
  echo "vendor integrity: MISMATCH" >&2
  git diff --stat -- vendor go.mod go.sum >&2
  rc=1
fi
git checkout -f -- vendor go.mod go.sum

echo "== [4/4] contract tests (consumer path + boundaries + version guard)"
go test ./test/verification/pr6214compress/ -count=1 -v

exit $rc
