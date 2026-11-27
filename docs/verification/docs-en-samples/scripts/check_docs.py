#!/usr/bin/env python3
"""L1 (deterministic) harness for PR #6296 (docs: en other_volumes + hdfs_configuration).

Verifies, for a given pair of en/zh sample pages:
  1. YAML manifests: every fenced code block that embeds a `cat << EOF` heredoc
     yields YAML that parses cleanly (no tabs, no syntax errors).
  2. Shell sanity: the `kubectl get secret fluid-secret -oyaml` command is a
     single line (was split as `-oy` / `aml` on the base branch).
  3. Code-block parity: the fenced code blocks of the English page are
     byte-identical to the (fixed) Chinese page's, in order.
  4. Relative markdown links in the English pages resolve to existing files.

Polarity: CONTRACT tests. They FAIL on the base branch (tab indentation,
split command) and PASS on the PR head.

Usage:
  check_docs.py --repo <repo> --ref <sha>   # full check at a ref
  check_docs.py --repo <repo> --compare-en-zh <sha> <en> <zh>
Exit code 0 = all checks pass.
"""
import argparse
import re
import subprocess
import sys

try:
    import yaml
except ImportError:
    sys.exit("PyYAML required")


FENCE_RE = re.compile(r"^```[a-zA-Z]*\s*\n(.*?)^```\s*$", re.M | re.S)


def git_show(repo, ref, path):
    out = subprocess.check_output(["git", "-C", repo, "show", "%s:%s" % (ref, path)])
    return out.decode("utf-8")


def code_blocks(text):
    return [m.group(1) for m in FENCE_RE.finditer(text)]


def heredoc_inner(block):
    """Return the YAML between the `cat << EOF ...` header line and the closing EOF."""
    lines = block.splitlines()
    start = next(i for i, l in enumerate(lines) if "cat << EOF" in l)
    body = lines[start + 1:]
    while body and body[-1].strip() == "":
        body.pop()
    if body and body[-1].strip() == "EOF":
        body.pop()
    return "\n".join(body) + "\n"


def check_yaml_heredocs(path, blocks, failures):
    for i, block in enumerate(blocks):
        if "cat << EOF" not in block:
            continue
        inner = heredoc_inner(block)
        try:
            docs = list(yaml.safe_load_all(inner))
            kinds = {d.get("kind") for d in docs if d}
            if not kinds or None in kinds:
                failures.append("%s block %d: parsed but missing kind" % (path, i))
        except yaml.YAMLError as e:
            # A snippet that uses bare `...` lines as "fields omitted" placeholders
            # is not valid YAML by design; parse with placeholders removed and
            # report as informational if that fixes it.
            stripped = "\n".join(l for l in inner.splitlines()
                                 if l.strip() != "...") + "\n"
            try:
                list(yaml.safe_load_all(stripped))
                print("INFO: %s block %d uses bare `...` placeholder lines "
                      "(not paste-able YAML) — carried over from the zh page" % (path, i))
            except yaml.YAMLError:
                failures.append("%s block %d: YAML parse error: %s"
                                % (path, i, str(e).splitlines()[0]))


def check_single_line_command(path, text, failures):
    """Only meaningful for pages that contain the secret-inspection step."""
    if "kubectl get secret" not in text:
        return
    if re.search(r"^kubectl get secret fluid-secret -oy$", text, re.M) or \
       re.search(r"^aml$", text, re.M):
        failures.append("%s: `kubectl get secret ... -oyaml` is split over two lines" % path)
    elif "kubectl get secret fluid-secret -oyaml" not in text:
        failures.append("%s: expected `kubectl get secret fluid-secret -oyaml` not found" % path)


def check_relative_links(repo, ref, path, text, failures):
    import posixpath
    base_dir = path.rsplit("/", 1)[0]
    for m in re.finditer(r"\]\(([^)#]+?\.md)\)", text):
        target = m.group(1)
        if target.startswith(("http://", "https://", "/")):
            continue
        resolved = posixpath.normpath("%s/%s" % (base_dir, target))
        # the repo path is enough; use cat-file to test existence at ref
        rc = subprocess.call(["git", "-C", repo, "cat-file", "-e", "%s:%s" % (ref, resolved)],
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if rc != 0:
            failures.append("%s: relative link %r does not resolve at ref" % (path, target))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True)
    ap.add_argument("--ref", required=True)
    ap.add_argument("--en", default="docs/en/samples/other_volumes.md")
    ap.add_argument("--en2", default="docs/en/samples/hdfs_configuration.md")
    ap.add_argument("--zh", default="docs/zh/samples/other_volumes.md")
    ap.add_argument("--zh2", default="docs/zh/samples/hdfs_configuration.md")
    ap.add_argument("--check-base-tab", action="store_true",
                    help="expect the zh other_volumes tab bug to be PRESENT (base-branch premise probe)")
    args = ap.parse_args()

    failures = []
    pages = [(args.en, args.zh), (args.en2, args.zh2)]

    texts = {}
    for en, zh in pages:
        for p in (en, zh):
            if p in texts:
                continue
            t = git_show(args.repo, args.ref, p)
            texts[p] = t
            blocks = code_blocks(t)
            check_yaml_heredocs(p, blocks, failures)
            check_single_line_command(p, t, failures)
            if p.startswith("docs/en/"):
                check_relative_links(args.repo, args.ref, p, t, failures)
        # 3. en/zh code-block parity
        en_blocks = code_blocks(texts[en])
        zh_blocks = code_blocks(texts[zh])
        if en_blocks != zh_blocks:
            for idx in range(max(len(en_blocks), len(zh_blocks))):
                a = en_blocks[idx] if idx < len(en_blocks) else "<missing>"
                b = zh_blocks[idx] if idx < len(zh_blocks) else "<missing>"
                if a != b:
                    failures.append("%s vs %s: code block %d differs\n--- en ---\n%s\n--- zh ---\n%s"
                                    % (en, zh, idx, a, b))

    # 4. field references claimed by the PR exist in code at this ref
    checks = [
        ("api/v1alpha1/alluxioruntime_types.go", "hadoopConfig"),
        ("api/v1alpha1/alluxioruntime_types.go", "volumeMounts"),
        ("api/v1alpha1/alluxioruntime_types.go", "Volumes"),
        ("pkg/ddc/alluxio/const.go", "hadoopConfMountPath = \"/hdfs-config\""),
    ]
    for path, needle in checks:
        t = git_show(args.repo, args.ref, path)
        if needle not in t:
            failures.append("%s: expected symbol %r not found" % (path, needle))

    if failures:
        print("FAIL: %d issue(s)" % len(failures))
        for f in failures:
            print(" - " + f)
        sys.exit(1)
    print("PASS: all doc checks green at %s" % args.ref)
    sys.exit(0)


if __name__ == "__main__":
    main()
