#!/usr/bin/env bash
# premise-check.sh — P0 premise check for PR #6293 (addons/curvine image fix).
#
# The PR claims:
#   1. the old image `fluid-cloudnative/curvine-thinruntime:v1.0.0` cannot be pulled
#      (Docker Hub returns 404 for the repository);
#   2. the Curvine upstream removed the thin-runtime build assets the dev guide
#      referenced (curvine-docker/fluid/thin-runtime/build-image.sh, README) —
#      so the old build instructions are dead;
#   3. the replacement image `ghcr.io/curvineio/curvine` is public, has tags
#      (latest + release tags), and ships the Fluid integration scripts at the
#      paths the docs name (/entrypoint.sh, /fluid-entrypoint.sh,
#      /app/curvine/config-parse.py), with /entrypoint.sh dispatching the
#      `fluid-thin-runtime` argument to /fluid-entrypoint.sh;
#   4. the new link target curvine-docker/fluid/thin-runtime/curvine-thinruntime.yaml
#      exists upstream and uses the same image/command/args this PR documents.
#
# This script checks each claim against the live registries and the CurvineIO/curvine
# main branch (raw.githubusercontent.com) and prints PASS/FAIL per claim.
# It is read-only: no cluster, no side effects.
set -u

RAW="https://raw.githubusercontent.com/CurvineIO/curvine/main"
RESULTS=0

check() { # name expected actual
  if [ "$2" = "$3" ]; then echo "PASS  $1"; else echo "FAIL  $1 (expected=$2 got=$3)"; RESULTS=1; fi
}

# --- claim 1: old image does not exist on Docker Hub ---
code=$(curl -s -o /dev/null -w '%{http_code}' "https://hub.docker.com/v2/repositories/fluid-cloudnative/curvine-thinruntime/")
check "dockerhub fluid-cloudnative/curvine-thinruntime is 404 (old image unpullable)" "404" "$code"

# --- claim 2: old build assets removed upstream ---
code=$(curl -s -o /dev/null -w '%{http_code}' "$RAW/curvine-docker/fluid/thin-runtime/build-image.sh")
check "upstream thin-runtime/build-image.sh removed (404)" "404" "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$RAW/curvine-docker/fluid/thin-runtime/README.md")
check "upstream thin-runtime README removed (404)" "404" "$code"

# --- claim 3: replacement image public on ghcr with latest tag ---
token=$(curl -s "https://ghcr.io/token?scope=repository:curvineio/curvine:pull" | python3 -c "import sys,json;print(json.load(sys.stdin).get('token',''))" 2>/dev/null)
tags=$(curl -s -H "Authorization: Bearer $token" "https://ghcr.io/v2/curvineio/curvine/tags/list")
echo "$tags" | python3 -c "
import sys, json
d = json.load(sys.stdin)
tags = d.get('tags') or []
print('PASS  ghcr.io/curvineio/curvine is public' if tags else 'FAIL  ghcr.io/curvineio/curvine tag list empty')
print('PASS  latest tag exists' if 'latest' in tags else 'FAIL  latest tag missing')
print('PASS  release tag v0.6.0-alpha exists' if 'v0.6.0-alpha' in tags else 'FAIL  release tag missing')
" || echo "FAIL  ghcr tag list unparseable"

for f in curvine-docker/deploy/Dockerfile_rocky9 curvine-docker/deploy/Dockerfile_ubuntu24 curvine-docker/deploy/entrypoint.sh curvine-docker/fluid/entrypoint.sh; do
  code=$(curl -s -o /dev/null -w '%{http_code}' "$RAW/$f")
  check "upstream file exists: $f" "200" "$code"
done

# fluid scripts shipped in the runtime image (both supported base images)
for df in Dockerfile_rocky9 Dockerfile_ubuntu24; do
  body=$(curl -s "$RAW/curvine-docker/deploy/$df")
  echo "$body" | grep -q "COPY curvine-docker/fluid/entrypoint.sh /fluid-entrypoint.sh" \
    && echo "PASS  $df copies fluid/entrypoint.sh to /fluid-entrypoint.sh" \
    || { echo "FAIL  $df does not copy /fluid-entrypoint.sh"; RESULTS=1; }
  echo "$body" | grep -q "COPY curvine-docker/fluid/config-parse.py /app/curvine/config-parse.py" \
    && echo "PASS  $df copies config-parse.py to /app/curvine/" \
    || { echo "FAIL  $df does not copy /app/curvine/config-parse.py"; RESULTS=1; }
done

# entrypoint dispatch: /entrypoint.sh hands fluid-thin-runtime to /fluid-entrypoint.sh
body=$(curl -s "$RAW/curvine-docker/deploy/entrypoint.sh")
echo "$body" | grep -q 'fluid-thin-runtime' && echo "$body" | grep -q 'exec /fluid-entrypoint.sh' \
  && echo "PASS  deploy/entrypoint.sh execs /fluid-entrypoint.sh for fluid-thin-runtime" \
  || { echo "FAIL  deploy/entrypoint.sh no longer dispatches fluid-thin-runtime"; RESULTS=1; }

# --- claim 4: the new upstream example link target exists and matches this PR ---
example=$(curl -s "$RAW/curvine-docker/fluid/thin-runtime/curvine-thinruntime.yaml")
echo "$example" | grep -q 'image: ghcr.io/curvineio/curvine' \
  && echo "PASS  upstream example uses ghcr.io/curvineio/curvine" \
  || { echo "FAIL  upstream example image differs"; RESULTS=1; }
echo "$example" | grep -q '/entrypoint.sh' && echo "$example" | grep -q 'fluid-thin-runtime' \
  && echo "PASS  upstream example uses command /entrypoint.sh + args fluid-thin-runtime" \
  || { echo "FAIL  upstream example command/args differ"; RESULTS=1; }

exit $RESULTS
