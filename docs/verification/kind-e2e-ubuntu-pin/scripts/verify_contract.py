#!/usr/bin/env python3
"""L1 (deterministic unit) layer for the PR #6225 verification harness.

The PR under review changes exactly one contract: the `kind-e2e-test` job of
.github/workflows/kind-e2e.yml must run on the `ubuntu-24.04` runner image
(pinned away from `ubuntu-latest`, which resolves to ubuntu-26.04 where the
JindoRuntime FUSE mount is denied by the host AppArmor fusermount3 profile),
with a TODO(#6224) marker so the pin is eventually reverted.

These tests read the workflow file from the repository root of whatever ref the
harness is grafted onto (re-verify.sh checks out the fixed ref first), so they
are red on the merge base and green on the PR head. That is the polarity of a
contract test: FAIL on unfixed code, PASS on fixed code.

Output is go-test-style JSON lines so the stock review-finding-verifier
re-verify.sh (`kind: gotest`) can parse the results.

Run:  python3 docs/verification/kind-e2e-ubuntu-pin/scripts/verify_contract.py [--json]
"""
import argparse
import json
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(HERE, "..", "..", "..", ".."))
WORKFLOW = os.path.join(REPO_ROOT, ".github", "workflows", "kind-e2e.yml")
BACKCOMPAT = os.path.join(REPO_ROOT, ".github", "workflows", "backward-compatibility-e2e.yml")
RESULTS = os.path.join(os.path.dirname(HERE), "results")

try:
    import yaml
except ImportError:  # pragma: no cover - PyYAML is present on the review host
    yaml = None

_results = []


def record(name, ok, detail=""):
    _results.append({"Test": name, "Action": "run"})
    _results.append({"Test": name, "Action": "pass" if ok else "fail",
                     "Output": detail, "Elapsed": 0.0})
    if not ok:
        print("FAIL: %s: %s" % (name, detail), file=sys.stderr)
    return ok


def read(path):
    with open(path, "r") as f:
        return f.read()


def load_yaml(path):
    if yaml is not None:
        return yaml.safe_load(read(path))
    # Fallback: no PyYAML. Only the assertions that need structure are skipped.
    return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", action="store_true", help="emit go-test style JSON")
    args = ap.parse_args()

    wf_text = read(WORKFLOW) if os.path.exists(WORKFLOW) else ""
    wf = load_yaml(WORKFLOW) if os.path.exists(WORKFLOW) else None

    # --- Contract 1: the workflow still parses and has the expected job. -----
    if wf is None:
        record("TestKindE2eWorkflowParsesAsYaml", os.path.exists(WORKFLOW) and bool(wf_text),
               "workflow file missing or PyYAML unavailable")
    else:
        jobs = (wf or {}).get("jobs") or {}
        record("TestKindE2eWorkflowParsesAsYaml", "kind-e2e-test" in jobs,
               "parsed jobs=%s" % sorted(jobs))

    # --- Contract 2 (the PR's core change): runner pinned to ubuntu-24.04. ---
    runs_on = None
    if wf is not None:
        runs_on = (((wf or {}).get("jobs") or {}).get("kind-e2e-test") or {}).get("runs-on")
    if runs_on is None:
        # structural fallback: look right below the job key
        m = re.search(r"^  kind-e2e-test:\s*\n(.*?)^\s{2}\S", wf_text, re.M | re.S)
        if m:
            r = re.search(r"^\s+runs-on:\s*(\S+)\s*$", m.group(1), re.M)
            runs_on = r.group(1) if r else None
    record("TestKindE2eRunnerPinnedToUbuntu2404", runs_on == "ubuntu-24.04",
           "jobs.kind-e2e-test.runs-on = %r (expected 'ubuntu-24.04')" % runs_on)

    # --- Contract 3: the pin is marked as temporary with the tracking issue. -
    todo_line = None
    for line in wf_text.splitlines():
        stripped = line.strip()
        if stripped.startswith("#") and "#6224" in stripped:
            todo_line = line.strip()
            break
    record("TestKindE2eTodoReferencesTrackingIssue6224", todo_line is not None,
           "TODO marker: %s" % todo_line if todo_line else
           "no '# TODO(#6224)' comment in kind-e2e.yml; the pin must stay marked temporary")

    # --- Contract 4 (scope guard): only kind-e2e was pinned. -----------------
    # backward-compatibility-e2e.yml passes on ubuntu-26.04 (run 40885813116)
    # and must stay on ubuntu-latest so the 26.04 image keeps some coverage.
    bc_runs_on = None
    if os.path.exists(BACKCOMPAT):
        bc = load_yaml(BACKCOMPAT)
        if bc is not None:
            bc_runs_on = ((bc.get("jobs") or {}).get("backward-compat-test") or {}).get("runs-on")
        else:
            r = re.search(r"^\s+runs-on:\s*(\S+)\s*$", read(BACKCOMPAT), re.M)
            bc_runs_on = r.group(1) if r else None
    record("TestBackwardCompatWorkflowLeftUnpinned", bc_runs_on == "ubuntu-latest",
           "backward-compat-test.runs-on = %r (expected 'ubuntu-latest'; pinning it too "
           "would remove all ubuntu-26.04 coverage)" % bc_runs_on)

    # --- Evidence guard: the recorded CI differential is intact. -------------
    ev = None
    try:
        with open(os.path.join(RESULTS, "ci-evidence.json")) as f:
            ev = json.load(f)
    except Exception as exc:  # noqa: BLE001
        record("TestPremiseEvidenceRecorded", False, "ci-evidence.json unreadable: %s" % exc)
    if ev is not None:
        runs = {r["id"]: r for r in ev.get("runs", [])}
        ok = (
            runs.get(40474966878, {}).get("runnerImage") == "ubuntu-26.04"
            and runs.get(40474966878, {}).get("conclusion") == "failure"
            and runs.get(40474966878, {}).get("fuseContainerError") == "fusermount3: mount failed: Permission denied"
            and runs.get(40125302787, {}).get("runnerImage") == "ubuntu-24.04"
            and runs.get(40125302787, {}).get("conclusion") == "success"
            and runs.get(40885813005, {}).get("runnerLabel") == "ubuntu-24.04"
            and runs.get(40885813005, {}).get("conclusion") == "success"
        )
        excerpt = os.path.join(RESULTS, "failing-run-40474966878-ubuntu2604.txt")
        excerpt_ok = os.path.exists(excerpt) and "fusermount3: mount failed: Permission denied" in read(excerpt)
        record("TestPremiseEvidenceRecorded", ok and excerpt_ok,
               "recorded differential %s, failing-run excerpt %s"
               % ("intact" if ok else "incomplete", "intact" if excerpt_ok else "missing"))

    failed = [r for r in _results if r["Action"] == "fail"]
    if args.json:
        for line in _results:
            print(json.dumps(line, separators=(",", ":")))
    # Summary goes to stderr: stdout must stay pure JSON lines for `jq -s`.
    print("%d passed, %d failed (go-test style)" % (
        len([r for r in _results if r["Action"] == "pass"]), len(failed)), file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
