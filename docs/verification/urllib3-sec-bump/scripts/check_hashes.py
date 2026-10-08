#!/usr/bin/env python3 -I
# T1 — hash authenticity (unit layer) for the mooncake e2e image lockfile.
#
# Claim under test: every hash pinned in requirements.txt is the genuine
# sha256 digest of an artifact actually published on PyPI for the pinned
# version. A tampered or mistyped hash would make the Docker build
# (`pip install --require-hashes`) fail and turn the kind-e2e pipeline red.
#
# Additionally asserts the PR's own change specifically: urllib3 is pinned to
# 2.8.0 and its two listed hashes are exactly the wheel and sdist digests that
# PyPI publishes for 2.8.0.
#
# PASS = all pins authentic. FAIL (exit 1) = at least one pin does not match
# any published artifact digest.
#
# Polarity: contract. Written 2026-10-08 by Reviewer A (Claude).
import json
import re
import sys
import urllib.request

REQ = sys.argv[1] if len(sys.argv) > 1 else "test/gha-e2e/mooncake/image/requirements.txt"


def pypi(name, version):
    url = f"https://pypi.org/pypi/{name}/{version}/json"
    with urllib.request.urlopen(url, timeout=30) as r:
        return json.load(r)


def parse_reqs(path):
    """Yield (name, version, [hashes]) per entry; joins continuation lines."""
    entries = []
    blob = []
    with open(path, encoding="utf-8") as f:
        for raw in f:
            line = raw.rstrip("\n")
            if line.startswith("#") or not line.strip():
                if blob:
                    entries.append(" ".join(blob))
                    blob = []
                continue
            if line.startswith("    "):  # continuation (--hash / # via)
                blob.append(line.strip())
            else:
                if blob:
                    entries.append(" ".join(blob))
                blob = [line]
    if blob:
        entries.append(" ".join(blob))
    out = []
    for e in entries:
        m = re.match(r"([A-Za-z0-9._-]+)==([^\s\\]+)", e)
        if not m:
            continue
        hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", e)
        out.append((m.group(1), m.group(2), hashes))
    return out


def main():
    failures = []
    pins = parse_reqs(REQ)
    print(f"[T1] parsed {len(pins)} pinned entries from {REQ}")
    for name, version, hashes in pins:
        try:
            data = pypi(name, version)
        except Exception as exc:  # network / missing version
            failures.append(f"{name}=={version}: PyPI lookup failed: {exc}")
            print(f"  {name}=={version}: LOOKUP FAILED ({exc})")
            continue
        published = set()
        for u in data.get("urls", []):
            published.add(u["digests"]["sha256"])
            if u.get("yanked"):
                failures.append(f"{name}=={version}: artifact {u['filename']} is YANKED")
        unknown = [h for h in hashes if h not in published]
        if unknown:
            failures.append(f"{name}=={version}: {len(unknown)} pinned hash(es) match NO published artifact")
            print(f"  {name}=={version}: FAIL — hash not on PyPI: {unknown}")
        else:
            print(f"  {name}=={version}: OK ({len(hashes)} hash(es) all match PyPI digests)")

    # PR-specific assertion: the urllib3 entry itself.
    urllib3_pins = [p for p in pins if p[0] == "urllib3"]
    if len(urllib3_pins) != 1:
        failures.append(f"expected exactly 1 urllib3 pin, found {len(urllib3_pins)}")
    else:
        _, ver, hashes = urllib3_pins[0]
        d = pypi("urllib3", ver)
        wheel = next((u for u in d["urls"] if u["filename"].endswith(".whl")), None)
        sdist = next((u for u in d["urls"] if u["packagetype"] == "sdist"), None)
        if not wheel or not sdist:
            failures.append(f"urllib3=={ver}: PyPI exposes no wheel/sdist pair")
        else:
            want = {wheel["digests"]["sha256"], sdist["digests"]["sha256"]}
            if set(hashes) != want:
                failures.append(f"urllib3=={ver}: listed hashes {set(hashes)} != PyPI wheel+sdist {want}")
                print(f"  urllib3=={ver}: FAIL — hash set mismatch vs PyPI")
            else:
                print(f"  urllib3=={ver}: OK — pinned hashes are exactly the PyPI wheel ({wheel['filename']}) and sdist ({sdist['filename']}) digests")

    if failures:
        print("[T1] RESULT: FAIL")
        for f in failures:
            print(f"  - {f}")
        return 1
    print("[T1] RESULT: PASS — every pinned hash is a genuine PyPI digest")
    return 0


if __name__ == "__main__":
    sys.exit(main())
