#!/usr/bin/env python3
"""Verify every hash in the PR's two lock files against PyPI.

For each pinned package in a lock file, fetch https://pypi.org/pypi/<pkg>/<version>/json
and check that every --hash in the lock is the sha256 of a real release artifact.

For the mooncake lock (single-wheel-per-package, cp312/manylinux x86_64 per its header),
additionally assert the locked hash IS that platform's wheel.
"""
import json
import re
import sys
import urllib.request


def parse_lock(path):
    entries = []  # (name, version, [hashes])
    name = None
    for line in open(path):
        m = re.match(r"^([A-Za-z0-9_.\-]+)==([A-Za-z0-9_.!\-]+) \\$", line)
        if m:
            name, ver = m.group(1), m.group(2)
            entries.append([name, ver, []])
            continue
        h = re.search(r"--hash=sha256:([0-9a-f]{64})", line)
        if h and name:
            entries[-1][2].append(h.group(1))
    return entries


def pypi_hashes(name, ver):
    with urllib.request.urlopen("https://pypi.org/pypi/%s/%s/json" % (name, ver)) as r:
        d = json.load(r)
    return {u["digests"]["sha256"]: u["filename"] for u in d["urls"]}


def main():
    rc = 0
    for lock, wheel_plat in (
        ("test/gha-e2e/mooncake/image/requirements.txt", "cp312-cp312-manylinux2014_x86_64"),
        ("samples/vineyard/requirements.txt", None),
    ):
        print("== %s ==" % lock)
        for name, ver, hashes in parse_lock(lock):
            try:
                artifacts = pypi_hashes(name, ver)
            except Exception as e:
                print("  FAIL %s==%s: PyPI lookup failed: %s" % (name, ver, e))
                rc = 1
                continue
            unknown = [h for h in hashes if h not in artifacts]
            if unknown:
                print("  FAIL %s==%s: %d hashes not on PyPI: %s" % (name, ver, len(unknown), unknown))
                rc = 1
                continue
            note = ""
            if wheel_plat:
                # locked artifacts must include a wheel pip can install on the image's
                # platform: a pure-python wheel, or a cp312 wheel tagged for linux x86_64.
                locked_files = [fn for h, fn in artifacts.items() if h in hashes]
                def installable(fn):
                    if not fn.endswith(".whl"):
                        return False
                    if "-py3-none-any.whl" in fn or "-py2.py3-none-any.whl" in fn:
                        return True
                    if "cp312" not in fn and "abi3" not in fn and "py3" not in fn:
                        return False
                    return "x86_64" in fn and "linux" in fn
                if not any(installable(fn) for fn in locked_files):
                    print("  FAIL %s==%s: no installable cp312/linux-x86_64 artifact among locked hashes: %s" % (name, ver, locked_files))
                    rc = 1
                    continue
                note = " [installable on cp312/linux-x86_64]"
            print("  ok   %s==%s (%d hashes)%s" % (name, ver, len(hashes), note))
    return rc


if __name__ == "__main__":
    sys.exit(main())
