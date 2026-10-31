#!/usr/bin/env python3
"""Unit layer for the msgpack 1.2.2 -> 1.2.3 bump review (fluid#6226).

Checks the hash-locked entry for msgpack in the image requirements file against
the authoritative PyPI file list, plus the consistency of the whole lock file.
Emits go-test-style JSON lines ({"Test":..., "Action":"pass|fail"}) into the
file named by $TESTJSON_OUT so the stock re-verify.sh can parse them; human
logs go to stderr.

Network policy: tries live PyPI first; falls back to the committed snapshot
under snapshots/ when offline or when PyPI errors. If the pinned version has
no snapshot and no network, the affected test FAILS with a clear message.
"""
import json
import os
import re
import sys
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
TOPIC_DIR = os.path.dirname(HERE)
SNAPDIR = os.path.join(TOPIC_DIR, "snapshots")
REQ = os.environ.get("REQUIREMENTS_FILE") or os.path.join(
    subprocess_root := os.popen("git rev-parse --show-toplevel 2>/dev/null").read().strip() or ".",
    "test/gha-e2e/mooncake/image/requirements.txt",
)
OUT = os.environ.get("TESTJSON_OUT", os.path.join(TOPIC_DIR, "results", "unit.jsonl"))

results = []
def record(name, ok, detail):
    results.append({"Test": name, "Action": "pass" if ok else "fail"})
    print(f"[{'PASS' if ok else 'FAIL'}] {name}: {detail}", file=sys.stderr)

def parse_entry(txt, pkg):
    m = re.search(rf"(?m)^{re.escape(pkg)}==([^\s\\]+)\s*\\?\n(?:\s+--hash=sha256:([0-9a-f]{{64}})\s*\\?\n?)*", txt)
    if not m:
        return None, []
    block = re.search(rf"(?m)^{re.escape(pkg)}==.*?(?=^[a-zA-Z0-9_-]+==|\Z)", txt, re.S)
    hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", block.group(0)) if block else []
    return m.group(1), hashes

def pypi_files(pkg, version):
    snap = os.path.join(SNAPDIR, f"{pkg}-{version}-pypi-files.json")
    try:
        with urllib.request.urlopen(f"https://pypi.org/pypi/{pkg}/{version}/json", timeout=30) as r:
            d = json.load(r)
        files = {f["filename"]: f["digests"]["sha256"] for f in d["urls"]}
        try:
            os.makedirs(SNAPDIR, exist_ok=True)
            with open(snap, "w") as f:
                json.dump(files, f, indent=1, sort_keys=True)
        except OSError:
            pass
        return files, "live-pypi"
    except Exception as e:
        print(f"[info] live PyPI fetch failed for {pkg}=={version}: {e}; trying snapshot", file=sys.stderr)
        if os.path.exists(snap):
            with open(snap) as f:
                return json.load(f), "snapshot"
        return None, "unavailable"

def split_ver(v):
    return [int(x) for x in re.split(r"[.]", re.match(r"[0-9.]+", v).group(0))]

def cmp(a, b):
    n = max(len(a), len(b))
    a += [0] * (n - len(a)); b += [0] * (n - len(b))
    return (a > b) - (a < b)

def spec_allows(spec, version):
    """Minimal PEP-440 clause evaluator (== , !=, >=, <=, >, <, ~=)."""
    if not spec:
        return True
    a = split_ver(version)
    for clause in spec.split(","):
        clause = clause.strip()
        if not clause:
            continue
        m = re.match(r"(===|==|!=|>=|<=|~=|>|<)\s*(\S+)", clause)
        if not m:
            return False
        op, b = m.group(1), split_ver(m.group(2))
        c = cmp(a, b)
        if op in ("==", "==="):
            if not (c == 0):
                return False
        elif op == "!=":
            if not (c != 0):
                return False
        elif op == ">=":
            if not (c >= 0):
                return False
        elif op == "<=":
            if not (c <= 0):
                return False
        elif op == ">":
            if not (c > 0):
                return False
        elif op == "<":
            if not (c < 0):
                return False
        elif op == "~=":
            if not (c >= 0 and a[0] == b[0]):
                return False
        else:
            return False
    return True

