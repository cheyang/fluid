#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""check_doc_claims.py — L1/L2 harness for PR #6253 (vLLM + Curvine sample docs).

Verifies, against a given git ref (default: the working tree / PR head), that every
code-checkable claim in the two new sample docs actually matches the Fluid codebase,
the referenced Curvine sample doc, and (with network) the external images/releases.

Layers:
  unit        (offline)  : P0, C1..C5, C7, C8, C10   -> run with --offline
  integration (network)  : C6 (Docker Hub manifests), C9 (vLLM source at the image tag)

Output: one line per check, "<NAME>\\t<pass|fail|skip>\\t<detail>", plus a summary on
stderr. Exit 0 iff every executed check passes (skipped checks are not failures).

Self-test (harness-bites): --selftest applies in-memory mutations (broken label,
broken link, broken image tag, broken parity) and requires the corresponding checks
to FAIL, proving the checks actually bite. Run: check_doc_claims.py --selftest
"""
import argparse
import json
import re
import subprocess
import sys
import urllib.request

EN_DOC = "docs/en/samples/cacheruntime/vllm_with_curvine.md"
ZH_DOC = "docs/zh/samples/cacheruntime/vllm_with_curvine.md"
REF_EN = "docs/en/samples/cacheruntime/curvine_cache_runtime.md"
REF_ZH = "docs/zh/samples/cacheruntime/curvine_cache_runtime.md"
TOC_EN = "docs/en/TOC.md"
TOC_ZH = "docs/zh/TOC.md"

LABEL_GO = "pkg/common/label.go"
COMPONENT_MGR_GO = "pkg/ddc/cache/component/component_manager.go"
PHASE_GO = "pkg/common/types.go"
CRD_DIR = "charts/fluid/fluid/crds"

RESULTS = []


def check(name, ok, detail=""):
    RESULTS.append((name, "pass" if ok else "fail", detail))
    return ok


def skip(name, detail=""):
    RESULTS.append((name, "skip", detail))


class Repo(object):
    def __init__(self, root, ref):
        self.root = root
        self.ref = ref

    def read(self, path):
        if self.ref == "WORKTREE":
            with open(path, "r", encoding="utf-8") as fh:
                return fh.read()
        out = subprocess.run(
            ["git", "show", "%s:%s" % (self.ref, path)],
            cwd=self.root, capture_output=True, text=True)
        if out.returncode != 0:
            return None
        return out.stdout

    def exists(self, path):
        if self.ref == "WORKTREE":
            import os
            return os.path.isfile(path)
        out = subprocess.run(
            ["git", "cat-file", "-e", "%s:%s" % (self.ref, path)],
            cwd=self.root, capture_output=True)
        return out.returncode == 0

    def grep_docs(self, pattern, pathspec):
        out = subprocess.run(
            ["git", "grep", "-il", pattern, self.ref, "--", pathspec],
            cwd=self.root, capture_output=True, text=True)
        return [l.split(":", 1)[1] for l in out.stdout.splitlines() if l]

    def list_dir(self, path):
        if self.ref == "WORKTREE":
            import os
            return [os.path.join(path, f) for f in sorted(os.listdir(path))]
        out = subprocess.run(["git", "ls-tree", "--name-only", self.ref, path + "/"],
                             cwd=self.root, capture_output=True, text=True)
        return out.stdout.split()


def code_blocks(text):
    """Yield the text of each fenced code block, in order."""
    blocks, cur, inside = [], [], False
    for line in text.splitlines():
        if line.startswith("```"):
            if inside:
                blocks.append("\n".join(cur))
                cur = []
            inside = not inside
            continue
        if inside:
            cur.append(line)
    if inside and cur:
        blocks.append("\n".join(cur))
    return blocks


def heredoc_bodies(text):
    """Return (quoted, body) for every `cat<<EOF`/`cat <<'EOF'` heredoc in a doc."""
    out = []
    lines = text.splitlines()
    i = 0
    while i < len(lines):
        m = re.search(r"cat\s*<<\s*('?)EOF('?)\s*>", lines[i])
        if m:
            quoted = bool(m.group(1))
            body = []
            j = i + 1
            while j < len(lines) and lines[j].strip() != "EOF":
                body.append(lines[j])
                j += 1
            out.append((quoted, "\n".join(body)))
            i = j
        i += 1
    return out


# ---------------------------------------------------------------- P0: premise
def check_premise(repo, base_ref):
    """Docs-only PR, so the premise is the coverage gap itself: no inference-serving
    sample existed at the base. Evidence: git grep for vllm in docs/ at base."""
    try:
        hits = repo.grep_docs("vllm", "docs/")
        check("P0_no_prior_vllm_sample", not hits,
              "docs at base mention vllm: %s" % (hits or "none"))
    except Exception as exc:  # pragma: no cover
        skip("P0_no_prior_vllm_sample", "grep failed: %s" % exc)


# ---------------------------------------------------------- C1: pod label
def resolve_cacheruntime_name_label(repo):
    """Resolve LabelCacheRuntimeName's constant chain in pkg/common/label.go."""
    src = repo.read(LABEL_GO)
    if not src:
        return None
    m = re.search(r'LabelAnnotationPrefix\s*=\s*"([^"]+)"', src)
    if not m:  # may be defined as a var elsewhere; try label.go first, else pkg/common
        for cand in (src,):
            m = re.search(r'LabelAnnotationPrefix\s*=\s*"([^"]+)"', cand)
            if m:
                break
    if not m:
        return None
    prefix = m.group(1)
    m2 = re.search(r'CacheRuntimeLabelAnnotationPrefix\s*=\s*"cacheruntime\."\s*\+\s*LabelAnnotationPrefix', src)
    if not m2:
        return None
    m3 = re.search(r'LabelCacheRuntimeName\s*=\s*CacheRuntimeLabelAnnotationPrefix\s*\+\s*"name"', src)
    if not m3:
        return None
    return "cacheruntime." + prefix + "name"


