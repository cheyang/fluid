#!/usr/bin/env python3
"""P0 helper (premise; decisive against the BASE pin): download the certifi wheel
pinned by the given requirements.txt and report whether the bundle still trusts
`emSign ECC Root CA - G3`, the root named by GHSA-6q48-6c7w-9gw2 (eight eMudhra
intermediate CAs chaining to it have compromised private keys).

On the base pin (2026.7.22) the root is PRESENT  -> premise confirmed.
On the fixed pin (2026.10.19+) the root is ABSENT -> fix verified.

Usage: check_bundle_delta.py [path-to-requirements.txt]
Exit 0 iff the pinned bundle does NOT contain emSign ECC Root CA - G3.
Network: pypi.org (wheel download). Requires: openssl on PATH.
"""
import io
import re
import subprocess
import sys
import urllib.request
import zipfile

REQS = "test/gha-e2e/mooncake/image/requirements.txt"
TARGET_SUBJECT = "CN = emSign ECC Root CA - G3"


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else REQS
    text = open(path).read()
    m = re.search(r"certifi==([0-9.]+)", text)
    if not m:
        print("FAIL: no certifi pin in %s" % path)
        return 1
    version = m.group(1)
    print("pinned certifi: %s" % version)

    # verify the download against the hash recorded in the requirements entry
    entry = re.search(r"certifi==%s[^\n]*(?:\\\n[^\n]*)*" % re.escape(version), text).group(0)
    hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", entry)
    meta_url = "https://pypi.org/pypi/certifi/%s/json" % version
    import json
    with urllib.request.urlopen(meta_url, timeout=30) as r:
        meta = json.load(r)
    wheel = next(u for u in meta["urls"] if u["filename"].endswith("py3-none-any.whl"))
    data = urllib.request.urlopen(wheel["url"], timeout=60).read()
    import hashlib
    digest = hashlib.sha256(data).hexdigest()
    if hashes and digest not in hashes:
        print("FAIL: downloaded wheel sha256 %s not in pinned hashes %s" % (digest, hashes))
        return 1
    print("wheel %s sha256=%s matches pin" % (wheel["filename"], digest))

    pem = zipfile.ZipFile(io.BytesIO(data)).read("certifi/cacert.pem").decode()
    certs = re.findall(r"-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----", pem, re.S)
    print("bundle contains %d certificates" % len(certs))
    hits = []
    for i, c in enumerate(certs):
        p = subprocess.run(["openssl", "x509", "-noout", "-subject"],
                           input=c.encode(), capture_output=True)
        subj = p.stdout.decode().strip()
        if "emSign" in subj or "eMudhra" in subj:
            hits.append(subj)
    for h in hits:
        print("  emSign-family root present: %s" % h)
    if any(TARGET_SUBJECT in h for h in hits):
        print("P0: %s IS present -> GHSA-6q48-6c7w-9gw2 exposure (premise holds on this pin)" % TARGET_SUBJECT)
        return 1
    print("P0: %s is absent from the pinned bundle" % TARGET_SUBJECT)
    return 0


if __name__ == "__main__":
    sys.exit(main())
