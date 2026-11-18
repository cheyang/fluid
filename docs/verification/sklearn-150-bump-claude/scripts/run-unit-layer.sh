#!/usr/bin/env bash
# Unit layer for the sklearn-150-bump harness: lockfile/metadata checks that
# need no python environment, only pypi.org JSON metadata.
# Emits "NAME<TAB>pass|fail" lines for re-verify.sh.
# Run from the repo root (or any dir; path passed as $1, default
# samples/vineyard/requirements.txt).
set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REQ="${1:-samples/vineyard/requirements.txt}"

mark() { # $1=name $2=exit
  if [ "$2" -eq 0 ]; then echo -e "$1\tpass"; else echo -e "$1\tfail"; fi
}

out="$(mktemp)"
python3 "$HERE/check_lockfile_hashes.py" "$REQ" scikit-learn cp310 >"$out" 2>&1
rc=$?
cat "$out"
mark "LOCKFILE_HASH_CONTRACT" $rc

python3 "$HERE/check_header_recipe.py" "$REQ" >"$out" 2>&1
rc=$?
cat "$out"
mark "HEADER_RECIPE_CONTRACT" $rc

rm -f "$out"
