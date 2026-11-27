#!/usr/bin/env python3
"""Verification harness for PR #6296 (docs: English other_volumes + hdfs_configuration samples).

Reads file contents at arbitrary git refs via `git show <ref>:<path>` so the same
script can run against the PR base (premise reproduction) and the PR head
(contract checks). Pure docs checks; no cluster needed.

Exit code: 0 iff every check that is expected to pass at the given ref passes.
Polarity: all checks are CONTRACT tests (they assert the intended-correct
behavior). On the base ref, P0a/P0b are EXPECTED to fail (that failure is the
premise reproduction); use --expect-premise-fail for base runs.
"""
import argparse
import json
import re
import subprocess
import sys

import yaml

REPO_ROOT = subprocess.check_output(
    ["git", "rev-parse", "--show-toplevel"], text=True).strip()

ZH_OTHER = "docs/zh/samples/other_volumes.md"
EN_OTHER = "docs/en/samples/other_volumes.md"
ZH_HDFS = "docs/zh/samples/hdfs_configuration.md"
EN_HDFS = "docs/en/samples/hdfs_configuration.md"
TYPES_GO = "api/v1alpha1/alluxioruntime_types.go"
CONST_GO = "pkg/ddc/alluxio/const.go"


def show(ref, path):
    """Return file content at ref, or None if the file does not exist there."""
    p = subprocess.run(
        ["git", "show", "%s:%s" % (ref, path)],
        capture_output=True, text=True, cwd=REPO_ROOT)
    if p.returncode != 0:
        return None
    return p.stdout


def fenced_blocks(md):
    """Extract (lang, body) for every ``` fenced code block, in order."""
    blocks = []
    pattern = re.compile(r"```(\w*)\n(.*?)```", re.DOTALL)
    for m in pattern.finditer(md):
        blocks.append((m.group(1), m.group(2)))
    return blocks


def extract_dataset_yaml(md):
    """Find the fenced block that contains the Dataset manifest heredoc."""
    for lang, body in fenced_blocks(md):
        if "kind: Dataset" in body:
            return body
    return None


def heredoc_payload(block):
    """Strip the `cat << EOF > file.yaml` / `EOF` wrapper from a heredoc block."""
    lines = block.splitlines()
    out = []
    for ln in lines:
        if ln.strip().startswith("cat <<"):
            continue
        if ln.strip() == "EOF":
            continue
        out.append(ln)
    return "\n".join(out)


def check(name, ok, detail):
    results.append({"check": name, "ok": bool(ok), "detail": detail})
    print("%s %-38s %s" % ("PASS" if ok else "FAIL", name, detail))


def parse_docs(yaml_text):
    return list(yaml.safe_load_all(yaml_text))


