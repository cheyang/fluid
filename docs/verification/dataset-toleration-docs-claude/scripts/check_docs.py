#!/usr/bin/env python3
"""check_docs.py — verification harness for PR fluid-cloudnative/fluid#6300.

Docs-only PR. This harness mechanically checks the PR's claims and the
reviewer's contract expectations against any git ref:

Layer 1 (unit / static repo checks):
  statefulset-api-removed  no fenced YAML block in docs/{en,zh} declares
                           kind: StatefulSet with an apiVersion removed in
                           Kubernetes 1.16 (apps/v1beta1, apps/v1beta2,
                           extensions/v1beta1). The demo page itself displays
                           a v1.16.9 cluster, so such a manifest fails with
                           `no matches for kind "StatefulSet" in version ...`.
  en-zh-codeblock-parity   the fenced code blocks of
                           docs/en/samples/data_toleration.md and
                           docs/zh/samples/data_toleration.md are identical
                           after newline normalization (the PR's own
                           verification claim).
  toc-entry-valid          docs/en/TOC.md links samples/data_toleration.md,
                           the target file exists at the ref, and the entry
                           sits right after data_co_locality.md exactly like
                           the Chinese TOC.
  no-stray-taint-line      every `kubectl taint` line in the zh page is
                           inside a fenced code block (the base branch had a
                           bare stray line naming a nonexistent `node1`).

Layer 2 (integration / manifest structural validation):
  manifests-parse          every fenced YAML manifest on the en page parses.
  statefulset-selector     the apps/v1 StatefulSet declares the
                           spec.selector.matchLabels that apps/v1 requires.
  dataset-toleration-shape the Dataset spec.tolerations entries match the
                           shape of corev1 Toleration (Equal => value set).
  api-fields-current       the field names the page uses still exist in the
                           shipped API types at the same ref:
                           DatasetSpec.tolerations, AlluxioRuntime
                           spec.replicas / spec.tieredstore.

Usage:  python3 check_docs.py <git-ref> [--repo <path>] [--layer unit|integration]
Prints `CHECK<TAB>pass|fail` lines plus human detail; exit 1 iff any check in
the selected layer(s) fails. All checks are CONTRACT polarity: they assert the
intended correct state, so they fail on the base branch and pass on the PR
head (see results/ in this directory).
"""
import argparse
import re
import subprocess
import sys

import yaml  # PyYAML; only needed for the integration layer

EN_PAGE = "docs/en/samples/data_toleration.md"
ZH_PAGE = "docs/zh/samples/data_toleration.md"
EN_TOC = "docs/en/TOC.md"
ZH_TOC = "docs/zh/TOC.md"
REMOVED_STATEFULSET_APIS = {"apps/v1beta1", "apps/v1beta2", "extensions/v1beta1"}


def git(repo, *args, **kw):
    out = subprocess.check_output(["git", "-C", repo] + list(args), stderr=kw.get("stderr", subprocess.PIPE))
    return out.decode("utf-8", errors="replace")


