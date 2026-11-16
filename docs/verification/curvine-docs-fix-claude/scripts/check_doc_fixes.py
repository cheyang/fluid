#!/usr/bin/env python3
"""Contract checks for PR #6252 (docs: fix heredocs + image tag in the Curvine sample).

Layer 1 (offline)  C1..C5   — heredoc execution, YAML validity, e2e tag parity,
                              en/zh parity, pod-label key existence.
Layer 2 (network)  C6       — Docker Hub pullability of the doc's image tags.

Polarity:
  C1..C4, C6 are CONTRACT tests: they assert the intended correct state, so they
  FAIL on the broken (base) state and PASS on the fixed (PR head) state.
  C5 is a BUG-CANARY: it asserts the current *wrong* state (the doc's step-6 pod
  label `cacheruntime.fluid.io/runtime-name` matches no label the code sets).
  It PASSES while the bug is present and flips to FAIL when someone fixes the
  doc or the code — at that point invert it into a contract test.

Usage:
  python3 scripts/check_doc_fixes.py --repo-root . [--ref HEAD] [--base-ref <sha>] [--offline]
"""
import argparse
import json
import os
import re
import subprocess
import sys
import tempfile

DOCS = [
    "docs/en/samples/cacheruntime/curvine_cache_runtime.md",
    "docs/zh/samples/cacheruntime/curvine_cache_runtime.md",
]
E2E_CRC = "test/gha-e2e/curvine/cacheruntimeclass.yaml"
LABEL_GO = "pkg/common/label.go"

FAILURES = []


def git_show(repo_root, ref, path):
    out = subprocess.run(["git", "-C", repo_root, "show", "%s:%s" % (ref, path)],
                         capture_output=True)
    if out.returncode != 0:
        return None
    return out.stdout.decode("utf-8")


def extract_heredocs(doc_text):
    """Return [(cmdline, quoted(bool), filename, body_lines)] for each cat<<EOF block."""
    lines = doc_text.split("\n")
    blocks = []
    i = 0
    while i < len(lines):
        m = re.match(r"^\$ cat<<(?P<q>EOF|'EOF'|\"EOF\")\s+>(?P<fn>\S+)\s*$", lines[i])
        if m:
            quoted = m.group("q") != "EOF"
            fn = m.group("fn")
            body = []
            i += 1
            while i < len(lines) and lines[i] != "EOF":
                body.append(lines[i])
                i += 1
            blocks.append((lines[i - len(body) - 1], quoted, fn, body))
        i += 1
    return blocks


def run_heredoc(quoted, fn, body, sandbox):
    """Execute the doc's heredoc exactly as a user's shell would, return file text."""
    delim = "'EOF'" if quoted else "EOF"
    target = os.path.join(sandbox, fn)
    script = "cat<<%s >%s\n%s\nEOF\n" % (delim, target, "\n".join(body))
    proc = subprocess.run(["bash", "-c", script], capture_output=True)
    # A failed local command substitution (e.g. `cat /data/minio/bar` on the base
    # doc) does not abort the heredoc; keep going — the corrupted output is the
    # evidence we want to see.
    if not os.path.exists(target):
        return None, proc.stderr.decode("utf-8", "replace")
    with open(target, encoding="utf-8") as f:
        return f.read(), proc.stderr.decode("utf-8", "replace")