results = []


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ref", default="HEAD",
                    help="git ref to read the docs from (base sha for premise runs)")
    ap.add_argument("--expect-premise-fail", action="store_true",
                    help="base-ref run: P0a/P0b MUST fail; a pass means the premise is refuted")
    args = ap.parse_args()
    ref = args.ref

    zh_other = show(ref, ZH_OTHER)
    en_other = show(ref, EN_OTHER)
    zh_hdfs = show(ref, ZH_HDFS)
    en_hdfs = show(ref, EN_HDFS)

    # ---- P0a: zh other_volumes dataset.yaml must be valid YAML ---------------
    premise_a_ok = None
    if zh_other is None:
        check("P0a-zh-yaml-parses", False, "%s missing at %s" % (ZH_OTHER, ref))
    else:
        block = extract_dataset_yaml(zh_other)
        payload = heredoc_payload(block)
        try:
            parse_docs(payload)
            premise_a_ok = True
            check("P0a-zh-yaml-parses", True, "zh dataset.yaml parses as YAML")
        except yaml.YAMLError as e:
            premise_a_ok = False
            check("P0a-zh-yaml-parses", False,
                  "yaml error: %s" % str(e).replace("\n", " | ")[:120])

    # ---- P0b: `kubectl get secret fluid-secret -oyaml` on a single line ------
    premise_b_ok = None
    if zh_other is not None:
        intact = "kubectl get secret fluid-secret -oyaml" in zh_other
        split = re.search(r"-oy\s*\n\s*aml", zh_other) is not None
        premise_b_ok = intact and not split
        check("P0b-oyaml-single-line", premise_b_ok,
              "command intact on one line" if premise_b_ok
              else "command is split across lines (intact=%s split=%s)" % (intact, split))

    # Premise polarity handling: on the base ref these two MUST fail.
    if args.expect_premise_fail:
        premise_confirmed = (premise_a_ok is False) and (premise_b_ok is False)
        check("P0-premise-confirmed-on-base", premise_confirmed,
              "both zh defects reproduce on base -> premise confirmed"
              if premise_confirmed else
              "zh defects do NOT reproduce on base -> premise refuted (a=%s b=%s)"
              % (premise_a_ok, premise_b_ok))

    if not args.expect_premise_fail:
        # ---- C1: code blocks identical between zh and en ----------------------
        for zh_text, en_text, tag in ((zh_other, en_other, "other_volumes"),
                                      (zh_hdfs, en_hdfs, "hdfs_configuration")):
            if zh_text is None or en_text is None:
                check("C1-codeblocks-%s" % tag, False,
                      "missing page (zh=%s en=%s)" % (zh_text is not None, en_text is not None))
                continue
            zb = fenced_blocks(zh_text)
            eb = fenced_blocks(en_text)
            same = zb == eb
            detail = "%d zh blocks vs %d en blocks" % (len(zb), len(eb))
            if not same and len(zb) == len(eb):
                diffs = [i for i, (a, b) in enumerate(zip(zb, eb)) if a != b]
                detail += "; differing block indexes: %s" % diffs
            check("C1-codeblocks-%s" % tag, same, detail)

        # ---- C2: en other_volumes manifest parses & nests correctly ----------
        if en_other is None:
            check("C2-en-yaml-structure", False, "en page missing at %s" % ref)
        else:
            payload = heredoc_payload(extract_dataset_yaml(en_other))
            try:
                docs = parse_docs(payload)
                ds = next(d for d in docs if d and d.get("kind") == "Dataset")
                rt = next(d for d in docs if d and d.get("kind") == "AlluxioRuntime")
                vm = rt["spec"]["master"]["volumeMounts"]
                vols = rt["spec"]["volumes"]
                ok = (isinstance(vm, list) and vm[0]["name"] == "secret-volume"
                      and isinstance(vols, list)
                      and vols[0]["secret"]["secretName"] == "fluid-secret"
                      and ds["metadata"]["name"] == rt["metadata"]["name"])
                check("C2-en-yaml-structure", ok,
                      "dataset+runtime parse; volumeMounts/volumes nested correctly")
            except Exception as e:  # noqa: BLE001 - report any parse/structure failure
                check("C2-en-yaml-structure", False,
                      "%s: %s" % (type(e).__name__, str(e)[:120]))

        # ---- C3: relative links in the new en pages resolve -------------------
        import os
        for en_text, path in ((en_other, EN_OTHER), (en_hdfs, EN_HDFS)):
            if en_text is None:
                check("C3-links-%s" % path.rsplit("/", 1)[-1], False, "page missing")
                continue
            links = re.findall(r"\[[^\]]*\]\(([^)]+)\)", en_text)
            rel = [l for l in links if not l.startswith(("http://", "https://", "#"))]
            bad = []
            for l in rel:
                target = os.path.normpath(os.path.join(os.path.dirname(path), l.split("#")[0]))
                if show(ref, target) is None:
                    bad.append(l)
            check("C3-links-%s" % path.rsplit("/", 1)[-1], not bad,
                  "relative links: %s; unresolved: %s" % (rel, bad or "none"))

        # ---- C4: API field claims used by the docs exist ----------------------
        types_go = show(ref, TYPES_GO) or ""
        const_go = show(ref, CONST_GO) or ""
        c4a = 'json:"hadoopConfig,omitempty"' in types_go
        c4b = 'json:"volumes,omitempty"' in types_go
        c4c = 'json:"volumeMounts,omitempty"' in types_go
        c4d = '"/hdfs-config"' in const_go
        check("C4-api-fields", c4a and c4b and c4c and c4d,
              "hadoopConfig=%s volumes=%s volumeMounts=%s /hdfs-config=%s"
              % (c4a, c4b, c4c, c4d))

    failed = [r for r in results if not r["ok"]]
    print("\nSUMMARY: %d/%d checks passed" % (len(results) - len(failed), len(results)))
    sys.exit(0 if not failed else 1)


if __name__ == "__main__":
    main()
