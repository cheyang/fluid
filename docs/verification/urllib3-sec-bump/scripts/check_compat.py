#!/usr/bin/env python3 -I
# T3 — version/dependency compatibility (unit layer) for the urllib3 bump.
#
# Claims under test (all would break the image build or the runtime if false):
#   a) requests==2.34.2 (also pinned in the lockfile, unchanged by this PR)
#      declares urllib3<3,>=1.26 — 2.8.0 must satisfy it, else pip resolution
#      fails at image build time.
#   b) urllib3==2.8.0's Requires-Python (>=3.10) admits the image base's
#      Python 3.12.13, else pip refuses to install it in the image.
#   c) urllib3==2.8.0 declares no mandatory runtime dependencies (only extras:
#      brotli/h2/socks/zstd), so the hash-locked file remains complete under
#      --require-hashes after the bump (pip would otherwise fail with
#      "In --require-hashes mode, all requirements must have their versions
#      pinned").
#
# PASS = the bump is compatible with the rest of the pin set and the image.
# FAIL (exit 1) = at least one claim is violated.
#
# Polarity: contract. Written 2026-10-08 by Reviewer A (Claude).
import json
import re
import sys
import urllib.request

IMAGE_PYTHON = (3, 12, 13)  # python:3.12.13-slim base of the mooncake image


def pypi(name, version):
    with urllib.request.urlopen(f"https://pypi.org/pypi/{name}/{version}/json", timeout=30) as r:
        return json.load(r)


def vt(s):
    parts = []
    for x in re.split(r"[.\-]", s):
        parts.append(int(x) if x.isdigit() else x)
    while len(parts) < 3:
        parts.append(0)
    return tuple(parts[:3]) if all(isinstance(p, int) for p in parts) else tuple(parts)


def satisfies(version, spec):
    """Evaluate a comma-separated specifier like '<3,>=1.26' against a version."""
    ok = True
    hit = False
    for m in re.finditer(r"(>=|<=|>|<|==|!=)\s*([0-9][0-9a-zA-Z.\-]*)", spec):
        op, ref = m.group(1), m.group(2)
        hit = True
        a, b = vt(version), vt(ref)
        try:
            if op == ">=":
                ok &= a >= b
            elif op == "<=":
                ok &= a <= b
            elif op == ">":
                ok &= a > b
            elif op == "<":
                ok &= a < b
            elif op == "==":
                ok &= a == b
            elif op == "!=":
                ok &= a != b
        except TypeError:
            ok = False
    return hit and bool(ok)


def main():
    failures = []

    # (a) requests' constraint on urllib3
    req = pypi("requests", "2.34.2")["info"]
    specs = [r for r in (req.get("requires_dist") or []) if r.split(";")[0].strip().startswith("urllib3")]
    spec = specs[0].split(";")[0].strip().replace("urllib3", "") if specs else ""
    print(f"[T3] requests 2.34.2 requires urllib3 {spec or '<none>'}")
    if spec and not satisfies("2.8.0", spec):
        failures.append(f"urllib3 2.8.0 violates requests 2.34.2 constraint {spec}")
    else:
        print("      -> 2.8.0 satisfies it")

    # (b) Requires-Python of urllib3 2.8.0 vs the image's base Python
    rp = pypi("urllib3", "2.8.0")["info"].get("requires_python") or ""
    print(f"[T3] urllib3 2.8.0 Requires-Python: {rp!r}; image base python: {'.'.join(map(str, IMAGE_PYTHON))}")
    base_str = ".".join(map(str, IMAGE_PYTHON))
    if rp and not satisfies(base_str, rp):
        failures.append(f"image python {base_str} violates urllib3 2.8.0 Requires-Python {rp}")
    else:
        print("      -> image base Python is admitted")

    # (c) no mandatory runtime deps in urllib3 2.8.0
    rd = pypi("urllib3", "2.8.0")["info"].get("requires_dist") or []
    mandatory = [r for r in rd if "extra" not in r]
    print(f"[T3] urllib3 2.8.0 mandatory runtime deps: {mandatory or 'none'} (all others are extras)")
    if mandatory:
        failures.append(f"urllib3 2.8.0 gained mandatory deps {mandatory} — lockfile may be incomplete under --require-hashes")

    if failures:
        print("[T3] RESULT: FAIL")
        for f in failures:
            print(f"  - {f}")
        return 1
    print("[T3] RESULT: PASS — urllib3 2.8.0 is compatible with the pin set and the image base")
    return 0


if __name__ == "__main__":
    sys.exit(main())
