#!/usr/bin/env bash
# re-verify.sh for PR #6269 (topic pr6269-security-alerts). Fetches the current PR head,
# grafts this harness onto it, re-runs unit + integration layers (docker layer when docker
# is available). All checks are contract-polarity: they must keep PASSING on the fixed code.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
TOPIC=pr6269-security-alerts
DIR=docs/verification/$TOPIC
PR_URL=$(python3 -c "import json;print(json.load(open('$DIR/verify-manifest.json'))['pr'])")
NUM=${PR_URL##*/pull/}

FIXED_REF="${1:-}"
if [ -z "$FIXED_REF" ]; then
    git fetch --quiet https://github.com/fluid-cloudnative/fluid.git "pull/$NUM/head"
    FIXED_REF=$(git rev-parse FETCH_HEAD)
fi
echo "re-verify: head = $FIXED_REF"
LAST=$(cat $DIR/.last-reviewed)
echo "re-verify: review delta = $LAST..$FIXED_REF"

ORIG=$(git rev-parse HEAD)
cleanup() { git checkout -f "$ORIG" >/dev/null 2>&1 || true; }
trap cleanup EXIT

git checkout -f "$FIXED_REF" >/dev/null
git checkout "$ORIG" -- "$DIR" hack/sdk/tests/test_post_gen.py hack/sdk/post-gen.base.py

FAIL=0
step() { echo; echo "=== $1 ==="; }

step "unit: post-gen.py allow-list"
python3 -m unittest discover -s hack/sdk/tests 2>&1 | tail -3 || FAIL=1

step "integration: lock hashes vs PyPI"
python3 $DIR/scripts/check_locks_against_pypi.py | grep -c "^  ok" || FAIL=1
python3 $DIR/scripts/check_locks_against_pypi.py >/dev/null 2>&1 || FAIL=1
python3 $DIR/scripts/check_submodule_build_reqs.py >/dev/null 2>&1 || FAIL=1

step "integration: submodule bump claims"
bash $DIR/scripts/verify_submodule.sh | tail -1 || FAIL=1

if command -v docker >/dev/null && sudo -n docker info >/dev/null 2>&1; then
    step "integration: mooncake resolver matrix (container)"
    bash $DIR/scripts/pip_resolve_in_container.sh | grep -E "OK$|RESOLVED" || FAIL=1
    step "docker: image builds + flows"
    bash $DIR/scripts/docker_lock_checks.sh all || FAIL=1
else
    step "docker layers SKIPPED (no docker)"
fi

echo
if [ "$FAIL" = 0 ]; then echo "RESULT: all checks pass on $FIXED_REF"; else echo "RESULT: FAILURES on $FIXED_REF"; fi
echo "Advance the marker after reviewing the delta:"
echo "  echo $FIXED_REF > $DIR/.last-reviewed && git add $DIR/.last-reviewed && git commit -m 'review: advance last-reviewed'"
exit $FAIL
