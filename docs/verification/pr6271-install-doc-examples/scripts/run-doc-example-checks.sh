#!/usr/bin/env bash
# run-doc-example-checks.sh — verification harness for PR #6271
# (docs: fix chart references in advanced installation examples).
#
# Verifies, with `helm template` (client-side render; no cluster needed, exactly
# as the PR body prescribes) and from an EMPTY working directory:
#
#   P0a/P0b/P0c  PREMISE — the OLD examples (quoted verbatim from the base
#                revision dcb76167b0143a239addc6bed83ed50b45267631) must FAIL
#                as the PR claims ("fail to resolve the chart or open the
#                kubelet placeholder as a file").
#   F1a/F1b/F1c  FIX — the NEW examples, extracted from the CURRENT checked-out
#                docs/en/userguide/install.md, must render and inject the
#                requested values (mount root, FUSE feature gate, custom
#                kubelet root/cert dirs).
#   F2           PARITY — docs/zh/userguide/install.md carries the same commands.
#
# Stdout: one "ID<TAB>pass|fail" line per executed check (consumed by
# re-verify.sh's shellscript layer). If a doc line the harness expects is gone
# (doc restructured / chart repo unreachable), the check emits NOTHING so
# re-verify reports HARNESS-UPDATE instead of a false pass/fail.
# Human-readable detail goes to stderr.
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
EN_DOC="$REPO_ROOT/docs/en/userguide/install.md"
ZH_DOC="$REPO_ROOT/docs/zh/userguide/install.md"
CHART_REPO_URL="https://fluid-cloudnative.github.io/charts"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
cd "$WORK" || exit 2   # premise condition: "From an empty directory"

log()  { printf '%s\n' "$*" >&2; }
emit() { printf '%s\t%s\n' "$1" "$2"; }   # exact "ID<TAB>pass|fail" contract

if ! command -v helm >/dev/null 2>&1; then
  log "harness-error: helm not found in PATH"; exit 2
fi
log "helm: $(helm version --short 2>/dev/null || helm version)"
helm repo add fluid "$CHART_REPO_URL" >/dev/null 2>&1   # idempotent
if ! helm repo update fluid >/dev/null 2>&1; then
  log "harness-error: cannot update fluid chart repo ($CHART_REPO_URL)"; exit 2
fi
log "published charts:"; helm search repo fluid/fluid --versions >&2 2>&1 | head -3 || true

# ---------------------------------------------------------------- P0: premise
# Old command text quoted from base revision dcb7616 (docs/en/userguide/install.md,
# "Advanced Configuration" section before this PR).

run_expect_fail() { # $1=id  $2=expected-stderr-substring  $3...=command
  local id="$1" want="$2"; shift 2
  local out rc
  out="$("$@" 2>&1)"; rc=$?
  log "--- $id: rc=$rc"; log "$out" | head -3
  if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -qF -- "$want"; then
    emit "$id" pass   # premise confirmed: old command fails the way the PR says
  else
    emit "$id" fail   # premise NOT confirmed (old command works, or fails differently)
  fi
}

# P0a/P0b: `helm install fluid --set ... fluid` — bare `fluid` is not a valid chart ref.
run_expect_fail P0a "non-absolute URLs should be in form of repo_name/path_to_chart" \
  helm template fluid --set runtime.mountRoot=/var/lib/docker/runtime-mnt fluid
run_expect_fail P0b "non-absolute URLs should be in form of repo_name/path_to_chart" \
  helm template fluid --set "csi.featureGates=FuseRecovery=true" fluid

# P0c: old example 3, pasted literally — `<kubelet-root-dir>` is parsed by the
# shell as input redirection, so bash tries to OPEN a file named
# `kubelet-root-dir` and fails before helm even runs.
out="$(bash -c 'helm template --set csi.kubelet.rootDir=<kubelet-root-dir> \
  --set csi.kubelet.certDir=<kubelet-root-dir>/pki fluid fluid.tgz' 2>&1)"; rc=$?
log "--- P0c: rc=$rc"; log "$out" | head -3
if [ "$rc" -ne 0 ] && printf '%s' "$out" | grep -q "kubelet-root-dir"; then
  emit P0c pass
else
  emit P0c fail
fi

# ------------------------------------------------------------------ F1: fixed
# Each check first asserts the CURRENT doc still carries the expected example
# line; if the line is gone the check stays silent -> re-verify = HARNESS-UPDATE.

