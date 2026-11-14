#!/usr/bin/env bash
# helm_render_check.sh — integration-layer check for PR #6245.
# Renders the juicefs chart (head and merge-base copies) under the value
# combinations the controller can emit and asserts where imagePullSecrets land.
# Usage: bash docs/verification/juicefs-imagepullsecrets-codex/scripts/helm_render_check.sh
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
HEAD_CHART="$REPO_ROOT/charts/juicefs"
BASE_SHA="ff99142f9ec5c32a0c9166de023cb8936f0e5ae5"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Extract the merge-base copy of the chart for the backward-compat diff.
mkdir -p "$WORK/base"
git -C "$REPO_ROOT" archive "$BASE_SHA" charts/juicefs charts/library | tar -x -C "$WORK/base"
BASE_CHART="$WORK/base/charts/juicefs"

render() { # <chart> <values-file> <out-file>
  helm template "$1" -f "$2" > "$3" 2>"$3.err" || { cat "$3.err" >&2; return 1; }
}

# secrets_of <render-file> <kind-name-substring> -> prints secret names, one per line
secrets_of() {
  python3 - "$1" "$2" <<'PY'
import sys, yaml
docs = [d for d in yaml.safe_load_all(open(sys.argv[1])) if d]
for d in docs:
    if d.get("metadata", {}).get("name", "").endswith(sys.argv[2]):
        for ref in d["spec"]["template"]["spec"].get("imagePullSecrets", []) or []:
            print(ref["name"])
PY
}

assert_eq() { # <actual> <expected> <msg>
  if [ "$1" != "$2" ]; then
    echo "FAIL: $3 (actual: [$1], expected: [$2])" >&2; exit 1
  fi
  echo "PASS: $3"
}

cat > "$WORK/global-only.yaml" <<'YAML'
imagePullSecrets:
  - name: runtime-secret
YAML
cat > "$WORK/worker-override.yaml" <<'YAML'
imagePullSecrets:
  - name: runtime-secret
worker:
  imagePullSecrets:
    - name: worker-secret
YAML
cat > "$WORK/fuse-override.yaml" <<'YAML'
imagePullSecrets:
  - name: runtime-secret
fuse:
  imagePullSecrets:
    - name: fuse-secret
YAML
cat > "$WORK/both-override.yaml" <<'YAML'
imagePullSecrets:
  - name: runtime-secret
worker:
  imagePullSecrets:
    - name: worker-secret
fuse:
  imagePullSecrets:
    - name: fuse-secret
YAML
cat > "$WORK/worker-only.yaml" <<'YAML'
worker:
  imagePullSecrets:
    - name: worker-secret
YAML
cat > "$WORK/none.yaml" <<'YAML'
{}
YAML

echo "== scenario A: global only (head chart) =="
render "$HEAD_CHART" "$WORK/global-only.yaml" "$WORK/a.yaml"
assert_eq "$(secrets_of "$WORK/a.yaml" worker | sort -u)" "runtime-secret" "worker gets runtime-level secret"
assert_eq "$(secrets_of "$WORK/a.yaml" fuse | sort -u)" "runtime-secret" "fuse gets runtime-level secret"

echo "== scenario B: worker override =="
render "$HEAD_CHART" "$WORK/worker-override.yaml" "$WORK/b.yaml"
assert_eq "$(secrets_of "$WORK/b.yaml" worker | sort -u)" "worker-secret" "worker override wins on worker"
assert_eq "$(secrets_of "$WORK/b.yaml" fuse | sort -u)" "runtime-secret" "fuse keeps runtime-level secret"

echo "== scenario C: fuse override =="
render "$HEAD_CHART" "$WORK/fuse-override.yaml" "$WORK/c.yaml"
assert_eq "$(secrets_of "$WORK/c.yaml" worker | sort -u)" "runtime-secret" "worker keeps runtime-level secret"
assert_eq "$(secrets_of "$WORK/c.yaml" fuse | sort -u)" "fuse-secret" "fuse override wins on fuse"

echo "== scenario D: both overrides =="
render "$HEAD_CHART" "$WORK/both-override.yaml" "$WORK/d.yaml"
assert_eq "$(secrets_of "$WORK/d.yaml" worker | sort -u)" "worker-secret" "worker override wins"
assert_eq "$(secrets_of "$WORK/d.yaml" fuse | sort -u)" "fuse-secret" "fuse override wins"

echo "== scenario E: worker override only, no global =="
render "$HEAD_CHART" "$WORK/worker-only.yaml" "$WORK/e.yaml"
assert_eq "$(secrets_of "$WORK/e.yaml" worker | sort -u)" "worker-secret" "worker gets its own secret"
assert_eq "$(secrets_of "$WORK/e.yaml" fuse | sort -u)" "" "fuse gets no imagePullSecrets"

echo "== scenario F: nothing set =="
render "$HEAD_CHART" "$WORK/none.yaml" "$WORK/f.yaml"
assert_eq "$(secrets_of "$WORK/f.yaml" worker | sort -u)" "" "worker has no imagePullSecrets"
assert_eq "$(secrets_of "$WORK/f.yaml" fuse | sort -u)" "" "fuse has no imagePullSecrets"

echo "== scenario G: backward compat — base chart vs head chart, identical values =="
for v in global-only none; do
  render "$BASE_CHART" "$WORK/$v.yaml" "$WORK/base-$v.yaml"
  render "$HEAD_CHART" "$WORK/$v.yaml" "$WORK/head-$v.yaml"
  # Ignore the intended chart-version bump (juicefs-0.2.16 -> 0.2.17 labels).
  if diff -u <(grep -v 'chart: juicefs-0\.2\.' "$WORK/base-$v.yaml") \
            <(grep -v 'chart: juicefs-0\.2\.' "$WORK/head-$v.yaml") > "$WORK/diff-$v.txt"; then
    echo "PASS: rendered manifests identical between base and head chart ($v)"
  else
    echo "FAIL: rendered manifests differ between base and head chart ($v):" >&2
    cat "$WORK/diff-$v.txt" >&2
    exit 1
  fi
done

echo "ALL HELM RENDER CHECKS PASSED"
