#!/usr/bin/env python3
"""L2 — runtime compatibility smoke for the PR's pin set. CONTRACT test.

multidict 6.9.1 must coexist and interoperate with the versions pinned in the
image lock (aiohttp 3.14.3, yarl 1.24.5): imports, case-insensitive and
repeated-header semantics, the two items-view set-op paths fixed by
GHSA-54p9-h82j-f925 returning *correct results*, and the yarl query parsing
that multidict backs. Run inside a venv with those exact versions installed.
"""
import sys

import aiohttp
import multidict
import yarl
from multidict import CIMultiDict, MultiDict

assert multidict.__version__ == "6.9.1", multidict.__version__
assert aiohttp.__version__ == "3.14.3", aiohttp.__version__
assert yarl.__version__ == "1.24.5", yarl.__version__

md = MultiDict([("a", "1"), ("b", "2")])
assert md["a"] == "1" and md.getall("a") == ["1"]

ci = CIMultiDict([("Content-Type", "text/plain")])
assert ci["content-type"] == "text/plain"

h = CIMultiDict()
h.add("X-Two", "v1")
h.add("X-Two", "v2")
assert h.getall("x-two") == ["v1", "v2"]

# the advisory's set-op paths: no leak (checked by leak_probe.py) AND correct
# results after the fix
d = CIMultiDict([("seed", "x")])
v = object()
r = [("k%d" % i, v) for i in range(1000)]
u = r | d.items()  # or2 path
assert len(u) == 1001, len(u)
s = d.items() - [("seed", "x"), ("nope", "z")]  # sub1 path
assert len(s) == 0, s

# multidict is what yarl/aiohttp use to carry query strings and headers
url = yarl.URL("http://example.com/path?a=1&a=2")
assert url.query.getall("a") == ["1", "2"]

print(
    f"OK multidict {multidict.__version__} + aiohttp {aiohttp.__version__} "
    f"+ yarl {yarl.__version__}: all smoke assertions passed"
)
sys.exit(0)
