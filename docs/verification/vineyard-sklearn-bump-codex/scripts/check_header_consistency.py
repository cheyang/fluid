#!/usr/bin/env python3
"""F1: the documented regeneration command in the requirements.txt header must
pin the same versions the file itself pins. The header says "replace everything
below this header with the output of: printf "pandas\\n...\\n<pkg>==<ver>\\n..."
| uv pip compile ...". If the printf command pins an older version than the
file, following the file's own instructions silently reverts the bump.

Usage: check_header_consistency.py [requirements.txt]
Exit 0 if consistent, 1 if any documented pin disagrees with the actual pin.
"""
import re
import sys

REQ = sys.argv[1] if len(sys.argv) > 1 else "samples/vineyard/requirements.txt"

with open(REQ) as fh:
    text = fh.read()

header, _, _ = text.partition("argcomplete==")  # header = leading comment block
m = re.search(r'printf\s+"([^"]+)"', header)
if not m:
    print("FAIL: could not find the documented regeneration printf command")
    sys.exit(1)
doc_pins = {}
for entry in m.group(1).split("\\n"):  # entries separated by literal backslash-n
    if "==" in entry:
        name, ver = entry.split("==", 1)
        doc_pins[name.strip()] = ver.strip()

ok = True
for pkg, doc_ver in sorted(doc_pins.items()):
    pm = re.search(r"^%s==(\S+?)\s*\\?\s*$" % re.escape(pkg), text, re.M)
    actual = pm.group(1) if pm else None
    if actual is None:
        ok = False
        print("FAIL: header documents %s==%s but the file does not pin %s"
              % (pkg, doc_ver, pkg))
    elif actual != doc_ver:
        ok = False
        print("FAIL: header documents %s==%s but the file pins %s==%s"
              % (pkg, doc_ver, pkg, actual))
    else:
        print("ok: header and file agree on %s==%s" % (pkg, actual))

if ok:
    print("PASS: documented regeneration command reproduces the pinned versions")
    sys.exit(0)
print("FAIL: following the documented regeneration command would change pinned versions")
sys.exit(1)
