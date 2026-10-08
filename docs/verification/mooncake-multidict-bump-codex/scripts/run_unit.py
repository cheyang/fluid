#!/usr/bin/env python3
"""Unit/deterministic layer for PR #6202 verification.

Emits go-test-JSON style lines ({"Test": ..., "Action": "pass|fail"}) so the
review-finding-verifier re-verify.sh parser can consume the results.

Checks:
  TestP0_BasePinsVulnerableMultidict   premise: base requirements.txt pins a
                                       multidict inside GHSA-54p9-h82j-f925's
                                       vulnerable range (>=6.7.0, <=6.9.0).
  TestHashSetMatchesPyPI               PR's multidict hash set == the set of
                                       files PyPI published for 6.9.1.
  TestTargetWheelHashPresent           the cp312 manylinux x86_64 wheel hash the
                                       image build needs is present.
  TestMultidictEntryAllPlatformHashes  CANARY (F1): the entry carries hashes for
                                       many platforms + the sdist, contrary to
                                       the file header's documented
                                       "cp312 / manylinux x86_64" one-hash
                                       convention. Flips to FAIL when the entry
                                       is trimmed back to the convention.
"""
import json
import os
import re
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
TOPIC_DIR = os.path.dirname(HERE)
REPO = subprocess.check_output(
    ["git", "rev-parse", "--show-toplevel"], cwd=TOPIC_DIR, text=True
).strip()
REQ_REL = "test/gha-e2e/mooncake/image/requirements.txt"
FIXTURE = os.path.join(TOPIC_DIR, "results", "pypi-multidict-6.9.1-files.json")
ADVISORY = os.path.join(TOPIC_DIR, "results", "ghsa-54p9-h82j-f925.json")

results = []


def emit(name, ok, note=""):
    results.append((name, ok, note))
    print("{\"Test\":\"%s\",\"Action\":\"%s\"}" % (name, "pass" if ok else "fail"))
    if note:
        print(json.dumps({"Action": "output", "Output": "# %s: %s" % (name, note)}))


def parse_multidict_entry(text):
    """Return (version, [hashes]) for the multidict entry of a requirements file."""
    m = re.search(r"^multidict==([^\s\\]+)\s*\\\n((?:\s+--hash=sha256:[0-9a-f]+\s*\\?\n)+)",
                  text, re.M)
    if not m:
        return None, []
    return m.group(1), re.findall(r"--hash=sha256:([0-9a-f]+)", m.group(2))


def base_file_text():
    base = subprocess.check_output(
        ["git", "merge-base", "origin/master", "HEAD"], cwd=REPO, text=True
    ).strip()
    return subprocess.check_output(
        ["git", "show", "%s:%s" % (base, REQ_REL)], cwd=REPO, text=True
    )


def parse_range(rng):
    """'>= 6.7.0, <= 6.9.0' -> list of (op, tuple) constraints."""
    out = []
    for part in rng.split(","):
        part = part.strip()
        m = re.match(r"(>=|<=|==|>|<|=)\s*([0-9.]+)", part)
        op = m.group(1)
        ver = tuple(int(x) for x in m.group(2).split("."))
        out.append((op, ver))
    return out


def in_range(ver, constraints):
    v = tuple(int(x) for x in ver.split("."))
    for op, c in constraints:
        if op in (">=",) and not v >= c:
            return False
        if op in ("<=",) and not v <= c:
            return False
        if op in ("=", "==") and not v == c:
            return False
        if op == ">" and not v > c:
            return False
        if op == "<" and not v < c:
            return False
    return True


def main():
    fixture = json.load(open(FIXTURE))
    advisory = json.load(open(ADVISORY))
    pypi_hashes = {f["sha256"] for f in fixture["files"]}
    wheel_by_name = {f["filename"]: f["sha256"] for f in fixture["files"]}

    # P0 — premise, runs against the BASE version of the file.
    base_text = base_file_text()
    base_ver, _ = parse_multidict_entry(base_text)
    rng = advisory["vulnerabilities"][0]["vulnerable_version_range"]
    vulnerable = base_ver is not None and in_range(base_ver, parse_range(rng))
    emit("TestP0_BasePinsVulnerableMultidict", vulnerable,
         "base pins multidict==%s; advisory %s range '%s' (severity %s)" %
         (base_ver, advisory["ghsa_id"], rng, advisory["severity"]))

    # PR-head version of the file.
    head_text = subprocess.check_output(
        ["git", "show", "HEAD:%s" % REQ_REL], cwd=REPO, text=True)
    head_ver, head_hashes = parse_multidict_entry(head_text)
    head_set = set(head_hashes)

    emit("TestHashSetMatchesPyPI",
         head_ver == fixture["version"] and head_set == pypi_hashes,
         "file pins %s with %d hashes; PyPI published %d files; symmetric diff: %d" %
         (head_ver, len(head_set), len(pypi_hashes),
          len(head_set ^ pypi_hashes)))

    target = None
    for name, h in wheel_by_name.items():
        if ("cp312" in name and "manylinux" in name and "x86_64" in name
                and name.endswith(".whl")):
            target = (name, h)
            break
    emit("TestTargetWheelHashPresent",
         target is not None and target[1] in head_set,
         "image base is python:3.12.13-slim on linux/amd64; needs %s" %
         (target[0] if target else "<none found in fixture>"))

    # CANARY for F1: currently the entry carries hashes for every platform and
    # the sdist (171), not the documented single cp312/manylinux x86_64 wheel.
    cp312_x86 = {h for n, h in wheel_by_name.items()
                 if "cp312" in n and "manylinux" in n and "x86_64" in n
                 and n.endswith(".whl")}
    extras = head_set - cp312_x86
    emit("TestMultidictEntryAllPlatformHashes", len(extras) > 0,
         "entry carries %d hashes beyond the cp312/manylinux x86_64 wheel "
         "(header documents one hash per package; base carried 1)" % len(extras))

    failed = [n for n, ok, _ in results if not ok]
    print(json.dumps({"Action": "output", "Output": "# unit layer: %d checks, %d failed" % (len(results), len(failed))}))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
