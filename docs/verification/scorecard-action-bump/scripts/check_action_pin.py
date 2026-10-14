#!/usr/bin/env python3
"""
Static verification harness for the ossf/scorecard-action version pin in
.github/workflows/sonarcloud.yaml (fluid PR #6207, bump 2.4.4 -> 2.5.0).

Layer: L1 (deterministic, static analysis). No cluster, no live run.
Requires: python3 with PyYAML, git, network access to github.com.

Checks:
  C1  contract: the workflow YAML parses.
  C2  contract: the scorecard-action pin SHA equals the commit dereferenced
      from the upstream annotated tag named in the trailing "# vX.Y.Z"
      comment on the `uses:` line.
  C3  canary:   every `with:` input at the usage is declared in the action's
      action.yaml at the pinned commit. Default WARN (the workflow already
      passes the inert exclude_checks/exclude_paths inputs on v2.4.4);
      --strict-inputs promotes undeclared inputs to FAIL.
  C4  contract: every `required: true` action input is supplied by the
      workflow's `with:` block.
  C5  contract (differential, needs --prev-pin): every workflow-used input
      declared at the previous pin is still declared at the current pin
      (catches an input being dropped/renamed across the bump).
  C6  contract: the docker image reference in the pinned action.yaml points
      at the same version tag as the pin comment.

Exit code 0 iff all FAIL-level checks pass. A JSON + text report is written
to --report (default: results/l1-report.json beside this script's parent).
"""

import argparse
import json
import re
import subprocess
import sys
import tempfile
import urllib.request

import yaml

ACTION_REPO_URL = "https://github.com/ossf/scorecard-action.git"
ACTION_REPO_NAME = "ossf/scorecard-action"
USES_RE = re.compile(
    r"uses:\s*" + re.escape(ACTION_REPO_NAME) + r"@([0-9a-f]{40})\s*#\s*(v[0-9][0-9A-Za-z.\-+]*)"
)


def run_git(args, cwd=None):
    out = subprocess.run(
        ["git"] + args, cwd=cwd, capture_output=True, text=True
    )
    if out.returncode != 0:
        raise RuntimeError("git %s failed: %s" % (args, out.stderr.strip()))
    return out.stdout


def ls_remote_tags(repo_url):
    """Return {tag_name: dereferenced_commit_sha} for annotated + lightweight tags."""
    out = run_git(["ls-remote", "--tags", repo_url])
    tags = {}
    for line in out.splitlines():
        sha, ref = line.split("\t")
        ref = ref[len("refs/tags/"):]
        if ref.endswith("^{}"):
            tags[ref[: -len("^{}")]] = sha  # dereferenced commit of annotated tag
    # keep annotated entries (they win over the tag-object sha)
    for line in out.splitlines():
        sha, ref = line.split("\t")
        ref = ref[len("refs/tags/"):]
        if not ref.endswith("^{}") and ref not in tags:
            tags[ref] = sha
    return tags


def fetch_action_yaml(pin_sha, action_repo_url):
    """Fetch action.yaml at the pinned commit. raw.githubusercontent first,
    git clone fallback (handles repos/refs raw does not serve)."""
    owner_repo = action_repo_url[len("https://github.com/"):]
    if owner_repo.endswith(".git"):
        owner_repo = owner_repo[: -len(".git")]
    url = "https://raw.githubusercontent.com/%s/%s/action.yaml" % (owner_repo, pin_sha)
    try:
        with urllib.request.urlopen(url, timeout=30) as resp:
            return resp.read().decode("utf-8"), "raw.githubusercontent"
    except Exception:
        pass
    with tempfile.TemporaryDirectory() as tmp:
        run_git(["clone", "--quiet", "--filter=blob:none", "--no-checkout",
                 action_repo_url, tmp])
        out = subprocess.run(["git", "show", "%s:action.yaml" % pin_sha],
                             cwd=tmp, capture_output=True, text=True)
        if out.returncode != 0:
            raise RuntimeError("cannot read action.yaml at %s: %s" % (pin_sha, out.stderr))
        return out.stdout, "git-clone"


