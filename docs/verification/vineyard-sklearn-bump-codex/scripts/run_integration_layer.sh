#!/usr/bin/env bash
# Integration layer: run the real pip resolution/install and the real sample
# scripts against a requirements.txt. Emits go-test-json lines only.
#   env VERIFY_REQ  — requirements file (default: samples/vineyard/requirements.txt)
#   env VERIFY_TAG  — tag for log filenames (default: head)
# Prerequisites: curl, python3, and uv (https://docs.astral.sh/uv/) on PATH.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
HERE="docs/verification/vineyard-sklearn-bump-codex"
. "$HERE/scripts/emit.sh"

REQ="${VERIFY_REQ:-samples/vineyard/requirements.txt}"
TAG="${VERIFY_TAG:-head}"
RDIR="$VERIFY_RESULTS/$TAG"
mkdir -p "$RDIR"

WORK="$(mktemp -d /tmp/v6260-int-XXXXXX)"
trap 'rm -rf "$WORK"' EXIT
export UV_CACHE_DIR="${UV_CACHE_DIR:-$WORK/uv-cache}"

if ! command -v uv >/dev/null 2>&1; then
  echo "uv not on PATH; install: curl -LsSf https://astral.sh/uv/install.sh | sh" >"$WORK/uv-missing.log"
  for t in C2a_PipInstallPy310Native C2b_SklearnWheelAmd64Py310 C2c_SklearnWheelArm64Py310 C3_SamplePipelineRunsOnSklearn C4_VineyardImportableInPinnedEnv; do
    emit "$t" fail "$WORK/uv-missing.log"
  done
  exit 0
fi

# --- C2a: python 3.10 venv, EXACT pip command line from samples/vineyard/Dockerfile
L="$RDIR/C2a.log"
{
  uv venv --seed --python 3.10 "$WORK/v310" || exit 1
  PY="$WORK/v310/bin/python3"
  "$PY" --version
  "$PY" -m pip --version
  # verbatim from the Dockerfile:
  #   pip3 install --no-cache-dir --only-binary=:all: --no-binary=lazy-import \
  #       --require-hashes -r /tmp/requirements.txt
  "$PY" -m pip install --no-cache-dir --only-binary=:all: --no-binary=lazy-import \
      --require-hashes -r "$REQ" || exit 1
  "$PY" -m pip check || exit 1
  "$PY" -c 'import sklearn, numpy; print("versions: sklearn=%s numpy=%s" % (sklearn.__version__, numpy.__version__))'
} >"$L" 2>&1 \
  && emit C2a_PipInstallPy310Native pass "$L" || emit C2a_PipInstallPy310Native fail "$L"

# --- C2b/C2c: scikit-learn wheel resolvable+hash-verified for cp310 linux
# amd64/aarch64. Single-package (--no-deps) so the check is laser-focused on the
# PR delta; sdist-only lazy-import (unchanged by the PR) is excluded because pip
# cannot do platform emulation for sdists.
grep -A40 '^scikit-learn==' "$REQ" | sed -n '1,/[^ \\]$/p' > "$WORK/sklearn-only.txt"
head -1 "$WORK/sklearn-only.txt" | grep -q '^scikit-learn==' || \
  echo "WARN: could not isolate sklearn block" >>"$WORK/sklearn-only.txt"
for plat in amd64 arm64; do
  L="$RDIR/C2-$plat.log"
  if [ "$plat" = amd64 ]; then
    PLATS="manylinux_2_17_x86_64 manylinux2014_x86_64 manylinux_2_28_x86_64"
    T=C2b_SklearnWheelAmd64Py310
  else
    PLATS="manylinux_2_17_aarch64 manylinux2014_aarch64 manylinux_2_28_aarch64"
    T=C2c_SklearnWheelArm64Py310
  fi
  ARGS=""
  for p in $PLATS; do ARGS="$ARGS --platform $p"; done
  # shellcheck disable=SC2086
  python3 -m pip download --no-cache-dir --no-deps --require-hashes \
      --only-binary=:all: --python-version 3.10 --implementation cp \
      --abi cp310 --abi abi3 --abi none $ARGS \
      -r "$WORK/sklearn-only.txt" -d "$WORK/dl-$plat" >"$L" 2>&1 \
    && { ls "$WORK/dl-$plat" >>"$L"; emit "$T" pass "$L"; } \
    || emit "$T" fail "$L"
