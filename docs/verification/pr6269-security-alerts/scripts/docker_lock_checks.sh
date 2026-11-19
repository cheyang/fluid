#!/usr/bin/env bash
# Integration layer: run the two Dockerfiles' exact pip install commands inside their
# exact base images, then exercise the bumped packages.
# Requires: docker (via sudo on this host). Usage: bash docker_lock_checks.sh [amd64|arm64|all]
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
MODE="${1:-all}"
DOCKER="sudo -n docker"

VINEYARD_IMG="python:3.10@sha256:33f72df2ad8c9f777bf0adb35b9d89c5d62935cee2af1f9c3224fb6f7da1dc6b"

vineyard_check() {
    local platform="$1"
    echo "=== vineyard lock install + sklearn flow [$platform] ==="
    $DOCKER run --rm --platform "$platform" \
        -v "$PWD/samples/vineyard/requirements.txt:/tmp/requirements.txt:ro" \
        "$VINEYARD_IMG" bash -c '
set -e
# exact Dockerfile command
pip3 install --no-cache-dir --only-binary=:all: --no-binary=lazy-import --require-hashes -r /tmp/requirements.txt >/tmp/pip.log 2>&1 || { tail -20 /tmp/pip.log; exit 1; }
python3 - <<PY
import sklearn, numpy, scipy, joblib, threadpoolctl
print("sklearn", sklearn.__version__, "| numpy", numpy.__version__, "| scipy", scipy.__version__, "| joblib", joblib.__version__)
assert sklearn.__version__ == "1.7.2"
# the sample pipeline flow: train_test_split -> LinearRegression -> joblib dump/load -> mean_squared_error
from sklearn.model_selection import train_test_split
from sklearn.linear_model import LinearRegression
from sklearn.metrics import mean_squared_error
import numpy as np
rng = np.random.RandomState(0)
X = rng.rand(200, 4); y = X @ np.array([1.0, -2.0, 0.5, 3.0]) + 0.1
Xtr, Xte, ytr, yte = train_test_split(X, y, test_size=0.2, random_state=42)
m = LinearRegression().fit(Xtr, ytr)
joblib.dump(m, "/tmp/model.pkl")
m2 = joblib.load("/tmp/model.pkl")
mse = mean_squared_error(yte, m2.predict(Xte))
print("flow ok, mse =", mse)
assert mse < 1e-20
import pip._internal.operations.check as _c  # noqa
PY
pip check
'
    echo "VINEYARD $platform: OK"
}

mooncake_check() {
    echo "=== mooncake image build (native amd64) ==="
    $DOCKER build -q -t fluid-verify-6269-mooncake test/gha-e2e/mooncake/image
    echo "=== mooncake: aiohttp imports on the built image ==="
    $DOCKER run --rm fluid-verify-6269-mooncake python3 -c \
        'import aiohttp, yarl; print("aiohttp", aiohttp.__version__, "| yarl", yarl.__version__); assert aiohttp.__version__ == "3.14.5" and yarl.__version__ == "1.25.1"'
    echo "MOONCAKE: OK"
}

case "$MODE" in
    amd64)   vineyard_check linux/amd64 ;;
    arm64)   vineyard_check linux/arm64 ;;
    mooncake) mooncake_check ;;
    all)     vineyard_check linux/amd64; vineyard_check linux/arm64; mooncake_check ;;
esac
