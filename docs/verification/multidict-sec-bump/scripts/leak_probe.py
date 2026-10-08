#!/usr/bin/env python3
"""P0 — GHSA-54p9-h82j-f925 (CVE-2026-104874) leak probe. CONTRACT test.

Contract: after evaluating the items-view set operations, the sentinel value's
refcount returns to its baseline (leaked == 0) on every probed path. Exits 1
(leaks) on multidict 6.7.0..6.9.0, exits 0 on 6.6.x and >= 6.9.1.

Adapted from the PoC embedded in the GitHub advisory
https://github.com/advisories/GHSA-54p9-h82j-f925 (reference leak in
CIMultiDict/MultiDict items-view union and subtraction, or2/sub1 impls).

Run inside a venv that has the multidict build under test installed.
"""
import gc
import sys

import multidict
from multidict import CIMultiDict


def probe(name, fn):
    d = CIMultiDict()
    d["seed"] = "x"
    v = object()
    n = 100000
    operand = [("k%d" % i, v) for i in range(n)]
    gc.collect()
    before = sys.getrefcount(v)
    fn(d, operand)
    gc.collect()
    after = sys.getrefcount(v)
    leaked = after - before
    print(f"multidict {multidict.__version__}: {name:32s} leaked={leaked}")
    return leaked


leaks = [
    probe("operand | d.items()  (or2)", lambda d, o: o | d.items()),
    probe("d.items() - operand  (sub1)", lambda d, o: d.items() - o),
    probe("d.items() | operand  (or1, control)", lambda d, o: d.items() | o),
    probe("d.items() & operand  (and, control)", lambda d, o: d.items() & o),
]
bad = leaks[0] or leaks[1]
print("RESULT:", "LEAKS (vulnerable)" if bad else "clean")
sys.exit(1 if bad else 0)
