#!/usr/bin/env bash
# Verify the sdk/python submodule bump claims against fluid-client-python.
set -euo pipefail
REPO_DIR="${CLIENT_PY_REPO:-$HOME/.cache/fluid-verify-6269/fluid-client-python}"
if [ ! -d "$REPO_DIR" ]; then
    mkdir -p "$(dirname "$REPO_DIR")"
    git clone --bare https://github.com/fluid-cloudnative/fluid-client-python "$REPO_DIR" >/dev/null 2>&1
fi
cd "$REPO_DIR"
git fetch -q https://github.com/fluid-cloudnative/fluid-client-python.git master 2>/dev/null || true

NEW=b06de4e8c6e3aefe9e90f897444d04a4fb031839
OLD=e089b77fe60d2243c2e59c57fceec9851c6e5d2b

SUBJ=$(git log -1 --format=%s $NEW)
echo "$NEW subject: $SUBJ"
echo "$SUBJ" | grep -q "Merge pull request #12" || { echo "FAIL: not the #12 merge"; exit 1; }
MASTER=$(git rev-parse FETCH_HEAD 2>/dev/null || git rev-parse master)
if [ "$MASTER" = "$NEW" ]; then echo "master HEAD == $NEW: OK"; else echo "NOTE: master moved to $MASTER (bump was master HEAD at PR time)"; fi

FILES=$(git diff --name-only $OLD $NEW)
echo "changed files:"; echo "$FILES"
N=$(echo "$FILES" | wc -l)
[ "$N" = "8" ] || { echo "FAIL: $N files changed, want 8"; exit 1; }
UNEXPECTED=$(echo "$FILES" | grep -vE '^(\.github/(build-requirements\.(in|txt)|workflows/release\.yml)|examples/02_ml_train_pipeline/(Dockerfile|main\.py)|examples/03_dataflow_with_vineyard/vineyard(-en)?\.ipynb|examples/04_integration/pytorch/main\.py)$' || true)
[ -z "$UNEXPECTED" ] || { echo "FAIL: unexpected files: $UNEXPECTED"; exit 1; }
echo "only the claimed 8 files: OK"

TREE=$(git rev-parse "$NEW^{tree}")
if git cat-file -e 94baaf22 2>/dev/null; then
    PRHEAD_TREE=$(git rev-parse "94baaf22^{tree}")
    [ "$TREE" = "$PRHEAD_TREE" ] && echo "tree($NEW) == tree(94baaf22): OK" || { echo "FAIL: tree mismatch"; exit 1; }
fi

check_sha() { # repo tag sha
    got=$(git ls-remote "https://github.com/$1" "refs/tags/$2" "refs/tags/$2^{}" | awk '{print $1}')
    if echo "$got" | grep -q "$3"; then echo "$1@$3 == $2: OK"; else echo "FAIL: $1 $2 != $3 (got $got)"; exit 1; fi
}
check_sha actions/checkout v7.0.1 3d3c42e5aac5ba805825da76410c181273ba90b1
check_sha actions/setup-python v7.1.0 6d4fd22b20465ccc0f7354476588e8df7c9ca0e0
check_sha pypa/gh-action-pypi-publish v1.14.3 7245906fb61fa33a13554f9d5fb856fe61758086
echo "SUBMODULE-VERIFY: OK"
