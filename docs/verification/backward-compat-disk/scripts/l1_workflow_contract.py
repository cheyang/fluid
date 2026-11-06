#!/usr/bin/env python3
"""L1 contract test on the workflow file for PR fluid-cloudnative/fluid#6002.

Contract under test (F3): the backward-compat workflow gains a "Free disk
space" step, positioned AFTER cluster creation and BEFORE image build, that
removes the big pre-installed runner toolchains (dotnet, android, ghc, CodeQL)
and prunes unused docker images.

Usage: l1_workflow_contract.py <workflow.yml>
Exit 0 iff all assertions pass (fails on the base version of the file, which
is the harness-bites check for this contract).
"""
import sys

import yaml


def main(path):
    with open(path) as fh:
        doc = yaml.safe_load(fh)
    failures = []
    ok = []

    job = doc["jobs"]["backward-compat-test"]
    steps = [s.get("name", "<unnamed>") for s in job["steps"]]

    def index(name):
        try:
            return steps.index(name)
        except ValueError:
            return None

    i_cluster = index("Create k8s Kind Cluster")
    i_free = index("Free disk space")
    i_build = index("Build current fluid docker images")

    if i_free is None:
        failures.append("step 'Free disk space' is missing from the workflow")
    else:
        ok.append("step 'Free disk space' present")

    if i_cluster is not None and i_free is not None and i_free > i_cluster:
        ok.append("'Free disk space' runs after kind cluster creation")
    elif i_free is not None:
        failures.append("'Free disk space' must run after 'Create k8s Kind Cluster'")

    if i_build is not None and i_free is not None and i_free < i_build:
        ok.append("'Free disk space' runs before 'Build current fluid docker images'")
    elif i_free is not None:
        failures.append("'Free disk space' must run before 'Build current fluid docker images'")

    if i_free is not None:
        run = job["steps"][i_free].get("run", "")
        for frag, label in [
            ("/usr/share/dotnet", "removes dotnet"),
            ("/usr/local/lib/android", "removes android SDK"),
            ("/opt/ghc", "removes ghc"),
            ("/opt/hostedtoolcache/CodeQL", "removes CodeQL"),
            ("/usr/local/.ghcup", "removes ghcup"),
            ("docker image prune --all --force", "prunes unused docker images"),
            ("df -h", "logs disk usage before/after"),
        ]:
            if frag in run:
                ok.append("cleanup step %s" % label)
            else:
                failures.append("cleanup step missing: %s" % label)

    for line in ok:
        print("ok:   %s" % line)
    for line in failures:
        print("FAIL: %s" % line)
    print()
    if failures:
        print("L1-VERDICT: FAIL")
        return 1
    print("L1-VERDICT: PASS")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: %s <workflow.yml>" % sys.argv[0])
        sys.exit(2)
    sys.exit(main(sys.argv[1]))
