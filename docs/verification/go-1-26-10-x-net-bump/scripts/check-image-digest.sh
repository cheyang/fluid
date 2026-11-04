#!/usr/bin/env bash
# C1 (contract): every pinned golang builder image in the repo must reference the
# current multi-arch manifest-list digest of its tag on Docker Hub.
# Exit 1 if any `FROM golang:<tag>@sha256:<pin>` has pin != live index digest,
# or if a Dockerfile uses golang without a digest pin.
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
FAIL=0
TOKEN="$(curl -sfS --max-time 30 "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/golang:pull" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['token'])")"
while IFS= read -r file; do
  line="$(grep -oE 'FROM golang:[^ @]+(@sha256:[0-9a-f]+)?' "$file" | head -1)"
  tag="${line#FROM golang:}"; tag="${tag%%@*}"
  pin=""; [[ "$line" == *@* ]] && pin="${line##*@}"
  digest="$(curl -sfSI --max-time 30 -H "Authorization: Bearer $TOKEN" \
      -H "Accept: application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.index.v1+json" \
      "https://registry-1.docker.io/v2/library/golang/manifests/${tag}" \
      | tr -d '\r' | awk 'tolower($1)=="docker-content-digest:"{print $2}')"
  if [ -z "$pin" ]; then
    echo "FAIL  ${file#"$ROOT"/}  golang:${tag} is not digest-pinned"; FAIL=1
  elif [ "$digest" = "$pin" ]; then
    echo "PASS  ${file#"$ROOT"/}  golang:${tag} -> ${digest}"
  else
    echo "FAIL  ${file#"$ROOT"/}  golang:${tag} pinned ${pin} but live index digest is ${digest:-<unavailable>}"; FAIL=1
  fi
done < <(grep -rlE 'FROM golang:' "$ROOT/docker" "$ROOT/test/gha-e2e" 2>/dev/null)
exit "$FAIL"
