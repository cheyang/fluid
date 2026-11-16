#!/usr/bin/env bash
# check_external_images.sh — integration layer (external registry evidence).
#
# The PR claims `curvine/curvine:0.3.6-alpha` does not exist on Docker Hub and
# `v0.3.6-alpha` does. Queries the Docker Hub v2 API. Also sanity-checks the
# other images the sample references, so "the sample works as written" is
# tested end to end for image resolvability.
#
# Output: go-test-JSON lines on stdout. Network/registry outages emit a
# build-fail marker (HARNESS-UPDATE semantics), never a fake pass/fail.
set -uo pipefail

emit() {
  local action="$1" name="$2" detail="${3:-}"
  detail="${detail//\\/\\\\}"; detail="${detail//\"/\\\"}"
  printf '{"Test":"%s","Action":"%s","Output":"%s"}\n' "$name" "$action" "$detail"
}

tag_code() { # $1 repo  $2 tag -> http code or 000
  curl -s -m 20 -o /dev/null -w '%{http_code}' \
    "https://hub.docker.com/v2/repositories/$1/tags/$2" 2>/dev/null || echo 000
}

check() { # $1 test name  $2 repo  $3 tag  $4 expected code
  local name="$1" repo="$2" tag="$3" want="$4" code
  code="$(tag_code "$repo" "$tag")"
  if [ "$code" = "000" ]; then
    printf '{"Action":"build-fail","Package":"docharness","Output":"network/registry unreachable checking %s:%s"}\n' "$repo" "$tag"
    return 0
  fi
  if [ "$code" = "$want" ]; then
    emit pass "$name" "$repo:$tag -> HTTP $code (expected $want)"
  else
    emit fail "$name" "$repo:$tag -> HTTP $code (expected $want)"
  fi
}

check "ExtImage/CurvineOldTagAbsent"  "curvine/curvine"  "0.3.6-alpha"  "404"
check "ExtImage/CurvineNewTagPresent" "curvine/curvine"  "v0.3.6-alpha" "200"
check "ExtImage/CurlImagePresent"     "curlimages/curl"  "8.22.0"       "200"
check "ExtImage/BusyboxPresent"       "library/busybox"  "latest"       "200"