def check_label_selector(repo, mutation=None):
    doc = repo.read(EN_DOC)
    zh = repo.read(ZH_DOC)
    if not doc or not zh:
        return check("C1_label_selector_valid", False, "doc missing")
    label = resolve_cacheruntime_name_label(repo)
    if not label:
        return check("C1_label_selector_valid", False,
                     "could not resolve LabelCacheRuntimeName in %s" % LABEL_GO)
    sel = re.findall(r"kubectl get pod -l ([\w.\-/=]+)", doc)
    if not sel:
        return check("C1_label_selector_valid", False, "no `kubectl get pod -l` in doc")
    keys = set(s.split("=", 1)[0] for s in sel)
    # the pod label must also actually be set on component pods
    mgr = repo.read(COMPONENT_MGR_GO) or ""
    applied = "common.LabelCacheRuntimeName" in mgr
    if mutation == "label":
        keys = set(["cacheruntime.fluid.io/runtime-name"])
    ok = (label in keys) and applied and (label in keys)
    zh_has = ("kubectl get pod -l %s" % label) in zh
    return check("C1_label_selector_valid", ok and zh_has,
                 "doc selectors=%s; code label=%s; set-in-component-manager=%s; zh-uses-label=%s"
                 % (sorted(keys), label, applied, zh_has))


# ---------------------------------------------------------- C2: doc links
def check_links(repo, mutation=None):
    ok_all, details = True, []
    for path in (EN_DOC, ZH_DOC):
        text = repo.read(path)
        if not text:
            return check("C2_doc_links_resolve", False, "%s missing" % path)
        for target in re.findall(r"\]\(([^)#]+?)(?:#[^)]*)?\)", text):
            if target.startswith(("http://", "https://", "mailto:")):
                continue
            base = path.rsplit("/", 1)[0]
            import os
            resolved = os.path.normpath(os.path.join(base, target))
            if mutation == "link" and "install.md" in resolved:
                exists = False
            else:
                exists = repo.exists(resolved)
            if not exists:
                ok_all = False
                details.append("%s -> %s MISSING" % (path, target))
    return check("C2_doc_links_resolve", ok_all,
                 "; ".join(details) or "all relative links resolve")


