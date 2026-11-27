#!/usr/bin/env python3
"""Resolve every relative link in docs/**/*.md against the filesystem.

Part of the PR #6294 verification harness (fluid-cloudnative/fluid).

Usage: check_doc_links.py <repo-root>
Prints one line per broken relative link:  <file>:<line>: <link>
then a summary. Exit code 0 regardless; this is a scanner, not a verdict.

Notes / known false positives:
- docs/{en,zh}/dev/api_doc.md contains a regex Pattern cell (escaped pipes like
  \+\|-) that the link regex misparses. Those are artifacts of generated API
  docs, not real links; callers should filter them out.
- docs/verification/ is skipped: it is this harness's own directory and must
  not feed back into the scan.
"""
import os
import re
import sys

LINK_RE = re.compile(r'\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)')

def is_external(link):
    l = link.lower()
    return (
        l.startswith('http://') or l.startswith('https://') or
        l.startswith('mailto:') or l.startswith('ftp://') or
        l.startswith('#') or l.startswith('//')
    )

def scan(root):
    broken = []
    total = 0
    docs = os.path.join(root, 'docs')
    for dirpath, dirnames, filenames in os.walk(docs):
        dirnames[:] = [d for d in dirnames if not d.startswith('.') and d != 'verification']
        for fn in filenames:
            if not fn.endswith('.md'):
                continue
            path = os.path.join(dirpath, fn)
            with open(path, encoding='utf-8', errors='replace') as f:
                for lineno, line in enumerate(f, 1):
                    for m in LINK_RE.finditer(line):
                        link = m.group(1)
                        if is_external(link):
                            continue
                        target = link.split('#', 1)[0]
                        if not target:
                            continue
                        total += 1
                        resolved = os.path.normpath(os.path.join(os.path.dirname(path), target))
                        if not os.path.exists(resolved):
                            broken.append((os.path.relpath(path, root), lineno, link))
    return total, broken

def main(root):
    total, broken = scan(root)
    for f, l, link in sorted(broken):
        print(f"{f}:{l}: {link}")
    print("---")
    print(f"TOTAL relative links checked: {total}")
    print(f"BROKEN: {len(broken)}")

if __name__ == '__main__':
    main(sys.argv[1])
