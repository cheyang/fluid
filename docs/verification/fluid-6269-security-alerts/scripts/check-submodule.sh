#!/usr/bin/env bash
# Verify the sdk/python submodule bump claims of PR #6269:
#   - HEAD's gitlink for sdk/python is b06de4e8... (merge of fluid-client-python#12)
#   - b06de4e8 is a merge commit with parents e089b77 and 94baaf2
#   - e089b77..b06de4e8 touches exactly the 8 files of upstream PR #12
#   - b06de4e8 is the master HEAD of fluid-cloudnative/fluid-client-python
# Requires network. Records output; safe to re-run.
set -uo pipefail
HERE="$(cd "$(dirname "$0")/.." && pwd)"          # docs/verification/fluid-6269-security-alerts
REPO="$(cd "$HERE/../../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail=0
link=$(git -C "$REPO" ls-tree HEAD sdk/python | awk '{print $3}')
echo "HEAD gitlink sdk/python = $link"
[ "$link" = "b06de4e8c6e3aefe9e90f897444d04a4fb031839" ] || { echo "MISMATCH: expected b06de4e8..."; fail=1; }

git clone --quiet --filter=blob:none https://github.com/fluid-cloudnative/fluid-client-python.git "$TMP/fcp"
cd "$TMP/fcp"
parents=$(git show --no-patch --format='%P' b06de4e8c6e3aefe9e90f897444d04a4fb031839)
echo "b06de4e8 parents = $parents (expected e089b77fe60d2243c2e59c57fceec9851c6e5d2b 94baaf2207d7bcb3de52d3d924da360e9b1402f8)"
[ "$parents" = "e089b77fe60d2243c2e59c57fceec9851c6e5d2b 94baaf2207d7bcb3de52d3d924da360e9b1402f8" ] || { echo "MISMATCH: parents"; fail=1; }

nfiles=$(git diff --name-only e089b77fe60d2243c2e59c57fceec9851c6e5d2b b06de4e8c6e3aefe9e90f897444d04a4fb031839 | wc -l)
echo "files changed e089b77..b06de4e8 = $nfiles (expected 8)"
[ "$nfiles" = "8" ] || { echo "MISMATCH: file count"; fail=1; }

remote_head=$(git rev-parse origin/master)
echo "fluid-client-python master HEAD = $remote_head"
[ "$remote_head" = "b06de4e8c6e3aefe9e90f897444d04a4fb031839" ] || { echo "NOTE: upstream master has moved past b06de4e8"; }

exit $fail
