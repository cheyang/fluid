#!/usr/bin/env python3
"""Contract check for PR #6267 (docs: drop removed GooseFS diagnose script).

Claims verified (all CONTRACT polarity — they PASS on correct code, FAIL on broken code):

  P0  (premise, runs against BASE): the troubleshooting guides reference
      tools/diagnose-fluid-goosefs.sh, which does not exist in the tree, so the
      documented `wget .../tools/diagnose-fluid-goosefs.sh` 404s.
  C1: every `tools/diagnose-fluid-<engine>.sh` URL referenced in
      docs/{en,zh}/userguide/troubleshooting.md resolves to a file that exists
      in tools/ at the ref under test.
  C2: no reference to `diagnose-fluid-goosefs` remains in docs/
      (the deprecated GooseFS runtime script was removed in #5703).

Run from a checkout of any ref:
    python3 check_troubleshooting_links.py            # repo-only checks (C1, C2)
    python3 check_troubleshooting_links.py --http     # additionally HEAD-request the raw URLs (P0 symptom)

Exit 0 iff all checks pass. Deterministic without --http.
"""
import os
import re
import sys

DOCS = [
    "docs/en/userguide/troubleshooting.md",
    "docs/zh/userguide/troubleshooting.md",
]
URL_RE = re.compile(
    r"https://raw\.githubusercontent\.com/fluid-cloudnative/fluid/master/(tools/[A-Za-z0-9._/-]+)"
)
GOOSEFS_SCRIPT = "diagnose-fluid-goosefs"


def repo_only():
    failures = []
    referenced = {}
    for doc in DOCS:
        if not os.path.exists(doc):
            failures.append("missing doc: %s" % doc)
            continue
        with open(doc) as f:
            for m in URL_RE.finditer(f.read()):
                referenced.setdefault(m.group(1), []).append(doc)

    # C1: every referenced tools/ path exists in the tree at this ref.
    for path, docs in sorted(referenced.items()):
        if not os.path.exists(path):
            failures.append(
                "C1: %s referenced in %s but missing from tree" % (path, ",".join(docs))
            )

    # C2: no goosefs diagnose references remain in docs/ (harness itself excluded).
    for root, dirs, files in os.walk("docs"):
        dirs[:] = [d for d in dirs if d != "verification"]
        for name in files:
            p = os.path.join(root, name)
            try:
                with open(p, errors="ignore") as f:
                    if GOOSEFS_SCRIPT in f.read():
                        failures.append("C2: %s still references %s" % (p, GOOSEFS_SCRIPT))
            except OSError:
                pass

    print("referenced scripts: %s" % (sorted(referenced) or "(none)"))
    return failures


def http_layer():
    """P0 symptom layer: HEAD each documented raw URL. Requires network."""
    try:
        from urllib.request import Request, urlopen
    except ImportError:
        return ["P0: urllib unavailable"]
    failures = []
    urls = set()
    for doc in DOCS:
        if os.path.exists(doc):
            with open(doc) as f:
                urls.update(m.group(0) for m in URL_RE.finditer(f.read()))
    for url in sorted(urls):
        req = Request(url, method="HEAD")
        try:
            code = urlopen(req, timeout=15).getcode()
        except Exception as e:  # noqa: BLE001
            code = getattr(e, "code", str(e))
        print("HTTP %s %s" % (code, url))
        if code != 200:
            failures.append("P0/C1: %s returns %s (documented link is dead)" % (url, code))
    return failures


def main():
    failures = repo_only()
    if "--http" in sys.argv:
        failures += http_layer()
    else:
        print("(skip HTTP layer; pass --http to HEAD-check the raw URLs)")
    if failures:
        print("FAIL")
        for f in failures:
            print(" - " + f)
        return 1
    print("PASS: all troubleshooting diagnose-script links resolve in-tree; no goosefs refs remain in docs")
    return 0


if __name__ == "__main__":
    sys.exit(main())