def show(repo, ref, path):
    try:
        return git(repo, "show", "%s:%s" % (ref, path), stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError:
        return None


def ls_docs(repo, ref):
    out = git(repo, "ls-tree", "-r", "--name-only", ref, "docs/")
    return [p for p in out.splitlines() if p.endswith(".md")]


def fenced_blocks(text):
    """Yield (in_fence, block_text) — normalized fences, CRLF-safe."""
    text = text.replace("\r\n", "\n")
    blocks, cur, in_fence = [], [], False
    for line in text.split("\n"):
        if line.strip().startswith("```"):
            if in_fence:
                blocks.append("\n".join(cur))
                cur, in_fence = [], False
            else:
                cur, in_fence = [], True
            continue
        if in_fence:
            cur.append(line)
    return [b for b in blocks if b.strip()]


def yaml_docs_from_blocks(blocks):
    """K8s manifests from fenced blocks.

    Handles both a bare manifest block (starts with `apiVersion:`) and the
    shell-heredoc form used throughout the sample docs:
        $ cat<<EOF >app.yaml
        apiVersion: ...
        ...
        EOF
    """
    docs = []
    for b in blocks:
        for s in _manifest_texts(b):
            try:
                docs.append(yaml.safe_load(s))
            except yaml.YAMLError as e:
                docs.append({"__parse_error__": str(e), "__raw__": s[:120]})
    return docs


def _manifest_texts(block):
    """Yield candidate manifest strings from one fenced block."""
    lines = block.split("\n")
    # heredoc form: content between `cat<<EOF` (any name) and the terminator
    starts = [i for i, l in enumerate(lines) if re.search(r"<<\s*\w+\s*>?\s*\S*", l) and "cat" in l]
    out = []
    for i in starts:
        m = re.search(r"<<\s*(\w+)", lines[i])
        term = m.group(1) if m else "EOF"
        body = []
        for l in lines[i + 1:]:
            if l.strip() == term:
                break
            body.append(l)
        out.append("\n".join(body))
    # bare manifest form
    if not out and block.strip().startswith("apiVersion:"):
        out.append(block.strip())
    return [s for s in out if s.strip().startswith("apiVersion:")]


def check(repo, ref, layer):
    results = []  # (name, ok, detail)

    def record(name, ok, detail):
        results.append((name, bool(ok), detail))

    # ---------------- Layer 1: static checks ----------------
    if layer in ("unit", "all"):
        # L1.1 removed StatefulSet apiVersions anywhere in docs
        bad = []
        for path in ls_docs(repo, ref):
            if "/vendor/" in path:
                continue
            text = show(repo, ref, path)
            if text is None:
                continue
            for doc in yaml_docs_from_blocks(fenced_blocks(text)):
                if isinstance(doc, dict) and doc.get("kind") == "StatefulSet":
                    api = doc.get("apiVersion")
                    if api in REMOVED_STATEFULSET_APIS:
                        bad.append("%s (apiVersion %s)" % (path, api))
        record("statefulset-api-removed", not bad,
               "StatefulSet manifests with k8s-1.16-removed apiVersions: %s" % (bad or "none"))

        # L1.2 en/zh code-block parity
        en, zh = show(repo, ref, EN_PAGE), show(repo, ref, ZH_PAGE)
        if en is None:
            record("en-zh-codeblock-parity", False, "%s absent at ref" % EN_PAGE)
        elif zh is None:
            record("en-zh-codeblock-parity", False, "%s absent at ref" % ZH_PAGE)
        else:
            eb, zb = fenced_blocks(en), fenced_blocks(zh)
            same = eb == zb
            detail = "%d vs %d blocks, identical=%s" % (len(eb), len(zb), same)
            if not same:
                for i in range(max(len(eb), len(zb))):
                    a = eb[i] if i < len(eb) else "<missing>"
                    b = zb[i] if i < len(zb) else "<missing>"
                    if a != b:
                        detail += "; first mismatch at block %d" % i
                        break
            record("en-zh-codeblock-parity", same, detail)

        # L1.3 TOC entry exists, resolves, and ordering matches zh TOC
        toc, zh_toc = show(repo, ref, EN_TOC), show(repo, ref, ZH_TOC)
        entry = re.search(r"\[([^\]]+)\]\(samples/data_toleration\.md\)", toc or "")
        target_exists = show(repo, ref, EN_PAGE) is not None
        # ordering: both TOCs list data_toleration immediately after data_co_locality
        def order_ok(text):
            if text is None:
                return False
            lines = [l for l in text.split("\n") if "data_co_locality.md" in l or "data_toleration.md" in l]
            return len(lines) == 2 and "data_co_locality.md" in lines[0] and "data_toleration.md" in lines[1]
        ok = bool(entry) and target_exists and order_ok(toc) and order_ok(zh_toc)
        record("toc-entry-valid", ok,
               "en TOC entry=%s, file exists=%s, en order ok=%s, zh order ok=%s"
               % (bool(entry), target_exists, order_ok(toc), order_ok(zh_toc)))

        # L1.4 no unfenced `kubectl taint` line in the zh page
        zh2 = show(repo, ref, ZH_PAGE)
        if zh2 is None:
            record("no-stray-taint-line", False, "%s absent at ref" % ZH_PAGE)
        else:
            text = zh2.replace("\r\n", "\n")
            in_fence, stray = False, []
            for lineno, line in enumerate(text.split("\n"), 1):
                if line.strip().startswith("```"):
                    in_fence = not in_fence
                    continue
                if not in_fence and "kubectl taint" in line:
                    stray.append("line %d: %s" % (lineno, line.strip()))
            record("no-stray-taint-line", not stray, "stray taint lines: %s" % (stray or "none"))

    # ---------------- Layer 2: manifest validation ----------------
    if layer in ("integration", "all"):
        en = show(repo, ref, EN_PAGE)
        if en is None:
            for name in ("manifests-parse", "statefulset-selector", "dataset-toleration-shape"):
                record(name, False, "%s absent at ref" % EN_PAGE)
        else:
            docs = yaml_docs_from_blocks(fenced_blocks(en))
            parse_errs = [d for d in docs if "__parse_error__" in d]
            record("manifests-parse", not parse_errs and len(docs) >= 3,
                   "%d manifests parsed, %d parse errors" % (len(docs), len(parse_errs)))

            sts = [d for d in docs if isinstance(d, dict) and d.get("kind") == "StatefulSet"]
            ok = bool(sts) and all(
                d.get("apiVersion") == "apps/v1"
                and isinstance(d.get("spec", {}).get("selector", {}).get("matchLabels"), dict)
                and d["spec"]["selector"]["matchLabels"]
                for d in sts)
            record("statefulset-selector", ok,
                   "StatefulSet count=%d, all apps/v1 with non-empty spec.selector.matchLabels=%s"
                   % (len(sts), ok))

            ds = [d for d in docs if isinstance(d, dict) and d.get("kind") == "Dataset"]
            tol_ok = False
            detail = "no Dataset manifest found"
            if ds:
                tol = ds[0].get("spec", {}).get("tolerations") or []
                tol_ok = bool(tol) and all(
                    isinstance(t, dict) and t.get("key") and t.get("operator") == "Equal" and t.get("value") is not None
                    for t in tol)
                detail = "tolerations entries=%d, all match corev1 Toleration(Equal=>value)=%s" % (len(tol), tol_ok)
            record("dataset-toleration-shape", tol_ok, detail)

        # L2.4 doc field names vs the shipped API types at the same ref
        dsts = show(repo, ref, "api/v1alpha1/dataset_types.go") or ""
        arts = show(repo, ref, "api/v1alpha1/alluxioruntime_types.go") or ""
        ok = ('Tolerations []v1.Toleration `json:"tolerations,omitempty"`' in dsts
              and 'json:"replicas,omitempty"' in arts
              and 'json:"tieredstore,omitempty"' in arts)
        record("api-fields-current", ok,
               "DatasetSpec.tolerations / AlluxioRuntime replicas+tieredstore present in API types at ref: %s" % ok)

    return results


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("ref")
    ap.add_argument("--repo", default=".")
    ap.add_argument("--layer", default="all", choices=["unit", "integration", "all"])
    args = ap.parse_args()

    results = check(args.repo, args.ref, args.layer)
    failed = [r for r in results if not r[1]]

    print("ref=%s layer=%s" % (args.ref, args.layer))
    for name, ok, detail in results:
        print("CHECK\t%s\t%s\t%s" % (name, "pass" if ok else "fail", detail))
    print("SUMMARY\t%d checks, %d failed" % (len(results), len(failed)))
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
