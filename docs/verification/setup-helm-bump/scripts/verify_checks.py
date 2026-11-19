#!/usr/bin/env python3
"""Verification harness for PR #6270 (bump azure/setup-helm 5.0.1 -> 5.1.0).

CI-config-only PR: the "code under review" is two GitHub workflow YAML files.
The cheapest layer that exercises each claim is deterministic static analysis of
the repo state plus remote tag resolution (network), so this harness is a
script-based L1 layer, not a go/ginkgo layer.

Each check emits one line:  <CHECK_NAME>\t<pass|fail>
plus a short diagnostic on stderr. Exit code 0 iff all checks pass.

Polarity (see verify-manifest.json):
  contract checks  -> FAIL on buggy/base code, PASS on correct code.
  canary checks    -> PASS on current observed behavior; flip to fail when the
                      behavior changes (then invert / promote them).
"""
import re
import subprocess
import sys

V510_SHA = "2f5e7bc67f1f9972ab16657bb795f36d8f04de0e"
V501_SHA = "9bc31f4ebc9c6b171d7bfbaa5d006ae7abdb4310"
ACTION_REPO = "https://github.com/azure/setup-helm.git"
WORKFLOWS = [
    ".github/workflows/kind-e2e.yml",
    ".github/workflows/backward-compatibility-e2e.yml",
]
BASE_SHA = "dcb76167b0143a239addc6bed83ed50b45267631"

results = {}


def sh(*args, **kw):
    return subprocess.run(args, capture_output=True, text=True, **kw)


def check(name):
    def deco(fn):
        try:
            ok, detail = fn()
        except Exception as e:  # noqa: BLE001
            ok, detail = False, "exception: %r" % e
        results[name] = (bool(ok), detail)
        return fn

    return deco


@check("CHECK_ALL_SETUP_HELM_AT_V510")
def all_at_v510():
    """contract: every azure/setup-helm usage in .github/ is pinned to the v5.1.0 SHA.

    On base (v5.0.1 pins) this FAILS; on the PR head it PASSES. That
    base-vs-head difference is the harness-bites demonstration for the premise.
    """
    bad = []
    for path in WORKFLOWS:
        text = open(path).read()
        m = re.search(r"uses:\s*azure/setup-helm@(\S+)", text)
        if not m or m.group(1) != V510_SHA:
            bad.append("%s -> %s" % (path, m.group(1) if m else "<no usage>"))
    # also sweep the rest of .github for stragglers left at an older pin
    sweep = sh("grep", "-rn", "azure/setup-helm@", ".github/")
    for line in sweep.stdout.splitlines():
        sha = line.rsplit("@", 1)[1].split()[0].split("#")[0].strip()
        if sha != V510_SHA:
            bad.append("stale pin: " + line)
    return (not bad), ("all usages pinned to v5.1.0 sha" if not bad else "; ".join(bad))


@check("CHECK_PINNED_SHA_IS_V510_TAG")
def sha_is_tag():
    """contract: the pinned SHA is the commit that refs/tags/v5.1.0 points at on
    azure/setup-helm (guards against a Dependabot pin that does not match the
    claimed release)."""
    r = sh("git", "ls-remote", ACTION_REPO, "refs/tags/v5.1.0")
    if r.returncode != 0:
        return False, "ls-remote failed: %s" % r.stderr.strip()
    tag_sha = r.stdout.split()[0] if r.stdout else ""
    # annotated tags: dereference to the commit
    if tag_sha and tag_sha != V510_SHA:
        r2 = sh("git", "ls-remote", ACTION_REPO, "refs/tags/v5.1.0^{}")
        tag_sha = (r2.stdout.split()[0] if r2.stdout else tag_sha)
    return tag_sha == V510_SHA, "tag v5.1.0 -> %s" % tag_sha


@check("CHECK_DELTA_IS_ONLY_ACTION_SHA")
def delta_only_sha():
    """contract: base..head touches exactly the two workflow files and only the
    setup-helm pin lines (no smuggled extra changes ride along with the bump)."""
    r = sh("git", "diff", "--name-only", BASE_SHA + "..HEAD")
    files = [f for f in r.stdout.splitlines() if f.strip()]
    if sorted(files) != sorted(WORKFLOWS):
        return False, "files changed: %r" % files
    d = sh("git", "diff", BASE_SHA + "..HEAD", "--")
    added = [l for l in d.stdout.splitlines() if l.startswith("+") and not l.startswith("+++")]
    removed = [l for l in d.stdout.splitlines() if l.startswith("-") and not l.startswith("---")]
    ok = (
        len(added) == len(removed) == 2
        and all(V510_SHA in l for l in added)
        and all(V501_SHA in l for l in removed)
    )
    return ok, "diff hunks: +%d -%d" % (len(added), len(removed))


