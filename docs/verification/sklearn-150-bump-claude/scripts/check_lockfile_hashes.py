#!/usr/bin/env python3
"""Contract test (layer: lockfile/metadata): the scikit-learn pin in
samples/vineyard/requirements.txt is a genuine, complete, platform-covering
pin for the version it claims.

Checks, for one package pin in a hash-locked pip requirements file:
  1. INTEGRITY  - every --hash line for the package matches the sha256 of a
     real file published on PyPI for that exact version.
  2. COMPLETENESS - the file lists *every* sha256 published on PyPI for that
     version (what `uv pip compile --generate-hashes` / a correct dependabot
     rewrite produces). A partial hash set can make `pip install
     --require-hashes` fail on platforms whose wheel hash was dropped.
  3. COVERAGE - the wheel hashes needed by the sample Dockerfile are present:
     python:3.10 on linux/amd64 and linux/arm64 (manylinux x86_64 + aarch64).

PASS  = the lockfile entry is exactly the published artifact set.
FAIL  = any of the three checks fails (bad hash, partial set, missing platform).

Usage:
  check_lockfile_hashes.py <requirements.txt> [package=scikit-learn] \
      [python_tag=cp310] [--platforms x86_64,aarch64]

Needs network access to pypi.org (JSON API only).
"""
import json
import re
import sys
import urllib.request

PYPI = "https://pypi.org/pypi/{pkg}/{ver}/json"


def lockfile_entry(path, package):
    """Return (version, [sha256...]) for `package==<ver> ... --hash=...` block."""
    version = None
    hashes = []
    in_block = False
    for line in open(path, encoding="utf-8").read().splitlines():
        m = re.match(r"^([A-Za-z0-9_.-]+)==(\S+)", line)
        if m:
            in_block = m.group(1).lower() == package.lower()
            if in_block:
                version = m.group(2)
            continue
        if in_block:
            hashes.extend(re.findall(r"--hash=sha256:([0-9a-f]{64})", line))
    if version is None:
        raise SystemExit("package %s not found in %s" % (package, path))
    return version, hashes


def pypi_files(package, version):
    url = PYPI.format(pkg=package, ver=version)
    with urllib.request.urlopen(url, timeout=60) as r:
        d = json.load(r)
    return {u["digests"]["sha256"]: u["filename"] for u in d["urls"]}


def wheel_for(files_by_hash, python_tag, arch):
    """The (hash, filename) PyPI publishes for a LINUX (python, arch) or None.

    The sample Dockerfile builds on python:3.10 linux/amd64|arm64, so only
    manylinux/musllinux wheels count — a macosx or win wheel whose filename
    contains the arch substring must NOT satisfy coverage.
    """
    for h, fn in files_by_hash.items():
        if not fn.endswith(".whl"):
            continue
        parts = fn.split("-")
        tags = [p for p in parts if p.startswith(("cp3", "py2", "py3"))]
        is_linux = "manylinux" in fn or "musllinux" in fn or parts[-1] == "any.whl"
        tag_ok = any(python_tag in t for t in tags) or any(
            t in ("py2.py3", "py3") and parts[-1] == "any.whl" for t in tags
        )
        if tag_ok and is_linux and (arch in fn or parts[-1] == "any.whl"):
            return h, fn
    return None


def main():
    args = [a for a in sys.argv[1:]]
    if not args:
        raise SystemExit(__doc__)
    path = args[0]
    package = args[1] if len(args) > 1 else "scikit-learn"
    python_tag = args[2] if len(args) > 2 else "cp310"
    platforms = ["x86_64", "aarch64"]
    if "--platforms" in args:
        platforms = args[args.index("--platforms") + 1].split(",")

    version, file_hashes = lockfile_entry(path, package)
    file_hashes = list(dict.fromkeys(file_hashes))
    print("lockfile pin: %s==%s with %d hashes" % (package, version, len(file_hashes)))

    published = pypi_files(package, version)
    print("PyPI %s==%s publishes %d files" % (package, version, len(published)))

    failures = []

    # 1. INTEGRITY
    unknown = [h for h in file_hashes if h not in published]
    for h in unknown:
        failures.append("INTEGRITY: hash %s... is not any %s==%s file on PyPI" % (h[:12], package, version))
    if not unknown:
        print("INTEGRITY: all %d hashes match real PyPI artifacts" % len(file_hashes))

    # 2. COMPLETENESS
    missing = set(published) - set(file_hashes)
    for h in sorted(missing):
        failures.append("COMPLETENESS: PyPI file %s (sha256 %s...) not listed in lockfile" % (published[h], h[:12]))
    if not missing:
        print("COMPLETENESS: lockfile lists every published file (full regeneration semantics)")

    # 3. COVERAGE
    for arch in platforms:
        got = wheel_for(published, python_tag, arch)
        if got is None:
            failures.append("COVERAGE: PyPI has no %s wheel for %s — cannot cover %s" % (python_tag, arch, arch))
            continue
        h, fn = got
        status = "present" if h in file_hashes else "MISSING from lockfile"
        print("COVERAGE: %s wheel %s -> %s" % (arch, fn, status))
        if h not in file_hashes:
            failures.append("COVERAGE: %s (%s) hash not in lockfile" % (fn, arch))

    if failures:
        print("LOCKFILE_HASH_CONTRACT: FAIL")
        for f in failures:
            print("  " + f)
        return 1
    print("LOCKFILE_HASH_CONTRACT: PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
