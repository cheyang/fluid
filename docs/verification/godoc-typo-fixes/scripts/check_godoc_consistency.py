#!/usr/bin/env python3
"""GoDoc consistency contract checker for PR #6127 (fluid).

Contract being tested (per the Go doc-comment convention the PR itself cites):
  1. Every exported function in the checked files has a doc comment.
  2. The first token of that doc comment equals the function's identifier.

Polarity: CONTRACT test.
  - On the current PR head it FAILS (reproduces F1 and F2).
  - It PASSES once the author completes the fix (align the comment at
    pkg/utils/quantity.go:72 with the identifier `TranformQuantityToUnits`,
    or rename the function and its call sites; and add a doc comment for
    `TransformEFCUnitToQuantity`).

Usage: check_godoc_consistency.py <file.go> [<file.go> ...]
Exit 0 iff all exported functions in every given file satisfy both rules.
"""
import re
import sys

FUNC_RE = re.compile(r"^func ([A-Z]\w*)\s*\(")


def check(path):
    """Return a list of (func_name, line_no, problem) tuples for one file."""
    problems = []
    last_comment = None  # (first_token, end_line)
    with open(path) as f:
        for lineno, raw in enumerate(f, 1):
            line = raw.rstrip("\n")
            stripped = line.strip()
            if stripped.startswith("//"):
                if last_comment is None or last_comment[1] != lineno - 1:
                    # start of a new comment block
                    first = stripped[3:].split()
                    last_comment = (first[0] if first else "", lineno)
                else:
                    last_comment = (last_comment[0], lineno)
                continue
            m = FUNC_RE.match(line)
            if m:
                name = m.group(1)
                if last_comment is None or last_comment[1] != lineno - 1:
                    problems.append((name, lineno, "F2: exported function has no doc comment"))
                elif last_comment[0] != name:
                    problems.append(
                        (name, lineno,
                         "F1: doc comment starts with %r but the identifier is %r"
                         % (last_comment[0], name)))
            if not stripped:  # blank line resets comment adjacency
                last_comment = None
            elif not stripped.startswith("//"):
                last_comment = None
    return problems


def main(argv):
    all_problems = []
    for path in argv[1:]:
        for name, lineno, problem in check(path):
            all_problems.append("%s:%d: %s" % (path, lineno, problem))
    if all_problems:
        print("FAIL: %d godoc consistency violation(s)" % len(all_problems))
        for p in all_problems:
            print("  " + p)
        return 1
    print("PASS: all exported functions documented with matching names")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
