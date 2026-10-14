#!/usr/bin/env python3 -I
"""Contract harness for PR fluid-cloudnative/fluid#6210.

Bump of actions/upload-artifact from v7.0.1 (043fb46d...) to v7.0.2
(cf430e03...) in 3 GitHub workflow files.

Contract checks (all must PASS on correct code; a FAIL is the reproduction):

  C1 pin-matches-tag     every `actions/upload-artifact@<sha> # vX.Y.Z` line
                         pins exactly the commit that tag vX.Y.Z points to in
                         the upstream actions/upload-artifact repo. Catches
                         lookalike / mistyped SHAs (supply-chain guard).
  C2 version-consistency all references in the repo pin the same version.
  C3 yaml-parse          every workflow referencing the action parses as YAML.
  C4 input-compat        every `with:` key used at a call site is a declared
                         input of the pinned action version (fetched from the
                         tag's action.yml). Catches silent input removals.

Premise check (--premise, run against the BASE ref):
  P0  tag v7.0.2 exists upstream and differs from tag v7.0.1, i.e. the
      dependency the PR bumps to is a real, distinct release.

Requires: git, gh (authenticated), PyYAML.
"""
import argparse
import base64
import json
import re
import subprocess
import sys

ACTION_REPO = "actions/upload-artifact"
USES_RE = re.compile(
    r"uses:\s*actions/upload-artifact@([0-9a-f]{40})\s*#\s*(v[0-9A-Za-z.\-]+)"
)


def run(cmd):
    out = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if out.returncode != 0:
        raise RuntimeError(
            "cmd failed: %s\n%s" % (" ".join(cmd), out.stderr.decode()[:400])
        )
    return out.stdout.decode()


def gh_api(path):
    return json.loads(run(["gh", "api", path]))


def show_file(repo, ref, path):
    return run(["git", "-C", repo, "show", "%s:%s" % (ref, path)])


def workflow_files(repo, ref):
    listing = run(
        ["git", "-C", repo, "ls-tree", "-r", "--name-only", ref, "--", ".github/"]
    )
    out = []
    for path in listing.splitlines():
        if not path.startswith(".github/workflows/"):
            continue
        if not path.endswith((".yml", ".yaml")):
            continue
        try:
            content = show_file(repo, ref, path)
        except RuntimeError:
            continue  # deleted between refs
        if "actions/upload-artifact" in content:
            out.append((path, content))
    return out


def tag_commit(tag):
    """Resolve a tag to the commit it points at (handles annotated tags)."""
    obj = gh_api("repos/%s/git/ref/tags/%s" % (ACTION_REPO, tag))["object"]
    if obj["type"] == "tag":  # annotated: dereference one level
        obj = gh_api("repos/%s/git/tags/%s" % (ACTION_REPO, obj["sha"]))["object"]
    if obj["type"] != "commit":
        raise RuntimeError("tag %s does not point at a commit" % tag)
    return obj["sha"]


def action_inputs(tag):
    """Declared input names of the action at the given tag."""
    data = gh_api("repos/%s/contents/action.yml?ref=%s" % (ACTION_REPO, tag))
    import yaml

    spec = yaml.safe_load(base64.b64decode(data["content"]))
    return set((spec.get("inputs") or {}).keys())


def call_site_with_keys(content):
    """`with:` keys at each upload-artifact call site, via YAML parsing."""
    import yaml

    doc = yaml.safe_load(content)
    keys = []
    for job in (doc.get("jobs") or {}).values():
        for step in job.get("steps") or []:
            uses = step.get("uses") or ""
            if uses.startswith("actions/upload-artifact@"):
                keys.extend((step.get("with") or {}).keys())
    return keys


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True)
    ap.add_argument("--ref", required=True)
    ap.add_argument("--premise", action="store_true",
                    help="run the P0 premise check (tag existence/distinctness)")
    args = ap.parse_args()

    failures = []

    def record(cid, desc, ok, detail=""):
        line = "%s %-20s %s %s" % (cid, desc, "PASS" if ok else "FAIL",
                                   ("| " + detail) if detail else "")
        print(line)
        if not ok:
            failures.append(cid)

    files = workflow_files(args.repo, args.ref)
    record("C3", "yaml-parse", True, "(parsed below per file)")
    if not files:
        record("C3", "yaml-parse", False, "no upload-artifact workflows found")
    import yaml

    versions, tag_cache = {}, {}

    for path, content in files:
        # C3: parse
        try:
            yaml.safe_load(content)
            record("C3", "yaml-parse", True, path)
        except Exception as exc:
            record("C3", "yaml-parse", False, "%s: %s" % (path, exc))
            continue

        for m in USES_RE.finditer(content):
            sha, ver = m.group(1), m.group(2)
            versions.setdefault(ver, []).append(path)
            if ver not in tag_cache:
                try:
                    tag_cache[ver] = tag_commit(ver)
                except Exception as exc:
                    tag_cache[ver] = None
                    record("C1", "pin-matches-tag", False,
                           "%s: cannot resolve tag %s (%s)" % (path, ver, exc))
                    continue
            record("C1", "pin-matches-tag", tag_cache[ver] == sha,
                   "%s: pinned %s vs tag %s %s" % (path, sha[:12], ver,
                                                   tag_cache[ver][:12]))

            # C4: with-keys declared by the pinned version
            try:
                declared = action_inputs(ver)
            except Exception as exc:
                record("C4", "input-compat", False,
                       "%s: cannot fetch action.yml@%s (%s)" % (path, ver, exc))
                continue
            used = call_site_with_keys(content)
            missing = [k for k in used if k not in declared]
            record("C4", "input-compat", not missing,
                   "%s: used %s, declared-only diff %s" % (path, sorted(used), missing))

    # C2: single version across the repo
    record("C2", "version-consistency", len(versions) <= 1,
           "versions in use: %s" % (sorted(versions) or ["<none>"]))

    # P0 premise: v7.0.2 exists and is a distinct release from v7.0.1
    if args.premise:
        try:
            v702, v701 = tag_commit("v7.0.2"), tag_commit("v7.0.1")
            record("P0", "premise (tag exists)", v702 != v701,
                   "v7.0.1=%s v7.0.2=%s distinct release confirmed"
                   % (v701[:12], v702[:12]))
        except Exception as exc:
            record("P0", "premise (tag exists)", False, str(exc))

    print("SUMMARY: %d failure(s)%s" % (len(failures),
          (": " + ", ".join(failures)) if failures else ""))
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
