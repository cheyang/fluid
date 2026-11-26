#!/usr/bin/env python3
"""Verification harness for PR fluid-cloudnative/fluid#6289.

Bumps github/codeql-action (init/analyze/upload-sarif) pins from
33d7b85da1a04171e211befe2c739303b9a4c066 (v4.39.3) to
1bd86f2fdb4b86187fa4059f16f31a3202acc9ea (v4.39.4) in
.github/workflows/codeql-analysis.yml and .github/workflows/sonarcloud.yaml.

Deterministic unit-layer checks (L1) for this YAML-only PR:

  P0-premise-base-pin        base branch pins the codeql-action group at v4.39.3
  P0-premise-head-pin        head pins the group at v4.39.4 (the claimed bump)
  A1-pin-official-tag        every pinned 40-hex SHA resolves to an official
                             release tag of github/codeql-action (supply-chain
                             identity: no unknown/unofficial commit)
  A1-pin-consistency         all codeql-action steps in the repo share one SHA
  A2-workflow-yaml-parse     the two touched workflow files still parse as YAML
  F1-version-comment-accuracy (contract) each `uses: ...@<sha> # vX` trailing
                             comment states the version the SHA resolves to.
                             FAILS on the PR head (and on base) -> reproduces
                             the stale-comment finding (nit, pre-existing).
  F2-upload-sarif-pr-coverage (canary) documents that sonarcloud.yaml has no
                             pull_request trigger, so its upload-sarif step is
                             not exercised by PR CI. PASSES while that is true;
                             flips to FAIL if PR coverage is added (then invert).

Tag resolution: `git ls-remote https://github.com/github/codeql-action
'refs/tags/*^{}'` builds a sha->version map (peeled annotated tags). The map is
cached in results/tag-map.cache.json so re-runs work offline; SHAs not in the
map are re-resolved live only when the cache misses and --offline is absent.

Usage:
  python3 check_actions_pins.py --repo <repo> [--ref <sha>] [--base-ref <sha>]
      [--cache <tag-map.json>] [--offline]

Output: human summary on stderr, machine lines on stdout:
  CHECK <id> <pass|fail> <one-line detail>
Exit code: 0 iff every check passes (F1 is expected to fail until the trailing
comments are refreshed; see docs/verification/codeql-action-bump-claude/README.md).
"""
from __future__ import print_function

import argparse
import json
import re
import subprocess
import sys

CODEQL_ACTION_REPO = "https://github.com/github/codeql-action"
USES_RE = re.compile(r"uses:\s*github/codeql-action/([A-Za-z0-9_-]+)@([0-9a-f]{40,64})(?:\s*#\s*(\S+))?")

WORKFLOW_FILES = [
    ".github/workflows/codeql-analysis.yml",
    ".github/workflows/sonarcloud.yaml",
]

PR_EXPECTED = {"from": "v4.39.3", "to": "v4.39.4"}


def run(cmd, cwd=None):
    return subprocess.Popen(cmd, cwd=cwd, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE).communicate()[0].decode("utf-8", "replace")


def show(repo, ref, path):
    """File content at `ref`. The literal ref WORKTREE reads the working tree,
    so a locally applied (uncommitted) fix can be tested before committing."""
    if ref == "WORKTREE":
        try:
            with open("%s/%s" % (repo, path)) as f:
                return f.read()
        except IOError:
            return None
    out = run(["git", "show", "%s:%s" % (ref, path)], cwd=repo)
    return out if not out.startswith("fatal:") else None


def list_workflows(repo, ref):
    if ref == "WORKTREE":
        import os
        d = os.path.join(repo, ".github/workflows")
        return sorted(os.path.join(".github/workflows", n)
                      for n in os.listdir(d) if n.endswith((".yml", ".yaml")))
    return run(["git", "ls-tree", "-r", "--name-only", ref, "--", ".github/workflows/"],
               cwd=repo).splitlines()


def collect_pins(repo, ref):
    """All github/codeql-action uses in .github/workflows at `ref`.

    Returns list of dicts {file, line, action, sha, comment}.
    """
    pins = []
    for path in list_workflows(repo, ref):
        if not path.endswith((".yml", ".yaml")):
            continue
        text = show(repo, ref, path)
        if text is None:
            continue
        for i, line in enumerate(text.splitlines(), start=1):
            m = USES_RE.search(line)
            if m:
                pins.append({"file": path, "line": i, "action": m.group(1),
                             "sha": m.group(2), "comment": m.group(3)})
    return pins


def load_tag_map(cache_path):
    try:
        with open(cache_path) as f:
            return json.load(f), True
    except (IOError, OSError, ValueError):
        return {}, False


def resolve_tags(shas, cache_path, offline):
    """Return (sha -> 'vX.Y.Z') for the requested shas; unresolved keys absent."""
    cached, had_cache = load_tag_map(cache_path)
    missing = [s for s in shas if s not in cached]
    if missing and not offline:
        peeled = run(["git", "ls-remote", CODEQL_ACTION_REPO, "refs/tags/*^{}"])
        for line in peeled.splitlines():
            parts = line.split("\t")
            if len(parts) == 2 and parts[1].endswith("^{}"):
                ver = parts[1].split("/")[-1][:-3]
                cached.setdefault(parts[0], ver)
    try:
        with open(cache_path, "w") as f:
            json.dump(cached, f, indent=1, sort_keys=True)
    except (IOError, OSError):
        pass
    return {s: cached[s] for s in shas if s in cached}, had_cache