def parse_workflow(raw):
    doc = yaml.safe_load(raw)
    uses = USES_RE.search(raw)
    if not uses:
        raise RuntimeError("no pinned %s uses: line found" % ACTION_REPO_NAME)
    pin_sha, tag_comment = uses.group(1), uses.group(2)
    with_keys = set()
    for job in (doc.get("jobs") or {}).values():
        for step in job.get("steps") or []:
            u = step.get("uses") or ""
            if u.startswith(ACTION_REPO_NAME + "@"):
                for k in (step.get("with") or {}):
                    with_keys.add(str(k))
    return doc, pin_sha, tag_comment, with_keys


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--workflow", default=".github/workflows/sonarcloud.yaml")
    ap.add_argument("--action-repo", default=ACTION_REPO_URL)
    ap.add_argument("--prev-pin", help="previous pin sha for the differential input check (C5)")
    ap.add_argument("--strict-inputs", action="store_true",
                    help="fail (instead of warn) on undeclared with: inputs (C3)")
    ap.add_argument("--report", default=None)
    args = ap.parse_args()

    checks = []  # (id, level, name, status, detail)

    def record(cid, level, name, ok, detail):
        checks.append({"id": cid, "level": level, "name": name,
                       "status": "pass" if ok else ("warn" if level == "warn" else "fail"),
                       "detail": detail})
        print("[%s] %s %s: %s" % ("PASS" if ok else ("WARN" if level == "warn" else "FAIL"),
                                  cid, name, detail))

    raw = open(args.workflow).read()

    # C1: YAML parses
    try:
        doc, pin_sha, tag_comment, with_keys = parse_workflow(raw)
        record("C1", "fail", "workflow YAML parses", True, "%d jobs, uses %s@%s (%s)"
               % (len(doc.get("jobs") or {}), ACTION_REPO_NAME, pin_sha[:12], tag_comment))
    except Exception as e:
        record("C1", "fail", "workflow YAML parses", False, str(e))
        finish(checks, args)

    # C2: pin matches upstream tag named in the comment
    try:
        tags = ls_remote_tags(args.action_repo)
        expected = tags.get(tag_comment)
        record("C2", "fail", "pin SHA is the commit of upstream tag %s" % tag_comment,
               expected == pin_sha,
               "tag %s -> %s, workflow pins %s" % (tag_comment, expected, pin_sha))
    except Exception as e:
        record("C2", "fail", "pin SHA is the commit of upstream tag %s" % tag_comment,
               False, "ls-remote failed: %s" % e)

    # action.yaml at the pinned commit
    try:
        action_raw, src = fetch_action_yaml(pin_sha, args.action_repo)
        action = yaml.safe_load(action_raw)
        declared = action.get("inputs") or {}
    except Exception as e:
        record("C4", "fail", "action.yaml readable at pin", False, str(e))
        finish(checks, args)

    # C3: with: inputs are declared (canary for inert inputs)
    undeclared = sorted(k for k in with_keys if k not in declared)
    record("C3", "fail" if args.strict_inputs else "warn",
           "all with: inputs declared by the action (canary: inert inputs)",
           not undeclared,
           "undeclared: %s (declared: %s) [action.yaml via %s]"
           % (undeclared or "none", sorted(declared), src))

    # C4: required inputs are supplied
    missing_required = sorted(
        k for k, v in declared.items()
        if v.get("required") and k not in with_keys and "default" not in (v or {})
    )
    record("C4", "fail", "all required action inputs supplied", not missing_required,
           "missing required: %s" % (missing_required or "none"))

    # C5: differential — no input used by the workflow was dropped across the bump
    if args.prev_pin:
        try:
            prev_raw, _ = fetch_action_yaml(args.prev_pin, args.action_repo)
            prev_declared = (yaml.safe_load(prev_raw).get("inputs") or {})
            dropped = sorted(k for k in with_keys
                             if k in prev_declared and k not in declared)
            record("C5", "fail", "no used input dropped between prev pin and head pin",
                   not dropped, "dropped: %s (prev declared: %s)" % (dropped or "none", sorted(prev_declared)))
        except Exception as e:
            record("C5", "fail", "no used input dropped between prev pin and head pin",
                   False, "prev action.yaml unreadable: %s" % e)

    # C6: docker image tag matches the version comment
    image = ((action.get("runs") or {}).get("image") or "")
    record("C6", "fail", "action docker image matches pin version",
           image.endswith(":" + tag_comment), "image: %s" % image)

    finish(checks, args)


def finish(checks, args):
    failed = [c for c in checks if c["status"] == "fail"]
    report = {
        "harness": "check_action_pin.py",
        "workflow": args.workflow,
        "checks": checks,
        "summary": {
            "fail": len(failed),
            "warn": len([c for c in checks if c["status"] == "warn"]),
            "pass": len([c for c in checks if c["status"] == "pass"]),
        },
    }
    path = args.report or "results/l1-report.json"
    import os
    os.makedirs(os.path.dirname(path) or ".", exist_ok=True)
    with open(path, "w") as f:
        json.dump(report, f, indent=2)
    print("report written: %s" % path)
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
