#!/usr/bin/env python3
"""api_doc.md placement check for PR #6245 (finding F-apidoc-rows-misplaced).

The three new JuiceFS imagePullSecrets rows must land INSIDE the field table of
their own type section in docs/{en,zh}/dev/api_doc.md:
  - JuiceFSCompTemplateSpec row  ("pull images for this component")
  - JuiceFSFuseSpec row          ("pull the JuiceFS fuse image")
  - JuiceFSRuntimeSpec row       ("pull images for all JuiceFS components")
They must NOT appear under the root type section (#### JuiceFSRuntime) or under
an unrelated type (#### MasterSpec is Vineyard's). A row outside a table (no
preceding `| Field |` header in its section) is broken markdown.

Contract polarity: FAILS on the misplaced docs, PASSES once the rows are moved
into their own sections' tables (e.g. by a clean `make update-api-doc`)."""
import sys

REPO = sys.argv[1] if len(sys.argv) > 1 else "."

EXPECTED = [
    ("JuiceFSCompTemplateSpec", "pull images for this component"),
    ("JuiceFSFuseSpec", "pull the JuiceFS fuse image"),
    ("JuiceFSRuntimeSpec", "pull images for all JuiceFS components"),
]
FORBIDDEN = ["JuiceFSRuntime", "MasterSpec"]  # root type; Vineyard's type

failures = []

def check(cond, msg):
    print(("PASS" if cond else "FAIL") + ": " + msg)
    if not cond:
        failures.append(msg)

def sections(lines):
    """Yield (name, start, end, header_idx) per '#### <name>' section.
    header_idx = index of the '| Field |' row inside it (None if absent)."""
    heads = [(i, l.strip()[5:].strip()) for i, l in enumerate(lines)
             if l.startswith("#### ")]
    for k, (i, name) in enumerate(heads):
        end = heads[k + 1][0] if k + 1 < len(heads) else len(lines)
        hdr = next((j for j in range(i, end)
                    if lines[j].startswith("| Field |")), None)
        yield name, i, end, hdr

for doc in ("docs/en/dev/api_doc.md", "docs/zh/dev/api_doc.md"):
    lines = open(REPO + "/" + doc).read().splitlines()
    rows = []  # (section, lineno, in_table, text) for juicefs imagePullSecrets rows
    for name, start, end, hdr in sections(lines):
        for j in range(start, end):
            l = lines[j]
            if l.startswith("| `imagePullSecrets`"):
                rows.append((name, j + 1, hdr is not None and j > hdr, l))
    for want_sec, desc_frag in EXPECTED:
        hit = [r for r in rows if desc_frag in r[3]]
        check(len(hit) == 1,
              "%s: exactly one row describes '%s' (found %d)"
              % (doc, desc_frag, len(hit)))
        if hit:
            sec, lineno, in_table, _ = hit[0]
            check(sec == want_sec and in_table,
                  "%s: '%s' row inside %s table (found in section '%s', line %d, in_table=%s)"
                  % (doc, desc_frag, want_sec, sec, lineno, in_table))
    for name, start, end, hdr in sections(lines):
        if name in FORBIDDEN:
            bad = [j + 1 for j in range(start, end)
                   if lines[j].startswith("| `imagePullSecrets`")]
            check(not bad,
                  "%s: no imagePullSecrets row under '%s' section (found at lines %s)"
                  % (doc, name, bad))

print()
if failures:
    print("RESULT: FAIL (%d checks failed)" % len(failures))
    sys.exit(1)
print("RESULT: PASS")
