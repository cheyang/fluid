#!/usr/bin/env bash
# Resolver checks for the mooncake lock, run inside the image's exact base
# (python:3.12.13-slim, native cp312/linux-x86_64) so no cross-platform flags are needed.
#   1. HEAD lock resolves with --require-hashes            -> prints HEAD-LOCK-RESOLVES: OK
#   2. #6212 variant (bump ONLY aiohttp to 3.14.4)         -> must fail ResolutionImpossible
#   3. BASE lock (aiohttp 3.14.3 + yarl 1.24.5)            -> resolves (control)
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
IMG="python:3.12.13-slim@sha256:229a2c5bfa27522db7815ea81f9bed70af17ccb9de9fc7ad142b1877b5830d36"
DOCKER="${DOCKER:-sudo -n docker}"
LOCK=test/gha-e2e/mooncake/image/requirements.txt
TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

AIOHASH=$(curl -s https://pypi.org/pypi/aiohttp/3.14.4/json | python3 -c "
import json,sys
d=json.load(sys.stdin)
for u in d['urls']:
    if u['filename']=='aiohttp-3.14.4-cp312-cp312-manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64.whl':
        print(u['digests']['sha256'])")
git show dcb76167b0143a239addc6bed83ed50b45267631:$LOCK > "$TMP/base.txt"
python3 - "$TMP/base.txt" "$AIOHASH" > "$TMP/pr6212.txt" <<'PY'
import sys
lines = open(sys.argv[1]).read().splitlines(keepends=True)
out = []
for line in lines:
    if line.rstrip() == "aiohttp==3.14.3 \\":
        line = line.replace("aiohttp==3.14.3", "aiohttp==3.14.4")
    line = line.replace("--hash=sha256:543906c127fb1d929b95076db19b83fa2d46751006ff1e23b093aa5ac4d8db42",
                        "--hash=sha256:" + sys.argv[2])
    out.append(line)
sys.stdout.writelines(out)
PY
cp "$LOCK" "$TMP/head.txt"

$DOCKER run --rm -v "$TMP:/locks:ro" "$IMG" bash -c '
set -e
pip install -q --dry-run --ignore-installed --only-binary=:all: --require-hashes -r /locks/head.txt 2>&1 | grep -v notice || true
echo "HEAD-LOCK-RESOLVES: OK"
set +e
OUT=$(pip install -q --dry-run --ignore-installed --only-binary=:all: --require-hashes -r /locks/pr6212.txt 2>&1)
set -e
if echo "$OUT" | grep -q ResolutionImpossible; then echo "6212-FAILS-AS-EXPECTED: OK"; else echo "$OUT"; echo "6212 UNEXPECTEDLY RESOLVED"; exit 1; fi
pip install -q --dry-run --ignore-installed --only-binary=:all: --require-hashes -r /locks/base.txt 2>&1 | grep -v notice || true
echo "BASE-LOCK-RESOLVES: OK"'
