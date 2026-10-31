#!/usr/bin/env python3
"""Integration layer, premise (P0): GHSA-j586-36cw-2gc2 / CVE-2026-114613.

Installs msgpack 1.2.2 (base pin) and the version pinned in the checked-out
requirements.txt (PR head pin) into isolated target dirs, runs leak_worker.py
against each, and emits go-test-style JSON lines into $TESTJSON_OUT:

  TestPremiseLeakReproducesOn122     pass iff 1.2.2 leaks ~4MiB per failed parse
  TestPinnedMsgpackDoesNotLeak       pass iff the pinned version does not leak
  TestStrictMapKeyGuardIntactOnPinned  pass iff ValueError still raised on pinned
  TestMsgpackSmokeRoundTripOnPinned  pass iff basic round-trips work on pinned

Prereqs: python 3.10-3.13 with network access to PyPI (wheels for both msgpack
versions must exist for the host interpreter; on 3.14+ msgpack 1.2.2 builds
from source and needs a C compiler).
"""
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
TOPIC_DIR = os.path.dirname(HERE)
ROOT = os.popen("git rev-parse --show-toplevel 2>/dev/null").read().strip() or "."
REQ = os.environ.get("REQUIREMENTS_FILE") or os.path.join(
    ROOT, "test/gha-e2e/mooncake/image/requirements.txt")
OUT = os.environ.get("TESTJSON_OUT", os.path.join(TOPIC_DIR, "results", "integration.jsonl"))
LEAK_RESULTS = os.path.join(TOPIC_DIR, "results")
BASE_VERSION = "1.2.2"
ITERS = os.environ.get("LEAK_ITERS", "25")
PIP_INDEX = os.environ.get("PIP_INDEX_URL", "https://pypi.org/simple")

results = []
def record(name, ok, detail):
    results.append({"Test": name, "Action": "pass" if ok else "fail"})
    print(f"[{'PASS' if ok else 'FAIL'}] {name}: {detail}", file=sys.stderr)

def pinned_msgpack():
    with open(REQ) as f:
        m = re.search(r"(?m)^msgpack==([^\s\\]+)", f.read())
    return m.group(1) if m else None

def install(version, dest):
    cmd = [sys.executable, "-m", "pip", "install", "-q", "--no-cache-dir",
           "--index-url", PIP_INDEX, "--target", dest, f"msgpack=={version}"]
    p = subprocess.run(cmd, capture_output=True, text=True, timeout=600)
    if p.returncode != 0:
        print(p.stdout[-2000:], file=sys.stderr)
        print(p.stderr[-2000:], file=sys.stderr)
        raise RuntimeError(f"pip install msgpack=={version} failed")

def run_worker(target_dir):
    env = dict(os.environ, PYTHONPATH=target_dir)
    p = subprocess.run([sys.executable, os.path.join(HERE, "leak_worker.py"), ITERS],
                       capture_output=True, text=True, env=env, timeout=600)
    if p.returncode != 0:
        raise RuntimeError(f"worker failed: {p.stderr[-2000:]}")
    return json.loads(p.stdout.strip().splitlines()[-1])

def main():
    pinned = pinned_msgpack()
    if not pinned:
        for t in ("TestPremiseLeakReproducesOn122", "TestPinnedMsgpackDoesNotLeak",
                  "TestStrictMapKeyGuardIntactOnPinned", "TestMsgpackSmokeRoundTripOnPinned"):
            record(t, False, "no msgpack pin found in requirements.txt")
        return emit()
    print(f"[info] requirements={REQ} pinned msgpack={pinned}", file=sys.stderr)
    tmp = tempfile.mkdtemp(prefix="msgpack-leak-check-")
    try:
        out = {}
        for ver in dict.fromkeys([BASE_VERSION, pinned]):
            dest = os.path.join(tmp, f"v{ver}")
            install(ver, dest)
            out[ver] = run_worker(dest)
            with open(os.path.join(LEAK_RESULTS, f"leak-{ver}.json"), "w") as f:
                json.dump(out[ver], f, indent=1)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

    base = out[BASE_VERSION]
    per = base["delta_kb"] / max(base["iters"], 1)
    record("TestPremiseLeakReproducesOn122",
           base["c_ext"] and base["delta_kb"] > base["iters"] * 1024,
           f"msgpack {BASE_VERSION}: {base['iters']} failed parses grew RSS by "
           f"{base['delta_kb']} KiB ({per:.0f} KiB/parse; advisory: ~4096 KiB/parse)")

    head = out[pinned]
    record("TestPinnedMsgpackDoesNotLeak",
           head["c_ext"] and head["delta_kb"] <= head["iters"] * 128,
           f"msgpack {pinned}: {head['iters']} failed parses grew RSS by {head['delta_kb']} KiB")
    record("TestStrictMapKeyGuardIntactOnPinned", head["guard"] == "value-error",
           f"msgpack {pinned}: invalid-key unpack outcome = {head['guard']} (want value-error)")
    record("TestMsgpackSmokeRoundTripOnPinned", head["roundtrip_ok"],
           f"msgpack {pinned}: basic pack/unpack round-trips")
    return emit()

def emit():
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        for r in results:
            f.write(json.dumps(r, separators=(",", ":")) + "\n")
    return 0 if all(r["Action"] == "pass" for r in results) else 1

if __name__ == "__main__":
    sys.exit(main())
