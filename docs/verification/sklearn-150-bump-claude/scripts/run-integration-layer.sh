#!/usr/bin/env bash
# Integration layer for the sklearn-150-bump harness: build a venv from the
# exact pinned set in samples/vineyard/requirements.txt (wheels verified
# against PyPI sha256 before install) and run the sample-path contract test.
# Emits "NAME<TAB>pass|fail" lines for re-verify.sh.
#
# Usage: run-integration-layer.sh [requirements.txt] [venv-dir]
#   venv-dir defaults to a fresh temp dir (rebuild, ~2-4 min + downloads).
set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REQ="${1:-samples/vineyard/requirements.txt}"
VENV="${2:-$(mktemp -d)/venv}"

mark() { # $1=name $2=exit
  if [ "$2" -eq 0 ]; then echo -e "$1\tpass"; else echo -e "$1\tfail"; fi
}

if bash "$HERE/build_pinned_env.sh" "$REQ" "$VENV"; then
  mark "PINNED_ENV_BUILD" 0
else
  mark "PINNED_ENV_BUILD" $?
  mark "SAMPLE_PATH_CONTRACT" 1
  exit 0
fi

"$VENV/bin/python" "$HERE/sample_path_harness.py"
mark "SAMPLE_PATH_CONTRACT" $?
