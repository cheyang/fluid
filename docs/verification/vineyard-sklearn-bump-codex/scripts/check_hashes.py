#!/usr/bin/env python3
"""C1: the scikit-learn hash block in samples/vineyard/requirements.txt must
exactly match the set of files PyPI publishes for the pinned version, and must
cover the cp310 manylinux x86_64 + aarch64 wheels the Dockerfile builds need.

Usage: check_hashes.py [requirements.txt]   (default: repo-relative path)
Exit 0 on exact match, 1 otherwise. Pure stdlib.
"""
import json
import re
import sys
import urllib.request

REQ = sys.argv[1] if len(sys.argv) > 1 else "samples/vineyard/requirements.txt"
PKG = "scikit-learn"


def parse_block(path, pkg):
    """Return (version, set-of-sha256) for the `pkg==version` entry."""
    with open(path) as fh:
        lines = fh.read().splitlines()
    logical, cur = [], ""
    for ln in lines:
        cur = (cur + " " + ln.strip()) if cur else ln.strip()
        if cur.endswith("\\"):
            cur = cur[:-1].rstrip()
        else:
            logical.append(cur)
            cur = ""
    for ln in logical:
        m = re.match(r"^%s==(\S+)\b" % re.escape(pkg), ln)
        if m:
            return m.group(1), set(re.findall(r"--hash=sha256:([0-9a-f]{64})", ln))
    return None, set()


def pypi_digests(pkg, version):
    url = "https://pypi.org/pypi/%s/%s/json" % (pkg, version)
    with urllib.request.urlopen(url, timeout=60) as resp:
        data = json.load(resp)
    return {u["digests"]["sha256"]: u["filename"] for u in data["urls"]}


version, hashes = parse_block(REQ, PKG)
if not version:
    print("FAIL: no %s pin found in %s" % (PKG, REQ))
    sys.exit(1)
print("pinned: %s==%s  hashes-in-file: %d" % (PKG, version, len(hashes)))

pypi = pypi_digests(PKG, version)
print("pypi files for %s: %d" % (version, len(pypi)))

ok = True
missing = set(pypi) - hashes
extra = hashes - set(pypi)
if missing:
    ok = False
    print("MISSING hashes (on PyPI, not in file):")
    for h in sorted(missing):
        print("  %s  %s" % (h, pypi[h]))
if extra:
    ok = False
    print("EXTRA hashes (in file, not on PyPI):")
    for h in sorted(extra):
        print("  %s" % h)

need = [
    ("cp310", "manylinux", "x86_64"),
    ("cp310", "manylinux", "aarch64"),
]
for abi, ml, arch in need:
    hit = [f for h, f in pypi.items()
           if h in hashes and abi in f and ml in f and arch in f]
    if hit:
        print("covered: %s %s wheel -> %s" % (abi, arch, hit[0]))
    else:
        ok = False
        print("NOT COVERED: %s %s %s wheel hash absent from file" % (abi, ml, arch))

if ok:
    print("PASS: file hash set == PyPI digest set for %s==%s; required wheels covered"
          % (PKG, version))
    sys.exit(0)
sys.exit(1)
