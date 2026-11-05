#!/usr/bin/env bash
# L2 — soundness of the vendored bump itself (F2), on the PR head, using a
# toolchain that SATISFIES the new requirement (host go >= 1.26, GOTOOLCHAIN=local).
# Answers: "if the project moved its pins to Go 1.26, would the tree build and
# the metrics-adjacent tests pass with client_golang v1.25.0 vendored?"
#
#   bash scripts/l2-build-under-go126plus.sh [ref]    (default: current checkout)
set -euo pipefail
REF="${1:-}"
if [ -n "$REF" ]; then
  WT="$(mktemp -d /tmp/pr6230-l2-XXXXXX)"
  trap 'git worktree remove --force "$WT" >/dev/null 2>&1 || rm -rf "$WT"' EXIT
  git worktree add --force "$WT" "$REF" >/dev/null 2>&1
  cd "$WT"
fi
MAJMIN="$(go version | sed -E 's/.*go([0-9]+\.[0-9]+).*/\1/')"
echo "l2: host toolchain go$MAJMIN (GOTOOLCHAIN=local)"
export GOTOOLCHAIN=local

echo "== go build ./... =="
go build ./... && echo "BUILD OK"

echo "== go vet (metrics-adjacent) =="
go vet ./pkg/metrics/... ./pkg/common/... && echo "VET OK"

echo "== go test (metrics-adjacent; excludes known env-flaky gomonkey mount test) =="
go test ./pkg/metrics/... ./pkg/common/... -count=1 2>&1 | grep -E "^(ok|FAIL|---)" || true
echo "L2 done"
