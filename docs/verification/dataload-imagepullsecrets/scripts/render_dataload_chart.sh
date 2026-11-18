#!/usr/bin/env bash
# Reviewer verification harness (PR #6266) — integration layer (L2).
# Renders the fluid-dataloader helm charts with values shaped exactly like
# what JindoFSxEngine/JindoCacheEngine.genDataLoadValue marshals (see
# TestVerifyDataLoadValuesYAMLEncoding), and asserts the DataLoad job pod
# carries the runtime imagePullSecrets — and omits the block when unset.
# Requires: helm on PATH. Run from the repo root (or a checkout of any ref
# carrying charts/fluid-dataloader).
set -u

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
OUT_DIR="${OUT_DIR:-$REPO_ROOT/docs/verification/dataload-imagepullsecrets/results}"
command -v helm >/dev/null 2>&1 || { echo "SKIP: helm not on PATH"; exit 0; }

fail=0
for engine in jindofsx jindocache; do
  chart="$REPO_ROOT/charts/fluid-dataloader/$engine"
  with_secret="$OUT_DIR/chart-render-${engine}-with-secret.yaml"
  without_secret="$OUT_DIR/chart-render-${engine}-without-secret.yaml"

  helm template verify "$chart" \
    --set dataloader.image=fluid:v0.0.1 \
    --set dataloader.targetDataset=test-dataset \
    --set "dataloader.imagePullSecrets[0].name=runtime-secret" \
    > "$with_secret" 2>"$OUT_DIR/chart-render-${engine}-with-secret.stderr"

  helm template verify "$chart" \
    --set dataloader.image=fluid:v0.0.1 \
    --set dataloader.targetDataset=test-dataset \
    > "$without_secret" 2>>"$OUT_DIR/chart-render-${engine}-without-secret.stderr"

  if grep -q -A1 "imagePullSecrets:" "$with_secret" && grep -q "name: runtime-secret" "$with_secret"; then
    echo "PASS ($engine): dataload job pod renders imagePullSecrets runtime-secret"
  else
    echo "FAIL ($engine): rendered job pod missing runtime-secret"
    fail=1
  fi
  if grep -q "imagePullSecrets:" "$without_secret"; then
    echo "FAIL ($engine): imagePullSecrets rendered when none supplied"
    fail=1
  else
    echo "PASS ($engine): no imagePullSecrets block when none supplied"
  fi
done
exit $fail