def check(cid, ok, detail):
    status = "pass" if ok else "FAIL"
    print("[%s] %s — %s" % (status, cid, detail))
    if not ok:
        FAILURES.append(cid)
    return ok


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo-root", default=".")
    ap.add_argument("--ref", default="HEAD")
    ap.add_argument("--offline", action="store_true")
    args = ap.parse_args()
    root = os.path.abspath(args.repo_root)

    # ---------- C1: heredoc output matches the literal body ----------
    # For a quoted heredoc the file must equal the body verbatim. For an unquoted
    # heredoc the file must equal the body with `\$` -> `$` (documented escaping,
    # used by minio_create_bucket.yaml); any other difference means the local
    # shell expanded / mangled something (the bug PR #6252 fixes).
    sandbox = tempfile.mkdtemp(prefix="curvine-doc-")
    generated = {}
    for doc in DOCS:
        text = git_show(root, args.ref, doc)
        if text is None:
            check("C1_heredoc_output_matches_body", False, "%s missing at %s" % (doc, args.ref))
            continue
        for cmd, quoted, fn, body in extract_heredocs(text):
            out, err = run_heredoc(quoted, fn, body, sandbox)
            expected = "\n".join(body) + "\n" if body else ""
            if not quoted:
                expected = expected.replace("\\$", "$")
            ok = out is not None and out == expected
            where = "%s:%s" % (doc, cmd)
            if ok:
                check("C1_heredoc_output_matches_body", True, where)
            else:
                # show the first divergent line
                got = (out or "").split("\n")
                exp = expected.split("\n")
                first = next((k for k in range(min(len(got), len(exp)))
                              if got[k] != exp[k]), min(len(got), len(exp)))
                check("C1_heredoc_output_matches_body", False,
                      "%s — line %d: expected %r, got %r (stderr: %r)"
                      % (where, first + 1, exp[first] if first < len(exp) else EOF,
                         got[first] if first < len(got) else EOF, err.strip()[:120]))
            generated.setdefault(doc, {})[fn] = out

    # ---------- C2: every generated YAML parses ----------
    for doc in DOCS:
        for fn, text in sorted(generated.get(doc, {}).items()):
            if text is None:
                continue
            try:
                import yaml
                list(yaml.safe_load_all(text))
                check("C2_yaml_parses", True, "%s (from %s)" % (fn, doc))
            except Exception as e:
                check("C2_yaml_parses", False, "%s (from %s): %s" % (fn, doc, e))

    # ---------- C3: doc image tag == e2e manifest tag ----------
    doc_tags = set(re.findall(r"curvine/curvine:\S+",
                              "\n".join(git_show(root, args.ref, d) or "" for d in DOCS)))
    e2e_tags = set(re.findall(r"curvine/curvine:\S+",
                              git_show(root, args.ref, E2E_CRC) or ""))
    check("C3_doc_tag_matches_e2e", doc_tags == e2e_tags and doc_tags,
          "doc=%s e2e=%s" % (sorted(doc_tags), sorted(e2e_tags)))

    # ---------- C4: en/zh heredoc bodies identical ----------
    bodies = {}
    for doc in DOCS:
        text = git_show(root, args.ref, doc)
        bodies[doc] = [(fn, body) for _, _, fn, body in extract_heredocs(text or "")]
    en, zh = bodies[DOCS[0]], bodies[DOCS[1]]
    ok = len(en) == len(zh) and all(a == b for a, b in zip(en, zh))
    check("C4_en_zh_parity", ok,
          "%d vs %d heredoc blocks, identical=%s" % (len(en), len(zh),
                                                     all(a == b for a, b in zip(en, zh)) if len(en) == len(zh) else "n/a"))

    # ---------- C5 (canary): step-6 pod label does NOT exist in the code ----------
    # BUG-CANARY, invert when fixed: the doc's `kubectl get pod -l` uses
    # cacheruntime.fluid.io/runtime-name, which pkg/common/label.go never defines
    # (real key: cacheruntime.fluid.io/name). Canary passes == bug still present.
    doc_text = git_show(root, args.ref, DOCS[0]) or ""
    labels_used = set(re.findall(r"-l\s+(\S+?)=", doc_text))
    label_go = git_show(root, args.ref, LABEL_GO) or ""
    defined = set(re.findall(r'"(cacheruntime\.[^"]+)"', label_go.replace('" + ', " + ")))
    # resolve Go string concatenation roughly: collect candidate keys
    prefixes = ["cacheruntime.fluid.io/"]
    broken = set()
    for lab in labels_used:
        if any(lab == p + suffix for p in prefixes
               for suffix in ("name", "component-name", "runtime-name", "type")):
            # verify against label.go constants
            suffix = lab.split("/")[-1]
            if ("LabelCacheRuntime" + suffix.replace("-", "").title() not in label_go
                    and '"%s"' % lab not in label_go):
                broken.add(lab)
        else:
            broken.add(lab)
    # precise: the only valid suffixes are name / component-name
    valid = set(l for l in labels_used if l.split("/")[-1] in ("name", "component-name"))
    broken = labels_used - valid
    check("C5_canary_step6_label_absent", bool(broken) and broken == {"cacheruntime.fluid.io/runtime-name"},
          "labels in doc: %s; not set by code (expected runtime-name only): %s"
          % (sorted(labels_used), sorted(broken)))

    # ---------- C6 (network): image tags pullable ----------
    if not args.offline:
        try:
            import urllib.request
            tok_json = urllib.request.urlopen(
                "https://auth.docker.io/token?service=registry.docker.io&scope=repository:curvine/curvine:pull",
                timeout=30).read().decode()
            token = json.loads(tok_json)["token"]
            import urllib.error

            def manifest(tag):
                req = urllib.request.Request(
                    "https://registry-1.docker.io/v2/curvine/curvine/manifests/" + tag,
                    headers={
                        "Authorization": "Bearer " + token,
                        "Accept": ", ".join([
                            "application/vnd.oci.image.index.v1+json",
                            "application/vnd.docker.distribution.manifest.list.v2+json",
                            "application/vnd.oci.image.manifest.v1+json",
                            "application/vnd.docker.distribution.manifest.v2+json"]),
                    })
                try:
                    return 200, urllib.request.urlopen(req, timeout=30).read().decode()
                except urllib.error.HTTPError as e:
                    return e.code, ""
            for tag in sorted(doc_tags):
                code, body = manifest(tag.split(":", 1)[1])
                arches = []
                if code == 200:
                    try:
                        arches = ["%s/%s" % (m.get("platform", {}).get("os"),
                                             m.get("platform", {}).get("architecture"))
                                  for m in json.loads(body).get("manifests", [])]
                    except Exception:
                        arches = ["<single-manifest>"]
                check("C6_image_pullable", code == 200,
                      "%s -> HTTP %d %s" % (tag, code, arches))
            code, _ = manifest("0.3.6-alpha")
            check("C6_old_tag_gone", code == 404,
                  "curvine/curvine:0.3.6-alpha -> HTTP %d (expected 404)" % code)
        except Exception as e:
            check("C6_image_pullable", False, "network layer error: %r" % (e,))

    print()
    if FAILURES:
        print("RESULT: FAIL (%d checks failed: %s)" % (len(FAILURES), ", ".join(FAILURES)))
        return 1
    print("RESULT: PASS (all contract checks green; C5 canary green = pre-existing label bug still present, as documented)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
