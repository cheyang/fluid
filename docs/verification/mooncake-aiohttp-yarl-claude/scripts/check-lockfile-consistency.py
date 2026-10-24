#!/usr/bin/env python3 -I
"""Unit-layer contract check for the mooncake e2e image dependency lock.

Reads a pip hash-locked requirements.txt (the format used by
test/gha-e2e/mooncake/image/requirements.txt) and checks it against live PyPI
metadata for the image's exact target environment:

  - python 3.12 / CPython / linux / x86_64 (the python:3.12.13-slim base image)
  - wheels only (the Dockerfile installs with --only-binary=:all:)

Checks:
  1. wheel-coverage  every pinned package has at least one wheel whose tags are
                     compatible with cp312 / manylinux x86_64 (or a pure wheel)
                     AND whose sha256 is listed in the lock file;
  2. deps-consistent every dependency constraint declared by every pinned
                     package (markers evaluated for the target env, extras
                     excluded) is satisfied by the other pins.

Any violation makes pip's --require-hashes resolution inside the image build
fail (a constraint violation is a ResolutionImpossible; a missing wheel hash is
a hash rejection), which turns the whole kind-e2e matrix red.

Stdlib only (PEP 440 subset: dotted release segments, == != >= <= > < ~=), so it
runs under `python3 -I` on a bare box. Unknown constructs fail loudly rather
than being waved through.

Usage: check-lockfile-consistency.py <requirements.txt> [--status-file F]
       Appends "name<TAB>pass|fail" lines to F when given (re-verify contract);
       always prints a human report and exits non-zero on any failure.
"""

import json
import re
import sys
import urllib.request

TARGET_ENV = {
    "python_version": "3.12",
    "python_full_version": "3.12.13",
    "sys_platform": "linux",
    "platform_system": "Linux",
    "platform_machine": "x86_64",
    "platform_python_implementation": "CPython",
    "os_name": "posix",
    "implementation_name": "cpython",
    "extra": "",  # no extras installed in the image
}

PYPI_URL = "https://pypi.org/pypi/{name}/{version}/json"


# ---------------------------------------------------------------- version math

def parse_version(s):
    """Dotted-numeric release segment -> tuple of ints (PEP 440 subset)."""
    s = s.strip().strip("\"'")
    if not re.fullmatch(r"\d+(\.\d+)*", s):
        return None
    return tuple(int(p) for p in s.split("."))


def cmp_versions(a, b):
    n = max(len(a), len(b))
    a = a + (0,) * (n - len(a))
    b = b + (0,) * (n - len(b))
    return (a > b) - (a < b)


def satisfies(version_tuple, op, spec_tuple):
    c = cmp_versions(version_tuple, spec_tuple)
    if op == "==":
        return c == 0
    if op == "!=":
        return c != 0
    if op == ">=":
        return c >= 0
    if op == "<=":
        return c <= 0
    if op == ">":
        return c > 0
    if op == "<":
        return c < 0
    if op == "~=":  # ~=X.Y.Z -> >=X.Y.Z, ==X.Y.*
        return cmp_versions(version_tuple, spec_tuple) >= 0 and \
            version_tuple[:len(spec_tuple) - 1] == spec_tuple[:len(spec_tuple) - 1]
    raise ValueError("unsupported operator: %s" % op)


def check_specifiers(version_tuple, spec_string):
    """spec like '<2.0,>=1.25.1' -> (ok, failures). Empty spec -> ok."""
    failures = []
    for clause in spec_string.split(","):
        clause = clause.strip()
        if not clause:
            continue
        m = re.fullmatch(r"(~=|==|!=|>=|<=|>|<)\s*([\w.]+!?\S*)", clause)
        if not m:
            return False, ["unparseable clause: %r" % clause]
        op, rhs = m.group(1), m.group(2).strip("\"'")
        rhs_t = parse_version(rhs)
        if rhs_t is None:
            return False, ["unparseable version in clause: %r" % clause]
        if not satisfies(version_tuple, op, rhs_t):
            failures.append(clause)
    return (not failures), failures


# ------------------------------------------------------------------- marker env

TOKEN_RE = re.compile(r"""
    \s*(
        \( | \) |
        ~=|==|!=|>=|<=|>|< |
        and | or | not | in |
        '(?:[^'\\]|\\.)*' | "(?:[^"\\]|\\.)*" |
        [A-Za-z_][A-Za-z0-9_.]*
    )""", re.VERBOSE)


def tokenize_marker(s):
    toks, pos = [], 0
    while pos < len(s):
        m = TOKEN_RE.match(s, pos)
        if not m:
            return None
        toks.append(m.group(1))
        pos = m.end()
    return toks


