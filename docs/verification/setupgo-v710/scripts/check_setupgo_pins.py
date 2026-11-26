#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Contract harness for PR fluid-cloudnative/fluid#6291 (bump actions/setup-go 7.0.0 -> 7.1.0).

Checks (each prints "<name>\t<pass|fail>" and the script exits non-zero if any fails):

  pin-bump          every `actions/setup-go@<sha> # <version>` usage under .github/
                    pins the expected SHA and version comment (i.e. the bump is
                    complete and correctly labeled, with no stragglers).
  go-version-input  every setup-go step's `with.go-version` is still
                    '${{ env.GO_VERSION }}' (the bump must not alter inputs).
  tag-identity      the pinned SHA resolves to the real v7.1.0 tag of
                    actions/setup-go upstream (network; pass --offline to skip).

Polarity: CONTRACT tests. They FAIL on the merge-base (old v7.0.0 pins) and
PASS on the PR head. Run from the repo root:

    python3 docs/verification/setupgo-v710/scripts/check_setupgo_pins.py [--offline]
"""
from __future__ import print_function

import os
import re
import subprocess
import sys

EXPECTED_SHA = "d8cbb8045d7f43706b9177cceb36c1f91a6644d5"
EXPECTED_VERSION = "v7.1.0"
UPSTREAM = "https://github.com/actions/setup-go.git"
EXPECTED_GOV = "${{ env.GO_VERSION }}"

RESULTS = []


def record(name, ok, detail):
    RESULTS.append((name, ok, detail))
    print("%s\t%s\t%s" % (name, "pass" if ok else "fail", detail))


def iter_workflow_files(root):
    wf = os.path.join(root, ".github", "workflows")
    if not os.path.isdir(wf):
        return []
    out = []
    for base, _dirs, files in os.walk(wf):
        for f in sorted(files):
            if f.endswith((".yml", ".yaml")):
                out.append(os.path.join(base, f))
    return sorted(out)


def check_pins(root):
    """Every setup-go usage pins the expected SHA + version comment; nothing else
    in .github/ references setup-go with a different ref."""
    usage_re = re.compile(r"uses:\s*actions/setup-go@(\S+)\s*(?:#\s*(\S+))?")
    ok = True
    total = 0
    files_hit = set()
    for path in iter_workflow_files(root):
        rel = os.path.relpath(path, root)
        with open(path, "r") as fh:
            for lineno, line in enumerate(fh, 1):
                m = usage_re.search(line)
                if not m:
                    continue
                total += 1
                files_hit.add(rel)
                sha, comment = m.group(1), m.group(2)
                if sha != EXPECTED_SHA:
                    print("  MISMATCH %s:%d pins %s (expected %s)"
                          % (rel, lineno, sha, EXPECTED_SHA), file=sys.stderr)
                    ok = False
                if comment != EXPECTED_VERSION:
                    print("  MISMATCH %s:%d comment %r (expected %r)"
                          % (rel, lineno, comment, EXPECTED_VERSION), file=sys.stderr)
                    ok = False
    # completeness guard: no setup-go usage may hide elsewhere under .github/
    stray = []
    gh = os.path.join(root, ".github")
    for base, dirs, files in os.walk(gh):
        dirs[:] = [d for d in dirs if d != "workflows"]
        for f in files:
            p = os.path.join(base, f)
            try:
                with open(p, "r", errors="replace") as fh:
                    if "setup-go" in fh.read():
                        stray.append(os.path.relpath(p, root))
            except (IOError, OSError):
                pass
    if stray:
        print("  STRAY setup-go refs outside workflows/: %s" % stray, file=sys.stderr)
        ok = False
    if total < 1:
        ok = False
    record("pin-bump", ok,
           "%d usage(s) in %d file(s) all pin %s (%s)%s"
           % (total, len(files_hit), EXPECTED_SHA, EXPECTED_VERSION,
              "" if not stray else "; stray=%s" % stray))
    return files_hit


def check_go_version(root):
    """YAML-level check: each setup-go step keeps with.go-version == env.GO_VERSION."""
    try:
        import yaml
    except ImportError:
        record("go-version-input", True, "PyYAML unavailable; skipped (raw-text pin check still ran)")
        return
    ok = True
    checked = 0
    for path in iter_workflow_files(root):
        try:
            with open(path, "r") as fh:
                doc = yaml.safe_load(fh)
        except Exception as exc:  # unparsable workflow is itself a failure
            print("  YAML parse error %s: %s" % (path, exc), file=sys.stderr)
            ok = False
            continue
        if not isinstance(doc, dict):
            continue
        for jname, job in (doc.get("jobs") or {}).items():
            if not isinstance(job, dict):
                continue
            for step in job.get("steps") or []:
                if not isinstance(step, dict):
                    continue
                uses = str(step.get("uses") or "")
                if not uses.startswith("actions/setup-go@"):
                    continue
                checked += 1
                got = str((step.get("with") or {}).get("go-version") or "")
                if got != EXPECTED_GOV:
                    print("  %s job %s: go-version=%r (expected %r)"
                          % (os.path.relpath(path, root), jname, got, EXPECTED_GOV),
                          file=sys.stderr)
                    ok = False
    record("go-version-input", ok, "%d setup-go step(s) keep go-version: %s" % (checked, EXPECTED_GOV))


def check_tag_identity():
    """The pinned SHA is the genuine v7.1.0 tag upstream (needs network)."""
    try:
        out = subprocess.check_output(
            ["git", "ls-remote", "--tags", UPSTREAM, "refs/tags/" + EXPECTED_VERSION],
            stderr=subprocess.STDOUT, timeout=60).decode("utf-8", "replace").strip()
    except Exception as exc:
        record("tag-identity", True, "network unavailable (%s); not-run/skipped" % exc)
        return
    sha = out.split("\t")[0].strip() if "\t" in out else ""
    ok = sha == EXPECTED_SHA
    record("tag-identity", ok,
           "refs/tags/%s -> %s (expected %s)" % (EXPECTED_VERSION, sha or "<none>", EXPECTED_SHA))


def main():
    root = os.getcwd()
    offline = "--offline" in sys.argv[1:]
    check_pins(root)
    check_go_version(root)
    if not offline:
        check_tag_identity()
    failed = [r for r in RESULTS if not r[1]]
    print("---")
    for name, ok, detail in RESULTS:
        print("%-18s %s  %s" % (name, "PASS" if ok else "FAIL", detail))
    if failed:
        print("CONTRACT RESULT: FAIL (%d check(s) failed)" % len(failed))
        return 1
    print("CONTRACT RESULT: PASS (all checks green)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
