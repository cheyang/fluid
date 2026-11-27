#!/usr/bin/env python3 -I
"""Check every relative link in docs/**/*.md against the file system.

Model: GitHub rendering of markdown files in a repo — a relative link target
resolves against the directory of the file that contains it. External
(http/https/mailto), anchor-only and site-absolute (/) targets are skipped,
as is anything inside fenced code blocks.

Usage: check_links.py <tree-root>
Prints one line per broken link:  <md-file>:<lineno>\t<target>\t-> <resolved>
and a final count. Exit code 0 always (measurement, not a gate).
"""
import os
import re
import sys
import urllib.parse

LINK_RE = re.compile(r"\[(?:[^\[\]]*)\]\(\s*([^)\s]+)(?:\s+\"[^\"]*\")?\s*\)")
FENCE_RE = re.compile(r"^\s*(?:```|~~~)")
# inline code spans — links inside them are not rendered by GitHub
CODESPAN_RE = re.compile(r"`[^`]*`")


def main():
    root = os.path.abspath(sys.argv[1])
    docs = os.path.join(root, "docs")
    broken = []
    checked = 0
    for dirpath, dirnames, filenames in os.walk(docs):
        dirnames.sort()
        for fn in sorted(filenames):
            if not fn.endswith(".md"):
                continue
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, root)
            in_fence = False
            with open(path, encoding="utf-8", errors="replace") as f:
                for lineno, line in enumerate(f, 1):
                    if FENCE_RE.match(line):
                        in_fence = not in_fence
                        continue
                    if in_fence:
                        continue
                    for m in LINK_RE.finditer(CODESPAN_RE.sub("", line)):
                        t = m.group(1)
                        if t.startswith(("http://", "https://", "mailto:",
                                          "tel:", "#")):
                            continue
                        if t.startswith("<") or "://" in t:
                            continue
                        if t.startswith("/"):
                            continue  # site-absolute, out of scope
                        checked += 1
                        target = urllib.parse.unquote(t.split("#")[0].strip())
                        if not target:
                            continue
                        resolved = os.path.normpath(
                            os.path.join(os.path.dirname(path), target)
                        )
                        if not os.path.exists(resolved):
                            broken.append(
                                "%s:%d\t%s\t-> %s"
                                % (rel, lineno, t,
                                   os.path.relpath(resolved, root))
                            )
    for b in broken:
        print(b)
    print("checked=%d broken=%d" % (checked, len(broken)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