class MarkerParser(object):
    """expr := term (or term)* ; term := factor (and factor)* ;
    factor := ( expr ) | [not] operand op operand | [not] operand in operand"""

    def __init__(self, toks):
        self.toks = toks
        self.i = 0

    def peek(self):
        return self.toks[self.i] if self.i < len(self.toks) else None

    def next(self):
        t = self.peek()
        self.i += 1
        return t

    def parse(self):
        v = self.expr()
        if self.peek() is not None:
            raise ValueError("trailing tokens: %r" % self.toks[self.i:])
        return v

    def expr(self):
        v = self.term()
        while self.peek() == "or":
            self.next()
            v2 = self.term()
            v = v or v2
        return v

    def term(self):
        v = self.factor()
        while self.peek() == "and":
            self.next()
            v2 = self.factor()
            v = v and v2
        return v

    def factor(self):
        if self.peek() == "not":
            self.next()
            return not self.factor()
        if self.peek() == "(":
            self.next()
            v = self.expr()
            if self.next() != ")":
                raise ValueError("missing )")
            return v
        lhs = self.next()
        if lhs is None:
            raise ValueError("unexpected end of marker")
        op = self.next()
        if op in ("in", "not in"):
            if op == "not" and self.peek() == "in":
                self.next()
                op = "not in"
            rhs = self.next()
            return (lhs in rhs) if op == "in" else (lhs not in rhs)
        if op not in ("~=", "==", "!=", ">=", "<=", ">", "<"):
            raise ValueError("unsupported marker op: %r" % op)
        rhs = self.next()
        return self.compare(lhs, op, rhs)

    def compare(self, lhs, op, rhs):
        lv = parse_version(lhs)
        rv = parse_version(rhs)
        if lv is not None and rv is not None:
            c = cmp_versions(lv, rv)
            return {"==": c == 0, "!=": c != 0, ">=": c >= 0,
                    "<=": c <= 0, ">": c > 0, "<": c < 0, "~=": False}[op] \
                if op != "~=" else satisfies(lv, "~=", rv)
        a, b = self.resolve(lhs), self.resolve(rhs)
        return {"==": a == b, "!=": a != b, ">=": a >= b, "<=": a <= b,
                ">": a > b, "<": a < b, "~=": a == b}[op]

    def resolve(self, name):
        if name in TARGET_ENV:
            return TARGET_ENV[name]
        return name.strip("\"'")


def eval_marker(marker):
    """-> True/False, or None when the marker cannot be parsed (loud failure)."""
    toks = tokenize_marker(marker)
    if toks is None:
        return None
    try:
        return MarkerParser(toks).parse()
    except Exception:
        return None


# ------------------------------------------------------------ requirements.txt

def normalize(name):
    return re.sub(r"[-_.]+", "-", name).lower()


def parse_lockfile(path):
    """-> {normalized_name: {"name","version","hashes":[sha,...]}} in file order."""
    pins = {}
    order = []
    with open(path, "r", encoding="utf-8") as f:
        text = f.read()
    # join continuation lines
    logical = []
    buf = ""
    for raw in text.splitlines():
        line = raw.rstrip()
        if line.endswith("\\"):
            buf += line[:-1] + " "
            continue
        buf += line
        logical.append(buf)
        buf = ""
    if buf:
        logical.append(buf)
    for entry in logical:
        entry = entry.strip()
        if not entry or entry.startswith("#"):
            continue
        m = re.match(r"^([A-Za-z0-9][A-Za-z0-9._-]*)==([^\s;]+)\s*(.*)$", entry)
        if not m:
            continue  # not a pinned requirement line (e.g. options)
        name, version, rest = m.group(1), m.group(2), m.group(3)
        hashes = re.findall(r"--hash=sha256:([0-9a-f]{64})", rest)
        pins[normalize(name)] = {"name": name, "version": version,
                                 "hashes": set(hashes)}
        order.append(normalize(name))
    return pins, order


# --------------------------------------------------------------------- PyPI IO

