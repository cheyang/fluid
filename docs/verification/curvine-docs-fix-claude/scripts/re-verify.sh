#!/usr/bin/env bash
# Re-run the PR #6252 verification harness against the CURRENT PR head.
# Usage: bash scripts/re-verify.sh [<ref>]     (no ref -> current PR head)
# Exit 0 iff every PR-scoped contract check passes. The C5 canary (pre-existing
# step-6 label bug, out of this PR's scope) is reported but does not gate.
set -uo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
topic_root="$(dirname "$here")"          # docs/verification/curvine-docs-fix-claude
repo_root="$(cd "$topic_root/../../.." && pwd)"
manifest="$topic_root/verify-manifest.json"
last_reviewed_file="$topic_root/.last-reviewed"
upstream="https://github.com/fluid-cloudnative/fluid"
pr_url=$(python3 -c "import json;print(json.load(open('$manifest'))['pr'])")
pr_n=$(printf '%s\n' "$pr_url" | grep -o 'pull/[0-9]*' | grep -o '[0-9]*')

if [ $# -ge 1 ]; then
  head_ref="$1"
else
  echo ">>> fetching current head of $pr_url"
  git -C "$repo_root" fetch "$upstream" "pull/$pr_n/head" || exit 2
  head_ref=$(git -C "$repo_root" rev-parse FETCH_HEAD)
fi
head_sha=$(git -C "$repo_root" rev-parse "$head_ref^{commit}")
echo ">>> verifying PR #$pr_n head $head_sha"

base_sha=$(git -C "$repo_root" merge-base "$head_sha" "origin/master" 2>/dev/null || true)
last_reviewed=$(cat "$last_reviewed_file" 2>/dev/null || echo "")
echo ">>> last reviewed: ${last_reviewed:-<none, using merge-base $base_sha>}"
if [ -n "$last_reviewed" ] && [ "$last_reviewed" != "$head_sha" ]; then
  echo ">>> incremental delta to review: $last_reviewed..$head_sha"
  git -C "$repo_root" log --oneline "$last_reviewed..$head_sha" 2>/dev/null | head -20
fi

out=$topic_root/results/reverify.out
python3 "$here/check_doc_fixes.py" --repo-root "$repo_root" --ref "$head_sha" 2>&1 | tee "$out"
rc=${PIPESTATUS[0]}

echo
echo ">>> per-finding verdicts:"
if grep -q '^RESULT: PASS' "$out"; then
  echo "    F1 (heredoc quoting, image tag): Fixed at $head_sha — all contract checks green"
else
  echo "    F1 (heredoc quoting, image tag): Still-broken / Partial at $head_sha — see $out"
fi
if grep -q '^\[pass\] C5_canary_step6_label_absent' "$out"; then
  echo "    F2 (step-6 pod label, PRE-EXISTING, non-blocking): still present (canary green) — follow-up fix needed, not this PR"
elif grep -q '^\[FAIL\] C5_canary_step6_label_absent' "$out"; then
  echo "    F2 (step-6 pod label, PRE-EXISTING): canary FLIPPED — a fix landed; invert C5 into a contract test"
else
  echo "    F2 (step-6 pod label): Harness-update — check script changed shape"
fi
if [ "$rc" -eq 0 ]; then
  echo ">>> PASS. To advance the marker after this round: git checkout <this-branch> && echo $head_sha > $last_reviewed_file"
fi
exit "$rc"
