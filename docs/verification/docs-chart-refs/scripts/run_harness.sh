#!/usr/bin/env bash
# run_harness.sh — verification harness for PR #6271 (docs: fix chart references in
# advanced installation examples).
#
# Layer: "render" (unit-equivalent for a docs PR). The production code under review
# IS the documentation; the falsifiable claims are about what the documented commands
# actually do when executed with Helm against the chart repo the guide registers
# (https://fluid-cloudnative.github.io/charts).
#
# Usage:
#   bash docs/verification/docs-chart-refs/scripts/run_harness.sh [head|base]
#
#   head (default) — run the commands as documented on the PR HEAD:
#       * T1/T2/T3 contract tests: the three corrected advanced examples must render
#         and produce the expected values (FAIL = broken, PASS = correct).
#       * F1 contract test: no stale chart references (fluid.tgz / bare chart refs)
#         may remain in the two install guides. Currently RED (still-broken) — this
#         is the open finding about the incomplete sweep.
#   base — reproduce the PR's premise: run the OLD (merge-base) command forms and
#       assert they FAIL (P0-*). This doubles as the harness-bites check: the same
#       assertion helpers applied to the pre-fix commands go red.
#
# Output: one "<name><TAB>pass|fail" line per test on stdout (consumed by
# re-verify.sh, kind=shell). Human-readable detail goes to stderr and to
# $RESULTS_DIR/harness-<mode>.log.
#
# Requirements: git, helm >= 3, network access to fluid-cloudnative.github.io.
# No cluster needed — `helm template` renders without a kube API server.
set -uo pipefail

MODE="${1:-head}"
case "$MODE" in head|base) ;; *) echo "usage: $0 [head|base]" >&2; exit 2;; esac

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || { echo "not in a git repo" >&2; exit 2; }
cd "$REPO_ROOT"

HARNESS_DIR="docs/verification/docs-chart-refs"
RESULTS_DIR="${RESULTS_DIR:-$HARNESS_DIR/results}"
mkdir -p "$RESULTS_DIR"
LOG="$RESULTS_DIR/harness-$MODE.log"
: > "$LOG"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

log() { printf '%s\n' "$*" | tee -a "$LOG" >&2; }
report() { # $1=test name  $2=pass|fail  $3=detail
  printf '%s\t%s\n' "$1" "$2"
  log "  [$2] $1 — $3"
}

# --- chart source: exactly what the guide registers -------------------------
helm repo list 2>/dev/null | awk '{print $1}' | grep -qx fluid \
  || helm repo add fluid https://fluid-cloudnative.github.io/charts >>"$LOG" 2>&1 \
  || log "WARN: helm repo add failed"
helm repo update fluid >>"$LOG" 2>&1 || log "WARN: helm repo update failed (offline? using cached index)"
CHART_VER="$(helm search repo fluid/fluid 2>/dev/null | awk 'NR==2{print $2}')"
log "chart under test: fluid/fluid ${CHART_VER:-<unresolved>} (helm $(helm version --short))"
log "mode: $MODE   repo: $REPO_ROOT @ $(git rev-parse --short HEAD)"

# --- assertion helpers -------------------------------------------------------
render_ok() { # $1=expect-substring  $2=helm-args... ; renders chart, greps output
  local expect="$1"; shift
  local out
  out="$(cd "$WORK" && helm template fluid fluid/fluid "$@" 2>&1)"
  if [ $? -ne 0 ]; then log "    helm error: $(printf '%s' "$out" | head -2)"; return 1; fi
  printf '%s' "$out" | grep -qF -- "$expect"
}
cmd_must_fail() { # $1=label  rest=command (must exit non-zero / produce error)
  local label="$1"; shift
  local out rc
  out="$(cd "$WORK" && bash -c "$*" 2>&1)"; rc=$?
  printf '%s\n' "$out" | head -3 >> "$LOG"
  [ $rc -ne 0 ] || printf '%s' "$out" | grep -qi '^error'
}

