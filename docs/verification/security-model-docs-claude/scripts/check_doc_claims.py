#!/usr/bin/env python3
"""Deterministic (L1) claim checker for PR #6211 (security model docs).

Checks the factual code claims made in security/security-model.md and
security/security-model_zh.md against the repository tree this script lives
in. Every check is a contract test: it asserts the documented claim is TRUE
at the checked-out ref. A failing check means the doc is wrong (or the code
drifted past the doc, which the doc itself calls a documentation bug).

Run:  python3 docs/verification/security-model-docs-claude/scripts/check_doc_claims.py
Exit 0 iff every check passes.
"""
import hashlib
import os
import re
import sys

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", ".."))
EN = os.path.join(REPO_ROOT, "security", "security-model.md")
ZH = os.path.join(REPO_ROOT, "security", "security-model_zh.md")

results = []  # (check_id, description, ok, detail)


def check(cid, desc, ok, detail=""):
    results.append((cid, desc, bool(ok), detail))
    return bool(ok)


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def file_has(path, pattern):
    full = os.path.join(REPO_ROOT, path)
    if not os.path.isfile(full):
        return False, "file missing"
    text = read(full)
    m = re.search(pattern, text)
    return bool(m), ("match" if m else "pattern not found")


# ---------------------------------------------------------------- doc files
check("D1", "both security model docs exist", os.path.isfile(EN) and os.path.isfile(ZH))

en_text = read(EN) if os.path.isfile(EN) else ""
zh_text = read(ZH) if os.path.isfile(ZH) else ""

# ------------------------------------------------- D2: referenced file paths
PATH_RE = re.compile(r"`([a-zA-Z0-9_./-]+\.(?:go|tpl|yaml|md))`")
refs = sorted(set(PATH_RE.findall(en_text)) | set(PATH_RE.findall(zh_text)))
missing = [p for p in refs if not os.path.isfile(os.path.join(REPO_ROOT, p))]
check("D2", "all %d referenced file paths resolve" % len(refs), not missing, "missing: %s" % missing)

# ------------------------------------------- D3: referenced symbols by file
SYMBOLS = [
    ("api/v1alpha1/container_network.go", r"IsHostNetwork"),
    ("api/v1alpha1/cacheruntimeclass_types.go", r"topology"),
    ("api/v1alpha1/cacheruntime_types.go", r"CacheRuntime"),
    ("api/v1alpha1/dataprocess_types.go", r"PodSpec"),
    ("api/v1alpha1/datamigrate_types.go", r"VersionSpec"),
    ("pkg/utils/docker/image.go", r"func GetWorkerImage"),
    ("pkg/ddc/juicefs/data_load.go", r"genDataLoadValue"),
    ("pkg/ddc/juicefs/transform_fuse.go", r"func .+genValue\("),
    ("pkg/ddc/alluxio/backup_data.go", r"Spec\.BackupPath"),
    ("pkg/ddc/alluxio/backup_data.go", r"Spec\.RunAs"),
    ("pkg/ddc/alluxio/transform.go", r"transformCommonPart"),
    ("pkg/ddc/alluxio/ufs_internal.go", r"[Ee]ncryptOption"),
    ("pkg/ddc/thin/ufs_internal.go", r"[Ee]ncryptOption"),
    ("pkg/ddc/thin/transform_config.go", r"extractMountOptions"),
    ("pkg/ddc/thin/transform_fuse.go", r"DatasetOptionFluidFuseHostVolume"),
    ("pkg/ddc/thin/transform_pvc_mounts.go", r"transfromSecretsForPersistentVolumeClaimMounts"),
    ("pkg/ddc/jindo/transform.go", r"transformMaster"),
    ("pkg/utils/kubeclient/secret.go", r"func CopySecretToNamespace"),
    ("pkg/ddc/thin/referencedataset/engine.go", r"checkDatasetMountSupport"),
    ("pkg/ddc/base/dataset.go", r"GetPhysicalDatasetSubPath"),
    ("pkg/csi/plugins/subpath_linux.go", r"func resolveMountSource"),
    ("pkg/csi/plugins/subpath_linux.go", r"O_NOFOLLOW"),
    ("pkg/csi/plugins/nodeserver.go", r"NodePublishVolume"),
    ("pkg/csi/plugins/nodeserver.go", r"func \(ns \*nodeServer\) getNode"),
    ("pkg/csi/plugins/nodeserver.go", r"func \(ns \*nodeServer\) patchNodeWithLabel"),
    ("pkg/csi/plugins/nodeserver.go", r"cmdguard"),
    ("pkg/controllers/v1alpha1/dataload/implement.go", r"[Nn]amespace"),
    ("pkg/controllers/v1alpha1/dataprocess/implement.go", r"[Nn]amespace"),
    ("pkg/application/inject/fuse/mutator/mutator_unprivileged.go", r"transformTemplateWithUnprivilegedSidecarEnabled"),
    ("pkg/application/inject/fuse/mutator/mutator_default.go", r"func .*defaultMutateDatasetVolumes"),
    ("pkg/ddc/thin/referencedataset/volume.go", r"accessModesForVirtualDataset"),
    ("pkg/ddc/thin/referencedataset/cm.go", r"copyFuseDaemonSetForRefDataset"),
    ("pkg/ddc/thin/referencedataset/cm.go", r"createConfigMapForRefDataset"),
    ("pkg/utils/annotations.go", r"GetServerlessPlatform"),
    ("pkg/utils/annotations.go", r"InjectFuseSidecar"),
]
for path, pat in SYMBOLS:
    ok, detail = file_has(path, pat)
    check("D3", "symbol %r in %s" % (pat, path), ok, detail)

