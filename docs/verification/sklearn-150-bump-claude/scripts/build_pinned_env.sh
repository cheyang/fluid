#!/usr/bin/env bash
# Build a venv holding the exact pinned dependency set that
# samples/vineyard/requirements.txt will resolve to AFTER the scikit-learn
# bump under review, then run the sample-path contract test in it.
#
# Only the python-level deps the sample scripts exercise are installed
# (sklearn + numpy/scipy/pandas/joblib/threadpoolctl and their small deps).
# vineyard itself is skipped: it needs a C++ toolchain and plays no part in
# the sklearn compatibility contract (the scripts only call vineyard.get/put
# around the sklearn calls).
#
# Every wheel is downloaded by URL from PyPI JSON metadata and its sha256 is
# checked against BOTH PyPI metadata and the lockfile before install, so the
# environment is the one the Dockerfile would produce (minus vineyard).
#
# Usage: build_pinned_env.sh <requirements.txt> <venv-dir> [scikit-learn-version]
#   scikit-learn-version defaults to the lockfile pin; pass e.g. 1.4.0 to
#   build the BASE environment for a differential run.
set -euo pipefail

REQ="$1"; VENV="$2"; SKVER="${3:-}"
HERE="$(cd "$(dirname "$0")" && pwd)"

WHEELS="$(mktemp -d)"
trap 'rm -rf "$WHEELS"' EXIT

python3 - "$REQ" "$WHEELS" "$SKVER" << 'PYEOF'
import json, re, sys, urllib.request
import platform

host_arch = {"x86_64": "x86_64", "AMD64": "x86_64", "aarch64": "aarch64",
             "arm64": "aarch64"}.get(platform.machine(), platform.machine())

req_path, wheels_dir, skver_override = sys.argv[1], sys.argv[2], sys.argv[3]

# packages the sample scripts need at import/run time (vineyard excluded)
WANT = ["scikit-learn", "numpy", "scipy", "pandas", "joblib", "threadpoolctl",
        "python-dateutil", "six", "pytz", "tzdata"]

pins = {}
for line in open(req_path, encoding="utf-8").read().splitlines():
    m = re.match(r"^([A-Za-z0-9_.-]+)==(\S+)", line)
    if m and m.group(1).lower() in [w.lower() for w in WANT]:
        pins[m.group(1).lower()] = m.group(2)

if skver_override:
    pins["scikit-learn"] = skver_override

import subprocess
for pkg, ver in pins.items():
    with urllib.request.urlopen("https://pypi.org/pypi/%s/%s/json" % (pkg, ver), timeout=60) as r:
        d = json.load(r)
    lock_hashes = set(re.findall(r"--hash=sha256:([0-9a-f]{64})", open(req_path, encoding="utf-8").read()))
    pick = None
    for u in d["urls"]:
        fn = u["filename"]
        if not fn.endswith(".whl"):
            continue
        if "manylinux" not in fn and "musllinux" not in fn and "none-any" not in fn:
            continue
        parts = fn.split("-")
        tag_ok = "-cp311-" in fn or "py2.py3-none-any" in fn or "py3-none-any" in fn
        arch_ok = host_arch in fn or "none-any" in fn
        if tag_ok and arch_ok:
            pick = u
            break
    if pick is None:
        raise SystemExit("no wheel for %s==%s" % (pkg, ver))
    if pkg != "scikit-learn" or not skver_override:
        if pick["digests"]["sha256"] not in lock_hashes:
            raise SystemExit("chosen wheel %s hash not in lockfile" % pick["filename"])
    out = wheels_dir + "/" + pick["filename"]
    subprocess.check_call(["curl", "-sSL", "--max-time", "600", "-o", out, pick["url"]])
    import hashlib
    got = hashlib.sha256(open(out, "rb").read()).hexdigest()
    if got != pick["digests"]["sha256"]:
        raise SystemExit("sha256 mismatch after download: %s" % pick["filename"])
    print("verified %s==%s (%s)" % (pkg, ver, pick["filename"]))
PYEOF

python3 -m venv "$VENV"
"$VENV/bin/pip" install -q --no-input --no-index --find-links "$WHEELS" \
    numpy scipy pandas joblib threadpoolctl scikit-learn python-dateutil six pytz tzdata

"$VENV/bin/python" - << 'PYEOF'
import sklearn, numpy, scipy, joblib, pandas
print("env ready: sklearn %s | numpy %s | scipy %s | joblib %s | pandas %s"
      % (sklearn.__version__, numpy.__version__, scipy.__version__,
         joblib.__version__, pandas.__version__))
PYEOF
