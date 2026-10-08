#!/usr/bin/env python3
"""L1 deterministic check for PR #6201 (urllib3 2.7.0 -> 2.8.0 in the mooncake e2e image lock file).

Contract polarity: asserts the *intended correct* state of the PR.
Fails on the base ref (pin is 2.7.0), passes on the PR head (pin is 2.8.0 with
hashes that match PyPI's published digests and metadata compatible with the image).

Checks:
  1. requirements.txt pins urllib3==2.8.0.
  2. Every --hash on the urllib3 entry equals a sha256 digest of a real
     (non-yanked) PyPI artifact of urllib3 2.8.0.
  3. urllib3 2.8.0 requires_python admits the image interpreter (python:3.12.13-slim).
  4. The reverse dependencies that pull urllib3 in (requests 2.34.2 via
     mooncake-transfer-engine-non-cuda 0.3.12.post1) declare constraints that
     admit urllib3 2.8.0, so pip's resolver cannot conflict.
Usage: l1_lockfile_static_check.py <requirements.txt>
"""
import json
import re
import sys
import urllib.request

EXPECTED_URLLIB3 = "2.8.0"
IMAGE_PYTHON = (3, 12)


def pypi(version):
    with urllib.request.urlopen("https://pypi.org/pypi/urllib3/%s/json" % version, timeout=30) as r:
        return json.load(r)


def parse_pin(path, package):
    """Return (version, [hashes]) for `package==X \\ --hash=...` entries."""
    text = open(path).read()
    # join continuation lines
    logical = re.sub(r"\\\n\s*", " ", text)
    for line in logical.splitlines():
        line = line.strip()
        if line.startswith(package + "=="):
            version = line.split("==")[1].split()[0]
            hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", line)
            return version, hashes
    return None, []


def main():
    path = sys.argv[1]
    failures = []

    version, hashes = parse_pin(path, "urllib3")
    if version != EXPECTED_URLLIB3:
        failures.append("pin is urllib3==%s, expected %s" % (version, EXPECTED_URLLIB3))

    meta = pypi(EXPECTED_URLLIB3)
    pypi_digests = {u["digests"]["sha256"]: u["filename"] for u in meta["urls"]}
    yanked = {u["digests"]["sha256"]: u["yanked"] for u in meta["urls"]}
    if version == EXPECTED_URLLIB3:
        if not hashes:
            failures.append("urllib3 entry has no --hash pins")
        for h in hashes:
            if h not in pypi_digests:
                failures.append("hash %s matches NO PyPI artifact of urllib3 %s" % (h, EXPECTED_URLLIB3))
            elif yanked[h]:
                failures.append("hash %s belongs to YANKED artifact %s" % (h, pypi_digests[h]))
        wheel_hashes = [h for h in hashes if pypi_digests.get(h, "").endswith(".whl")]
        if not wheel_hashes:
            failures.append("no wheel hash pinned; Dockerfile installs with --only-binary=:all:")

    rp = meta["info"]["requires_python"]
    # minimal specifier eval: all our pins here are simple >=X.Y
    m = re.search(r">=\s*(\d+)\.(\d+)", rp or "")
    if m and IMAGE_PYTHON < (int(m.group(1)), int(m.group(2))):
        failures.append("requires_python %s excludes image python %s" % (rp, ".".join(map(str, IMAGE_PYTHON))))

    # reverse-dependency constraints
    for dep_name, dep_ver in (("requests", "2.34.2"), ("mooncake-transfer-engine-non-cuda", "0.3.12.post1")):
        with urllib.request.urlopen("https://pypi.org/pypi/%s/%s/json" % (dep_name, dep_ver), timeout=30) as r:
            dmeta = json.load(r)
        constraints = [c for c in (dmeta["info"]["requires_dist"] or [])
                       if c.split(";")[0].strip().lower().startswith("urllib3")]
        for c in constraints:
            c = c.split(";")[0]
            # every pin here is of the form urllib3<3,>=1.26 ; evaluate the two bounds
            ok = True
            for op, bound in re.findall(r"(>=|<=|<|>|==)\s*([\d.]+)", c):
                def t(v): return tuple(int(x) for x in v.split("."))
                ev, eb = t(EXPECTED_URLLIB3), t(bound)
                n = max(len(ev), len(eb)); ev += (0,) * (n - len(ev)); eb += (0,) * (n - len(eb))
                ok &= {">=": ev >= eb, "<=": ev <= eb, "<": ev < eb, ">": ev > eb, "==": ev == eb}[op]
            if not ok:
                failures.append("%s %s constraint %r rejects urllib3 %s" % (dep_name, dep_ver, c, EXPECTED_URLLIB3))

    if failures:
        print("L1 FAIL:")
        for f in failures:
            print("  -", f)
        sys.exit(1)
    print("L1 PASS: pin=urllib3==%s, %d hash(es) all match PyPI digests, "
          "requires_python=%r admits python %s, reverse-dep constraints admit %s"
          % (version, len(hashes), rp, ".".join(map(str, IMAGE_PYTHON)), EXPECTED_URLLIB3))


if __name__ == "__main__":
    main()
