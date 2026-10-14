#!/usr/bin/env bash
# Verification harness for fluid PR #6207 (dependabot bump of ossf/scorecard-action
# 2.4.4 -> 2.5.0 in .github/workflows/sonarcloud.yaml).
#
# All checks are CONTRACT tests: they assert the behavior a correct bump must have.
# A failing check = the bump is broken (wrong pin, missing image, incompatible inputs).
# Deterministic layer (L1): pin<->tag, input compatibility (works fully offline given
# git/curl network access to github.com/ghcr.io; no cluster needed).
# Integration layer (L2): the referenced container image manifest and the publish
# endpoint actually exist (real upstream services, read-only GETs).
set -uo pipefail

PINNED_SHA="fa7117db83c52d787ae5a89f350999701505d78e"
TAG="v2.5.0"
UPSTREAM="https://github.com/ossf/scorecard-action.git"
WORKFLOW=".github/workflows/sonarcloud.yaml"
# The workflow file under review: prefer the git ref under review, fall back to worktree.
REF="${1:-HEAD}"

failures=0
report() {
  printf '%-8s %s\n' "$1" "$2"
  if [ "$1" = "FAIL" ]; then failures=$((failures+1)); fi
  return 0
}

echo "=== check-action-bump.sh against ref: $REF ==="

# --- C1: pinned SHA in the workflow == peeled commit of the upstream tag -------------
echo "--- C1: pinned SHA matches refs/tags/$TAG^{} upstream"
actual_pin=$(git show "$REF:$WORKFLOW" | grep -oP 'ossf/scorecard-action@\K[0-9a-f]{40}')
tag_commit=$(git ls-remote "$UPSTREAM" "refs/tags/$TAG^{}" | cut -f1)
echo "workflow pin : $actual_pin"
echo "tag commit   : $tag_commit"
if [ -n "$actual_pin" ] && [ "$actual_pin" = "$tag_commit" ]; then
  report PASS "C1 pin == $TAG"
else
  report FAIL "C1 pin ($actual_pin) != $TAG commit ($tag_commit)"
fi

# --- C2: every with: input the workflow passes is still read by the new action -------
echo "--- C2: workflow inputs still honored by $TAG"
action_yaml=$(curl -sfL "https://raw.githubusercontent.com/ossf/scorecard-action/$actual_pin/action.yaml") \
  || { report FAIL "C2 could not fetch action.yaml at $actual_pin"; action_yaml=""; }
if [ -n "$action_yaml" ]; then
  for req in results_file results_format; do
    echo "$action_yaml" | grep -q "^  $req:" \
      && report PASS "C2 required input still declared: $req" \
      || report FAIL "C2 required input MISSING in $TAG: $req"
  done
  echo "$action_yaml" | grep -q 'publish_results:' \
    && report PASS "C2 optional input still declared: publish_results" \
    || report FAIL "C2 publish_results no longer declared in $TAG"
  echo "$action_yaml" | grep -q 'sarif' \
    && report PASS "C2 results_format=sarif still supported" \
    || report FAIL "C2 sarif format dropped in $TAG"
fi

# --- C3: the container image referenced by the action manifest exists on GHCR --------
echo "--- C3: docker image for $TAG exists on ghcr.io"
image=$(echo "$action_yaml" | grep -oP 'image: "docker://\K[^"]+' || true)
echo "image: $image"
repo=$(echo "$image" | sed -e 's|ghcr.io/||' -e 's|:.*||')
tagp=$(echo "$image" | awk -F: '{print $NF}')
token=$(curl -sf "https://ghcr.io/token?scope=repository:$repo:pull" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token" \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  "https://ghcr.io/v2/$repo/manifests/$tagp")
echo "manifest GET https://ghcr.io/v2/$repo/manifests/$tagp -> $code"
[ "$code" = "200" ] && report PASS "C3 image manifest exists" || report FAIL "C3 image manifest missing ($code)"

# --- C4: publish endpoint used by publish_results:true is alive ----------------------
echo "--- C4: publish endpoint reachable"
purl=$(echo "$action_yaml" | grep -A3 'internal_publish_base_url' | grep -oP 'default: "\K[^"]+')
pcode=$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$purl/")
echo "GET $purl/ -> $pcode"
[ "$pcode" != "000" ] && report PASS "C4 publish endpoint reachable" || report FAIL "C4 publish endpoint unreachable"

# --- F1 (pre-existing, informational): exclude_* inputs are not read by the action ---
echo "--- F1(pre-existing): exclude_checks / exclude_paths recognized by action source?"
if curl -sfL "https://raw.githubusercontent.com/ossf/scorecard-action/$actual_pin/options/env.go" | grep -q 'EXCLUDE'; then
  echo "action reads an EXCLUDE env var"
else
  echo "action source has NO EXCLUDE handling: workflow's exclude_checks/exclude_paths are dead inputs (pre-existing, unchanged by bump)"
fi

echo "=== failures: $failures ==="
exit "$failures"
