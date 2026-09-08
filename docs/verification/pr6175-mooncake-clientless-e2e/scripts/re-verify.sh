#!/bin/bash
# re-verify.sh — resume entry point. Resolves the CURRENT PR head from
# manifest.pr (never ask the user for a sha) and the delta start from
# .last-reviewed, re-runs the relevant layers, and advances .last-reviewed.
#
# Usage: scripts/re-verify.sh [--live]
#   (no flag) run L1 static + L3 script-unit + L5 gc-selector (no cluster swap)
#   --live    also run L0 premise + L4 live e2e (needs KUBECONFIG + Docker Hub push)
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"
cd "$(git rev-parse --show-toplevel)"
ROOT=$(git rev-parse --show-toplevel)
MANIFEST="$HERE/verify-manifest.json"
LAST="$HERE/.last-reviewed"
LIVE=0; [[ "${1:-}" == "--live" ]] && LIVE=1

PR=$(sed -n "s/.*\"pr\":[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$MANIFEST")
HEAD_REPO=$(sed -n "s/.*\"head_repo\":[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$MANIFEST")
HEAD_BRANCH=$(sed -n "s/.*\"head_branch\":[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$MANIFEST")
SINCE=$(cat "$LAST" 2>/dev/null || echo "")

echo "PR=$PR  head=$HEAD_REPO:$HEAD_BRANCH  since=${SINCE:0:12}"

echo "== resolve current PR head =="
git remote get-url pr-head >/dev/null 2>&1 \
  || git remote add pr-head "https://github.com/$HEAD_REPO.git"
git fetch -q pr-head "$HEAD_BRANCH"
NOW=$(git rev-parse FETCH_HEAD)
echo "  reviewed=${SINCE:0:12} -> current=${NOW:0:12}"
if [[ "$NOW" == "$SINCE" ]]; then
  echo "  no delta; re-running full sweep anyway"
  DELTA=""
else
  echo "== delta $SINCE..$NOW =="
  git --no-pager log --oneline "$SINCE..$NOW"
  git --no-pager diff --stat "$SINCE..$NOW" -- test/gha-e2e/mooncake .github/scripts pkg/ddc/cache
  DELTA="$SINCE..$NOW"
fi

echo "== L1 static (bash -n + shellcheck) =="
docker run --rm -v "$ROOT:/mnt:ro" koalaman/shellcheck:stable \
  /mnt/test/gha-e2e/mooncake/test.sh \
  /mnt/test/gha-e2e/mooncake/image/reportSummary.sh \
  /mnt/test/gha-e2e/mooncake/image/custom-entrypoint.sh \
  && echo "  shellcheck OK" || echo "  shellcheck findings ^"

echo "== L3 script-unit (docker mooncake master/worker) =="
bash "$HERE/scripts/layer3-image-scripts.sh"

echo "== L5 gc-selector (synthetic labels vs test selector) =="
bash "$HERE/scripts/layer5-gc-selector.sh"

if [[ $LIVE -eq 1 ]]; then
  echo "== L0 premise (canary vs pre-fix controller) =="
  bash "$HERE/scripts/layer0-premise.sh"
  echo "== L4 live e2e (full test.sh on master controllers) =="
  bash "$HERE/scripts/layer4-live-e2e.sh"
fi

echo "$NOW" > "$LAST"
echo "advanced .last-reviewed -> ${NOW:0:12}"
