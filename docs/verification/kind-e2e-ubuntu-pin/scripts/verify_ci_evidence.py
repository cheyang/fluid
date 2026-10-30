#!/usr/bin/env python3
"""L2 (integration) layer for the PR #6225 verification harness.

For a CI-config-only PR the "integration" surface is the workflow actually
executing on GitHub Actions: these tests re-query the (read-only) GitHub API and
assert the differential that the PR's premise and fix stand on. They are
machine-recheckable on any host with `gh` authenticated and network access.

Polarity: contract tests. On a ref whose kind-e2e run is red or runs on the
wrong image, they FAIL.

  - TestPRHeadKindE2eRunGreenOnPinnedImage: the PR head's own
    "E2E Functionality Check" run must be green with all 5 kind-e2e-test jobs
    on the ubuntu-24.04 runner label.
  - TestFailingRunOnUbuntu2604IsFailure: the recorded failing run
    (40474966878, ubuntu-latest -> ubuntu-26.04) is a failure on all 5 jobs.
  - TestBackwardCompatPassesOnUbuntu2604: backward-compat stays green on
    ubuntu-latest (-> 26.04), which is why this PR does not pin it.

Run:  gh auth status >/dev/null && python3 <this> [--json]
"""
import argparse
import json
import subprocess
import sys

REPO = "fluid-cloudnative/fluid"
PR = 6225
FAILING_RUN = 40474966878      # PR #6219 head b11fb6e4, ubuntu-latest -> 26.04
BACKCOMPAT_RUN = 40885813116   # PR #6225 head, backward-compat on ubuntu-latest
WORKFLOW_NAME = "E2E Functionality Check"

_results = []


def gh(path, jq=None):
    cmd = ["gh", "api", path]
    if jq:
        cmd += ["--jq", jq]
    out = subprocess.run(cmd, capture_output=True, text=True)
    if out.returncode != 0:
        return None
    return out.stdout.strip()


def record(name, ok, detail=""):
    _results.append({"Test": name, "Action": "run"})
    _results.append({"Test": name, "Action": "pass" if ok else "fail",
                     "Output": detail, "Elapsed": 0.0})
    return ok


def run_jobs(run_id):
    raw = gh("repos/%s/actions/runs/%d/jobs?per_page=50" % (REPO, run_id))
    if raw is None:
        return None
    return json.loads(raw).get("jobs") or []


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args()

    # 1. The PR head's own kind-e2e run must be green on the pinned image.
    head = gh("repos/%s/pulls/%d" % (REPO, PR), ".head.sha")
    runs_raw = gh("repos/%s/actions/workflows/kind-e2e.yml/runs?head_sha=%s&per_page=10"
                  % (REPO, head or ""))
    run = None
    if runs_raw:
        try:
            for r in json.loads(runs_raw).get("workflow_runs", []):
                if r.get("name") == WORKFLOW_NAME and r.get("status") == "completed":
                    run = r
                    break
        except Exception:  # noqa: BLE001
            run = None
    ok = False
    detail = "no completed '%s' run found for head %s" % (WORKFLOW_NAME, head)
    if run is not None:
        jobs = run_jobs(run["id"]) or []
        ke2e = [j for j in jobs if j.get("name", "").startswith("kind-e2e-test")]
        labels_ok = bool(ke2e) and all("ubuntu-24.04" in (j.get("labels") or []) for j in ke2e)
        concl_ok = bool(ke2e) and all(j.get("conclusion") == "success" for j in ke2e) \
            and run.get("conclusion") == "success"
        ok = labels_ok and concl_ok and len(ke2e) == 5
        detail = "run %s (head %s): %d kind-e2e-test jobs, labels_ok=%s, conclusions_ok=%s" % (
            run.get("id"), head, len(ke2e), labels_ok, concl_ok)
    record("TestPRHeadKindE2eRunGreenOnPinnedImage", ok, detail)

    # 2. The recorded failing run on ubuntu-latest(->26.04) is a failure.
    fail_raw = gh("repos/%s/actions/runs/%d" % (REPO, FAILING_RUN),
                  "{conclusion, status, path}")
    jobs = run_jobs(FAILING_RUN) or []
    ke2e = [j for j in jobs if j.get("name", "").startswith("kind-e2e-test")]
    record("TestFailingRunOnUbuntu2604IsFailure",
           fail_raw is not None and json.loads(fail_raw).get("conclusion") == "failure"
           and len(ke2e) == 5 and all(j.get("conclusion") == "failure" for j in ke2e)
           and all("ubuntu-latest" in (j.get("labels") or []) for j in ke2e),
           "run %d: %d kind-e2e-test jobs (expected 5 failures on ubuntu-latest)" % (
               FAILING_RUN, len(ke2e)))

    # 3. backward-compat still green on ubuntu-latest.
    bc_raw = gh("repos/%s/actions/runs/%d" % (REPO, BACKCOMPAT_RUN),
                "{conclusion, status, path}")
    bc_jobs = run_jobs(BACKCOMPAT_RUN) or []
    bc = [j for j in bc_jobs if j.get("name", "").startswith("backward-compat-test")]
    record("TestBackwardCompatPassesOnUbuntu2604",
           bc_raw is not None and json.loads(bc_raw).get("conclusion") == "success"
           and bool(bc) and all(j.get("conclusion") == "success" for j in bc)
           and all("ubuntu-latest" in (j.get("labels") or []) for j in bc),
           "run %d: %d backward-compat-test jobs on ubuntu-latest (expected all success)"
           % (BACKCOMPAT_RUN, len(bc)))

    failed = [r for r in _results if r["Action"] == "fail"]
    if args.json:
        for line in _results:
            print(json.dumps(line, separators=(",", ":")))
    # With --json the process output is parsed with `jq -s` (and re-verify.sh
    # merges stderr into the captured file), so emit nothing but JSON lines.
    if not args.json:
        print("%d passed, %d failed (go-test style)" % (
            len([r for r in _results if r["Action"] == "pass"]), len(failed)))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
