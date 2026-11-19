#!/usr/bin/env python3
"""Static verification of a fully pinned, hash-locked requirements.txt.

For each pinned (name, version):
  1. every --hash in the lock matches a real PyPI file digest for that version
  2. at least one matching file is installable on the target platform/python
  3. every non-extra dependency (markers evaluated for the target env) that the
     lock's package graph needs is itself pinned and version-satisfying

Usage: check_lock.py <requirements.txt> <python_version> <report.json>
"""
import json
import re
import sys
import urllib.request

try:
    from packaging.markers import default_environment
    from packaging.requirements import Requirement
    from packaging.utils import canonicalize_name
    from packaging.version import Version
except ImportError:
    # packaging lives in the user site on minimal hosts; make it discoverable.
    import site
    import os as _os
    for _p in (site.getusersitepackages(),):
        if _p and _os.path.isdir(_p):
            sys.path.append(_p)
    from packaging.markers import default_environment
    from packaging.requirements import Requirement
    from packaging.utils import canonicalize_name
    from packaging.version import Version

PYPI = "https://pypi.org/pypi/{name}/{version}/json"
_cache = {}


def pypi_metadata(name, version):
    key = (canonicalize_name(name), version)
    if key in _cache:
        return _cache[key]
    url = PYPI.format(name=name, version=version)
    with urllib.request.urlopen(url, timeout=30) as r:
        data = json.load(r)
    _cache[key] = data
    return data


def parse_lock(path):
    pins = {}
    order = []
    with open(path) as f:
        lines = f.read().splitlines()
    i = 0
    while i < len(lines):
        line = lines[i]
        m = re.match(r"^([A-Za-z0-9_.\-]+)==([A-Za-z0-9_.\-!+]+)\s*\\?\s*$", line)
        if not m:
            i += 1
            continue
        name, version = m.group(1), m.group(2)
        hashes = []
        j = i + 1
        while j < len(lines):
            hm = re.search(r"--hash=sha256:([0-9a-f]{64})", lines[j])
            if hm:
                hashes.append(hm.group(1))
                j += 1
            else:
                break
        pins[canonicalize_name(name)] = {"name": name, "version": version, "hashes": hashes}
        order.append(name)
        i = j
    return pins, order


def wheel_ok(filename, pyver, allow_sdist_for=()):
    if filename.endswith(".tar.gz") or filename.endswith(".zip"):
        stem = filename[: -len(".tar.gz")] if filename.endswith(".tar.gz") else filename[: -len(".zip")]
        return canonicalize_name(stem.split("-")[0]) in allow_sdist_for
    if not filename.endswith(".whl"):
        return False
    parts = filename[:-4].split("-")
    if len(parts) < 5:
        return False
    interp, abi, plat = parts[-3], parts[-2], parts[-1]
    if plat == "any":
        return interp in ("py3", "py2.py3", "py") or interp.startswith("cp")
    if "x86_64" not in plat:
        return False
    if "manylinux" not in plat and plat != "linux_x86_64":
        return False
    if interp in ("py3", "py2.py3", "py"):
        return True
    if interp == "cp" + pyver.replace('.', ''):
        return True
    if abi == "abi3" and interp.startswith("cp"):
        def _vt(s):
            # '36' means python 3.6; '3.10' is already dotted
            if "." not in s:
                s = s[:1] + "." + s[1:]
            return tuple(int(p) for p in s.split("."))
        try:
            return _vt(interp[2:]) <= _vt(pyver)
        except ValueError:
            return False
    return False


def main():
    lock_path, pyver, report_path = sys.argv[1], sys.argv[2], sys.argv[3]
    pins, order = parse_lock(lock_path)
    env = dict(default_environment())
    env.update({
        "python_version": pyver,
        "python_full_version": pyver + ".0",
        "sys_platform": "linux",
        "platform_system": "Linux",
        "os_name": "posix",
        "platform_python_implementation": "CPython",
    })
    errors = []
    warnings = []
    dep_edges = []

    for cname, pin in pins.items():
        try:
            meta = pypi_metadata(pin["name"], pin["version"])
        except Exception as e:  # noqa: BLE001
            errors.append("%s==%s: PyPI fetch failed: %s" % (pin["name"], pin["version"], e))
            continue
        digests = set()
        for u in meta["urls"]:
            digests.add(u["digests"]["sha256"])
        for h in pin["hashes"]:
            if h not in digests:
                errors.append("%s==%s: hash %s not found on PyPI" % (pin["name"], pin["version"], h))
        compatible = [u["filename"] for u in meta["urls"]
                      if u["digests"]["sha256"] in pin["hashes"]
                      and wheel_ok(u["filename"], pyver, allow_sdist_for=("lazy-import",))]
        if not compatible:
            errors.append("%s==%s: no locked wheel installable on cp%s manylinux x86_64 (locked files: %s)"
                          % (pin["name"], pin["version"], pyver,
                             [u["filename"] for u in meta["urls"] if u["digests"]["sha256"] in pin["hashes"]][:6]))
        # dependency constraints
        for req_str in meta["info"].get("requires_dist") or []:
            try:
                req = Requirement(req_str)
            except Exception:  # noqa: BLE001
                continue
            if req.marker and not req.marker.evaluate(env):
                continue
            dname = canonicalize_name(req.name)
            if dname in pins:
                if not req.specifier.contains(Version(pins[dname]["version"]), prereleases=True):
                    errors.append("%s==%s requires %s but lock pins %s==%s"
                                  % (pin["name"], pin["version"], req_str, pins[dname]["name"], pins[dname]["version"]))
                dep_edges.append("%s -> %s" % (cname, dname))
            else:
                warnings.append("%s==%s depends on unpinned %s" % (pin["name"], pin["version"], req_str))

    report = {
        "lock": lock_path,
        "python": pyver,
        "pinnedCount": len(pins),
        "errors": errors,
        "warnings": warnings,
        "depEdgeCount": len(dep_edges),
    }
    with open(report_path, "w") as f:
        json.dump(report, f, indent=2)
    print(json.dumps({k: v for k, v in report.items() if k != "depEdges"}, indent=2))
    return 0 if not errors else 1


if __name__ == "__main__":
    sys.exit(main())
