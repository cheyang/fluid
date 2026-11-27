#!/usr/bin/env python3
"""doc_checks.py — verification harness for PR #6295 (docs: add the English
metadata sync policy sample).

Checks (contract polarity: PASS means the English doc is accurate):
  TestCodeBlockParityEnZh   all fenced code blocks in docs/en/samples/metadata_sync.md
                            are identical (modulo trailing whitespace) to the Chinese
                            source docs/zh/samples/metadata_sync.md.
  TestRelativeLinksResolve  every relative link in the English doc resolves to a file
                            that exists in the repo.
  TestTOCEntryPresent       docs/en/TOC.md lists samples/metadata_sync.md (under
                            Advanced Configuration, mirroring the zh TOC) and the
                            target file exists.
  TestAPIFieldMatchesDoc    the documented spec path management.metadataSyncPolicy.autoSync
                            matches api/v1alpha1/common.go (RuntimeManagement ->
                            MetadataSyncPolicy -> AutoSync *bool, json tag autoSync).

Emits one JSON object per line in `go test -json` event shape so the standard
re-verify.sh machinery can parse it. Exit code 0 iff all checks pass.
"""
import argparse
import json
import os
import re
import sys

FAILED = []


def emit(action, test, output=None):
    obj = {"Action": action, "Test": test}
    if output is not None:
        obj["Output"] = [output]
    sys.stdout.write(json.dumps(obj) + "\n")


def report(test, ok, detail):
    emit("run", test)
    if ok:
        emit("pass", test, detail)
    else:
        emit("fail", test, detail)
        FAILED.append(test)


def read_file(path):
    with open(path, "r", encoding="utf-8") as f:
        return f.read()


def fenced_blocks(text):
    return [b.rstrip() for b in re.findall(r"```[^\n]*\n(.*?)```", text, re.S)]


def check_code_block_parity(en_path, zh_path):
    en, zh = read_file(en_path), read_file(zh_path)
    en_blocks, zh_blocks = fenced_blocks(en), fenced_blocks(zh)
    if not en_blocks:
        return False, "no fenced code blocks found in %s" % en_path
    if len(en_blocks) != len(zh_blocks):
        return False, "block count differs: en=%d zh=%d" % (len(en_blocks), len(zh_blocks))
    for i, (a, b) in enumerate(zip(en_blocks, zh_blocks)):
        if a != b:
            return False, "block #%d differs between en and zh" % (i + 1)
    return True, "all %d code blocks identical (trailing whitespace ignored)" % len(en_blocks)


def check_relative_links(en_path, repo_root):
    text = read_file(en_path)
    base = os.path.dirname(os.path.realpath(en_path))
    targets = re.findall(r"\]\(([^)]+)\)", text)
    seen, broken = set(), []
    for t in targets:
        if t.startswith(("http://", "https://", "mailto:", "#")):
            continue
        path = t.split("#")[0]
        if not path or path in seen:
            continue
        seen.add(path)
        resolved = os.path.normpath(os.path.join(base, path))
        if not os.path.exists(resolved):
            broken.append(t)
    if broken:
        return False, "broken relative links: %s" % ", ".join(sorted(broken))
    return True, "all %d relative link(s) resolve: %s" % (len(seen), ", ".join(sorted(seen)) or "(none)")


def check_toc_entry(toc_path, repo_root):
    toc = read_file(toc_path)
    entry = re.search(r"^\s*-\s*\[.*\]\((\./)?samples/metadata_sync\.md\)\s*$", toc, re.M)
    if not entry:
        return False, "docs/en/TOC.md has no entry pointing at samples/metadata_sync.md"
    target = os.path.join(os.path.dirname(os.path.realpath(toc_path)), "samples", "metadata_sync.md")
    if not os.path.exists(target):
        return False, "TOC entry exists but target file missing: %s" % target
    # the entry sits under an "Advanced Configuration" section, mirroring zh 高级配置
    idx = toc.find(entry.group(0))
    section = re.findall(r"^\s*\+\s*(.+)$", toc[:idx], re.M)
    if not section or section[-1].strip() != "Advanced Configuration":
        return False, "entry is not under the 'Advanced Configuration' section (last section: %r)" % (
            section[-1].strip() if section else "(none)")
    return True, "TOC lists samples/metadata_sync.md under Advanced Configuration; target exists"


def check_api_field(api_path):
    src = read_file(api_path)
    ok_runtime = re.search(r"MetadataSyncPolicy\s+MetadataSyncPolicy\s+`json:\"metadataSyncPolicy", src) is not None
    ok_auto = re.search(r"AutoSync\s+\*bool\s+`json:\"autoSync", src) is not None
    ok_struct = re.search(r"type\s+(RuntimeManagement|MetadataSyncPolicy)\s+struct", src) is not None
    if not (ok_runtime and ok_auto and ok_struct):
        return False, "api/v1alpha1/common.go does not define management.metadataSyncPolicy.autoSync (runtime=%s autosync=%s)" % (
            ok_runtime, ok_auto)
    return True, "api/v1alpha1/common.go defines RuntimeManagement.MetadataSyncPolicy and MetadataSyncPolicy.AutoSync (*bool)"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo-root", default=".")
    ap.add_argument("--en-doc", default="docs/en/samples/metadata_sync.md")
    ap.add_argument("--zh-doc", default="docs/zh/samples/metadata_sync.md")
    ap.add_argument("--toc", default="docs/en/TOC.md")
    ap.add_argument("--api", default="api/v1alpha1/common.go")
    args = ap.parse_args()
    root = args.repo_root

    def p(rel):
        return os.path.join(root, rel)

    for rel in (args.en_doc, args.zh_doc, args.toc, args.api):
        if not os.path.exists(p(rel)):
            sys.stderr.write("doc_checks: missing input %s\n" % p(rel))
            return 2

    report("TestCodeBlockParityEnZh", *check_code_block_parity(p(args.en_doc), p(args.zh_doc)))
    report("TestRelativeLinksResolve", *check_relative_links(p(args.en_doc), root))
    report("TestTOCEntryPresent", *check_toc_entry(p(args.toc), root))
    report("TestAPIFieldMatchesDoc", *check_api_field(p(args.api)))

    if FAILED:
        sys.stderr.write("doc_checks: FAILED: %s\n" % ", ".join(FAILED))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