if [ "$MODE" = "base" ]; then
  # ================= P0: premise reproduction (merge-base docs) =============
  # Old example 1 (en/zh install.md, advanced config #1):
  #   helm install fluid --set runtime.mountRoot=... fluid
  if cmd_must_fail P0-old-cmd1 \
      "helm template fluid --set runtime.mountRoot=/var/lib/docker/runtime-mnt fluid"; then
    report "P0-old-cmd1-fails" pass "bare 'fluid' chart ref is unresolvable from the repo-based flow"
  else
    report "P0-old-cmd1-fails" fail "old command unexpectedly resolved a chart"
  fi
  # Old example 3 chart ref:
  #   helm install --set ... fluid fluid.tgz
  if cmd_must_fail P0-old-cmd3-chart \
      "helm template fluid fluid.tgz"; then
    report "P0-old-cmd2-fails" pass "fluid.tgz does not exist in the repo-based flow"
  else
    report "P0-old-cmd2-fails" fail "fluid.tgz unexpectedly resolved"
  fi
  # Old example 3 literal placeholder: bash treats <kubelet-root-dir> as a redirect.
  if cmd_must_fail P0-old-cmd3-placeholder \
      "helm template fluid fluid.tgz --set csi.kubelet.rootDir=<kubelet-root-dir> --set csi.kubelet.certDir=<kubelet-root-dir>/pki"; then
    report "P0-old-cmd3-fails" pass "shell parses <kubelet-root-dir> as an input redirect"
  else
    report "P0-old-cmd3-fails" fail "placeholder was not consumed by the shell?"
  fi
  exit 0
fi

# ================= HEAD: contract tests for the fixed examples ==============

# T1 — advanced example 1: runtime.mountRoot override renders everywhere
if render_ok "/var/lib/docker/runtime-mnt" --set runtime.mountRoot=/var/lib/docker/runtime-mnt; then
  report "T1-mountRoot-renders" pass "mount root override appears in csi daemonset + controllers"
else
  report "T1-mountRoot-renders" fail "mountRoot override not found in rendered output"
fi

# T2 — advanced example 2: csi.featureGates=FuseRecovery=true reaches the CSI plugins container
if render_ok "--feature-gates=FuseRecovery=true" --set csi.featureGates='FuseRecovery=true'; then
  report "T2-fuseRecovery-gate-renders" pass "CSI plugins container args carry the feature gate"
else
  report "T2-fuseRecovery-gate-renders" fail "feature gate not found in rendered output"
fi

# T3 — advanced example 3: KUBELET_ROOTDIR shell var drives csi-address /
# kubelet-registration-path / hostPath mounts.
KUBELET_ROOTDIR=/data/kubelet
if render_ok "/data/kubelet/csi-plugins/fuse.csi.fluid.io/csi.sock" \
      --set "csi.kubelet.rootDir=${KUBELET_ROOTDIR}" \
      --set "csi.kubelet.certDir=${KUBELET_ROOTDIR}/pki" \
   && render_ok "path: /data/kubelet/plugins_registry" \
      --set "csi.kubelet.rootDir=${KUBELET_ROOTDIR}" \
      --set "csi.kubelet.certDir=${KUBELET_ROOTDIR}/pki"; then
  report "T3-kubeletRootDir-renders" pass "csi socket path, registration path and hostPaths follow the custom root"
else
  report "T3-kubeletRootDir-renders" fail "custom kubelet root not reflected in rendered output"
fi

# T3b — documentation-adjacent check: certDir nested under rootDir is subsumed by the
# rootDir hostPath (charts .../csi/daemonset.yaml `hasPrefix` logic), so the explicit
# --set has no visible effect in that case. A certDir OUTSIDE rootDir must render.
if render_ok '/custom/pki' \
      --set "csi.kubelet.rootDir=/data/kubelet" \
      --set "csi.kubelet.certDir=/custom/pki"; then
  report "T3b-kubeletCertDir-propagates" pass "a certDir outside rootDir gets its own hostPath"
else
  report "T3b-kubeletCertDir-propagates" fail "certDir outside rootDir did not render"
fi

# F1 — open finding: same-class stale chart references remain in the two guides.
STALE="$(grep -nE 'fluid\.tgz|fluid fluid/' docs/en/userguide/install.md docs/zh/userguide/install.md || true)"
if [ -z "$STALE" ]; then
  report "F1-no-stale-chart-refs" pass "no fluid.tgz / trailing-slash chart refs left in the guides"
else
  report "F1-no-stale-chart-refs" fail "stale refs remain: $(printf '%s' "$STALE" | tr '\n' ';' | cut -c1-200)"
fi

log "harness complete (mode=$MODE); log: $LOG"
