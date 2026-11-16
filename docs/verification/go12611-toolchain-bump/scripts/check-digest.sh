#!/usr/bin/env bash
# C4 — image digest contract for PR #6255 (Go 1.26.11 toolchain bump).
#
# The 12 Dockerfiles pin golang:1.26.11-bookworm@sha256:dc40a13b... That digest
# must be the multi-arch image *index* digest of the golang:1.26.11-bookworm tag
# (so all build platforms resolve), and every golang: FROM line in the repo must
# use the same pin.
#
# Emits "CHECK C4-digest pass|fail" lines; exit 0 iff all pass.
# Layer: unit (network registry queries, no cluster needed).
set -uo pipefail

TAG="1.26.11-bookworm"
EXPECT_DIGEST="sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0"
EXPECT_PLATFORMS="amd64 arm64/v8 arm/v7 386 ppc64le"
ACCEPT="application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json"

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

r1=0 r2=0 r3=0 r4=0

# 1) tag -> digest on mirror.gcr.io
got="$(curl -sS --max-time 30 -I "https://mirror.gcr.io/v2/library/golang/manifests/${TAG}" \
  -H "Accept: ${ACCEPT}" 2>/dev/null | tr -d '\r' | awk -F': ' 'tolower($1)=="docker-content-digest"{print $2}')"
if [ "$got" = "$EXPECT_DIGEST" ]; then
  echo "CHECK C4-digest-registry pass  mirror.gcr.io ${TAG} -> ${got}"
else
  echo "CHECK C4-digest-registry fail  mirror.gcr.io ${TAG} -> '${got:-<none>}' expected ${EXPECT_DIGEST}"
  r1=1
fi

# 2) tag -> digest on Docker Hub (token auth)
token="$(curl -sS --max-time 30 "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/golang:pull" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])' 2>/dev/null)"
got2="$(curl -sS --max-time 30 -I "https://registry-1.docker.io/v2/library/golang/manifests/${TAG}" \
  -H "Authorization: Bearer ${token}" -H "Accept: ${ACCEPT}" 2>/dev/null | tr -d '\r' | awk -F': ' 'tolower($1)=="docker-content-digest"{print $2}')"
if [ "$got2" = "$EXPECT_DIGEST" ]; then
  echo "CHECK C4-digest-dockerhub pass  registry-1.docker.io ${TAG} -> ${got2}"
else
  echo "CHECK C4-digest-dockerhub fail  registry-1.docker.io ${TAG} -> '${got2:-<none>}' expected ${EXPECT_DIGEST}"
  r2=1
fi

# 3) index covers the expected build platforms
plats="$(curl -sS --max-time 30 "https://mirror.gcr.io/v2/library/golang/manifests/${TAG}" \
  -H "Accept: ${ACCEPT}" 2>/dev/null \
  | python3 -c 'import sys,json
d=json.load(sys.stdin)
print(" ".join(sorted((m["platform"].get("os","")+"/"+m["platform"].get("architecture","")+("/"+m["platform"]["variant"] if m["platform"].get("variant") else ""))
  for m in d["manifests"] if m.get("platform",{}).get("os")=="linux")))')"
missing=""
for p in $EXPECT_PLATFORMS; do
  case " $plats " in *"linux/${p}"*) ;; *) missing="$missing linux/${p}";; esac
done
if [ -z "$missing" ]; then
  echo "CHECK C4-digest-platforms pass  linux platforms: $plats"
else
  echo "CHECK C4-digest-platforms fail  missing:${missing} (got: $plats)"
  r3=1
fi

# 4) every golang: FROM line in the repo uses exactly this pin
count="$(grep -rhoE 'FROM golang:[^ ]+' docker test/gha-e2e 2>/dev/null | sort -u)"
bad="$(grep -rn 'FROM golang:' docker test/gha-e2e 2>/dev/null | grep -v "golang:${TAG}@${EXPECT_DIGEST}" || true)"
n_total="$(grep -rln 'FROM golang:' docker test/gha-e2e 2>/dev/null | wc -l)"
if [ -z "$bad" ] && [ "$n_total" -eq 12 ]; then
  echo "CHECK C4-digest-dockerfiles pass  ${n_total}/12 Dockerfiles pin golang:${TAG}@${EXPECT_DIGEST} consistently"
else
  echo "CHECK C4-digest-dockerfiles fail  ${n_total}/12 Dockerfiles; inconsistent refs:"
  echo "$bad"
  r4=1
fi

exit $((r1+r2+r3+r4))
