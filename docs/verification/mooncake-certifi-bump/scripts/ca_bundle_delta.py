#!/usr/bin/env python3
"""Premise evidence — what actually changes inside certifi between the base pin
(2026.7.22) and the PR pin (2026.10.19) of the mooncake e2e image.

Downloads both wheels straight from PyPI (via the JSON API URLs), compares the
cacert.pem contents root-by-root, and prints the delta. Used to validate the
PR's premise (a real, security-relevant CA-bundle update) and to assess blast
radius: the mooncake image's own scripts talk plain http://localhost, so the
only TLS consumers are transitive (requests/aiohttp inside mooncake), which the
e2e suite exercises against in-cluster plain-HTTP services.

Usage: python3 ca_bundle_delta.py [old_version] [new_version]
stdlib only.
"""
import base64
import hashlib
import json
import re
import sys
import tempfile
import urllib.request
import zipfile

OLD = sys.argv[1] if len(sys.argv) > 1 else "2026.7.22"
NEW = sys.argv[2] if len(sys.argv) > 2 else "2026.10.19"


def fetch_bundle(version):
    with urllib.request.urlopen("https://pypi.org/pypi/certifi/%s/json" % version, timeout=60) as r:
        d = json.load(r)
    wheel = next(u for u in d["urls"] if u["filename"].endswith(".whl"))
    with tempfile.TemporaryDirectory() as td:
        whl = td + "/wheel.whl"
        with urllib.request.urlopen(wheel["url"], timeout=120) as r, open(whl, "wb") as f:
            f.write(r.read())
        with zipfile.ZipFile(whl) as z:
            pem = z.read("certifi/cacert.pem").decode("utf-8", "replace")
    return re.findall(r"-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----", pem, re.S)


def fingerprint(cert_pem):
    der = base64.b64decode("".join(cert_pem.strip().splitlines()[1:-1]))
    return hashlib.sha256(der).hexdigest()


def names_in(der):
    # crude: ASCII runs inside the DER (CN/OU/O are UTF8Strings) — good enough
    # to identify which root changed, not a parser
    return [m.decode() for m in re.findall(rb"[ -~]{8,}", der)]


old, new = fetch_bundle(OLD), fetch_bundle(NEW)
old_fp = {fingerprint(c): c for c in old}
new_fp = {fingerprint(c): c for c in new}
removed = old_fp.keys() - new_fp.keys()
added = new_fp.keys() - old_fp.keys()
print("certifi %s: %d roots" % (OLD, len(old)))
print("certifi %s: %d roots" % (NEW, len(new)))
print("removed roots: %d" % len(removed))
for fp in sorted(removed):
    der = base64.b64decode("".join(old_fp[fp].strip().splitlines()[1:-1]))
    cands = [n for n in names_in(der) if any(k in n for k in ("Root", "CA", "Trust"))]
    print("  - %s  (%s)" % (fp[:16], cands[:2] or "unnamed"))
print("added roots:   %d" % len(added))
for fp in sorted(added):
    der = base64.b64decode("".join(new_fp[fp].strip().splitlines()[1:-1]))
    cands = [n for n in names_in(der) if any(k in n for k in ("Root", "CA", "Trust"))]
    print("  + %s  (%s)" % (fp[:16], cands[:2] or "unnamed"))
