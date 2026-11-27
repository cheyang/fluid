#!/usr/bin/env bash
# Premise (P0) check for fluid PR #6293 (reviewer: codex).
# P0 claim (from the PR body): "runtime-profile.yaml and the readmes use
# fluid-cloudnative/curvine-thinruntime:v1.0.0. That name can't be pulled ...
# CurvineIO/curvine#571 removed [build-image.sh and fluid-config-parse.py] ...
# Since CurvineIO/curvine#1078, the Fluid scripts ship in the regular Curvine
# runtime image, ghcr.io/curvineio/curvine."
# This script re-checks each external claim live. Network access required.
set -uo pipefail

fail=0
note() { echo "== $* =="; }
ok()   { echo "PASS: $*"; }
bad()  { echo "FAIL: $*"; fail=1; }

note "1. Old image fluid-cloudnative/curvine-thinruntime on Docker Hub"
code="$(curl -s -o /dev/null -w '%{http_code}' https://hub.docker.com/v2/repositories/fluid-cloudnative/curvine-thinruntime/)"
echo "hub.docker.com/v2/repositories/fluid-cloudnative/curvine-thinruntime -> HTTP $code"
if [ "$code" = "404" ]; then
  ok "old image is absent from Docker Hub (premise holds: example cannot be pulled)"
else
  bad "old image now resolves (HTTP $code); premise partially outdated"
fi

note "2. New image ghcr.io/curvineio/curvine tag list"
token="$(curl -s 'https://ghcr.io/token?scope=repository:curvineio/curvine:pull' | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
tags="$(curl -s -H "Authorization: Bearer $token" https://ghcr.io/v2/curvineio/curvine/tags/list)"
echo "$tags" | head -c 1200; echo
if printf '%s' "$tags" | grep -q '"latest"'; then
  ok "ghcr.io/curvineio/curvine exists and publishes the 'latest' tag used by the PR"
else
  bad "ghcr.io/curvineio/curvine has no 'latest' tag"
fi

note "3. Curvine repo: removed thin-runtime build assets"
listing="$(curl -s https://api.github.com/repos/CurvineIO/curvine/contents/curvine-docker/fluid/thin-runtime)"
echo "$listing" | grep -o '"name": *"[^"]*"' | head -10
if printf '%s' "$listing" | grep -q 'build-image.sh'; then
  bad "build-image.sh still present upstream"
else
  ok "build-image.sh gone from curvine-docker/fluid/thin-runtime (premise holds)"
fi
if printf '%s' "$listing" | grep -q 'curvine-thinruntime.yaml'; then
  ok "curvine-thinruntime.yaml (the example the PR now links) exists upstream"
else
  bad "curvine-thinruntime.yaml missing upstream (PR's new link would be dead)"
fi

note "4. Curvine runtime image contains the Fluid integration"
df="$(curl -s https://raw.githubusercontent.com/CurvineIO/curvine/main/curvine-docker/deploy/Dockerfile_rocky9)"
printf '%s\n' "$df" | grep -n 'fluid-entrypoint.sh\|config-parse.py' | head -5
printf '%s' "$df" | grep -q 'COPY curvine-docker/fluid/entrypoint.sh /fluid-entrypoint.sh' \
  && ok "Dockerfile_rocky9 installs /fluid-entrypoint.sh" || bad "no /fluid-entrypoint.sh in runtime image"
printf '%s' "$df" | grep -q 'COPY curvine-docker/fluid/config-parse.py /app/curvine/config-parse.py' \
  && ok "Dockerfile_rocky9 installs /app/curvine/config-parse.py" || bad "no /app/curvine/config-parse.py in runtime image"

note "5. /entrypoint.sh hands off to /fluid-entrypoint.sh for fluid-thin-runtime"
ep="$(curl -s https://raw.githubusercontent.com/CurvineIO/curvine/main/curvine-docker/deploy/entrypoint.sh)"
printf '%s\n' "$ep" | grep -n 'fluid-thin-runtime\|exec /fluid-entrypoint.sh' | head -5
printf '%s' "$ep" | grep -q 'exec /fluid-entrypoint.sh' \
  && ok "entrypoint.sh execs /fluid-entrypoint.sh in fluid mode" || bad "entrypoint.sh has no fluid handoff"

note "6. Curvine's own example uses the same command/args the PR adds"
ex="$(curl -s https://raw.githubusercontent.com/CurvineIO/curvine/main/curvine-docker/fluid/thin-runtime/curvine-thinruntime.yaml)"
printf '%s\n' "$ex" | grep -n 'image:\|imageTag\|entrypoint.sh\|fluid-thin-runtime' | head -8
printf '%s' "$ex" | grep -q 'ghcr.io/curvineio/curvine' \
  && ok "upstream example uses ghcr.io/curvineio/curvine" || bad "upstream example image differs"
printf '%s' "$ex" | grep -q '\- /entrypoint.sh' \
  && ok "upstream example sets command: [/entrypoint.sh]" || bad "upstream example command differs"
printf '%s' "$ex" | grep -q '\- fluid-thin-runtime' \
  && ok "upstream example sets args: [fluid-thin-runtime]" || bad "upstream example args differ"

note "7. Curvine root Makefile has the docker-build target the dev guide now references"
mk="$(curl -s https://raw.githubusercontent.com/CurvineIO/curvine/main/Makefile)"
printf '%s\n' "$mk" | grep -n '^docker-build:' 
printf '%s' "$mk" | grep -q '^docker-build:' \
  && ok "make docker-build exists in Curvine repo root" || bad "make docker-build missing"

echo
if [ "$fail" -eq 0 ]; then
  echo "PREMISE P0: CONFIRMED — every external claim in the PR body re-checked true"
else
  echo "PREMISE P0: one or more claims FAILED — see FAIL lines above"
  exit 1
fi
