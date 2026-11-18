#!/usr/bin/env bash
# Re-run the P0 premise check: graft the TestVerify harness onto the PR's BASE
# and confirm the contract tests FAIL there (i.e. the non-determinism exists
# without the patch). Usage: run from the repo root, any branch:
#   bash docs/verification/pr-6262-webhook-deterministic-mutation/scripts/verify-premise-on-base.sh [base-sha]
set -euo pipefail
BASE="${1:-dcb76167b0143a239addc6bed83ed50b45267631}"
REPO="$(git rev-parse --show-toplevel)"
WT="$(mktemp -d)/base-wt"
trap 'git -C "$REPO" worktree remove --force "$WT" >/dev/null 2>&1 || true' EXIT
git -C "$REPO" worktree add "$WT" "$BASE" >/dev/null
for f in \
  pkg/application/inject/fuse/zz_verify_pr6262_test.go \
  pkg/webhook/plugins/fileprefetcher/zz_verify_pr6262_test.go \
  pkg/webhook/plugins/nodeaffinitywithcache/zz_verify_pr6262_test.go \
  pkg/webhook/plugins/mountpropagationinjector/zz_verify_pr6262_test.go; do
  git -C "$REPO" show "HEAD:$f" > "$WT/$f"
done
cd "$WT"
# Expect FAILURES on the two real-fix packages; invariance probes must still pass.
go test -count=1 -run 'TestVerify' \
  ./pkg/application/inject/fuse/ \
  ./pkg/webhook/plugins/fileprefetcher/ \
  ./pkg/webhook/plugins/nodeaffinitywithcache/ \
  ./pkg/webhook/plugins/mountpropagationinjector/