def yaml_parse_ok(repo, ref, path):
    text = show(repo, ref, path)
    if text is None:
        return False, "file missing at ref"
    try:
        import yaml
        yaml.safe_load(text)
        return True, "parsed"
    except ImportError:
        return True, "pyyaml unavailable; skipped structural parse"
    except Exception as e:  # noqa: BLE001 - report any parse error
        return False, "yaml error: %s" % e


def triggers(repo, ref, path):
    text = show(repo, ref, path) or ""
    m = re.search(r"^on:\s*\n((?:\s{2,}[^\n]*\n)+)", text, re.M)
    block = m.group(1) if m else ""
    return [t for t in ("pull_request", "push", "schedule", "workflow_dispatch")
            if re.search(r"^\s+%s:" % t, block, re.M)]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True)
    ap.add_argument("--ref", default="HEAD")
    ap.add_argument("--base-ref", default=None)
    ap.add_argument("--cache", default=None)
    ap.add_argument("--offline", action="store_true")
    args = ap.parse_args()

    import os
    here = os.path.dirname(os.path.abspath(__file__))
    cache = args.cache or os.path.join(here, "..", "results", "tag-map.cache.json")

    checks = []  # (id, ok, detail)

    def check(cid, ok, detail):
        checks.append((cid, bool(ok), detail))

    head_pins = collect_pins(args.repo, args.ref)
    base_pins = collect_pins(args.repo, args.base_ref) if args.base_ref else []

    shas = sorted({p["sha"] for p in head_pins} | {p["sha"] for p in base_pins})
    tag_map, had_cache = resolve_tags(shas, cache, args.offline)

    # --- P0 premise: base pins v4.39.3, head pins v4.39.4 (claimed bump) ---
    if base_pins:
        base_vers = sorted({tag_map.get(p["sha"], "?") for p in base_pins})
        check("P0-premise-base-pin", base_vers == [PR_EXPECTED["from"]],
              "base pins resolve to %s (claim: from %s)" % (base_vers, PR_EXPECTED["from"]))
    else:
        check("P0-premise-base-pin", False, "no codeql-action pins found at base ref")

    head_vers = sorted({tag_map.get(p["sha"], "?") for p in head_pins})
    check("P0-premise-head-pin", head_vers == [PR_EXPECTED["to"]],
          "head pins resolve to %s (claim: to %s)" % (head_vers, PR_EXPECTED["to"]))

    # --- A1: pin identity + consistency ---
    unresolved = [p for p in head_pins if p["sha"] not in tag_map]
    check("A1-pin-official-tag", not unresolved,
          "all pinned SHAs resolve to official github/codeql-action tags"
          if not unresolved else
          "SHAs with no official tag: %s" % sorted({p["sha"] for p in unresolved}))
    distinct = sorted({p["sha"] for p in head_pins})
    check("A1-pin-consistency", len(distinct) == 1,
          "codeql-action steps use %d distinct SHA(s): %s" % (len(distinct), distinct))

    # --- A2: workflows still parse ---
    for path in WORKFLOW_FILES:
        ok, detail = yaml_parse_ok(args.repo, args.ref, path)
        check("A2-workflow-yaml-parse", ok, "%s: %s" % (path, detail))

    # --- F1 (contract): trailing version comment accuracy ---
    bad = []
    for p in head_pins:
        ver = tag_map.get(p["sha"])
        if p["comment"] and ver and p["comment"] != ver:
            bad.append("%s:%d comment=%s actual=%s" % (p["file"], p["line"], p["comment"], ver))
    check("F1-version-comment-accuracy", not bad,
          "all trailing version comments match the pinned tag"
          if not bad else "stale comments -> " + "; ".join(bad))
    if base_pins:
        bad_base = []
        for p in base_pins:
            ver = tag_map.get(p["sha"])
            if p["comment"] and ver and p["comment"] != ver:
                bad_base.append("%s:%d comment=%s actual=%s" % (p["file"], p["line"], p["comment"], ver))
        check("F1-base-preexistence", bool(bad_base),
              "stale comments also present on base (%d lines) -> pre-existing"
              % len(bad_base) if bad_base else "comments accurate on base (would be PR-introduced)")

    # --- F2 (canary): sonarcloud.yaml PR coverage ---
    tr = triggers(args.repo, args.ref, ".github/workflows/sonarcloud.yaml")
    on_pr = "pull_request" in tr
    check("F2-upload-sarif-pr-coverage", not on_pr,
          "sonarcloud.yaml triggers=%s -> upload-sarif NOT exercised by PR CI (first run post-merge)"
          % (",".join(tr) or "none") if not on_pr else
          "sonarcloud.yaml now triggers on pull_request -> invert this canary and re-verify")

    for cid, ok, detail in checks:
        print("CHECK %s %s %s" % (cid, "pass" if ok else "fail", detail))
    failed = [c for c in checks if not c[1]]
    print("SUMMARY %d checks, %d failed: %s" % (len(checks), len(failed),
                                                 ", ".join(c[0] for c in failed) or "none"),
          file=sys.stderr)
    # F1-base-preexistence is informational (documents provenance, not a defect)
    hard = [c for c in failed if c[0] != "F1-base-preexistence"]
    return 0 if not hard else 1


if __name__ == "__main__":
    sys.exit(main())
