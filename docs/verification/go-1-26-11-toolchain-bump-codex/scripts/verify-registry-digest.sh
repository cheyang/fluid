#!/usr/bin/env bash
# verify-registry-digest.sh — confirm the golang image digest pinned in the
# Dockerfiles is the current multi-arch index digest of golang:<tag>-bookworm
# on Docker Hub. Network-dependent evidence layer (not part of re-verify.sh's
# gotest layers).
set -euo pipefail
TAG="${1:-1.26.11-bookworm}"
REPO_ROOT="$(git rev-parse --show-toplevel)"
PIN="$(grep -rhoE 'golang:[0-9.]+-bookworm@sha256:[0-9a-f]{64}' "$REPO_ROOT/docker" "$REPO_ROOT/test" | sort -u)"
echo "pinned refs:"; echo "$PIN"
DIGEST="$(echo "$PIN" | sed -E 's/.*@//' | sort -u)"
[ "$(echo "$DIGEST" | wc -l)" -eq 1 ] || { echo "FAIL: multiple distinct digests pinned"; exit 1; }
TOKEN="$(curl -s "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/golang:pull" | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')"
# NB: a small GET (the index is ~6.5KB) with a header dump; `curl -X HEAD`
# fails with exit 18 here because it expects a Content-Length-sized body.
HDRS="$(curl -sS -D - -o /dev/null \
  -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json" \
  "https://registry-1.docker.io/v2/library/golang/manifests/$TAG")"
REG_DIGEST="$(printf '%s' "$HDRS" | tr -d '\r' | awk 'tolower($1)=="docker-content-digest:"{print $2}')"
CTYPE="$(printf '%s' "$HDRS" | tr -d '\r' | awk 'tolower($1)=="content-type:"{print $2}')"
echo "registry: golang:$TAG -> $REG_DIGEST ($CTYPE)"
echo "pinned:   $DIGEST"
[ "$REG_DIGEST" = "$DIGEST" ] || { echo "FAIL: pinned digest != registry index digest"; exit 1; }
echo "PASS: pinned digest matches the multi-arch index digest of golang:$TAG"
