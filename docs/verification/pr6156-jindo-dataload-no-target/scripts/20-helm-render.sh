#!/usr/bin/env bash
# L2 (integration) -- render the REAL charts/fluid-dataloader/jindo chart the way the
# operator does (`helm install -f <generated-values> ... <chart>`) and extract the
# DATA_PATH / PATH_REPLICAS env the DataLoad job actually receives.
#
# Proves F1: on master the generated values OMIT `targetPaths` (json omitempty on an
# empty slice), so helm coalesces the chart default `path: "/"` and the job loads the
# WHOLE dataset. The PR replaces that with one path per dataset mount.
#
# Requires: helm v3. No cluster needed (`helm template` only).
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
CHART="$REPO_ROOT/charts/fluid-dataloader/jindo"
OUT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/results"
mkdir -p "$OUT"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "== chart: $CHART"
helm version --short

# ---------------------------------------------------------------------------
# (a) MASTER behaviour: genDataLoadValue produced TargetPaths = []  ->  json
#     `omitempty` drops the key entirely from the values file.
cat > "$WORK/values-master.yaml" <<'EOF'
dataloader:
  targetDataset: test-dataset
  image: fluid:v0.0.1
  loadMetadata: false
  # NOTE: no `targetPaths` key at all -- this is exactly what master emits for a
  # DataLoad with no spec.target, because DataLoadInfo.TargetPaths is
  # `json:"targetPaths,omitempty"` and the slice is empty.
EOF

# (b) PR #6156 behaviour: one target path per dataset mount.
cat > "$WORK/values-pr6156.yaml" <<'EOF'
dataloader:
  targetDataset: test-dataset
  image: fluid:v0.0.1
  loadMetadata: false
  targetPaths:
    - path: /mnt0
      replicas: 1
      fluidNative: true
    - path: /mnt1
      replicas: 1
      fluidNative: true
EOF

render() {
  local name="$1" vals="$2"
  helm template "dl-$name" "$CHART" -f "$vals" --namespace fluid 2>"$WORK/$name.err" \
    > "$WORK/$name.yaml"
  local rc=$?
  if [ $rc -ne 0 ]; then
    echo "!! helm template failed for $name (rc=$rc)"; cat "$WORK/$name.err"; return $rc
  fi
  # Pull the two env values out of the rendered Job.
  local dp pr
  dp=$(awk '/name: DATA_PATH/{getline; print; exit}' "$WORK/$name.yaml" | sed 's/.*value: //')
  pr=$(awk '/name: PATH_REPLICAS/{getline; print; exit}' "$WORK/$name.yaml" | sed 's/.*value: //')
  printf '%-10s DATA_PATH=%-24s PATH_REPLICAS=%s\n' "$name" "$dp" "$pr"
}

{
  echo "### L2 helm render of charts/fluid-dataloader/jindo"
  echo
  echo "-- (a) master: values file omits targetPaths -> chart default applies"
  render master  "$WORK/values-master.yaml"
  echo
  echo "-- (b) PR #6156: values file carries one path per dataset mount"
  render pr6156  "$WORK/values-pr6156.yaml"
  echo
  echo "-- chart documented default (charts/fluid-dataloader/jindo/values.yaml):"
  sed -n '/^  # Default: (path/,/fluidNative: false/p' "$CHART/values.yaml"
} 2>&1 | tee "$OUT/L2-helm-render.txt"

# Keep the rendered jobs for inspection.
cp "$WORK/master.yaml" "$OUT/L2-rendered-job-master.yaml" 2>/dev/null || true
cp "$WORK/pr6156.yaml" "$OUT/L2-rendered-job-pr6156.yaml" 2>/dev/null || true

echo
echo "wrote $OUT/L2-helm-render.txt"
