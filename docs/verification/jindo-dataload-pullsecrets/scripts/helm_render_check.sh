#!/usr/bin/env bash
# Integration layer for PR #6266 (Reviewer B / Codex).
# Proves the end-to-end plumbing: genDataLoadValue() output -> values YAML -> `helm template`
# -> imagePullSecrets in the rendered pod spec, for both charts (jindofsx, cache) and both
# policies (Job, CronJob). Run from the repo root of the ref under test, with the harness
# test files (pkg/ddc/*/dataload_pullsecrets_verify_test.go) present.
#
# On the PR head all checks pass. On the base branch the runtime-secret values contain
# env/empty secrets instead, so checks 1/2/4/5 fail (premise reproduction at chart level).
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
VERIFY_DIR="$REPO_ROOT/docs/verification/jindo-dataload-pullsecrets"
RESULTS="$VERIFY_DIR/results"
mkdir -p "$RESULTS"

failures=0
note() { echo "[helm-render] $*"; }
fail() { echo "[helm-render] FAIL: $*"; failures=$((failures+1)); }
pass() { echo "[helm-render] PASS: $*"; }

# --- 1. Emit real values files from production genDataLoadValue (additive emitter tests) ---
VERIFY_EMIT_DIR="$RESULTS" go test ./pkg/ddc/jindofsx -run 'TestVerifyEmitJindoFSxDataLoadValues' -count=1 >/dev/null
VERIFY_EMIT_DIR="$RESULTS" go test ./pkg/ddc/jindocache -run 'TestVerifyEmitJindoCacheDataLoadValues' -count=1 >/dev/null
note "emitted values files into $RESULTS"

render() { # <chart> <values-file> [extra helm args...]
  local chart="$1" values="$2"; shift 2
  helm template test-release "$chart" -f "$values" "$@" 2>"$RESULTS/helm-stderr.log"
}

# --- 2. jindofsx, Job policy: runtime-secret must reach the Job pod spec ---
out="$(render charts/fluid-dataloader/jindofsx "$RESULTS/jindofsx-values-runtime-secret.yaml")"
echo "$out" > "$RESULTS/jindofsx-job-runtime-secret.yaml"
if echo "$out" | grep -q "kind: Job" && echo "$out" | grep -q "imagePullSecrets:" && echo "$out" | grep -q "name: runtime-secret"; then
  pass "jindofsx Job carries runtime-secret imagePullSecrets"
else
  fail "jindofsx Job carries runtime-secret imagePullSecrets"
fi

# --- 3. jindofsx, no secrets: imagePullSecrets key must be absent entirely ---
out="$(render charts/fluid-dataloader/jindofsx "$RESULTS/jindofsx-values-no-secret.yaml")"
echo "$out" > "$RESULTS/jindofsx-job-no-secret.yaml"
if echo "$out" | grep -q "imagePullSecrets:"; then
  fail "jindofsx Job omits imagePullSecrets when none configured"
else
  pass "jindofsx Job omits imagePullSecrets when none configured"
fi

# --- 4. jindofsx, Cron policy: runtime-secret must reach the CronJob pod template ---
out="$(render charts/fluid-dataloader/jindofsx "$RESULTS/jindofsx-values-runtime-secret.yaml" \
  --set dataloader.policy=Cron --set 'dataloader.schedule=*/5 * * * *')"
echo "$out" > "$RESULTS/jindofsx-cronjob-runtime-secret.yaml"
if echo "$out" | grep -q "kind: CronJob" && echo "$out" | grep -q "name: runtime-secret"; then
  pass "jindofsx CronJob carries runtime-secret imagePullSecrets"
else
  fail "jindofsx CronJob carries runtime-secret imagePullSecrets"
fi

# --- 5. cache (jindocache), Job policy ---
out="$(render charts/fluid-dataloader/cache "$RESULTS/jindocache-values-runtime-secret.yaml")"
echo "$out" > "$RESULTS/jindocache-job-runtime-secret.yaml"
if echo "$out" | grep -q "kind: Job" && echo "$out" | grep -q "name: runtime-secret"; then
  pass "jindocache Job carries runtime-secret imagePullSecrets"
else
  fail "jindocache Job carries runtime-secret imagePullSecrets"
fi

# --- 6. cache (jindocache), Cron policy ---
out="$(render charts/fluid-dataloader/cache "$RESULTS/jindocache-values-runtime-secret.yaml" \
  --set dataloader.policy=Cron --set 'dataloader.schedule=*/5 * * * *')"
echo "$out" > "$RESULTS/jindocache-cronjob-runtime-secret.yaml"
if echo "$out" | grep -q "kind: CronJob" && echo "$out" | grep -q "name: runtime-secret"; then
  pass "jindocache CronJob carries runtime-secret imagePullSecrets"
else
  fail "jindocache CronJob carries runtime-secret imagePullSecrets"
fi

if [ "$failures" -gt 0 ]; then
  echo "[helm-render] $failures check(s) FAILED"
  exit 1
fi
echo "[helm-render] all checks passed"
