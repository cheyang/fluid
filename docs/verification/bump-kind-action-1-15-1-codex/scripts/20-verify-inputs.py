#!/usr/bin/env python3
"""20-verify-inputs.py — prove the helm/kind-action v1.15.0->v1.15.1 upstream
changes are behaviorally inert for fluid's workflows.

Upstream v1.15.1 (1544676c07bb3570fcb70166beaa07c4528fd4aa) changed:
  * action.yml: new optional input `version_file` (unset -> inert)
  * action.yml/kind.sh: kubectl default v1.37.0 -> v1.37.1
                        (inert iff `kubectl_version` is passed explicitly)
  * kind.sh: cloud-provider-kind 0.6.0 -> 0.12.0
             (inert iff `cloud_provider` is not enabled)

So for every workflow step that uses helm/kind-action this asserts:
  yaml-parse[<file>]         file parses as YAML
  inputs-pinned[<file>]      `version` and `kubectl_version` are set explicitly,
                             and `version_file` / `cloud_provider` are absent

Emits "<name>\tpass|fail" lines (parsed by re-verify.sh, kind=shell).
Usage: 20-verify-inputs.py [REPO_ROOT]
"""
import glob
import os
import sys

import yaml

RESULTS = []

REPO_ROOT = sys.argv[1] if len(sys.argv) > 1 else "."
WORKFLOWS = sorted(glob.glob(os.path.join(REPO_ROOT, ".github/workflows/*.yml")))

def emit(name, ok):
    print("%s\t%s" % (name, "pass" if ok else "fail"))

found = 0
for path in WORKFLOWS:
    f = os.path.basename(path)
    try:
        with open(path) as fh:
            doc = yaml.safe_load(fh)
        RESULTS.append(("yaml-parse[%s]" % f, True))
    except Exception as exc:  # noqa: BLE001
        RESULTS.append(("yaml-parse[%s]" % f, False))
        print("BAD  %s: YAML parse error: %s" % (f, exc), file=sys.stderr)
        continue
    jobs = (doc or {}).get("jobs") or {}
    for job in jobs.values():
        for step in (job or {}).get("steps") or []:
            uses = (step or {}).get("uses") or ""
            if not uses.startswith("helm/kind-action@"):
                continue
            found += 1
            w = step.get("with") or {}
            ok = True
            for required in ("version", "kubectl_version"):
                if not w.get(required):
                    ok = False
                    print("BAD  %s: kind-action step missing explicit `%s` input"
                          % (f, required), file=sys.stderr)
            for forbidden in ("version_file", "cloud_provider"):
                if forbidden in w:
                    ok = False
                    print("BAD  %s: kind-action step sets `%s`; upstream default "
                          "changes in v1.15.1 are no longer inert" % (f, forbidden),
                          file=sys.stderr)
            RESULTS.append(("inputs-pinned[%s]" % f, ok))

# Summary lines LAST per group: re-verify.sh keeps the last line matching each
# substring, so "[all]" must reflect any per-file failure.
parse_ok = all(r for n, r in RESULTS if n.startswith("yaml-parse["))
inputs_ok = found > 0 and all(r for n, r in RESULTS if n.startswith("inputs-pinned["))
for n, r in RESULTS:
    emit(n, r)
emit("yaml-parse[all]", parse_ok)
emit("inputs-pinned[all]", inputs_ok)

if found == 0:
    print("BAD  no helm/kind-action steps found under %s" % REPO_ROOT, file=sys.stderr)
