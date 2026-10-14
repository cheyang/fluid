#!/usr/bin/env bash
# Informational (not part of re-verify.sh's gate): did the PR's own pull_request CI
# actually execute the bumped upload-artifact pin, and did those steps succeed?
# Requires: gh authenticated against github.com.
# Note: results are a snapshot for the reviewed head; re-run after new pushes.
set -uo pipefail
PR=6210
echo "== workflows triggered on pull_request (these run the PR-head workflow files) =="
for wf in kind-e2e.yml backward-compatibility-e2e.yml; do
  echo "-- .github/workflows/$wf trigger:"; sed -n '/^on:/,/^[a-z]/p' ".github/workflows/$wf" | head -6
done
echo
echo "== 'Dump environment' step conclusions on the PR runs (these use the new pin) =="
for run in $(gh api "repos/fluid-cloudnative/fluid/pulls/$PR" --jq '.head.sha' >/dev/null 2>&1 && \
             gh api "repos/fluid-cloudnative/fluid/commits/$(gh api repos/fluid-cloudnative/fluid/pulls/$PR --jq .head.sha)/check-runs?per_page=100" \
               --jq '.check_runs[].details_url' 2>/dev/null | grep -oP 'actions/runs/\K[0-9]+' | sort -u); do
  gh api "repos/fluid-cloudnative/fluid/actions/runs/$run/jobs?per_page=100" \
    --jq '.jobs[] | .name as $n | .steps[] | select(.name | test("Dump environment")) | "run '"$run"' \($n): \(.conclusion)"' 2>/dev/null
done
echo
echo "== full check rollup =="
gh pr checks $PR --repo fluid-cloudnative/fluid 2>/dev/null || true
