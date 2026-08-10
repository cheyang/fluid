#!/usr/bin/env bash
# L3 (behavioral) -- run the repo's OWN dataload script (the `dataloader.distributedLoad`
# entry of charts/fluid-dataloader/jindo/templates/configmap.yaml, verbatim) against a
# stubbed JindoFS CLI, for both the master-generated and PR-generated DATA_PATH.
#
# Proves F2: the PR's per-mount target paths are not present in the single JindoFS
# namespace, and the script's own checkPathExistence() turns that into `exit 1` --
# i.e. a FAILED DataLoad job, where master loaded the whole dataset successfully.
#
# The stub models `jfs://jindo/` as the ONE configured UFS root (see
# pkg/ddc/jindo/transform.go: jfs.namespaces is the single literal "jindo" and every
# mount overwrites jfs.namespaces.jindo.<mode>.uri, so the last mount's bucket root IS
# the namespace root). Paths that exist there are "/" and the bucket's real contents;
# /mnt0, /mnt1, /spark, /hive are NOT namespace paths.
#
# Requires: bash, helm (to render the configmap), coreutils timeout. No cluster.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../../.." && pwd)"
CHART="$REPO_ROOT/charts/fluid-dataloader/jindo"
OUT="$(cd "$HERE/.." && pwd)/results"
mkdir -p "$OUT"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# --- 1. extract dataloader.distributedLoad verbatim from the rendered ConfigMap ------
helm template dl-extract "$CHART" \
  --set dataloader.targetDataset=test-dataset \
  --set dataloader.image=fluid:v0.0.1 \
  --namespace fluid --show-only templates/configmap.yaml > "$WORK/cm.yaml" 2>"$WORK/cm.err"
if [ ! -s "$WORK/cm.yaml" ]; then
  echo "!! failed to render configmap"; cat "$WORK/cm.err"; exit 2
fi

awk '
  /^  dataloader\.distributedLoad: \|/ { grab=1; next }
  grab {
    # stop at the next key at the same (2-space) indent level
    if ($0 ~ /^  [A-Za-z0-9._-]+:/) exit
    sub(/^    /, "")
    print
  }
' "$WORK/cm.yaml" > "$WORK/jindo_dataload.sh"

if [ ! -s "$WORK/jindo_dataload.sh" ]; then
  echo "!! failed to extract dataloader.distributedLoad"; exit 2
fi
chmod +x "$WORK/jindo_dataload.sh"
echo "extracted $(wc -l < "$WORK/jindo_dataload.sh") lines of the repo's dataload script"

# --- 2. stub the JindoFS CLI ---------------------------------------------------------
mkdir -p "$WORK/bin"

# `hadoop fs -ls jfs://jindo<path>` -- succeeds only for paths that really exist in the
# single configured UFS root. Configure via EXISTING_PATHS (colon separated).
cat > "$WORK/bin/hadoop" <<'STUB'
#!/usr/bin/env bash
# stub: models the single JindoFS namespace rooted at the one configured UFS URI
target="${!#}"                      # last arg, e.g. jfs://jindo/mnt0
path="${target#jfs://jindo}"
[ -z "$path" ] && path="/"
existing="${EXISTING_PATHS:-/}"
IFS=':' read -r -a arr <<< "$existing"
for p in "${arr[@]}"; do
  if [ "$p" = "$path" ]; then
    echo "Found 1 items"
    echo "drwxr-xr-x   - root root          0 2026-08-10 00:00 $path"
    exit 0
  fi
done
echo "ls: \`$target': No such file or directory" >&2
exit 1
STUB

# `jindo jfs -load ...` -- records each load invocation.
cat > "$WORK/bin/jindo" <<'STUB'
#!/usr/bin/env bash
echo "LOAD_INVOCATION: jindo $*" >> "$LOAD_LOG"
exit 0
STUB
chmod +x "$WORK/bin/hadoop" "$WORK/bin/jindo"

# --- 3. run both scenarios ----------------------------------------------------------
run_case() {
  local name="$1" data_path="$2" replicas="$3" existing="$4"
  local log="$WORK/$name.load"
  : > "$log"
  (
    export PATH="$WORK/bin:$PATH"
    export LOAD_LOG="$log"
    export EXISTING_PATHS="$existing"
    export DATA_PATH="$data_path"
    export PATH_REPLICAS="$replicas"
    export NEED_LOAD_METADATA=false
    export LOAD_MEMORY_DATA=true
    export LOAD_METADATA_ONLY=false
    export ENABLE_ATOMIC_CACHE=false
    bash "$WORK/jindo_dataload.sh"
  ) > "$WORK/$name.out" 2>&1
  local rc=$?
  local loads
  loads=$(grep -c 'LOAD_INVOCATION' "$log" 2>/dev/null || echo 0)
  printf '%-24s DATA_PATH=%-18s exit=%-3s loads=%s  => %s\n' \
    "$name" "'${data_path}'" "$rc" "$loads" \
    "$( [ "$rc" -eq 0 ] && echo "job SUCCEEDS" || echo "job FAILS (DataLoad -> Failed)" )"
  echo "      loaded: $(sed 's/^LOAD_INVOCATION: //' "$log" | tr '\n' '|' )"
}

{
  echo "### L3 behaviour of the repo's own dataloader.distributedLoad script"
  echo "### UFS namespace jfs://jindo/ contains: / and /data (the real bucket contents)"
  echo
  echo "-- (a) master: chart default supplies path '/' (targetPaths omitted from values)"
  run_case "master-root"        "/"           "1"    "/:/data"
  echo
  echo "-- (b) PR #6156: per-mount paths derived from spec.mounts[*].path"
  run_case "pr6156-mount-paths" "/mnt0:/mnt1" "1:1"  "/:/data"
  echo
  echo "-- (c) PR #6156: per-mount paths derived from spec.mounts[*].name (no .path set)"
  run_case "pr6156-mount-names" "/spark:/hive" "1:1" "/:/data"
  echo
  echo "-- (d) control: if the derived path DID exist in the UFS, the PR path would work"
  run_case "pr6156-if-existed"  "/mnt0:/mnt1" "1:1"  "/:/data:/mnt0:/mnt1"
} 2>&1 | tee "$OUT/L3-script-behavior.txt"

cp "$WORK"/*.out "$OUT/" 2>/dev/null || true
echo
echo "wrote $OUT/L3-script-behavior.txt"