# ---------------------------------------------------- D4: CRD inventory
crds_dir = os.path.join(REPO_ROOT, "charts/fluid/fluid/crds")
if os.path.isdir(crds_dir):
    crd_files = sorted(os.listdir(crds_dir))
    data_crds = [f for f in crd_files if f.startswith("data.fluid.io_")]
    wl_crds = [f for f in crd_files if f.startswith("workload.fluid.io_")]
    check("D4", "14 data.fluid.io CRDs + 1 workload.fluid.io CRD",
          len(data_crds) == 14 and len(wl_crds) == 1,
          "data=%d workload=%d" % (len(data_crds), len(wl_crds)))
else:
    check("D4", "CRD dir exists", False, "charts/fluid/fluid/crds missing")

# -------------------------------------------- D5: key option/label constants
for label, pattern, where in [
    ("serverless.fluid.io/inject", r"serverless\.fluid\.io/inject|InjectServerless\s*=", "pkg/common/constants.go"),
    ("unprivileged.sidecar.fluid.io/inject", r"unprivileged\.sidecar\.fluid\.io/inject", "pkg/common/constants.go"),
    ("fuse.serverful.fluid.io/inject", r"fuse\.serverful\.fluid\.io/inject|InjectServerfulFuse", "pkg/common/constants.go"),
    ("runtime.fluid.io/fuse.hostpid", r"runtime\.fluid\.io/fuse\.hostpid", "pkg/common/constants.go"),
    ("fluid.fuse.hostvolume", r"DatasetOptionFluidFuseHostVolume|fluid\.fuse", "pkg/common/dataset.go"),
    ("fuse-balloon nodeSelector", r"fluid\.io/fuse-balloon", "pkg/ddc/thin/referencedataset/cm.go"),
]:
    ok, detail = file_has(where, pattern)
    check("D5", "constant/label %s in %s" % (label, where), ok, detail)

# ---------------------------------------------------------- D6: YAML blocks
def yaml_blocks(text):
    blocks, cur, on = [], [], False
    for line in text.splitlines():
        if line.startswith("```yaml"):
            on, cur = True, []
            continue
        if on and line.startswith("```"):
            blocks.append("\n".join(cur))
            on = False
            continue
        if on:
            cur.append(line)
    return blocks


en_blocks = yaml_blocks(en_text)
zh_blocks = yaml_blocks(zh_text)
check("D6", "EN and ZH carry the same number of YAML blocks (%d)" % len(en_blocks),
      len(en_blocks) == len(zh_blocks) and len(en_blocks) > 0)
same = [hashlib.sha256(b.encode()).hexdigest() for b in en_blocks] == \
       [hashlib.sha256(b.encode()).hexdigest() for b in zh_blocks]
check("D6", "EN and ZH YAML blocks are byte-identical", same)

# try parsing them if PyYAML is available (soft check: script must not hard-fail without it)
try:
    import yaml  # noqa
    HAVE_YAML = True
except ImportError:
    HAVE_YAML = False
    check("D6", "PyYAML available to parse blocks", False, "pyyaml not installed (environment, not doc bug)")
if HAVE_YAML:
    bad = []
    for i, b in enumerate(en_blocks):
        try:
            list(yaml.safe_load_all(b))
        except Exception as e:
            bad.append((i, str(e)))
    check("D6", "all EN YAML blocks parse", not bad, "bad: %s" % bad)