def fetch_metadata(name, version):
    url = PYPI_URL.format(name=name, version=version)
    req = urllib.request.Request(url, headers={"User-Agent": "fluid-review-harness"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.loads(r.read().decode("utf-8"))


def wheel_compatible(filename):
    """wheel filename tags compatible with cp312 / glibc x86_64?"""
    if not filename.endswith(".whl"):
        return False
    parts = filename[:-4].split("-")
    if len(parts) < 5:
        return False
    py, abi, plat = parts[-3], parts[-2], parts[-1]
    interp_ok = py in ("py3", "py2.py3", "cp312") or \
        (py.startswith("cp3") and abi == "abi3")
    if not interp_ok:
        return False
    plats = plat.split(".")
    for p in plats:
        if p == "any":
            return True
        if ("manylinux" in p or p == "linux_x86_64") and p.endswith("x86_64"):
            return True
    return False


# ------------------------------------------------------------------------ main

def main():
    args = [a for a in sys.argv[1:]]
    status_file = None
    if "--status-file" in args:
        i = args.index("--status-file")
        status_file = args[i + 1]
        del args[i:i + 2]
    if len(args) != 1:
        sys.stderr.write(__doc__)
        return 2
    lock_path = args[0]

    pins, order = parse_lockfile(lock_path)
    if not pins:
        print("FAIL: no pinned requirements parsed from %s" % lock_path)
        return 1
    print("parsed %d pins from %s" % (len(pins), lock_path))

    failures = []   # (check, package, message)
    warnings = []
    metas = {}

    for key in order:
        pin = pins[key]
        try:
            metas[key] = fetch_metadata(pin["name"], pin["version"])
        except Exception as e:
            failures.append(("wheel-coverage", pin["name"],
                             "PyPI metadata fetch failed for %s==%s: %s" %
                             (pin["name"], pin["version"], e)))
            metas[key] = None

    # check 1: wheel + hash coverage
    for key in order:
        pin = pins[key]
        meta = metas[key]
        if meta is None:
            continue
        compat = [u for u in meta.get("urls", []) if wheel_compatible(u["filename"])]
        if not compat:
            failures.append(("wheel-coverage", pin["name"],
                             "no cp312/manylinux-x86_64 wheel for %s==%s" %
                             (pin["name"], pin["version"])))
            continue
        if not any(u["digests"]["sha256"] in pin["hashes"] for u in compat):
            names = ", ".join(u["filename"] for u in compat[:3])
            failures.append(("wheel-coverage", pin["name"],
                             "compatible wheel(s) [%s ...] sha256 not listed in lock file" % names))

    # check 2: dependency constraints satisfied by the other pins
    for key in order:
        pin = pins[key]
        meta = metas[key]
        if meta is None:
            continue
        requires = meta.get("info", {}).get("requires_dist") or []
        for req in requires:
            # split off marker
            m = re.match(r"^(.*?);\s*(.*)$", req)
            if m:
                dep_part, marker = m.group(1).strip(), m.group(2).strip()
                applies = eval_marker(marker)
                if applies is None:
                    warnings.append("unparsed marker for %s dep %r" % (pin["name"], req))
                    failures.append(("deps-consistent", pin["name"],
                                     "could not evaluate marker: %r" % req))
                    continue
                if not applies:
                    continue
            else:
                dep_part = req.strip()
            dm = re.match(r"^([A-Za-z0-9][A-Za-z0-9._-]*)\s*(\[[^\]]*\])?\s*(.*)$", dep_part)
            if not dm:
                failures.append(("deps-consistent", pin["name"],
                                 "unparseable requirement: %r" % req))
                continue
            dep_name, spec = normalize(dm.group(1)), dm.group(3).strip()
            target = pins.get(dep_name)
            if target is None:
                failures.append(("deps-consistent", pin["name"],
                                 "%s==%s requires %r but it is not pinned in the lock file" %
                                 (pin["name"], pin["version"], dep_part)))
                continue
            if spec:
                vt = parse_version(target["version"])
                if vt is None:
                    failures.append(("deps-consistent", target["name"],
                                     "cannot parse pinned version %s" % target["version"]))
                    continue
                ok, fails = check_specifiers(vt, spec)
                if not ok:
                    failures.append(("deps-consistent", pin["name"],
                                     "%s==%s requires %s%s but the lock pins %s==%s" %
                                     (pin["name"], pin["version"], dep_name,
                                      spec, target["name"], target["version"])))

    def status(check):
        bad = [f for f in failures if f[0] == check]
        return "fail" if bad else "pass"

    checks = ["wheel-coverage", "deps-consistent"]
    lines = ["%s\t%s" % (c, status(c)) for c in checks]
    if status_file:
        with open(status_file, "a", encoding="utf-8") as f:
            for ln in lines:
                f.write(ln + "\n")

    print()
    for ln in lines:
        print(ln.replace("\t", ": "))
    for w in warnings:
        print("WARNING:", w)
    if failures:
        print("\nviolations:")
        for check, pkg, msg in failures:
            print("  [%s] %s: %s" % (check, pkg, msg))
    else:
        print("\nno violations: lock is consistent for cp312/manylinux-x86_64")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