render() { # $1=outfile  $2...=helm args (with `template` substituted for `install`)
  local out="$1"; shift
  helm template "$@" >"$out" 2>"$out.err"
}

check_line_in_doc() { grep -qF -- "$1" "$EN_DOC"; }

# F1a — runtime.mountRoot
L1A='helm install fluid fluid/fluid --set runtime.mountRoot=/var/lib/docker/runtime-mnt'
O1A='helm install fluid --set runtime.mountRoot=/var/lib/docker/runtime-mnt fluid'
if check_line_in_doc "$L1A"; then
  if render f1a.yaml fluid fluid/fluid --set runtime.mountRoot=/var/lib/docker/runtime-mnt \
     && grep -qF -- '--prune-path=/var/lib/docker/runtime-mnt' f1a.yaml \
     && ! grep -qF -- '--prune-path=/runtime-mnt' f1a.yaml; then
    emit F1a pass
  else
    log "F1a: render failed or value not injected"; head -3 f1a.yaml.err >&2 2>/dev/null || true
    emit F1a fail
  fi
elif check_line_in_doc "$O1A"; then
  log "F1a: OLD (broken) example still present in $EN_DOC"
  emit F1a fail
else
  log "F1a: neither new nor old example found in $EN_DOC (harness-update needed)"
fi

# F1b — csi.featureGates
L1B="helm install fluid fluid/fluid --set csi.featureGates='FuseRecovery=true'"
O1B="helm install fluid --set csi.featureGates='FuseRecovery=true' fluid"
if check_line_in_doc "$L1B"; then
  if render f1b.yaml fluid fluid/fluid --set "csi.featureGates=FuseRecovery=true" \
     && grep -qF -- '--feature-gates=FuseRecovery=true' f1b.yaml; then
    emit F1b pass
  else
    log "F1b: render failed or gate not injected"; head -3 f1b.yaml.err >&2 2>/dev/null || true
    emit F1b fail
  fi
elif check_line_in_doc "$O1B"; then
  log "F1b: OLD (broken) example still present in $EN_DOC"
  emit F1b fail
else
  log "F1b: neither new nor old example found in $EN_DOC (harness-update needed)"
fi

# F1c — kubelet rootDir/certDir via KUBELET_ROOTDIR shell variable.
# The four component lines of the snippet are each extracted from the doc, then
# the reassembled snippet is executed with bash (multi-line paste semantics).
L3A='KUBELET_ROOTDIR=/data/kubelet'
L3B='helm install fluid fluid/fluid \'
L3C='--set "csi.kubelet.rootDir=${KUBELET_ROOTDIR}"'
L3D='--set "csi.kubelet.certDir=${KUBELET_ROOTDIR}/pki"'
if check_line_in_doc "$L3A" && check_line_in_doc "$L3B" \
   && check_line_in_doc "$L3C" && check_line_in_doc "$L3D"; then
  cat > f1c.sh <<SNIP
KUBELET_ROOTDIR=/data/kubelet
helm template fluid fluid/fluid \\
  --set "csi.kubelet.rootDir=\${KUBELET_ROOTDIR}" \\
  --set "csi.kubelet.certDir=\${KUBELET_ROOTDIR}/pki"
SNIP
  if bash f1c.sh >f1c.yaml 2>f1c.err \
     && grep -qF -- '--kubelet-registration-path=/data/kubelet/csi-plugins/fuse.csi.fluid.io/csi.sock' f1c.yaml \
     && grep -qF -- 'path: "/data/kubelet"' f1c.yaml; then
    emit F1c pass
  else
    log "F1c: render failed or kubelet dirs not injected"; head -3 f1c.err >&2 2>/dev/null || true
    emit F1c fail
  fi
elif grep -qF 'csi.kubelet.rootDir=<kubelet-root-dir>' "$EN_DOC"; then
  log "F1c: OLD (broken) example still present in $EN_DOC"
  emit F1c fail
else
  log "F1c: neither new nor old example found in $EN_DOC (harness-update needed)"
fi

# ---------------------------------------------------------------- F2: en/zh parity
missing=0
for line in "$L1A" "$L1B" "$L3A" "$L3B" "$L3C" "$L3D"; do
  if ! grep -qF -- "$line" "$ZH_DOC"; then
    log "F2: zh doc missing line: $line"; missing=1
  fi
done
if [ "$missing" -eq 0 ]; then emit F2 pass; else emit F2 fail; fi

log "done."
