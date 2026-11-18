#!/usr/bin/env bash
# Render the fluid chart at the checked-out ref and list every image reference
# that carries the chart's defaultVersion tag, then check each against the
# Docker Hub v2 tags API. Exits non-zero if any rendered image is missing.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

DEFAULT_VERSION="$(grep -E '^version: &defaultVersion' charts/fluid/fluid/values.yaml | awk '{print $3}')"
echo "defaultVersion=${DEFAULT_VERSION}"

helm template fluid charts/fluid/fluid --namespace fluid-system > /tmp/fluid-rendered.yaml
echo "--- rendered images carrying the defaultVersion tag:"
grep -oE 'image: *"?[^ "]+:'"${DEFAULT_VERSION}"'"?' /tmp/fluid-rendered.yaml | sed 's/image: *//; s/"//g' | sort -u | tee /tmp/fluid-rendered-images.txt

FAIL=0
while read -r ref; do
  repo_tag="${ref#docker.io/}"; repo_tag="${repo_tag%:*}"; tag="${ref##*:}"
  code="$(curl -s -o /dev/null -w '%{http_code}' "https://hub.docker.com/v2/repositories/${repo_tag}/tags/${tag}")"
  echo "${ref} -> HTTP ${code}"
  [ "${code}" = "200" ] || FAIL=1
  sleep 0.2
done < /tmp/fluid-rendered-images.txt

if [ "${FAIL}" = "1" ]; then
  echo "RESULT: FAIL — rendered chart references image tag(s) that do not exist on Docker Hub"
  exit 1
fi
echo "RESULT: PASS — all rendered defaultVersion images exist on Docker Hub"
