#!/usr/bin/env python3
# Integration-layer evidence check for PR fluid-cloudnative/fluid#6208.
#
# The real integration test for a kind-action bump is the workflows themselves
# running on GitHub runners. This script re-queries the check-run conclusions
# for the PR head (via the GitHub API, unauthenticated or `gh` credentials if
# present) and emits contract-check results:
#
#   kind-e2e-matrix        every kind-e2e-test matrix job on the head sha is SUCCESS
#   backward-compat-matrix every backward-compat-test matrix job is SUCCESS
#
# Report mode (--report) prints `NAME<TAB>pass|fail` lines for re-verify.sh.
import argparse
import json
import subprocess
import sys
import urllib.request


def gh_api(url):
    try:
        out = subprocess.run(
            ["gh", "api", url], capture_output=True, text=True, timeout=60)
        if out.returncode == 0:
            return json.loads(out.stdout)
    except (OSError, subprocess.SubprocessError):
        pass
    req = urllib.request.Request(url, headers={"User-Agent": "verify-harness"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default="fluid-cloudnative/fluid")
    ap.add_argument("--pr", type=int, default=6208)
    ap.add_argument("--report", action="store_true")
    args = ap.parse_args()

    pr = gh_api("repos/%s/pulls/%d" % (args.repo, args.pr))
    head_sha = pr["head"]["sha"]
    runs = gh_api(
        "repos/%s/commits/%s/check-runs?per_page=100" % (args.repo, head_sha))
    checks = [(c["name"], c["conclusion"]) for c in runs.get("check_runs", [])]

    results = {}
    for prefix, name in (("kind-e2e-test", "kind-e2e-matrix"),
                         ("backward-compat-test", "backward-compat-matrix")):
        matrix = [(n, c) for n, c in checks if n.startswith(prefix)]
        if not matrix:
            results[name] = (False, "no check runs found for prefix %s" % prefix)
        else:
            bad = ["%s=%s" % (n, c)
                   for n, c in matrix
                   if str(c).upper() != "SUCCESS"]
            results[name] = (not bad,
                             "%d/%d success; bad: %s" %
                             (len(matrix) - len(bad), len(matrix), bad or "none"))

    print("head sha: %s" % head_sha)
    for name, (ok, detail) in results.items():
        print("[%s] %s: %s" % ("PASS" if ok else "FAIL", name, detail))
    if args.report:
        for name, (ok, _detail) in results.items():
            print("%s\t%s" % (name, "pass" if ok else "fail"))
    sys.exit(0 if all(ok for ok, _ in results.values()) else 1)


if __name__ == "__main__":
    main()
