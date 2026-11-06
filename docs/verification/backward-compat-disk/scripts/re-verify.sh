#!/usr/bin/env bash
# re-verify.sh for PR fluid-cloudnative/fluid#6002 (topic: backward-compat-disk)
#
# Usage: bash scripts/re-verify.sh [<ref>]
#   no ref  -> fetches the CURRENT PR head from the manifest's `pr` URL
#              (git fetch <repo-url> pull/<n>/head; no local remote needed)
#   <ref>   -> any commit-ish (e.g. a fixed head sha or a local branch)
#
# Grafts this harness (docs/verification/backward-compat-disk) onto that ref
# in a temp checkout, runs the L1 layers, and prints per finding
# Fixed / Still-broken / Partial / Harness-update.
# Exit 0 iff all findings pass their polarity check.
#
# L2 (ci-observation) is not runnable locally: it is the GitHub Actions run
# itself — see results/ci-evidence.md for how it was observed.

set -euo pipefail

HERE="$(cd "$(dirname "$0")/.." && pwd)"     # docs/verification/backward-compat-disk
BRANCH_ROOT="$(cd "${HERE}/../.." && pwd)"   # repo root of the harness branch

REPO_URL="https://github.com/fluid-cloudnative/fluid"
PR_URL="$(python3 -c "import json,sys; print(json.load(open('${HERE}/verify-manifest.json'))['pr'])")"
PR_NUM="${PR_URL##*/pull/}"
PR_NUM="${PR_NUM%%/*}"

REF="${1:-}"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

echo ">>> harness topic: backward-compat-disk (PR ${PR_URL})"
echo ">>> target ref: ${REF:-<current PR head via pull/${PR_NUM}/head>}"

git init --quiet "${WORK}/repo"
git -C "${WORK}/repo" remote add origin "${REPO_URL}"
if [[ -z "${REF}" ]]; then
    git -C "${WORK}/repo" fetch --quiet --depth=50 origin "pull/${PR_NUM}/head"
    REF=FETCH_HEAD
else
    git -C "${WORK}/repo" fetch --quiet --depth=50 origin "${REF}"
fi
git -C "${WORK}/repo" checkout --quiet "${REF}"
HEAD_SHA="$(git -C "${WORK}/repo" rev-parse HEAD)"
echo ">>> grafted harness onto ${HEAD_SHA}"

# graft the harness onto the code under review (additive only)
mkdir -p "${WORK}/repo/docs/verification"
cp -r "${HERE}" "${WORK}/repo/docs/verification/backward-compat-disk"
rm -rf "${WORK}/repo/docs/verification/backward-compat-disk/results"

# incremental review delta
LAST_REVIEWED=""
if [[ -f "${HERE}/.last-reviewed" ]]; then
    LAST_REVIEWED="$(cat "${HERE}/.last-reviewed" | tr -d '[:space:]')"
fi
if [[ -n "${LAST_REVIEWED}" ]]; then
    echo
    echo ">>> incremental delta ${LAST_REVIEWED:0:10}..${HEAD_SHA:0:10}:"
    git -C "${WORK}/repo" log --oneline "${LAST_REVIEWED}..${HEAD_SHA}" 2>/dev/null || \
        echo "    (last-reviewed commit not reachable in shallow fetch; fetch full history to see the delta)"
    echo "    after this round, advance the marker:  echo ${HEAD_SHA} > ${HERE}/.last-reviewed"
else
    echo ">>> no .last-reviewed marker; set one after this round:  echo ${HEAD_SHA} > ${HERE}/.last-reviewed"
fi

# --- L1: script contract (F1 + F2) -----------------------------------------
echo
echo ">>> L1 run 1/3: script contract (F1) on PR head code"
set +e
bash "${WORK}/repo/docs/verification/backward-compat-disk/scripts/l1_mock_harness.sh" "${WORK}/repo" \
    > "${WORK}/l1-head.out" 2>&1
F1_RC=$?
set -e
grep -E "^(ok|FAIL|L1-VERDICT)" "${WORK}/l1-head.out" || true

echo
echo ">>> L1 run 2/3: rmi-failure robustness (F2)"
set +e
bash "${WORK}/repo/docs/verification/backward-compat-disk/scripts/l1_mock_harness.sh" "${WORK}/repo" --rmi-fail \
    > "${WORK}/l1-rmi-fail.out" 2>&1
F2_RC=$?
set -e
grep -E "^(ok:   docker rmi failure|FAIL|L1-VERDICT)" "${WORK}/l1-rmi-fail.out" || true

echo
echo ">>> L1 run 3/3: workflow contract (F3)"
set +e
python3 -I "${WORK}/repo/docs/verification/backward-compat-disk/scripts/l1_workflow_contract.py" \
    "${WORK}/repo/.github/workflows/backward-compatibility-e2e.yml" \
    > "${WORK}/l1-wf.out" 2>&1
F3_RC=$?
set -e
grep -E "^(ok|FAIL|L1-VERDICT)" "${WORK}/l1-wf.out" || true

# --- verdicts ---------------------------------------------------------------
report() { # id rc expected
    if [[ "$2" -eq 0 ]]; then
        echo "$1: PASS (contract holds on this head — Fixed/still-fixed)"
    elif [[ "$2" -eq 2 ]]; then
        echo "$1: HARNESS-UPDATE (harness could not run against this head — code shape changed?)"
    else
        echo "$1: FAIL (Still-broken / regressed on this head)"
    fi
}
echo
echo "================ per-finding verdicts ================"
report F1 "${F1_RC}"
report F2 "${F2_RC}"
report F3 "${F3_RC}"
echo "F4 (shared-script blast radius, ci-observation): check that kind-e2e-test is green on this head's CI run — see results/ci-evidence.md for the method"
echo "====================================================="
[[ $((F1_RC + F2_RC + F3_RC)) -eq 0 ]]
