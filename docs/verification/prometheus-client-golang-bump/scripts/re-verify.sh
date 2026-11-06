#!/usr/bin/env bash
# Re-verify the review findings for PR #6230 against the current PR head.
# No arguments: fetches the PR head from the manifest's "pr" URL.
# With a ref: grafts the harness onto that ref instead.
#
# Prints per finding: Fixed / Still-broken / Partial / Harness-update, and the
# incremental review delta (last-reviewed..head). Exit 0 iff all findings are
# resolved (or their harness is satisfied).
#
# Findings covered:
#   F1 (blocker) go.mod minimum Go > CI's GO_VERSION pin -> every CI job fails
#                (contract: check_ci_toolchain_contract.sh; red = still-broken)
#   F2 (major)   go.mod minimum Go > pinned builder images' Go (11 Dockerfiles)
#                (contract: check_builder_image_go.sh; red = still-broken)
#   S1 (info)    dependency bump soundness: builds/vets/links with Go >= 1.26
#                (contract: check_dep_bump_soundness.sh; red = NEW, stronger
#                finding - the dep itself breaks fluid)
#
# Requires: git, go (>= 1.26 for S1), network on first run (toolchain download
# for the F1 check). No cluster, no docker, no KUBECONFIG needed.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="$HERE/verify-manifest.json"
REPO="$(cd "$HERE/../../.." && pwd)"
PR_URL="$(python3 -c "import json;print(json.load(open('$MANIFEST'))['pr'])" 2>/dev/null)"
PR_NUM="${PR_URL##*/}"
ORIGIN="https://github.com/fluid-cloudnative/fluid.git"

REF="${1:-}"
if [ -z "$REF" ]; then
  echo "== fetching current PR head for $PR_URL =="
  git -C "$REPO" fetch "$ORIGIN" "pull/$PR_NUM/head" || exit 1
  REF="FETCH_HEAD"
fi
HEAD_SHA="$(git -C "$REPO" rev-parse "$REF")"
echo "PR head: $HEAD_SHA"

echo "== grafting harness onto $REF =="
# Round-2 note: a plain `git checkout <ref> -- .` overlay leaves stale files
# behind when the PR (or its base, via a rebase) DELETED files - which broke
# vendored builds. Graft by detaching the worktree onto the ref (this moves
# HEAD only, NOT the verify branch pointer, and applies deletions), then
# restore the harness directory from the verify branch.
git -C "$REPO" checkout --detach "$REF" >/dev/null 2>&1 || \
  git -C "$REPO" checkout "$REF" -- . 2>/dev/null || true
git -C "$REPO" clean -fdq >/dev/null 2>&1 || true
git -C "$REPO" checkout verify/prometheus-client-golang-bump-claude -- \
  docs/verification/prometheus-client-golang-bump 2>/dev/null || \
  echo "WARN: could not graft harness paths (already detached at the harness commit?)"

if [ -f "$HERE/.last-reviewed" ]; then
  LAST="$(cat "$HERE/.last-reviewed")"
  echo
  echo "== incremental review delta ($LAST..$HEAD_SHA) =="
  git -C "$REPO" log --oneline "$LAST..$HEAD_SHA" 2>/dev/null || echo "(no delta)"
  echo "advance the marker after the round:  echo $HEAD_SHA > '$HERE/.last-reviewed'"
fi

echo
echo "== F1: CI toolchain contract =="
bash "$HERE/scripts/check_ci_toolchain_contract.sh" "$HERE/results/ci-toolchain-contract.txt"
F1=$?

echo
echo "== F2: builder image pin contract =="
bash "$HERE/scripts/check_builder_image_go.sh" "$HERE/results/builder-image-go.txt"
F2=$?

echo
echo "== S1: dependency bump soundness (needs go >= 1.26 locally) =="
bash "$HERE/scripts/check_dep_bump_soundness.sh" "$HERE/results/dep-bump-soundness.txt"
S1=$?

echo
echo "================ per-finding verdict ================"
verdict() { # exit-code, id, label
  if [ "$1" -eq 0 ]; then echo "$2: Fixed   ($3)"; elif [ "$1" -eq 2 ]; then echo "$2: Harness-update ($3 - skip: no suitable local go)"; else echo "$2: Still-broken ($3)"; fi
}
verdict $F1 F1 "ci-toolchain-contract"
verdict $F2 F2 "builder-image-go"
if [ "$S1" -eq 0 ]; then echo "S1: Pass (dependency bump remains sound)"; elif [ "$S1" -eq 2 ]; then echo "S1: Harness-update (no local go >= module minimum)"; else echo "S1: NEW FINDING - dependency bump itself is broken"; fi

echo
echo "raw output: $HERE/results/*.txt"
[ "$F1" -eq 0 ] && [ "$F2" -eq 0 ] && [ "$S1" -ne 1 ] && exit 0
exit 1
