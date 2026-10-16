#!/usr/bin/env python3
"""Offline check of the plugins-profile upgrade merge in PR #6197.

`charts/fluid/fluid/templates/webhook/plugins-profile.yaml` keeps an existing
webhook-plugins ConfigMap on helm upgrade and merges the new `clientless` plugin
group into it (fromYaml -> set plugins.clientless -> toYaml). helm's `lookup`
does not run under `helm template`, so this script reproduces the merge with
plain YAML round-trip semantics (the same parse-modify-serialize that
fromYaml/set/toYaml perform) and asserts:

1. the merged profile contains the clientless group from values;
2. everything else in the old profile survives the round-trip semantically,
   including pluginConfig args written as a block scalar;
3. an existing profile that already has a clientless key is left untouched;
4. an unparseable existing profile falls through unchanged (kept as-is).

Exit code 0 = all checks pass.
"""
import copy
import sys

import yaml

OLD_PROFILE = """plugins:
  serverful:
    withDataset:
    - FilePrefetcher
    - RequireNodeWithFuse
    - NodeAffinityWithCache
    - MountPropagationInjector
    - DatasetUsageInjector
    withoutDataset:
    - PreferNodesWithoutCache
  serverless:
    withDataset:
    - FilePrefetcher
    - FuseSidecar
    - DatasetUsageInjector
    withoutDataset: []
pluginConfig:
- name: NodeAffinityWithCache
  args: |
    preferred:
      - name: fluid.io/node
        weight: 100
      - name: topology.kubernetes.io/zone
        weight: 50
    required:
      - fluid.io/node
- name: FilePrefetcher
  args: |
    pvc: true
"""

NEW_CLIENTLESS = {"withDataset": ["RuntimeConfigInjector"], "withoutDataset": []}


def helm_merge(existing_profile: str, clientless_from_values: dict) -> str:
    """The template's merge branch: fromYaml -> guard -> set -> toYaml."""
    try:
        profile = yaml.safe_load(existing_profile)
    except yaml.YAMLError:
        profile = {"Error": "parse error"}  # helm fromYaml puts an Error key in the map
    if not isinstance(profile, dict) or "Error" in profile:
        return existing_profile  # template: parse error -> keep as-is
    plugins = profile.get("plugins")
    if not isinstance(plugins, dict) or "clientless" in plugins:
        return existing_profile  # template: not mergeable / already merged -> keep as-is
    merged = copy.deepcopy(profile)
    merged["plugins"]["clientless"] = copy.deepcopy(clientless_from_values)
    return yaml.safe_dump(merged, default_flow_style=False, sort_keys=False)


def main() -> int:
    failures = []

    # 1 + 2: merge adds clientless and preserves the rest semantically
    merged = helm_merge(OLD_PROFILE, NEW_CLIENTLESS)
    parsed = yaml.safe_load(merged)
    if parsed["plugins"].get("clientless") != NEW_CLIENTLESS:
        failures.append("clientless group not merged in: %r" % parsed["plugins"].get("clientless"))
    old = yaml.safe_load(OLD_PROFILE)
    for key in ("serverful", "serverless"):
        if parsed["plugins"][key] != old["plugins"][key]:
            failures.append("plugins.%s changed by the merge" % key)
    if parsed.get("pluginConfig") != old["pluginConfig"]:
        failures.append("pluginConfig changed by the merge")
    # the block-scalar args must still parse as the same YAML string
    args = parsed["pluginConfig"][0]["args"]
    if not isinstance(args, str) or yaml.safe_load(args)["preferred"][0]["name"] != "fluid.io/node":
        failures.append("pluginConfig args did not survive the round-trip: %r" % args)

    # 3: an existing profile that already has clientless is kept verbatim
    already = OLD_PROFILE.replace(
        "  serverless:", "  clientless:\n    withDataset:\n    - RuntimeConfigInjector\n    withoutDataset: []\n  serverless:")
    if helm_merge(already, NEW_CLIENTLESS) != already:
        failures.append("profile with existing clientless key was rewritten")

    # 4: an unparseable profile falls through unchanged
    broken = "plugins: [unclosed"
    if helm_merge(broken, NEW_CLIENTLESS) != broken:
        failures.append("unparseable profile was not kept as-is")

    if failures:
        print("FAIL")
        for f in failures:
            print(" - " + f)
        return 1
    print("PASS: merge adds clientless, preserves serverful/serverless/pluginConfig,")
    print("      keeps an already-merged or unparseable profile untouched.")
    print("--- merged profile head ---")
    print("\n".join(merged.splitlines()[:12]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