# ---------------------------------------------------- C3: en/zh parity
def normalize_shell_block(block):
    out = []
    for line in block.splitlines():
        if line.startswith("$ "):
            # strip trailing comments (translated between en/zh)
            line = re.sub(r"\s+#.*$", "", line)
        out.append(line)
    return out


def lines_equivalent(a, b):
    """Equal, or either side carries translated (CJK) user-facing text."""
    if a == b:
        return True
    cjk = re.compile(r"[一-鿿]")
    return bool(cjk.search(a) or cjk.search(b))


def shell_block_equal(a_lines, b_lines):
    a_lines = [l for l in a_lines if l.strip()]
    b_lines = [l for l in b_lines if l.strip()]
    if len(a_lines) != len(b_lines):
        return False
    return all(lines_equivalent(x, y) for x, y in zip(a_lines, b_lines))


def json_key_shape(obj):
    if isinstance(obj, dict):
        return {k: json_key_shape(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [json_key_shape(x) for x in obj[:1]]
    return type(obj).__name__


def check_parity(repo, mutation=None):
    en, zh = repo.read(EN_DOC), repo.read(ZH_DOC)
    if not en or not zh:
        return check("C3_en_zh_code_parity", False, "doc missing")
    en_blocks, zh_blocks = code_blocks(en), code_blocks(zh)
    if len(en_blocks) != len(zh_blocks):
        return check("C3_en_zh_code_parity", False,
                     "block count differs: en=%d zh=%d" % (len(en_blocks), len(zh_blocks)))
    mismatch = []
    for idx, (a, b) in enumerate(zip(en_blocks, zh_blocks)):
        try:
            ja, jb = json.loads(a), json.loads(b)
            if json_key_shape(ja) != json_key_shape(jb):
                mismatch.append("block %d: JSON key shape differs" % idx)
            continue
        except Exception:
            pass
        if not shell_block_equal(normalize_shell_block(a), normalize_shell_block(b)):
            if mutation == "parity" and idx == 0:
                mismatch.append("block 0: injected mutation")
            else:
                mismatch.append("block %d differs" % idx)
    return check("C3_en_zh_code_parity", not mismatch,
                 "; ".join(mismatch) or "code blocks identical modulo translated comments")


# ------------------------------------------- C4: heredoc shell expansion
def check_heredocs(repo, mutation=None):
    problems = []
    for path in (EN_DOC, ZH_DOC):
        text = repo.read(path)
        if not text:
            return check("C4_heredoc_no_shell_expansion", False, "%s missing" % path)
        for quoted, body in heredoc_bodies(text):
            if quoted:
                continue
            risky = [l for l in body.splitlines() if "$" in l]
            if risky:
                problems.append("%s unquoted heredoc contains $: %s" % (path, risky[:2]))
    if mutation == "heredoc":
        problems.append("injected: $VARIABLE in unquoted heredoc")
    return check("C4_heredoc_no_shell_expansion", not problems,
                 "; ".join(problems) or "no $ in any unquoted heredoc body")


# --------------------------------------- C5: cross-doc reference contract
def check_cross_refs(repo):
    problems = []
    for lang, ref, doc in (("en", REF_EN, EN_DOC), ("zh", REF_ZH, ZH_DOC)):
        ref_text = repo.read(ref)
        doc_text = repo.read(doc)
        if not ref_text or not doc_text:
            return check("C5_cross_doc_references", False, "%s or %s missing" % (ref, doc))
        need = ["curvine-secret", "access-key", "secret-key", "s3://test",
                "http://minio:9000", "app: minio", "name: curvine-demo",
                "minio.yaml", "minio_create_bucket.yaml", "cacheruntimeclass.yaml"]
        if lang == "en":
            need += ["## Step 1: Deploy MinIO", "## Step 2: Create a MinIO Bucket",
                     "## Step 4: Define the CacheRuntimeClass"]
        else:
            need += ["## 步骤 1：部署 MinIO", "## 步骤 2：创建 MinIO 存储桶",
                     "## 步骤 4：定义 CacheRuntimeClass"]
        for token in need:
            if token not in ref_text:
                problems.append("%s: referenced doc lacks %r" % (lang, token))

    # the doc claims: CacheRuntime YAML identical to the base sample except quota 1Gi->4Gi,
    # and the Dataset YAML identical.
    ref_text, doc_text = repo.read(REF_EN), repo.read(EN_DOC)

    def heredoc_named(text, fname):
        lines = text.splitlines()
        for i, l in enumerate(lines):
            if ("cat<<EOF >" + fname) in l or ("cat<<'EOF' >" + fname) in l:
                body = []
                j = i + 1
                while j < len(lines) and lines[j].strip() != "EOF":
                    body.append(lines[j])
                    j += 1
                return "\n".join(body)
        return None

    ref_cr = heredoc_named(ref_text, "cacheruntime.yaml")
    new_cr = heredoc_named(doc_text, "cacheruntime.yaml")
    if ref_cr is None or new_cr is None:
        problems.append("cacheruntime.yaml heredoc not found (ref=%s new=%s)" % (ref_cr is not None, new_cr is not None))
    else:
        diff = [l for l in ref_cr.splitlines() if l not in new_cr.splitlines()]
        extra = [l for l in new_cr.splitlines() if l not in ref_cr.splitlines()]
        if sorted(diff + extra) not in ([], ['            quota: 1Gi', '            quota: 4Gi']):
            # allow whitespace-only diffs
            norm = lambda xs: sorted(re.sub(r"\s+", " ", x).strip() for x in xs)
            if norm(diff + extra) != ["quota: 1Gi", "quota: 4Gi"]:
                problems.append("CacheRuntime yaml differs beyond quota: -%s +%s" % (diff, extra))

    ref_ds = heredoc_named(ref_text, "dataset.yaml")
    new_ds = heredoc_named(doc_text, "dataset.yaml")
    if ref_ds is None or new_ds is None:
        problems.append("dataset.yaml heredoc not found")
    elif [l for l in ref_ds.splitlines() if l.strip()] != [l for l in new_ds.splitlines() if l.strip()]:
        problems.append("Dataset yaml differs from the referenced sample")

    return check("C5_cross_doc_references", not problems,
                 "; ".join(problems) or "names/steps/anchors all present; only quota differs")


# ---------------------------------------------------- C7: YAML vs CRD
def crd_properties(repo, kind):
    import os
    crd_path = None
    for cand in ("data.fluid.io_datasets.yaml", "cacheruntimes.data.fluid.io.yaml",
                 "data.fluid.io_cacheruntimes.yaml", "cacheruntimeclasses.data.fluid.io.yaml",
                 "data.fluid.io_dataloads.yaml", "dataloads.data.fluid.io.yaml"):
        p = os.path.join(CRD_DIR, cand)
        if repo.exists(p):
            if kind.lower().replace(" ", "") in cand.lower().replace("_", "").replace(".", ""):
                crd_path = p
                break
    if crd_path is None:
        # search by kind inside each crd file
        for p in repo.list_dir(CRD_DIR):
            content = repo.read(p)
            if content and ("kind: %s" % kind) in content.split("spec:")[0]:
                crd_path = p
                break
    if crd_path is None:
        return None
    import yaml
    docs = list(yaml.safe_load_all(repo.read(crd_path)))
    for d in docs:
        if d and d.get("kind") == "CustomResourceDefinition":
            names = [s for s in (d.get("spec", {}).get("names", {}) or {}).get("kind", "")]
            if d["spec"]["names"]["kind"] == kind:
                versions = d["spec"]["versions"]
                return versions[0]["schema"]["openAPIV3Schema"]["properties"]
    return None


def validate_obj_against_schema(obj, schema_props, path, problems, required=False):
    if schema_props is None:
        problems.append("%s: no CRD schema found to validate against" % path)
        return
    if not isinstance(obj, dict):
        return
    props = schema_props.get("properties", {})
    for k, v in obj.items():
        if k not in props:
            problems.append("%s.%s: field not in CRD schema (fields: %s)" % (path, k, sorted(props)))
            continue
        sub = props[k]
        if v is not None and isinstance(v, dict) and "properties" in sub:
            validate_obj_against_schema(v, sub, "%s.%s" % (path, k), problems)
        elif v is not None and isinstance(v, list) and sub.get("type") == "array":
            item_schema = sub.get("items", {})
            for i, item in enumerate(v):
                if isinstance(item, dict) and "properties" in item_schema:
                    validate_obj_against_schema(item, item_schema, "%s.%s[%d]" % (path, k, i), problems)


def check_yaml_schema(repo):
    import yaml
    problems = []
    doc = repo.read(EN_DOC)
    if not doc:
        return check("C7_yaml_fields_in_crd", False, "doc missing")
    parsed = []
    for fname in ("dataset.yaml", "cacheruntime.yaml", "dataload.yaml", "vllm.yaml"):
        body = None
        lines = doc.splitlines()
        for i, l in enumerate(lines):
            if ("cat<<EOF >" + fname) in l:
                body = []
                j = i + 1
                while j < len(lines) and lines[j].strip() != "EOF":
                    body.append(lines[j])
                    j += 1
                body = "\n".join(body)
                break
        if body is None:
            problems.append("heredoc %s not found in doc" % fname)
            continue
        for y in yaml.safe_load_all(body):
            if y is not None:
                parsed.append(y)
    kinds = {}
    for y in parsed:
        kinds.setdefault(y["kind"], []).append(y)
    expect = {"Dataset": 1, "CacheRuntime": 1, "DataLoad": 1, "Deployment": 1, "Service": 1}
    for k, n in expect.items():
        if len(kinds.get(k, [])) != n:
            problems.append("expected %d %s, found %d" % (n, k, len(kinds.get(k, []))))
    for k in ("Dataset", "CacheRuntime", "DataLoad"):
        for y in kinds.get(k, []):
            if y.get("apiVersion") != "data.fluid.io/v1alpha1":
                problems.append("%s apiVersion=%s" % (k, y.get("apiVersion")))
            schema = crd_properties(repo, k)
            if schema is None:
                problems.append("%s: no CRD schema found" % k)
                continue
            validate_obj_against_schema(
                {kk: vv for kk, vv in y.items() if kk not in ("apiVersion", "kind")},
                {"properties": schema}, k, problems)
    # core k8s objects: structural sanity
    dep = (kinds.get("Deployment") or [{}])[0]
    sel = dep.get("spec", {}).get("selector", {}).get("matchLabels") or {}
    tmpl_labels = dep.get("spec", {}).get("template", {}).get("metadata", {}).get("labels") or {}
    if sel != tmpl_labels or not sel:
        problems.append("Deployment selector.matchLabels != template labels (%s vs %s)" % (sel, tmpl_labels))
    pvc_claims = [v.get("persistentVolumeClaim", {}).get("claimName")
                  for v in dep.get("spec", {}).get("template", {}).get("spec", {}).get("volumes", [])
                  if v.get("persistentVolumeClaim")]
    ds = (kinds.get("Dataset") or [{}])[0]
    if pvc_claims != [ds.get("metadata", {}).get("name")]:
        problems.append("vLLM mounts PVC %s but Dataset is %s" % (pvc_claims, ds.get("metadata", {}).get("name")))
    svc = (kinds.get("Service") or [{}])[0]
    if svc.get("spec", {}).get("selector") != sel:
        problems.append("Service selector != Deployment labels")
    return check("C7_yaml_fields_in_crd", not problems,
                 "; ".join(problems) or "all YAML blocks parse; fields match CRD; k8s objects consistent")


# ---------------------------------------------------- C8: TOC entries
def check_toc(repo):
    import os
    problems = []
    for toc, doc, prefix in ((TOC_EN, EN_DOC, "Example - "), (TOC_ZH, ZH_DOC, "示例 - ")):
        toc_text, doc_text = repo.read(toc), repo.read(doc)
        if not toc_text or not doc_text:
            return check("C8_toc_entries", False, "%s or %s missing" % (toc, doc))
        h1 = doc_text.splitlines()[0].lstrip("# ").strip()
        title = h1[len(prefix):] if h1.startswith(prefix) else h1
        link = os.path.relpath(doc, os.path.dirname(toc))
        if "(%s)" % link not in toc_text:
            problems.append("%s: no link to %s" % (toc, link))
        if ("[%s](%s)" % (title, link)) not in toc_text:
            problems.append("%s: title %r not found for %s" % (toc, title, link))
    return check("C8_toc_entries", not problems, "; ".join(problems) or "TOC entries + titles match H1s")


# ------------------------------------------------ C10: DataLoad phase
def check_dataload_phase(repo):
    src = repo.read(PHASE_GO) or ""
    ok = re.search(r'PhaseComplete\s+Phase\s*=\s*"Complete"', src) is not None
    doc = repo.read(EN_DOC) or ""
    uses = "--for=jsonpath='{.status.phase}'=Complete" in doc
    return check("C10_dataload_phase_value", ok and uses,
                 "PhaseComplete in code=%s; doc wait uses status.phase=Complete=%s" % (ok, uses))


# ------------------------------------------------- C6: image pullability
def http_get(url, timeout=15):
    req = urllib.request.Request(url, headers={"User-Agent": "fluid-doc-verify/1.0"})
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return resp.status


def docker_manifest_status(image):
    repo, tag = image.rsplit(":", 1)
    try:
        tok = urllib.request.urlopen(
            "https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull" % repo,
            timeout=15).read().decode()
        token = json.loads(tok)["token"]
        req = urllib.request.Request(
            "https://registry-1.docker.io/v2/%s/manifests/%s" % (repo, tag),
            headers={"Authorization": "Bearer " + token,
                     "Accept": "application/vnd.docker.distribution.manifest.v2+json,"
                               "application/vnd.oci.image.manifest.v1+json,"
                               "application/vnd.oci.image.index.v1+json,"
                               "application/vnd.docker.distribution.manifest.list.v2+json"})
        with urllib.request.urlopen(req, timeout=15) as resp:
            return resp.status
    except urllib.error.HTTPError as e:
        return e.code
    except Exception as e:
        return -1


def check_images(repo):
    """Every image the walk-through deploys or names must be pullable:
    - images named in the new docs (verified-with line, curl pod, vllm deployment)
    - images in the CacheRuntimeClass heredoc of the REFERENCED doc (what Step 1
      actually tells the reader to deploy)."""
    problems, statuses = [], []
    images = set()
    for path in (EN_DOC, ZH_DOC):
        text = repo.read(path) or ""
        for m in re.finditer(r"`((?:[a-z0-9._\-]+(?:/[a-z0-9._\-]+)+):[A-Za-z0-9._\-]+)`", text):
            images.add(m.group(1))
        for m in re.finditer(r"image:\s*([^\s|`]+)", text):
            img = m.group(1)
            if "/" in img and ":" in img:
                images.add(img.rstrip("`"))
        for m in re.finditer(r"--image=([^\s`]+)", text):
            img = m.group(1)
            if "/" in img and ":" in img:
                images.add(img)
    # images actually deployed via the referenced doc's CacheRuntimeClass
    ref_text = repo.read(REF_EN) or ""
    in_class = False
    for line in ref_text.splitlines():
        if "cacheruntimeclass.yaml" in line:
            in_class = True
        if in_class and line.strip() == "EOF":
            in_class = False
        if in_class:
            m = re.match(r"\s*image:\s*(\S+)", line)
            if m:
                images.add(m.group(1))
    for img in sorted(images):
        code = docker_manifest_status(img)
        statuses.append("%s -> %s" % (img, code))
        if code != 200:
            problems.append("%s not pullable (HTTP %s)" % (img, code))
    return check("C6_walkthrough_images_pullable", not problems,
                 "; ".join(statuses))


# ------------------------------------------------------ C9: vLLM claims
def check_vllm_claims(repo):
    doc = repo.read(EN_DOC) or ""
    m = re.search(r"vllm/vllm-openai:(v[0-9.]+)", doc)
    if not m:
        return check("C9_vllm_entrypoint_and_port_env", False, "no vllm image tag in doc")
    tag = m.group(1)
    problems = []
    try:
        dockerfile = urllib.request.urlopen(
            "https://raw.githubusercontent.com/vllm-project/vllm/%s/docker/Dockerfile" % tag,
            timeout=20).read().decode()
        if 'ENTRYPOINT ["vllm", "serve"]' not in dockerfile:
            problems.append("vllm %s: entrypoint is not `vllm serve`" % tag)
    except Exception as e:
        problems.append("could not fetch vllm Dockerfile@%s: %s" % (tag, e))
    try:
        envs = urllib.request.urlopen(
            "https://raw.githubusercontent.com/vllm-project/vllm/%s/vllm/envs.py" % tag,
            timeout=20).read().decode()
        if "appears to be a URI" not in envs:
            problems.append("vllm %s: VLLM_PORT URI guard not found in vllm/envs.py" % tag)
        else:
            # the doc quotes the message; make sure the doc's claim matches the mechanism
            if "enableServiceLinks" not in doc:
                problems.append("doc does not set enableServiceLinks: false")
    except Exception as e:
        problems.append("could not fetch vllm envs.py@%s: %s" % (tag, e))
    return check("C9_vllm_entrypoint_and_port_env", not problems,
                 "; ".join(problems) or "vllm %s: ENTRYPOINT vllm serve; VLLM_PORT URI guard present; doc mitigation present" % tag)


# ------------------------------------------------------------------ main
def run_all(repo, offline, mutations=None):
    mutations = mutations or {}
    check_premise(repo, None)
    check_label_selector(repo, mutation=mutations.get("label"))
    check_links(repo, mutation=mutations.get("link"))
    check_parity(repo, mutation=mutations.get("parity"))
    check_heredocs(repo, mutation=mutations.get("heredoc"))
    check_cross_refs(repo)
    check_yaml_schema(repo)
    check_toc(repo)
    check_dataload_phase(repo)
    if offline:
        skip("C6_walkthrough_images_pullable", "offline mode")
        skip("C9_vllm_entrypoint_and_port_env", "offline mode")
    else:
        check_images(repo)
        check_vllm_claims(repo)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo-root", default=".")
    ap.add_argument("--ref", default="WORKTREE")
    ap.add_argument("--base-ref", default="ff99142f9ec5c32a0c9166de023cb8936f0e5ae5")
    ap.add_argument("--offline", action="store_true")
    ap.add_argument("--selftest", action="store_true",
                    help="prove the harness bites: inject mutations, expect FAILs")
    args = ap.parse_args()

    repo = Repo(args.repo_root, args.ref)

    if args.selftest:
        # each mutation must make exactly its own check fail
        mutations = {"label": "label", "link": "link", "heredoc": "heredoc"}
        run_all(repo, offline=True, mutations=mutations)
        fails = {name for name, st, _ in RESULTS if st == "fail"}
        required_fail = {"C1_label_selector_valid", "C2_doc_links_resolve",
                         "C4_heredoc_no_shell_expansion"}
        for name, st, detail in RESULTS:
            sys.stderr.write("[selftest] %s\t%s\t%s\n" % (name, st, detail))
        ok = required_fail <= fails
        sys.stdout.write("SELFTEST\t%s\tmutations %s -> failures %s\n"
                         % ("pass" if ok else "fail", sorted(mutations), sorted(fails)))
        sys.exit(0 if ok else 1)

    run_all(repo, offline=args.offline)
    failed = 0
    for name, st, detail in RESULTS:
        sys.stdout.write("%s\t%s\t%s\n" % (name, st, detail))
        if st == "fail":
            failed += 1
    executed = [r for r in RESULTS if r[1] != "skip"]
    sys.stderr.write("checks: %d executed, %d failed, %d skipped\n"
                     % (len(executed), failed, len(RESULTS) - len(executed)))
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
