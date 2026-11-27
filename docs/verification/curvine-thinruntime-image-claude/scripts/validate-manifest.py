#!/usr/bin/env python3
"""validate-manifest.py — schema check for addons/curvine/runtime-profile.yaml.

Contract test (polarity: contract — FAILS if the shipped example manifest stops
matching the ThinRuntimeProfile CRD schema or stops carrying the fields the
Curvine image needs). Validates, without a cluster:

  1. the manifest decodes as apiVersion data.fluid.io/v1alpha1,
     kind ThinRuntimeProfile;
  2. every field it sets under spec exists in the CRD's openAPI v3 schema
     (config/crd/bases/data.fluid.io_thinruntimeprofiles.yaml) with a
     compatible type;
  3. the fuse block carries image/imageTag/imagePullPolicy/command/args with
     the exact values the Curvine runtime image requires
     (ghcr.io/curvineio/curvine, /entrypoint.sh, fluid-thin-runtime);
  4. the same holds for the two ThinRuntimeProfile snippets embedded in
     addons/curvine/readme.md and readme-zh_CN.md (extracted from the fenced
     heredoc blocks) and the dev-guide profiles.

Usage: python3 validate-manifest.py [repo-root]
"""
import re
import sys

import yaml

EXPECT_IMAGE = "ghcr.io/curvineio/curvine"
EXPECT_TAG = "latest"
EXPECT_POLICY = "IfNotPresent"
EXPECT_COMMAND = ["/entrypoint.sh"]
EXPECT_ARGS = ["fluid-thin-runtime"]


def crd_props(root):
    with open(f"{root}/config/crd/bases/data.fluid.io_thinruntimeprofiles.yaml") as f:
        crd = yaml.safe_load(f)
    ver = crd["spec"]["versions"]
    # prefer v1alpha1
    for v in ver:
        if v["name"] == "v1alpha1":
            return v["schema"]["openAPIV3Schema"]["properties"]
    return ver[0]["schema"]["openAPIV3Schema"]["properties"]


def check_against_schema(props, obj, path, errors):
    for k, v in obj.items():
        if k not in props:
            errors.append(f"{path}.{k}: field not in CRD schema")
            continue
        typ = props[k].get("type")
        child = props[k].get("properties")
        if typ == "object" and child and isinstance(v, dict):
            check_against_schema(child, v, f"{path}.{k}", errors)
        elif typ == "array" and not isinstance(v, list):
            errors.append(f"{path}.{k}: CRD wants array, got {type(v).__name__}")
        elif typ == "string" and not isinstance(v, str):
            errors.append(f"{path}.{k}: CRD wants string, got {type(v).__name__}")


def check_fuse(profile, src, errors):
    fuse = profile.get("spec", {}).get("fuse", {})
    if fuse.get("image") != EXPECT_IMAGE:
        errors.append(f"{src}: fuse.image={fuse.get('image')!r}, want {EXPECT_IMAGE!r}")
    if fuse.get("imageTag") != EXPECT_TAG:
        errors.append(f"{src}: fuse.imageTag={fuse.get('imageTag')!r}, want {EXPECT_TAG!r}")
    if fuse.get("imagePullPolicy") != EXPECT_POLICY:
        errors.append(f"{src}: fuse.imagePullPolicy={fuse.get('imagePullPolicy')!r}, want {EXPECT_POLICY!r}")
    if fuse.get("command") != EXPECT_COMMAND:
        errors.append(f"{src}: fuse.command={fuse.get('command')!r}, want {EXPECT_COMMAND!r}")
    if fuse.get("args") != EXPECT_ARGS:
        errors.append(f"{src}: fuse.args={fuse.get('args')!r}, want {EXPECT_ARGS!r}")


def yaml_docs_from_md(path):
    """Extract the ThinRuntimeProfile yaml blocks from a readme/dev-guide."""
    blocks = []
    in_block = False
    current = []
    for line in open(path):
        if not in_block and line.startswith("```"):
            in_block = True
            current = []
            continue
        if in_block and line.startswith("```"):
            blocks.append("\n".join(current))
            in_block = False
            continue
        if in_block:
            current.append(line.rstrip("\n"))
    for block in blocks:
        # strip shell heredoc wrapper (`$ cat <<EOF > ...` / `$ kubectl apply`)
        lines = [
            ln for ln in block.splitlines()
            if not ln.strip().startswith("$") and ln.strip() != "EOF"
        ]
        body = "\n".join(lines)
        if "ThinRuntimeProfile" not in body:
            continue
        try:
            doc = yaml.safe_load(body)
        except yaml.YAMLError as e:
            continue
        if isinstance(doc, dict) and doc.get("kind") == "ThinRuntimeProfile":
            yield doc


def main():
    root = sys.argv[1] if len(sys.argv) > 1 else "."
    props = crd_props(root)
    errors = []
    checked = []

    manifest = f"{root}/addons/curvine/runtime-profile.yaml"
    doc = yaml.safe_load(open(manifest))
    if doc.get("apiVersion") != "data.fluid.io/v1alpha1" or doc.get("kind") != "ThinRuntimeProfile":
        errors.append(f"{manifest}: not a v1alpha1 ThinRuntimeProfile")
    check_against_schema(props, doc, "", errors)
    check_fuse(doc, manifest, errors)
    checked.append(manifest)

    for md in [
        f"{root}/addons/curvine/readme.md",
        f"{root}/addons/curvine/readme-zh_CN.md",
        f"{root}/addons/curvine/dev-guide/curvine.md",
        f"{root}/addons/curvine/dev-guide/curvine-zh_CN.md",
    ]:
        for i, prof in enumerate(yaml_docs_from_md(md)):
            check_against_schema(props, prof, "", errors)
            check_fuse(prof, f"{md}[profile#{i}]", errors)
            checked.append(f"{md}[profile#{i}]")

    print(f"validated {len(checked)} profile doc(s):")
    for c in checked:
        print(f"  - {c}")
    if errors:
        print("FAIL:")
        for e in errors:
            print(f"  - {e}")
        sys.exit(1)
    print("PASS: manifest and embedded profiles match the CRD schema and carry the required image/command/args")


if __name__ == "__main__":
    main()
