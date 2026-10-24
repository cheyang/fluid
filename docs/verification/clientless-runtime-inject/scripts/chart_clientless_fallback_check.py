#!/usr/bin/env python3
"""Round-4 chart checks for the values-side clientless fallback (PR #6197).

The delta 268012a5..41b47141 makes charts/fluid/fluid/templates/webhook/
plugins-profile.yaml inject the chart's own clientless plugin group into
`webhook.pluginsProfile` when the values lack it (commit be4e30d3), so the
unconditional clientless.fluid.io webhook rule always has a plugin behind it,
and keeps null / non-map values rendering as before (commit 41b47141).

This script renders the real chart with the real helm CLI and checks:

1. (contract, A-F3 fixed) values that remove the clientless key still get the
   chart's group — RED on 268012a5 (the group was absent there);
2. (contract) default values render the clientless group and the
   checksum/plugins-profile annotation is the recorded baseline, so upgrading
   with default values does not roll the webhook;
3. (canary, A-F5) an explicit falsey `plugins.clientless` value (bare `[]`,
   `{}` or null) is overridden by the
   chart group — this is the CURRENT head behavior (Go-template truthiness
   treats the empty list as falsy, so `if get ... "clientless"` misses it).
   GREEN while the behavior stands; FLIPS RED if the author makes an explicit
   empty list stick — then invert this check into a contract;
4. (contract) `pluginsProfile: null` renders `null` and a non-map value
   renders verbatim, exactly as before the delta;
5. (contract) forceReplacePluginsProfile also gains the fallback group (the
   flag no longer implies verbatim values; documented in the round-4 ledger).

Run from the repo root:  python3 -I scripts/chart_clientless_fallback_check.py
Exit code 0 = contracts pass and the A-F5 canary holds.
"""
import os
import subprocess
import sys
import tempfile

import yaml

CHART = "charts/fluid/fluid"
# checksum/plugins-profile on the fluid-webhook Deployment for default values,
# recorded at heads 268012a5 and 41b47141 (byte-identical default renders).
BASELINE_CHECKSUM = "f3d8edf97844cd89c5e52f951b84c2cfe55e1ad046ed8dd152d6df594d98fbe0"

failures = []


def check(name, cond, detail):
    print(("PASS  " if cond else "FAIL  ") + name + " -- " + detail)
    if not cond:
        failures.append(name)


def render(values):
    """helm template the chart with the given values document."""
    fd, path = tempfile.mkstemp(suffix=".yaml")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(values)
        out = subprocess.run(
            ["helm", "template", "fluid", CHART, "-f", path],
            capture_output=True, text=True, check=True,
        )
        return out.stdout
    finally:
        os.unlink(path)


def profile_of(rendered):
    """The parsed pluginsProfile out of the webhook-plugins ConfigMap."""
    for doc in yaml.safe_load_all(rendered):
        if not isinstance(doc, dict):
            continue
        meta = doc.get("metadata") or {}
        if doc.get("kind") == "ConfigMap" and meta.get("name") == "webhook-plugins":
            return yaml.safe_load(doc["data"]["pluginsProfile"])
    raise AssertionError("webhook-plugins ConfigMap not found in render")


def checksum_of(rendered):
    for doc in yaml.safe_load_all(rendered):
        if not isinstance(doc, dict):
            continue
        meta = doc.get("metadata") or {}
        if doc.get("kind") == "Deployment" and meta.get("name") == "fluid-webhook":
            anns = (doc["spec"]["template"]["metadata"] or {}).get("annotations") or {}
            return anns.get("checksum/plugins-profile")
    raise AssertionError("fluid-webhook Deployment not found in render")


def clientless_group(profile):
    if not isinstance(profile, dict):
        return None
    plugins = profile.get("plugins")
    if not isinstance(plugins, dict):
        return None
    return plugins.get("clientless")


CHART_GROUP = {"withDataset": ["RuntimeConfigInjector"], "withoutDataset": []}

# 1. A-F3 fixed: values remove the clientless key -> chart group injected.
p = clientless_group(profile_of(render(
    "webhook:\n  pluginsProfile:\n    plugins:\n      clientless: null\n")))
check("A-F3 values fallback", p == CHART_GROUP,
      "values without a clientless group still get the chart's RuntimeConfigInjector group")

# 2. default values: group present, checksum unchanged (no webhook churn).
r = render("")
check("default render has the clientless group",
      clientless_group(profile_of(r)) == CHART_GROUP,
      "chart defaults keep the group from values.yaml")
check("default checksum/plugins-profile is the recorded baseline",
      checksum_of(r) == BASELINE_CHECKSUM,
      "default render byte-identical to 268012a5 -> no spurious webhook rollout")

# 3. A-F5 canary: explicit falsey value is overridden (current head behavior).
p = clientless_group(profile_of(render(
    "webhook:\n  pluginsProfile:\n    plugins:\n      clientless: []\n")))
check("A-F5 canary: explicit clientless: [] is overridden by the chart group",
      p == CHART_GROUP,
      "canary of the current truthiness behavior; flips when an explicit empty sticks")

# 3b. contract: the schema-valid empty group is respected (values-level opt-out).
p = clientless_group(profile_of(render(
    "webhook:\n  pluginsProfile:\n    plugins:\n"
    "      clientless:\n        withDataset: []\n        withoutDataset: []\n")))
check("empty-group opt-out form is respected verbatim",
      p == {"withDataset": [], "withoutDataset": []},
      "a 2-key empty map is truthy, so the explicit off switch survives")

# 4. null / non-map values render exactly as before the delta.
p = profile_of(render("webhook:\n  pluginsProfile: null\n"))
check("null pluginsProfile renders null", p is None, "same render as 268012a5")
p = profile_of(render("webhook:\n  pluginsProfile: \"not-a-map\"\n"))
check("non-map pluginsProfile renders verbatim", p == "not-a-map",
      "same render as 268012a5")

# 5. forceReplacePluginsProfile also gains the fallback group.
p = clientless_group(profile_of(render(
    "webhook:\n  forceReplacePluginsProfile: true\n  pluginsProfile:\n"
    "    plugins:\n      clientless: null\n")))
check("forceReplace also gains the fallback group", p == CHART_GROUP,
      "the flag no longer renders the values verbatim; documented in the round-4 ledger")

print()
if failures:
    print("FAILED: " + ", ".join(failures))
    sys.exit(1)
print("all round-4 chart checks green (A-F5 canary holding)")
