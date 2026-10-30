#!/usr/bin/env python3
"""Verification harness for PR #6222 (bump github/codeql-action 4.39.0 -> 4.39.1).

Emits go-test-style JSON events on stdout so scripts/re-verify.sh (which parses
`go test -json` output) can drive it as a `gotest` layer.

Layers:
  unit        -- offline structural checks on .github/workflows/*
  integration -- checks that resolve the pinned SHA against the real
                 github/codeql-action repository via `git ls-remote`

Polarity is recorded per test in the manifest:
  TestPinnedIsLatestV4Release      contract  (fails while the repo pins a
                                             release older than the current
                                             v4 tip; i.e. fails on the PR base)
  TestVersionCommentsMatchPins     canary    (passes while the stale
                                             `# v3.29.5` / `# v2.22.11`
                                             comments remain wrong; flips to
                                             fail once they are corrected)
  TestSonarcloudCodeqlNotOnPRs     canary    (passes while the sonarcloud
                                             upload-sarif step is never
                                             exercised by PR CI; flips to
                                             fail if PR coverage is added)
"""
import json
import re
import subprocess
import sys

CODEQL_REPO = "https://github.com/github/codeql-action.git"
USES_RE = re.compile(r"github/codeql-action/([A-Za-z0-9_-]+)@([0-9a-zA-Z]{7,40})(?:\s*#\s*v?([0-9]+\.[0-9]+\.[0-9]+))?")