def main():
    with open(REQ) as f:
        txt = f.read()
    print(f"[info] requirements file: {REQ}", file=sys.stderr)

    # lock-file sanity: every 'pkg==ver' entry carries at least one hash
    entries = re.findall(r"(?m)^([a-zA-Z0-9_-]+)==", txt)
    no_hash = [p for p in entries
               if not re.search(rf"(?m)^{re.escape(p)}==.*?--hash=sha256:", txt, re.S)
               or not re.search(rf"(?ms)^{re.escape(p)}==.*?(?=^[a-zA-Z0-9_-]+==|\Z)", txt).group(0).count("--hash=")]
    record("TestAllEntriesHashLocked", not no_hash,
           f"{len(entries)} pinned entries; entries without hashes: {no_hash or 'none'}")

    version, hashes = parse_entry(txt, "msgpack")
    if version is None:
        record("TestMsgpackHashesGenuine", False, "no msgpack entry found")
        record("TestCriticalImageWheelHashPresent", False, "no msgpack entry found")
    else:
        files, src = pypi_files("msgpack", version)
        if files is None:
            record("TestMsgpackHashesGenuine", False,
                   f"cannot decide: no network and no snapshot for msgpack {version}")
            record("TestCriticalImageWheelHashPresent", False, "same")
        else:
            pr_set, pypi_set = set(hashes), set(files.values())
            extra = pr_set - pypi_set
            missing = pypi_set - pr_set
            # A defect is a hash PyPI never published for this version (typo'd or
            # foreign artifact). A subset list is fine as long as the image wheel
            # is covered (checked separately); a superset beyond PyPI is not.
            record("TestMsgpackHashesGenuine", not extra,
                   f"msgpack=={version} ({src}): entry has {len(pr_set)} hashes, PyPI has "
                   f"{len(pypi_set)} files; unknown-to-PyPI={len(extra)}, "
                   f"PyPI-not-listed={len(missing)} (informational)")
            crit = "msgpack-%s-cp312-cp312-manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64.whl" % version
            ch = files.get(crit)
            record("TestCriticalImageWheelHashPresent", bool(ch) and ch in pr_set,
                   f"image wheel {crit} hash {'present' if ch in pr_set else 'MISSING'} in entry")

    # mooncake constraint must admit the pinned msgpack
    mc_ver, _ = parse_entry(txt, "mooncake-transfer-engine-non-cuda")
    if mc_ver is None:
        record("TestMooncakeConstraintAdmitsPinnedMsgpack", False, "no mooncake entry found")
    else:
        meta_snap = os.path.join(SNAPDIR, f"mooncake-transfer-engine-non-cuda-{mc_ver}-meta.json")
        reqd = None
        try:
            with urllib.request.urlopen(
                    f"https://pypi.org/pypi/mooncake-transfer-engine-non-cuda/{mc_ver}/json", timeout=30) as r:
                reqd = json.load(r)["info"].get("requires_dist") or []
            try:
                with open(meta_snap, "w") as f:
                    json.dump(reqd, f, indent=1)
            except OSError:
                pass
        except Exception as e:
            print(f"[info] live PyPI fetch failed for mooncake meta: {e}; trying snapshot", file=sys.stderr)
            if os.path.exists(meta_snap):
                with open(meta_snap) as f:
                    reqd = json.load(f)
        if reqd is None:
            record("TestMooncakeConstraintAdmitsPinnedMsgpack", False, "cannot decide: no metadata")
        else:
            spec = ""
            for r in reqd:
                if r.split(";")[0].strip().lower().startswith("msgpack"):
                    tail = r.split(";")[0].strip()
                    m = re.match(r"msgpack\s*\((.*)\)", tail) or re.match(r"msgpack\s*(.*)", tail)
                    spec = (m.group(1) or "").strip()
                    break
            ok = spec_allows(spec, version) if version else False
            record("TestMooncakeConstraintAdmitsPinnedMsgpack", ok,
                   f"mooncake=={mc_ver} requires msgpack spec '{spec or '<unbounded>'}'; pinned {version}")

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w") as f:
        for r in results:
            f.write(json.dumps(r, separators=(",", ":")) + "\n")
    return 0 if all(r["Action"] == "pass" for r in results) else 1

if __name__ == "__main__":
    sys.exit(main())
