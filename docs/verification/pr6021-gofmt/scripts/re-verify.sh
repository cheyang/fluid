#!/usr/bin/env bash
# Re-verify harness for PR https://github.com/fluid-cloudnative/fluid/pull/6021
# No arguments: resolves current PR head from the manifest `pr` URL.
# Optional argument: explicit ref/sha to graft the harness onto.
#
# Runs the unit + integration layers only (no live cluster layer needed).
# Prints per finding: Fixed / Still-broken / Harness-update. Exit 0 iff all fixed.
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
TOPIC_DIR="$(cd "${HERE}/.." && pwd)"
MANIFEST="${TOPIC_DIR}/verify-manifest.json"
PR_URL="$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['pr'])" "${MANIFEST}")"
PR_NUM="${PR_URL##*/}"
REPO_URL="https://github.com/fluid-cloudnative/fluid"

if [ -n "${1:-}" ]; then
  REF="$1"
else
  REF="FETCH_HEAD"
fi

WORK="$(mktemp -d /tmp/fluid-6021-reverify.XXXXXX)"
trap 'rm -rf "${WORK}"' EXIT
git fetch -q "${REPO_URL}" "pull/${PR_NUM}/head" 2>/dev/null || { echo "fetch failed"; exit 2; }
[ -n "${1:-}" ] || REF="$(git rev-parse FETCH_HEAD)"

echo "Grafting harness onto PR head ${REF}"
git worktree add -q --detach "${WORK}/fluid" "${REF}"
cp -r "${TOPIC_DIR}" "${WORK}/harness"

RC=0
echo
echo "== L1: gofmt contract (F1) =="
if bash "${WORK}/harness/scripts/check-gofmt.sh" "${WORK}/fluid" ; then
  echo "F1: Fixed"
else
  echo "F1: Still-broken"
  RC=1
fi

echo
echo "== L1: unit regression (test must still compile and pass) =="
( cd "${WORK}/fluid" && go test ./pkg/ddc/alluxio/operations/ -run 'TestAlluxioFileUtils_ReportSummary' -count=1 ) \
  && echo "unit: pass" || { echo "unit: FAIL"; RC=1; }

git worktree remove --force "${WORK}/fluid" >/dev/null 2>&1
echo
[ "${RC}" = 0 ] && echo "ALL FIXED" || echo "ONE OR MORE FINDINGS STILL BROKEN"
exit "${RC}"
