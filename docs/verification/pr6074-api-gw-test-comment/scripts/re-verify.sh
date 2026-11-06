#!/usr/bin/env bash
# re-verify.sh — PR #6074 (https://github.com/fluid-cloudnative/fluid/pull/6074)
# Comments-only PR: adds a doc comment to TestGetAPIGatewayStatus.
#
# Usage: bash docs/verification/pr6074-api-gw-test-comment/scripts/re-verify.sh [<ref>]
#   No ref  -> the CURRENT PR head is fetched from the manifest's `pr` URL
#              (git fetch <repo> pull/6074/head) — works from any clone/machine.
#   <ref>   -> graft the harness onto that commit-ish instead.
#
# Findings covered (see verify-manifest.json):
#   F1  contract  DCO: PR head commit must carry a Signed-off-by trailer      (gating)
#   AC3 contract  touched package: gofmt/vet clean, API-gw tests pass         (gating)
#   F2  canary    doc comment attached directly above func                    (nit, non-gating:
#                 reported but does not gate the exit code — documented in README)
#
# Exit 0 iff all gating findings are fixed/passing.
set -u

TOPIC_DIR=$(cd "$(dirname "$0")/.." && pwd)          # .../docs/verification/pr6074-api-gw-test-comment
REPO_ROOT=$(cd "$TOPIC_DIR/../../.." && pwd)
MANIFEST="$TOPIC_DIR/verify-manifest.json"
PR_URL=$(grep -o '"pr"[[:space:]]*:[[:space:]]*"[^"]*"' "$MANIFEST" | head -1 | sed 's/.*"\(https[^"]*\)"/\1/')
PR_REPO=$(dirname "$(dirname "$PR_URL")")
PR_NUM=$(basename "$PR_URL")

if [ -n "${1:-}" ]; then
  TARGET="$1"
  # Resolve to a sha so the DCO commit check inspects the right commit.
  TARGET_SHA=$(git -C "$REPO_ROOT" rev-parse --verify "$TARGET^{commit}" 2>/dev/null) || {
    echo "cannot resolve $TARGET"; exit 2; }
else
  git -C "$REPO_ROOT" fetch "$PR_REPO" "pull/$PR_NUM/head" >/dev/null 2>&1 || {
    echo "fetch of $PR_REPO pull/$PR_NUM/head failed"; exit 2; }
  TARGET_SHA=$(git -C "$REPO_ROOT" rev-parse FETCH_HEAD)
fi
echo "== re-verify PR #$PR_NUM against $TARGET_SHA =="

# Incremental review delta (falls back to merge-base when no marker exists).
LAST=$(cat "$TOPIC_DIR/.last-reviewed" 2>/dev/null || true)
if [ -z "$LAST" ]; then
  LAST=$(git -C "$REPO_ROOT" merge-base "$TARGET_SHA" "$(git -C "$REPO_ROOT" rev-parse HEAD)" 2>/dev/null || echo "$TARGET_SHA")
fi
echo "== incremental delta: $LAST..$TARGET_SHA =="
git -C "$REPO_ROOT" log --oneline "$LAST..$TARGET_SHA" 2>/dev/null | sed 's/^/   /'
echo "   (after this round: echo $TARGET_SHA > $TOPIC_DIR/.last-reviewed && commit)"

# Graft the harness onto the target ref in a throwaway worktree.
WORKTREE=$(mktemp -d /tmp/pr6074-verify.XXXXXX)
git -C "$REPO_ROOT" worktree add --detach "$WORKTREE" "$TARGET_SHA" >/dev/null 2>&1 || {
  echo "worktree add failed"; exit 2; }
trap 'git -C "$REPO_ROOT" worktree remove --force "$WORKTREE" 2>/dev/null; rm -rf "$WORKTREE"' EXIT
mkdir -p "$WORKTREE/docs/verification"
cp -r "$TOPIC_DIR" "$WORKTREE/docs/verification/pr6074-api-gw-test-comment"

GATE_FAIL=0
declare -a REPORT

# ---- F1 (contract, gating): DCO sign-off on the PR head commit ----
if (cd "$WORKTREE" && bash docs/verification/pr6074-api-gw-test-comment/scripts/check-dco-trailer.sh "$TARGET_SHA"); then
  REPORT+=("F1  DCO Signed-off-by        : FIXED (commit carries the trailer)")
else
  REPORT+=("F1  DCO Signed-off-by        : STILL-BROKEN (no Signed-off-by on $TARGET_SHA -> DCO check fails, tide blocks merge)")
  GATE_FAIL=1
fi

# ---- AC3 (contract, gating): purely additive, no behavior change ----
ac3=0
gofmt_out=$(cd "$WORKTREE" && gofmt -l pkg/ddc/alluxio/api_gateway_test.go)
if [ -n "$gofmt_out" ]; then REPORT+=("AC3 gofmt                   : FAIL ($gofmt_out)"); ac3=1; else REPORT+=("AC3 gofmt                   : pass"); fi
if (cd "$WORKTREE" && go vet ./pkg/ddc/alluxio/ >/dev/null 2>&1); then
  REPORT+=("AC3 go vet                  : pass")
else
  REPORT+=("AC3 go vet                  : FAIL"); ac3=1
fi
if (cd "$WORKTREE" && go test -run 'TestGetAPIGatewayStatus|TestQueryAPIGatewayEndpoint' ./pkg/ddc/alluxio/ >/dev/null 2>&1); then
  REPORT+=("AC3 go test (API gateway)   : pass (behavior unchanged)")
else
  REPORT+=("AC3 go test (API gateway)   : FAIL (touched package regressed)"); ac3=1
fi
[ "$ac3" -eq 0 ] || GATE_FAIL=1

# ---- F2 (CANARY, nit, non-gating): comment attached above func? ----
f2rc=0
f2out=$(cd "$WORKTREE" && bash docs/verification/pr6074-api-gw-test-comment/scripts/check-doc-comment-attachment.sh) || f2rc=$?
case "$f2rc" in
  0)  REPORT+=("F2  comment attachment (nit) : STILL-PRESENT (canary green: blank line keeps the comment detached from func) [non-gating]") ;;
  1)  REPORT+=("F2  comment attachment (nit) : FIXED (canary flipped — invert into a contract test before next round) [non-gating]") ;;
  *)  REPORT+=("F2  comment attachment (nit) : HARNESS-UPDATE ($f2out) [non-gating]") ;;
esac

echo
echo "== per-finding verdicts =="
printf '   %s\n' "${REPORT[@]}"
echo
if [ "$GATE_FAIL" -eq 0 ]; then
  echo "RESULT: all gating findings fixed (F2 nit may remain; see README for why it is non-gating)."
  exit 0
else
  echo "RESULT: gating findings remain broken."
  exit 1
fi