# ------------------------------------------------- D7: heading structure
en_h = [l for l in en_text.splitlines() if re.match(r"^#{2,4} ", l)]
zh_h = [l for l in zh_text.splitlines() if re.match(r"^#{2,4} ", l)]
check("D7", "EN (%d) and ZH (%d) heading counts match" % (len(en_h), len(zh_h)), len(en_h) == len(zh_h))

# ------------------------------------------- D8: relative links resolve
links = sorted(set(re.findall(r"\]\((?!http)([^)#]+?)(?:#[^)]*)?\)", en_text + zh_text)))
bad_links = [l for l in links
             if not os.path.isfile(os.path.normpath(os.path.join(REPO_ROOT, "security", l)))]
check("D8", "all %d relative links resolve" % len(links), not bad_links, "broken: %s" % bad_links)

# ------------------------------------------------ D9: chart claims (static)
webhook_yaml = "charts/fluid/fluid/templates/webhook/webhookconfiguration.yaml"
ok, detail = file_has(webhook_yaml, r"failurePolicy: Fail")
# both webhooks use Fail; count occurrences
if os.path.isfile(os.path.join(REPO_ROOT, webhook_yaml)):
    n_fail = len(re.findall(r"failurePolicy: Fail", read(os.path.join(REPO_ROOT, webhook_yaml))))
    n_webhooks = len(re.findall(r"- name: (?:sidecar\.fuse\.fluid\.io|fuse\.serverful\.fluid\.io)", read(os.path.join(REPO_ROOT, webhook_yaml))))
    check("D9", "both injection webhooks render failurePolicy: Fail", n_fail >= 2 and n_webhooks >= 2,
          "fail=%d webhooks=%d" % (n_fail, n_webhooks))
    sel = read(os.path.join(REPO_ROOT, webhook_yaml))
    check("D9", "webhook objectSelectors are serverless.fuse.fluid.io / fuse.serverful.fluid.io only",
          "fuse.sidecar.fluid.io" not in sel and "serverless.fluid.io/inject" in sel)

values_yaml = "charts/fluid/fluid/values.yaml"
ok, detail = file_has(values_yaml, r"(?m)^helmDriver: configmap")
check("D9", "default helmDriver is configmap", ok, detail)
ok, detail = file_has("charts/fluid/fluid/templates/_helpers.tpl", r"fluid\.helmDriver\.rbacs")
check("D9", "fluid.helmDriver.rbacs helper exists", ok, detail)
ok, detail = file_has("charts/fluid/fluid/templates/_helpers.tpl", r'eq \.Values\.helmDriver "secret"')
check("D9", "Secret rules gated on helmDriver=secret", ok, detail)

# useNodeAuthorization default
ok, detail = file_has(values_yaml, r"useNodeAuthorization: true")
check("D9", "csi.useNodeAuthorization defaults to true", ok, detail)

# nodePublishMethod default bindMount
ok, detail = file_has(values_yaml, r"nodePublishMethod: bindMount")
check("D9", "csi.nodePublishMethod defaults to bindMount", ok, detail)

# no validating webhook shipped
ok, detail = file_has("pkg/webhook/handler/mutating", r".")
reg = os.path.join(REPO_ROOT, "pkg/webhook/handler")
has_validating = False
if os.path.isdir(reg):
    for root, _dirs, files in os.walk(reg):
        if "validating" in root:
            has_validating = True
        for f in files:
            pass
check("D9", "no validating webhook handler dir shipped", not has_validating)
webhook_dir = os.path.join(REPO_ROOT, "charts/fluid/fluid/templates/webhook")
vw = [f for f in os.listdir(webhook_dir) if os.path.isfile(os.path.join(webhook_dir, f))] if os.path.isdir(webhook_dir) else []
check("D9", "no ValidatingWebhookConfiguration in chart", not any("validating" in f.lower() for f in vw), str(vw))

# config/rbac editor/viewer roles not referenced by kustomization
kust = os.path.join(REPO_ROOT, "config/rbac/kustomization.yaml")
if os.path.isfile(kust):
    k = read(kust)
    refs_editor = re.findall(r"editor_role|viewer_role", k)
    check("D9", "kustomization.yaml does not reference editor/viewer roles", not refs_editor, str(refs_editor))

# ------------------------------------------------------------- report
print("Claim check results (PR #6211 security model docs)")
print("=" * 78)
failed = 0
for cid, desc, ok, detail in results:
    status = "PASS" if ok else "FAIL"
    if not ok:
        failed += 1
    line = "%-4s %-4s %s" % (cid, status, desc)
    if detail and not ok:
        line += "  [%s]" % detail
    print(line)
print("-" * 78)
print("%d checks, %d failed" % (len(results), failed))
sys.exit(1 if failed else 0)
