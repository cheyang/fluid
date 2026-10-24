#!/usr/bin/env python3
"""C1 (contract): the certifi pin in the mooncake e2e requirements.txt must be
(a) exactly one entry, (b) version >= 2026.10.19 (first version patched for
GHSA-6q48-6c7w-9gw2), (c) hash-locked to the official PyPI artifacts, including
the py3-none-any wheel hash that `pip install --only-binary=:all:` will fetch,
and (d) acceptable to every other pinned distribution that constrains certifi.

Usage: check_certifi_pin.py [path-to-requirements.txt]
Exit 0 iff all checks pass. Network: pypi.org JSON API only.
"""
import json
import re
import sys
import urllib.request

REQS = "test/gha-e2e/mooncake/image/requirements.txt"
MIN_SAFE = (2026, 10, 19)  # GHSA-6q48-6c7w-9gw2 first_patched_version
PKG = "certifi"


def fail(msg):
    print("FAIL: %s" % msg)
    raise SystemExit(1)


def pypi_json(name, version=None):
    url = "https://pypi.org/pypi/%s%s/json" % (name, "/%s" % version if version else "")
    with urllib.request.urlopen(url, timeout=30) as r:
        return json.load(r)


def version_tuple(v):
    return tuple(int(p) for p in v.split("."))


def parse_requirements(path):
    text = open(path).read()
    # join backslash-continuations, drop comments
    logical = []
    buf = ""
    for line in text.splitlines():
        s = line.strip()
        if s.startswith("#"):
            continue
        if s.endswith("\\"):
            buf += s[:-1] + " "
            continue
        buf += s
        if buf.strip():
            entry = buf.split("#", 1)[0].strip()
            if entry:
                m = re.match(r"^([A-Za-z0-9_.\-]+)==([A-Za-z0-9.\-+]+)\s*(.*)$", entry)
                if not m:
                    fail("unparseable requirement line: %r" % entry)
                hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", entry)
                if not hashes:
                    fail("entry %s has no --hash (file is installed with --require-hashes)" % m.group(1))
                yield m.group(1).lower(), m.group(2), hashes
        buf = ""


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else REQS
    entries = list(parse_requirements(path))
    print("parsed %d pinned entries from %s" % (len(entries), path))

    cert_entries = [e for e in entries if e[0] == PKG]
    if len(cert_entries) != 1:
        fail("expected exactly 1 certifi entry, found %d" % len(cert_entries))
    name, version, hashes = cert_entries[0]
    print("certifi entry: version=%s hashes=%d" % (version, len(hashes)))

    if version_tuple(version) < MIN_SAFE:
        fail("certifi %s is in the vulnerable range of GHSA-6q48-6c7w-9gw2 "
             "(patched: >= 2026.10.19); bundle still trusts emSign ECC Root CA - G3" % version)
    print("PASS: version %s is >= first patched 2026.10.19 (GHSA-6q48-6c7w-9gw2)" % version)

    meta = pypi_json(PKG, version)
    official = {}
    for u in meta["urls"]:
        official[u["digests"]["sha256"]] = u["filename"]
    unknown = [h for h in hashes if h not in official]
    if unknown:
        fail("hashes not present on PyPI for certifi %s: %s" % (version, unknown))
    wheel_hashes = [h for h, f in official.items() if f.endswith("py3-none-any.whl")]
    if not any(h in wheel_hashes for h in hashes):
        fail("no py3-none-any wheel hash listed; pip --only-binary=:all: cannot accept this entry")
    print("PASS: all %d listed hashes match official PyPI artifacts; wheel hash present (%s)"
          % (len(hashes), [official[h] for h in hashes]))

    # every other pinned dist that declares a certifi constraint must accept the pin
    constrained = []
    for pkg, ver, _ in entries:
        if pkg == PKG:
            continue
        try:
            info = pypi_json(pkg, ver)
        except Exception as e:
            fail("could not fetch metadata for %s==%s: %s" % (pkg, ver, e))
        for dep in info["info"].get("requires_dist") or []:
            dm = re.match(r"^certifi\s*\(([^)]*)\)|^certifi\s*([<>=!~][^;]*)", dep.strip(), re.I)
            if dm:
                spec = (dm.group(1) or dm.group(2) or "").strip()
                constrained.append((pkg, spec))
                for clause in spec.split(","):
                    clause = clause.strip()
                    cm = re.match(r"^(>=|<=|==|!=|>|<)\s*([0-9.]+)", clause)
                    if not cm:
                        continue
                    op, cv = cm.group(1), version_tuple(cm.group(2))
                    pv = version_tuple(version)
                    ok = {">=": pv >= cv, "<=": pv <= cv, "==": pv == cv,
                          "!=": pv != cv, ">": pv > cv, "<": pv < cv}[op]
                    if not ok:
                        fail("certifi==%s violates constraint %r from %s==%s" % (version, spec, pkg, ver))
    for pkg, spec in constrained:
        print("PASS: certifi==%s satisfies %r required by %s" % (version, spec, pkg))
    print("C1 OK")


if __name__ == "__main__":
    main()
