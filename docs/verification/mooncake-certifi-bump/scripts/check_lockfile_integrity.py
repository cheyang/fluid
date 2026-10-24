#!/usr/bin/env python3
"""L1 (unit/deterministic) layer — lockfile integrity check for the mooncake e2e image.

Contract: every entry in the hash-locked requirements.txt pins a version that
really exists on PyPI, every pinned sha256 matches a real artifact of exactly
that version, a binary wheel compatible with the image's interpreter
(python:3.12-slim, cp312 / py3-none-any, manylinux x86_64) exists for that
version, and requires_python accepts 3.12. The image's Dockerfile installs with
`pip install --only-binary=:all: --require-hashes`, so any violation of this
contract turns the e2e image build (and therefore every kind-e2e PR run) red.

Polarity: contract test. It is green only while the lockfile is consistent; a
bump with a wrong/missing hash or a version that has no installable wheel for
the image platform fails it.

Usage:  python3 check_lockfile_integrity.py [requirements.txt]
        (default path: test/gha-e2e/mooncake/image/requirements.txt)

Output: one "<entry>\t<pass|fail>" line per package (for re-verify.sh's shell
        parser) plus a final "L1-LOCKFILE-INTEGRITY\tpass|fail" rollup.
        Human-readable details go to stderr. Exit 0 iff everything passes.

stdlib only; PyPI JSON API responses are read for digests, never executed.
"""
import json
import sys
import urllib.request

DEFAULT_REQ = "test/gha-e2e/mooncake/image/requirements.txt"
PYPI = "https://pypi.org/pypi/{name}/{version}/json"
TARGET_PY = (3, 12)

# Wheel tags installable inside python:3.12.13-slim on linux/amd64:
# pure wheels plus cp312 C-ABI wheels for x86_64 manylinux (any glibc level
# >= 2.17 works on the slim base, which is glibc 2.36 / Debian bookworm).
def _wheel_ok(filename):
    if not filename.endswith(".whl"):
        return False
    tags = filename[:-4].split("-")[2:]  # e.g. ["py3", "none", "any"] / ["cp312", "cp312", "manylinux..."]
    if len(tags) != 3:
        return False
    impl, abi, plat = tags
    impl_ok = impl in ("py3", "py2.py3", "cp312")
    abi_ok = abi in ("none", "abi3", "cp312")
    plat_ok = plat == "any" or ("x86_64" in plat and "linux" in plat)
    return impl_ok and abi_ok and plat_ok


def _parse(path):
    """Yield (name, version, [sha256, ...]) from a pip-compile style requirements.txt."""
    entries = []
    name = version = None
    hashes = []
    for line in open(path, encoding="utf-8"):
        line = line.rstrip("\n")
        if not line or line.startswith("#"):
            continue
        stripped = line.strip()
        if "==" in line and not stripped.startswith("--hash"):
            if name:
                entries.append((name, version, hashes))
            name, _, rest = line.partition("==")
            version, _, _ = rest.partition(" ")
            version = version.strip()
            hashes = []
        elif stripped.startswith("--hash=sha256:"):
            # drop the pip-compile line-continuation backslash, if present
            hashes.append(stripped[len("--hash=sha256:"):].rstrip(" \\").strip())
    if name:
        entries.append((name, version, hashes))
    return entries


def _fetch(name, version):
    url = PYPI.format(name=name, version=version)
    for attempt in range(3):
        try:
            with urllib.request.urlopen(url, timeout=60) as r:
                return json.load(r)
        except Exception as e:  # noqa: BLE001 - retried below, then reported
            if attempt == 2:
                print("  pypi fetch failed for %s==%s: %s" % (name, version, e), file=sys.stderr)
                return None


def check(path):
    failures = 0
    for name, version, hashes in _parse(path):
        ok = True
        why = []
        data = _fetch(name, version)
        if data is None:
            ok, why = False, ["version not resolvable on PyPI"]
        else:
            files = data.get("urls", [])
            by_hash = {u["digests"]["sha256"]: u for u in files}
            for h in hashes:
                if h not in by_hash:
                    ok = False
                    why.append("hash %s.. matches no artifact of this release" % h[:12])
            if not hashes:
                ok = False
                why.append("no hash pinned")
            if not any(_wheel_ok(u["filename"]) for u in files):
                ok = False
                why.append("no wheel for py3.12/manylinux x86_64")
            rp = data.get("info", {}).get("requires_python") or ""
            if rp:
                # only flag a hard incompatibility with the image's python
                lower = rp.replace(" ", "")
                if lower.startswith(">=") and lower[2:].split(",")[0].split(".")[0].isdigit():
                    minv = tuple(int(x) for x in lower[2:].split(",")[0].split(".")[:2])
                    if minv > TARGET_PY:
                        ok = False
                        why.append("requires_python %s excludes 3.12" % rp)
        print("%s==%s\t%s" % (name, version, "pass" if ok else "fail"))
        for w in why:
            print("  %s==%s: %s" % (name, version, w), file=sys.stderr)
        if not ok:
            failures += 1
    total = len(_parse(path))
    verdict = "fail" if failures else "pass"
    print("L1-LOCKFILE-INTEGRITY\t%s" % verdict)
    print("checked %d entries, %d failures (%s)" % (total, failures, path), file=sys.stderr)
    return 0 if not failures else 1


if __name__ == "__main__":
    sys.exit(check(sys.argv[1] if len(sys.argv) > 1 else DEFAULT_REQ))
