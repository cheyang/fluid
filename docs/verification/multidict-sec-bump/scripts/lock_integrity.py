#!/usr/bin/env python3
"""L0 — static lock integrity + hash-scope check for the mooncake e2e image lock.

Usage: lock_integrity.py <requirements.txt>

Part A (integrity, decides exit code): every --hash line in the file must map
to a genuine PyPI artifact of the pinned version. Any miss = tamper signal,
exit 1. This proves the PR's hash list contains no fabricated digests.

Part B (F1 evidence, does not decide exit code): reports, per entry, how many
hashes are pinned and how many of them map to artifacts outside the documented
target platform (cp312 / linux x86_64 wheels, or universal py3-none-any wheels).
The file's own header documents single-platform pinning; multidict is expected
to be the outlier under the PR (all 171 release artifacts).
"""
import json
import re
import subprocess  # noqa: F401  (unused, kept for parity with ad-hoc run)
import sys
import urllib.request


def pypi_urls(name, ver):
    d = json.load(urllib.request.urlopen(f"https://pypi.org/pypi/{name}/{ver}/json"))
    return {f["digests"]["sha256"]: f["filename"] for f in d["urls"]}


def entry_blocks(txt):
    return re.findall(
        r"^([A-Za-z0-9_.-]+)==(\S+?) \\\n((?:[ \t]+--hash=sha256:[0-9a-f]{64}[ \t]*\\?\n)+)",
        txt,
        re.M,
    )


def on_target(filename):
    if "py3-none-any" in filename:
        return True  # universal wheel: fine on any platform
    return (
        filename.endswith(".whl")
        and "linux" in filename
        and "x86_64" in filename
        and ("cp312" in filename or "py3-none" in filename or "abi3" in filename)
    )


def main(path):
    txt = open(path).read()
    entries = entry_blocks(txt)
    if len(entries) != 17:
        print(f"NOTE: expected 17 pinned entries, found {len(entries)}")
    failures = []
    print("== Part A: every pinned hash is a genuine PyPI digest ==")
    for name, ver, block in entries:
        hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", block)
        try:
            by_hash = pypi_urls(name, ver)
        except Exception as e:  # network / index error
            failures.append(f"{name}=={ver}: PyPI lookup failed: {e}")
            continue
        for h in hashes:
            fn = by_hash.get(h)
            if fn is None:
                failures.append(f"{name}=={ver}: hash {h[:12]}... is NOT a PyPI digest")
        files = [by_hash.get(h) for h in hashes if by_hash.get(h)]
        show = files[:1] + (["..."] if len(files) > 1 else [])
        print(f"  {name}=={ver}: {len(hashes)} hash(es) -> {show}")

    print()
    print("== Part B: hash scope vs documented platform (cp312 / linux x86_64) ==")
    for name, ver, block in entries:
        hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", block)
        if len(hashes) <= 2:
            continue
        try:
            by_hash = pypi_urls(name, ver)
        except Exception:
            continue
        off_target = sum(1 for h in hashes if h in by_hash and not on_target(by_hash[h]))
        print(f"  {name}=={ver}: {len(hashes)} hashes, {off_target} map to off-platform artifacts")

    print()
    if failures:
        print("INTEGRITY FAILURES:")
        for f in failures:
            print(" ", f)
        return 1
    print("INTEGRITY OK: all pinned hashes are genuine PyPI digests of the pinned versions")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