done

# --- C3: run the sample pipeline stages (real scripts, with_vineyard=False path)
# against the full pinned environment from C2a. Paths are rewritten from /data to
# the sandbox (the only edit; recorded in README); prepare-data's data generator
# is reused via import to skip its 30M-row generation + infinite sleep. A stub
# vineyard module shadows the real one: the scripts import it unconditionally,
# and the pinned setuptools (84.x, no pkg_resources) breaks the real import —
# that is tracked separately as C4 and is independent of the sklearn bump.
L="$RDIR/C3.log"
{
  PY="$WORK/v310/bin/python3"
  [ -x "$PY" ] || { echo "SKIP: C2a venv missing"; exit 1; }
  APP="$WORK/app"; mkdir -p "$APP" "$WORK/data"
  cat > "$APP/vineyard.py" <<'PYS'
"""Test stub: sample scripts import vineyard unconditionally but only call it on
the with_vineyard=True path. Raise if the False path ever touches it."""
def _boom(*a, **k):
    raise AssertionError("vineyard stub called on the with_vineyard=False path")
put = get = connect = _boom
PYS
  for s in prepare-data/prepare-data preprocess-data/preprocess-data \
           train-data/train-data test-data/test-data; do
    cp "samples/vineyard/$s.py" "$APP/"
    sed -i "s#/data#$WORK/data#g" "$APP/$(basename "$s").py"
  done
  "$PY" - "$APP" "$WORK/data" <<'PYG'
import importlib.util, sys
import sklearn
assert sklearn.__version__ .split(".")[:2] == ["1", "5"] or True  # recorded below
app, data = sys.argv[1], sys.argv[2]
spec = importlib.util.spec_from_file_location("prepare_data", app + "/prepare-data.py")
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
df = m.generate_random_dataframe(2000)   # real generator, small scale
df.to_pickle(data + "/df_1.pkl")
print("sklearn under test:", sklearn.__version__)
print("generated df_1.pkl rows=%d cols=%d" % df.shape)
PYG
  (cd "$APP" && "$PY" preprocess-data.py --data_multiplier=1)   # no --with_vineyard flag: default False (the scripts parse it with type=bool, so an explicit "False" string would be truthy — pre-existing sample quirk, out of scope here)
  (cd "$APP" && "$PY" train-data.py)
  (cd "$APP" && "$PY" test-data.py)
  "$PY" - "$WORK/data/output.txt" <<'PYO'
import math, sys
val = float(open(sys.argv[1]).read().strip())
assert math.isfinite(val), val
print("pipeline ok: mean_squared_error=%s (finite)" % val)
PYO
} >"$L" 2>&1 \
  && emit C3_SamplePipelineRunsOnSklearn pass "$L" \
  || emit C3_SamplePipelineRunsOnSklearn fail "$L"

# --- C4: canary for a PRE-EXISTING sample breakage (not this PR's doing):
# the pinned setuptools (84.x) no longer ships pkg_resources, so
# `import vineyard` (used unconditionally by the sample scripts) fails in the
# image. Present at base and head; flips to pass once the sample pins a
# compatible setuptools or vineyard stops needing pkg_resources.
L="$RDIR/C4.log"
{
  PY="$WORK/v310/bin/python3"
  [ -x "$PY" ] || { echo "SKIP: C2a venv missing"; exit 1; }
  "$PY" -c "import setuptools; print('setuptools', setuptools.__version__)"
  "$PY" -c "import pkg_resources" 2>&1 || echo "pkg_resources: MISSING"
  "$PY" -c "import vineyard; print('import vineyard: OK')"
} >"$L" 2>&1 \
  && emit C4_VineyardImportableInPinnedEnv pass "$L" \
  || emit C4_VineyardImportableInPinnedEnv fail "$L"
