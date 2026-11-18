#!/bin/bash
# Contract test (polarity: contract — FAILS on the buggy chart, PASSES once the tag matches
# a tag that actually exists on Docker Hub).
#
# Claim F1: "charts/fluid/fluid rendered with default values must reference only image tags
# that exist on Docker Hub (registry the default imagePrefix `fluidcloudnative` points at)."
# Suspicion: PR #6268 bumps the tag to v1.1.0-dcb76167b (9-char sha prefix) while the
# published images for master commit dcb76167b014... are tagged v1.1.0-dcb76167 (8-char).
#
# Usage: bash check_image_tags.sh [chart-dir]   (default charts/fluid/fluid)
set -u

CHART_DIR="${1:-charts/fluid/fluid}"
OUT="${CHECK_IMAGE_OUT:-/tmp/check-image-tags.out}"

if ! command -v helm >/dev/null 2>&1; then
  echo "FATAL: helm not found" >&2
  exit 2
fi

helm template "$CHART_DIR" > /tmp/helm-render-$$.yaml 2>/tmp/helm-render-$$.err || {
  echo "FATAL: helm template failed:" >&2; cat /tmp/helm-render-$$.err >&2; exit 2
}

# All fluid-managed images (default imagePrefix fluidcloudnative). Third-party pinned
# images (registrar, init-users, vineyardd, ...) are out of scope for this bump.
mapfile -t IMAGES < <(grep -oE 'image: fluidcloudnative/[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+' /tmp/helm-render-$$.yaml \
  | sed 's/^image: //' | sort -u)
rm -f /tmp/helm-render-$$.yaml /tmp/helm-render-$$.err

if [ "${#IMAGES[@]}" -eq 0 ]; then
  echo "FATAL: no fluidcloudnative images found in rendered chart" >&2
  exit 2
fi

fail=0
: > "$OUT"
for img in "${IMAGES[@]}"; do
  repo="${img%%:*}"
  tag="${img##*:}"
  code=999
  # retry loop: Docker Hub anonymous API occasionally rate-limits
  for attempt in 1 2 3; do
    code=$(curl -s -o /dev/null -w "%{http_code}" \
      "https://hub.docker.com/v2/repositories/${repo}/tags/${tag}")
    [ "$code" != "429" ] && break
    sleep 2
  done
  if [ "$code" = "200" ]; then
    echo "OK       ${img}" | tee -a "$OUT"
  else
    echo "MISSING  ${img}  (hub API HTTP ${code})" | tee -a "$OUT"
    fail=$((fail+1))
  fi
done

echo "---"
echo "checked: ${#IMAGES[@]} images, missing: $fail (full log: $OUT)"
[ "$fail" -eq 0 ] && exit 0 || exit 1
