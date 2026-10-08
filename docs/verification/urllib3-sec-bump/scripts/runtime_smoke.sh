#!/usr/bin/env bash
# T4 — runtime smoke test (integration layer).
#
# Claim under test: requests 2.34.2 (the only pinned consumer of urllib3 in
# the mooncake image) works at runtime against urllib3 2.8.0 for the usage
# pattern the image actually has — plain HTTP to a local endpoint (the
# mooncake metadata server and metrics endpoints are plain HTTP; see
# custom-entrypoint.sh / reportSummary.sh, and the entrypoint's own comment
# that no TLS variant exists).
#
# Installs the pinned requests+urllib3 pair into a throwaway --target dir
# using the system pip (the host has Python 3.11; both packages are
# pure-Python and support >=3.10, so the pairing behavior under test is the
# same as in the image's 3.12), serves a local page with http.server, and
# does a real requests.get round trip plus direct urllib3 reads.
#
# Host quirk (documented in results/runtime_smoke.log of the first run):
# `python -m venv`'s pip vendors a distro-patched urllib3 1.26.17 whose
# cachecontrol read path crashes on amt=None, so the venv is avoided;
# `pip install --no-cache-dir --target` with the system pip bypasses it
# (same mechanism the T2 download uses). This is host noise, not PR signal —
# upstream urllib3 2.8.0's HTTPResponse.read() guards the buffer comparison
# behind `amt is not None`, which this test asserts directly.
#
# PASS (exit 0) = round trip returns the expected body with the pinned
# versions loaded. FAIL (exit 1) = import error / HTTP error / wrong version.
#
# Polarity: contract. Written 2026-10-08 by Reviewer A (Claude).
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
LOG="${VERIFY_LOG_DIR:-$REPO_ROOT/docs/verification/urllib3-sec-bump/results}/runtime_smoke.log"
mkdir -p "$(dirname "$LOG")"

WORK="$(mktemp -d)"
SITE="$WORK/site"
SRV_PORT=18743
SRV_DIR="$WORK/srv"
SRV_PID=""
mkdir -p "$SITE" "$SRV_DIR"
echo 'fluid-urllib3-smoke-payload' > "$SRV_DIR/index.html"

cleanup() {
    [ -n "$SRV_PID" ] && kill "$SRV_PID" 2>/dev/null
    rm -rf "$WORK"
}
trap cleanup EXIT

{
    echo "[T4] installing the pinned pair into $SITE (system pip, --no-cache-dir)"
    pip3 install --no-cache-dir --quiet --disable-pip-version-check --target "$SITE" \
        'urllib3==2.8.0' 'requests==2.34.2' \
        || { echo "[T4] RESULT: FAIL — install of pinned pair failed"; exit 1; }

    (cd "$SRV_DIR" && exec python3 -m http.server "$SRV_PORT" --bind 127.0.0.1) >/dev/null 2>&1 &
    SRV_PID=$!

    SITE="$SITE" SRV_PORT="$SRV_PORT" python3 -P -s - <<'PYEOF'
import os
import sys

sys.path.insert(0, os.environ["SITE"])  # the throwaway target dir, ahead of stdlib user paths

import requests
import urllib3

assert urllib3.__version__ == "2.8.0", f"urllib3 {urllib3.__version__}"
assert requests.__version__ == "2.34.2", f"requests {requests.__version__}"
print(f"[T4] loaded urllib3 {urllib3.__version__} + requests {requests.__version__}")

base = f"http://127.0.0.1:{os.environ['SRV_PORT']}/"

r = requests.get(base, timeout=10)
assert r.status_code == 200, r.status_code
assert r.text.strip() == "fluid-urllib3-smoke-payload", r.text
print("[T4] requests.get round trip OK (status 200, body matches)")

# Direct urllib3 use: streamed read with an explicit amt, then a full
# read() with amt=None — the code path (HTTPResponse.read's decoded-buffer
# handling) that the distro's old vendored 1.26.17 backport gets wrong, and
# one of the paths the 2.8.0 security fixes touched (read_chunked).
with urllib3.PoolManager() as pm:
    r2 = pm.request("GET", base, timeout=10, preload_content=False)
    chunks = list(r2.stream(64))
    r2.release_conn()
    assert r2.status == 200
    assert b"".join(chunks).strip() == b"fluid-urllib3-smoke-payload"
    print("[T4] urllib3 streamed read (amt=64) OK")

    r3 = pm.request("GET", base, timeout=10, preload_content=False)
    body = r3.read()  # amt=None — must not raise
    r3.release_conn()
    assert r3.status == 200
    assert body.strip() == b"fluid-urllib3-smoke-payload"
    print("[T4] urllib3 full read (amt=None) OK — no TypeError from the decoded-buffer path")

print("[T4] RESULT: PASS")
PYEOF
    rc=$?
    exit $rc
} >> "$LOG" 2>&1

rc=$?
tail -8 "$LOG"
exit $rc