def emit(action, test=None):
    ev = {"Action": action}
    if test:
        ev["Test"] = test
    # compact separators: re-verify.sh greps for "Action":"pass" (no space)
    sys.stdout.write(json.dumps(ev, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def say(test, text):
    # Diagnostics as go-test "output" events so the stream stays pure JSON:
    # re-verify.sh merges stderr into the captured output, which would break
    # its `jq` pass. jq's select(.Test!=null and pass|fail) ignores these.
    ev = {"Action": "output", "Output": text + "\n"}
    if test:
        ev["Test"] = test
    sys.stdout.write(json.dumps(ev, separators=(",", ":")) + "\n")
    sys.stdout.flush()


def run_test(name, fn):
    emit("start", name)
    try:
        detail = fn()
        emit("pass", name)
        if detail:
            say(name, "[ok] %s: %s" % (name, detail))
        return True
    except AssertionError as e:
        emit("fail", name)
        say(name, "[FAIL] %s: %s" % (name, e))
        return False


def workflow_files(repo):
    import glob
    return sorted(glob.glob(repo + "/.github/workflows/*.yml") +
                  glob.glob(repo + "/.github/workflows/*.yaml"))


def codeql_refs(repo):
    """[(file, action, sha, comment_version_or_None)] for every codeql-action step."""
    out = []
    for path in workflow_files(repo):
        with open(path) as f:
            for i, line in enumerate(f, 1):
                m = USES_RE.search(line)
                if m:
                    out.append((path, m.group(1), m.group(2), m.group(3)))
    return out


# ---------------- unit layer (offline) ----------------

def test_yaml_parses(repo):
    import yaml
    for path in workflow_files(repo):
        with open(path) as f:
            doc = yaml.safe_load(f)
        assert isinstance(doc, dict), path + " did not parse to a mapping"
        assert "jobs" in doc, path + " has no jobs key"
    return "%d workflow files parse as YAML" % len(workflow_files(repo))


def test_pins_consistent(repo):
    refs = codeql_refs(repo)
    assert refs, "no github/codeql-action steps found in any workflow"
    shas = set(r[2] for r in refs)
    assert len(shas) == 1, (
        "codeql-action steps pin different versions (%s); the dependabot.yml "
        "group requires all init/analyze/upload-sarif steps on the same version"
        % ", ".join(sorted(shas)))
    return "all %d codeql-action steps pin %s (%s)" % (
        len(refs), list(shas)[0][:12], ", ".join(sorted(set(r[1] for r in refs))))


def test_pins_are_full_shas(repo):
    refs = codeql_refs(repo)
    for path, action, sha, _ in refs:
        assert re.fullmatch(r"[0-9a-f]{40}", sha), (
            "%s: %s pinned by %r, expected a full 40-hex commit SHA "
            "(supply-chain pinning discipline used by this repo)" % (path, action, sha))
    return "%d pins are full commit SHAs" % len(refs)


# ---------------- integration layer (network) ----------------

def ls_remote():
    out = subprocess.check_output(
        ["git", "ls-remote", CODEQL_REPO], universal_newlines=True).strip()
    tags = {}      # peeled tag sha -> version
    for line in out.splitlines():
        sha, ref = line.split("\t")
        m = re.fullmatch(r"refs/tags/(v[0-9]+\.[0-9]+\.[0-9]+)\^\{\}", ref)
        if m:
            tags.setdefault(sha, m.group(1))
    tip = None
    for line in out.splitlines():
        sha, ref = line.split("\t")
        if ref == "refs/heads/releases/v4":
            tip = sha
    return tags, tip


def test_pin_matches_release_tag(repo, tags):
    refs = codeql_refs(repo)
    sha = refs[0][2]
    assert sha in tags, (
        "pinned codeql-action SHA %s does not resolve to any release tag of "
        "github/codeql-action (bump points at a non-release commit?)" % sha)
    return "pinned %s == tag %s" % (sha[:12], tags[sha])


def test_pinned_is_latest_v4(repo, tags, tip):
    refs = codeql_refs(repo)
    sha = refs[0][2]
    assert tip, "could not resolve refs/heads/releases/v4 on github/codeql-action"
    assert sha == tip, (
        "pinned codeql-action %s is not the tip of the v4 release line (%s); "
        "the bump is behind the current release" % (tags.get(sha, sha[:12]), tags.get(tip, tip[:12])))
    return "pinned %s is the current v4 tip" % tags[sha]


def test_version_comments_match_pins(repo, tags):
    # CANARY (bug-canary): asserts the CURRENT stale state. It passes while the
    # trailing `# vX.Y.Z` comments disagree with the version actually pinned,
    # and flips to fail once the comments are corrected. Invert it then.
    mismatches = []
    for path, action, sha, comment in codeql_refs(repo):
        if comment is None:
            continue
        actual = tags.get(sha)
        if actual and comment != actual.lstrip("v"):
            mismatches.append("%s: %s comment says v%s, pin is %s" %
                              (path.split("/")[-1], action, comment, actual))
    assert mismatches, (
        "stale-version-comment canary: expected the `# vX.Y.Z` comments to "
        "disagree with the pinned version (they are stale today); they now "
        "match -> the comments were fixed, invert this canary into a contract "
        "test")
    return "comments still stale: " + "; ".join(mismatches)


def test_sonarcloud_codeql_not_on_prs(repo):
    # CANARY (bug-canary): documents that the sonarcloud.yaml upload-sarif step
    # is only exercised on schedule + push to master, never on pull_requests.
    # Passes while that coverage gap exists; flips to fail if PR coverage is
    # added (then invert it).
    import yaml
    with open(repo + "/.github/workflows/sonarcloud.yaml") as f:
        doc = yaml.safe_load(f)
    triggers = doc.get(True) or doc.get("on") or {}
    assert "pull_request" not in triggers, (
        "sonarcloud PR-coverage canary: sonarcloud.yaml now triggers on "
        "pull_request -> the upload-sarif step is exercised by PR CI, invert "
        "this canary into a contract test")
    return "triggers=%s; upload-sarif not exercised by PR CI" % sorted(triggers.keys())


def main():
    repo = "."
    layer = "unit"
    args = sys.argv[1:]
    i = 0
    while i < len(args):
        if args[i] == "--repo":
            repo = args[i + 1]; i += 2
        elif args[i] == "--layer":
            layer = args[i + 1]; i += 2
        else:
            sys.stderr.write("unknown arg %r\n" % args[i]); sys.exit(2)

    ok = True
    if layer == "unit":
        for name, fn in [("TestWorkflowYamlParses", test_yaml_parses),
                         ("TestCodeqlPinsConsistent", test_pins_consistent),
                         ("TestPinsAreFullCommitShas", test_pins_are_full_shas)]:
            ok = run_test(name, lambda fn=fn: fn(repo)) and ok
    elif layer == "integration":
        tags, tip = ls_remote()
        say(None, "resolved %d release tags from %s; releases/v4 tip=%s"
            % (len(tags), CODEQL_REPO, (tip or "?")[:12]))
        for name, fn in [
                ("TestPinMatchesReleaseTag", lambda: test_pin_matches_release_tag(repo, tags)),
                ("TestPinnedIsLatestV4Release", lambda: test_pinned_is_latest_v4(repo, tags, tip)),
                ("TestVersionCommentsMatchPins", lambda: test_version_comments_match_pins(repo, tags)),
                ("TestSonarcloudCodeqlNotOnPRs", lambda: test_sonarcloud_codeql_not_on_prs(repo))]:
            ok = run_test(name, fn) and ok
    else:
        sys.stderr.write("unknown layer %r (unit|integration)\n" % layer)
        sys.exit(2)
    emit("pass")  # package-level
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
