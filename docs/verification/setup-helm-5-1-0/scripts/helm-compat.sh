#!/usr/bin/env bash
# L2 (integration) — PR #6270: the Helm versions azure/setup-helm@v5.1.0 can
# install must work with the exact helm operations fluid's CI performs.
#
# Background: both modified workflows call setup-helm with NO 'version' input,
# so the action installs 'latest' (fetched from get.helm.sh/helm-latest-version)
# and, if that lookup fails, the built-in fallback. v5.1.0 moved the fallback
# from v3.18.4 to v4.3.1, so the two versions that matter are:
#   - latest          (what CI actually installs; helm 4.x today)
#   - v4.3.1          (the new built-in fallback)
#
# Contracts (polarity=contract; PASS = healthy), run for BOTH versions:
#   LATEST_RESOLVES          get.helm.sh/helm-latest-version returns a semver tag
#   HELM_<v>_CHECKSUM        archive sha256 matches the published .sha256
#                            (mirrors the action's new checksum-policy: auto)
#   HELM_<v>_TEMPLATE_LOCAL  'helm template' renders charts/fluid/fluid with the
#                            flags used by .github/scripts/deploy-fluid-to-kind.sh
#   HELM_<v>_TEMPLATE_REPO   'helm repo add/update + template fluid/fluid' works
#                            (the path used by gha-backward-compatibility.sh)
#
# Usage: helm-compat.sh [REPO_DIR]   (REPO_DIR = fluid checkout; default cwd)
# Emits "<check>\tpass|fail" lines; exit 0 iff all pass.
set -uo pipefail

REPO_DIR="${1:-.}"
FALLBACK_V="v4.3.1"   # stableHelmVersion baked into azure/setup-helm@v5.1.0
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
ok=1
status() { printf '%s\t%s\n' "$1" "$2"; }
fail() { status "$1" fail; ok=0; }

LATEST="$(curl -fsS --max-time 30 https://get.helm.sh/helm-latest-version 2>/dev/null | tr -d '[:space:]')"
echo "helm-latest-version -> ${LATEST:-<unreachable>}"
if [[ "$LATEST" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then status LATEST_RESOLVES pass; else status LATEST_RESOLVES fail; ok=0; LATEST=""; fi

check_one() { # $1 = helm version tag
  local v="$1" tag="HELM_${1//[.]/_}"
  local tgz="$WORK/helm-$v.tgz" sum_file="$WORK/helm-$v.tgz.sha256"
  if ! curl -fsSL --max-time 120 -o "$tgz" "https://get.helm.sh/helm-$v-linux-amd64.tar.gz"; then
    echo "download failed for $v"; fail "${tag}_CHECKSUM"; fail "${tag}_TEMPLATE_LOCAL"; fail "${tag}_TEMPLATE_REPO"; return
  fi
  if ! curl -fsSL --max-time 30 -o "$sum_file" "https://get.helm.sh/helm-$v-linux-amd64.tar.gz.sha256"; then
    echo "checksum sidecar missing for $v"; fail "${tag}_CHECKSUM"
  else
    (cd "$WORK" && echo "$(cat "$sum_file")  helm-$v.tgz" | sha256sum -c - >/dev/null 2>&1) \
      && status "${tag}_CHECKSUM" pass || { echo "sha256 mismatch for $v"; fail "${tag}_CHECKSUM"; }
  fi
  mkdir -p "$WORK/$v"; tar -xzf "$tgz" -C "$WORK/$v"
  local helm="$WORK/$v/linux-amd64/helm"
  "$helm" version
  # 1) local chart, same --set flags as deploy-fluid-to-kind.sh.
  # NB: the chart ships CRDs under crds/, which 'helm template' skips unless
  # --include-crds is given; assert Deployment on the plain render and CRDs on
  # the --include-crds render.
  if "$helm" template fluid "$REPO_DIR/charts/fluid/fluid" \
      --namespace fluid-system \
      --set runtime.jindo.smartdata.imagePrefix=registry-cn-hongkong.ack.aliyuncs.com/acs \
      --set runtime.jindo.fuse.imagePrefix=registry-cn-hongkong.ack.aliyuncs.com/acs \
      --set runtime.cacheruntime.enabled=true > "$WORK/local-$v.yaml" 2>"$WORK/local-$v.err" \
     && grep -q '^kind: Deployment' "$WORK/local-$v.yaml" \
     && "$helm" template fluid "$REPO_DIR/charts/fluid/fluid" --namespace fluid-system --include-crds \
        > "$WORK/local-crds-$v.yaml" 2>>"$WORK/local-$v.err" \
     && grep -q '^kind: CustomResourceDefinition' "$WORK/local-crds-$v.yaml"; then
    status "${tag}_TEMPLATE_LOCAL" pass
  else
    echo "--- template local stderr ($v):"; cat "$WORK/local-$v.err"; fail "${tag}_TEMPLATE_LOCAL"
  fi
  # 2) published chart (backward-compat path)
  "$helm" repo add fluid https://fluid-cloudnative.github.io/charts --force-update >/dev/null 2>&1
  "$helm" repo update fluid >/dev/null 2>&1
  if "$helm" template fluid fluid/fluid --namespace fluid-system > "$WORK/repo-$v.yaml" 2>"$WORK/repo-$v.err" \
     && grep -q '^kind: ' "$WORK/repo-$v.yaml"; then
    status "${tag}_TEMPLATE_REPO" pass
  else
    echo "--- template repo stderr ($v):"; cat "$WORK/repo-$v.err"; fail "${tag}_TEMPLATE_REPO"
  fi
}

[ -n "$LATEST" ] && check_one "$LATEST"
check_one "$FALLBACK_V"
exit $((1-ok))
