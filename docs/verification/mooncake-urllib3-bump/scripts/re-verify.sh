#!/usr/bin/env bash
# re-verify.sh — re-run the mooncake-urllib3-bump harness against the current PR
# head (or an explicit ref) and print per-finding Fixed / Still-broken.
#
# Usage: re-verify.sh [<ref>]   (default: fetch the current head of the PR
# recorded in verify-manifest.json; no local remote name needed)
#
# Layers: L1 lock-file static check, L2a full lock-file resolve against real
# PyPI (cp312/manylinux x86_64, --require-hashes), L2b requests+urllib3 runtime
# smoke, P0 GHSA-vxq7-64xx-v4gw chunk-line repro on old vs new urllib3.
# All tests are contract polarity: green on the fixed/PR state, red on base.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFEST="$HERE/../verify-manifest.json"
PR_URL="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["pr"])' "$MANIFEST")"
PRNUM="${PR_URL##*/}"

REF="${1:-}"
if [ -z "$REF" ]; then
  git fetch -q https://github.com/fluid-cloudnative/fluid.git "pull/$PRNUM/head"
  REF=FETCH_HEAD
fi
echo ">> re-verifying against $REF ($PR_URL)"

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
git show "$REF:test/gha-e2e/mooncake/image/requirements.txt" > "$WORK/requirements.txt"

LAST="$(cat "$HERE/../.last-reviewed" 2>/dev/null || true)"
if [ -n "$LAST" ]; then
  echo ">> incremental review delta: $LAST..$REF"
  git log --oneline "$LAST..$REF" || true
fi

VENV="$WORK/venv"; python3 -m venv "$VENV"
"$VENV/bin/pip" install -q --no-cache-dir --upgrade pip

verdict() { # <id> <exit-code>
  if [ "$2" -eq 0 ]; then echo "$1: Fixed"; else echo "$1: Still-broken"; fi
}

rc=0

echo "== L1 lock-file static check =="
if python3 "$HERE/l1_lockfile_static_check.py" "$WORK/requirements.txt"; then verdict F1 0; else verdict F1 1; rc=1; fi

echo "== L2a full lock-file resolve (cp312/manylinux x86_64, --require-hashes) =="
if VENV="$VENV" bash "$HERE/l2_full_lockfile_resolve.sh" "$WORK/requirements.txt" "$WORK/dl" >/dev/null; then verdict F1 0; else verdict F1 1; rc=1; fi

echo "== L2b requests 2.34.2 + urllib3 2.8.0 runtime smoke =="
SMOKE="$WORK/smoke"; python3 -m venv "$SMOKE"
"$SMOKE/bin/pip" install -q --no-cache-dir requests==2.34.2 urllib3==2.8.0
if "$SMOKE/bin/python" "$HERE/l2_runtime_smoke.py" >/dev/null 2>&1; then verdict F2 0; else verdict F2 1; rc=1; fi

echo "== P0 premise repro (GHSA-vxq7-64xx-v4gw) =="
NEWVER="$(grep -oP '^urllib3==\K[0-9.]+' "$WORK/requirements.txt" | head -1)"
P0V="$WORK/p0v"; python3 -m venv "$P0V"; "$P0V/bin/pip" install -q --no-cache-dir "urllib3==$NEWVER"
if "$P0V/bin/python" "$HERE/p0_chunked_line_repro.py" "$NEWVER"; then verdict P0 0; else verdict P0 1; rc=1; fi

exit $rc