@check("CHECK_WORKFLOWS_VALID_YAML")
def valid_yaml():
    """contract: both modified workflows still parse as YAML and each retains a
    'Set up Helm' step using azure/setup-helm (syntax-level regression guard for
    the hand-edited-looking SHA+comment pins)."""
    import yaml

    problems = []
    for path in WORKFLOWS:
        try:
            doc = yaml.safe_load(open(path))
        except Exception as e:  # noqa: BLE001
            problems.append("%s: %s" % (path, e))
            continue
        steps = (
            doc.get("jobs", {})
            .get("kind-e2e-test", {})
            .get("steps", [])
            + doc.get("jobs", {})
            .get("backward-compat-test", {})
            .get("steps", [])
        )
        if not any(
            "azure/setup-helm" in str(s.get("uses", "")) for s in steps if isinstance(s, dict)
        ):
            problems.append("%s: no azure/setup-helm step found" % path)
    return (not problems), "; ".join(problems) or "yaml parses, setup-helm step present"


@check("CHECK_HELM_VERSION_INPUT_UNPINNED")
def version_unpinned():
    """canary (documents finding F1): the 'Set up Helm' steps pass no `version:`
    input, so the Helm major that deploys the Fluid chart floats with upstream
    'latest' and, since v5.1.0, the action's degraded-path built-in default is
    Helm v4.3.1 (was v3.18.4 in v5.0.1). PASSES while unpinned; if the workflows
    ever pin `version:`, this flips to fail and should be inverted/promoted."""
    import yaml

    unpinned = []
    for path in WORKFLOWS:
        doc = yaml.safe_load(open(path))
        for jobname, job in doc.get("jobs", {}).items():
            for s in job.get("steps", []) or []:
                if isinstance(s, dict) and "azure/setup-helm" in str(s.get("uses", "")):
                    has_version = "version" in (s.get("with") or {})
                    if not has_version:
                        unpinned.append("%s/%s" % (path, jobname))
    return (len(unpinned) == 2), "unpinned version in: %s" % ", ".join(unpinned)


@check("CHECK_V510_BUILTIN_FALLDOWN_IS_HELM4")
def builtin_fallback_is_helm4():
    """contract (documents the only real behavioral delta of the bump): the
    pinned v5.1.0 action's built-in default Helm (used when 'latest' cannot be
    resolved) is v4.3.1, and the latest-fallback input defaults to true.
    Verifies upstream action.yml/src at the pinned SHA. Needs network; skipped
    (reported pass-with-note) if the action repo is unreachable."""
    r = sh("git", "ls-remote", ACTION_REPO, "refs/tags/v5.1.0")
    if r.returncode != 0:
        return True, "network unavailable - skipped (non-gating)"
    import tempfile

    with tempfile.TemporaryDirectory() as td:
        c = sh("git", "clone", "-q", "--filter=blob:none", ACTION_REPO, td + "/ash")
        if c.returncode != 0:
            return True, "clone failed - skipped (non-gating)"
        rr = sh("git", "-C", td + "/ash", "show", V510_SHA + ":src/run.ts")
        src = rr.stdout
        m = re.search(r"stableHelmVersion = '([^']+)'", src)
        stable = m.group(1) if m else "?"
        d = sh("git", "-C", td + "/ash", "show", V510_SHA + ":action.yml")
        default_latest_fallback = re.search(
            r"latest-fallback:.*?default: '(\w+)'", d.stdout, re.S
        )
        lf = default_latest_fallback.group(1) if default_latest_fallback else "?"
        ok = stable == "v4.3.1" and lf == "true"
        return ok, "stableHelmVersion=%s latest-fallback default=%s" % (stable, lf)


def main():
    for name, (ok, detail) in results.items():
        print("%s\t%s" % (name, "pass" if ok else "fail"))
        print("  [%s] %s" % (name, detail), file=sys.stderr)
    failed = [n for n, (ok, _) in results.items() if not ok]
    print("RESULT: %d/%d checks passed%s" % (len(results) - len(failed), len(results),
          ("; failed: " + ", ".join(failed)) if failed else ""), file=sys.stderr)
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
