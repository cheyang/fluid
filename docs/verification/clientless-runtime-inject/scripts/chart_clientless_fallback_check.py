#!/usr/bin/env python3
"""Round-4/5 chart checks for the values-side clientless fallback (PR #6197).

Round 4 (delta 268012a5..41b47141) made charts/fluid/fluid/templates/webhook/
plugins-profile.yaml inject the chart's own clientless plugin group when the
values lack it, so the unconditional clientless.fluid.io webhook rule always has
a plugin behind it, and kept null / non-map values rendering as before.

Round 5 (delta 41b47141..9c8d0e9, commit bcd497c7a) switches the gate from
Go-template truthiness to hasKey, so an *explicitly set* clientless group is
kept verbatim, even when falsey. This script re-models the shapes with the real
helm CLI:

1. (contract, A-F3) values that LACK the clientless key still get the chart's
   group. A plain `-f` override cannot express "lacks the key" (helm coalesces
   the chart's values.yaml defaults back in), so this is modeled the way the
   real trigger works -- `helm upgrade --reuse-values` from a pre-clientless
   release, whose effective values have no clientless key: a copy of the chart
   with the clientless group removed from values.yaml, rendered with no -f.
   Checked with forceReplacePluginsProfile both false and true;
2. (contract) default values render the clientless group and the
   checksum/plugins-profile annotation is the recorded baseline (unchanged from
   268012a5 through 9c8d0e9), so upgrading with default values does not roll
   the webhook -- including across the round-5 hasKey change and the label
   rename, neither of which touches the rendered profile bytes;
3. (contract, A-F5 round-5 semantics) an explicit falsey value is no longer
   replaced *by the template*: `clientless: []` renders as `[]` (a list replaces
   the map during helm coalescing, and hasKey keeps it). NOTE: `{}` and null
   keep the chart's default group through helm's own map coalescing before the
   template runs -- that is helm merge semantics, uniform with every other map
   value in the chart, not the template overriding an explicit choice;
4. (contract) the schema-valid empty-group opt-out documented in values.yaml
   (withDataset: [], withoutDataset: []) is respected verbatim;
5. (contract) null / non-map pluginsProfile values render exactly as before;
6. (canary, A-F7) `clientless: []` sticks and is NOT a valid plugins profile:
   the rendered profile carries a list under plugins.clientless, which the
   webhook's yaml unmarshal rejects at startup (see
   findings_claude_r5_test.go). The chart does not guard the spelling; green
   while that stands.
7. (contract, round-5 label rename) the clientless webhook rule selects pods
   with the clientless.fluid.io/inject label.

Run from the repo root:  python3 -I scripts/chart_clientless_fallback_check.py
Exit code 0 = contracts pass and the A-F7 canary holds.
"""
import os
import shutil
import subprocess
import sys
import tempfile

import yaml

CHART = "charts/fluid/fluid"
# checksum/plugins-profile on the fluid-webhook Deployment for default values,
# recorded at heads 268012a5, 41b47141 and 9c8d0e9 (byte-identical default renders).
BASELINE_CHECKSUM = "f3d8edf97844cd89c5e52f951b84c2cfe55e1ad046ed8dd152d6df594d98fbe0"

failures = []


def check(name, cond, detail):
    print(("PASS  " if cond else "FAIL  ") + name + " -- " + detail)
    if not cond:
        failures.append(name)


def render(values, chart=CHART):
    """helm template the chart with the given values document."""
    fd, path = tempfile.mkstemp(suffix=".yaml")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(values)
        out = subprocess.run(
            ["helm", "template", "fluid", chart, "-f", path],
            capture_output=True, text=True, check=True,
        )
        return out.stdout
    finally:
        os.unlink(path)


def render_doc(documents):
    """All dict documents of the rendered output."""
    for doc in yaml.safe_load_all(documents):
        if isinstance(doc, dict):
            yield doc


def profile_of(rendered):
    """The parsed pluginsProfile out of the webhook-plugins ConfigMap."""
    for doc in render_doc(rendered):
        meta = doc.get("metadata") or {}
        if doc.get("kind") == "ConfigMap" and meta.get("name") == "webhook-plugins":
            return yaml.safe_load(doc["data"]["pluginsProfile"])
    raise AssertionError("webhook-plugins ConfigMap not found in render")


def checksum_of(rendered):
    for doc in render_doc(rendered):
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


def chart_without_clientless_defaults():
    """A copy of the chart whose values.yaml has no clientless group.

    This models the effective values of `helm upgrade --reuse-values` from a
    release that predates the clientless feature (or any values source that
    genuinely lacks the key): the values' plugins map has no clientless key,
    so the template's values-render branch is the one that runs.
    """
    tmp = tempfile.mkdtemp(prefix="fluid-chart-noclientless-")
    shutil.copytree(CHART, os.path.join(tmp, "fluid"))
    values_path = os.path.join(tmp, "fluid", "values.yaml")
    with open(values_path) as f:
        values = yaml.safe_load(f)
    del values["webhook"]["pluginsProfile"]["plugins"]["clientless"]
    with open(values_path, "w") as f:
        yaml.safe_dump(values, f, default_flow_style=False)
    return os.path.join(tmp, "fluid"), tmp


CHART_GROUP = {"withDataset": ["RuntimeConfigInjector"], "withoutDataset": []}

# 1. A-F3 holds after the hasKey change: a genuinely missing key still falls back.
nokey_chart, tmpdir = chart_without_clientless_defaults()
try:
    p = clientless_group(profile_of(render("", chart=nokey_chart)))
    check("A-F3 values fallback (values lack the key)", p == CHART_GROUP,
          "values without a clientless group still get the chart's RuntimeConfigInjector group")

    p = clientless_group(profile_of(render(
        "webhook:\n  forceReplacePluginsProfile: true\n", chart=nokey_chart)))
    check("A-F3 values fallback under forceReplacePluginsProfile", p == CHART_GROUP,
          "force-replacing values that lack the group still gets the chart's group (documented in values.yaml)")
finally:
    shutil.rmtree(tmpdir)

# 2. default values: group present, checksum unchanged (no webhook churn).
r = render("")
check("default render has the clientless group",
      clientless_group(profile_of(r)) == CHART_GROUP,
      "chart defaults keep the group from values.yaml")
check("default checksum/plugins-profile is the recorded baseline",
      checksum_of(r) == BASELINE_CHECKSUM,
      "default render byte-identical to 268012a5/41b47141 -> no spurious webhook rollout across the round-5 delta")

# 3. A-F5 round-5 semantics: the template no longer replaces an explicit falsey
#    value. `[]` is the only falsey spelling a -f file can deliver verbatim
#    (lists replace maps during helm coalescing; maps and nulls merge with the
#    chart defaults instead -- helm semantics, not the template).
p = clientless_group(profile_of(render(
    "webhook:\n  pluginsProfile:\n    plugins:\n      clientless: []\n")))
check("A-F5 round 5: explicit clientless: [] is kept, not replaced",
      p == [],
      "hasKey keeps the explicit list; the webhook-side consequence is the A-F7 canary below")

p = clientless_group(profile_of(render(
    "webhook:\n  pluginsProfile:\n    plugins:\n      clientless: null\n")))
check("explicit clientless: null coalesces to the chart default group (helm merge)",
      p == CHART_GROUP,
      "helm keeps the chart's default for null/-f overrides before the template runs; the template-level null passthrough is covered by the pluginsProfile: null check")

p = clientless_group(profile_of(render(
    "webhook:\n  pluginsProfile:\n    plugins:\n      clientless: {}\n")))
check("explicit clientless: {} coalesces to the chart default group (helm merge)",
      p == CHART_GROUP,
      "an empty map merges with the chart defaults at the inner keys; use the 2-key empty group to switch it off")

# 4. the schema-valid empty-group opt-out form is respected (values-level off switch).
p = clientless_group(profile_of(render(
    "webhook:\n  pluginsProfile:\n    plugins:\n"
    "      clientless:\n        withDataset: []\n        withoutDataset: []\n")))
check("empty-group opt-out form is respected verbatim",
      p == {"withDataset": [], "withoutDataset": []},
      "the off switch documented in values.yaml works")

# 5. null / non-map values render exactly as before the deltas.
p = profile_of(render("webhook:\n  pluginsProfile: null\n"))
check("null pluginsProfile renders null", p is None, "same render as 268012a5")
p = profile_of(render("webhook:\n  pluginsProfile: \"not-a-map\"\n"))
check("non-map pluginsProfile renders verbatim", p == "not-a-map",
      "same render as 268012a5")

# 6. A-F7 canary: the [] spelling reaches the profile unparseable by the webhook.
raw = render("webhook:\n  pluginsProfile:\n    plugins:\n      clientless: []\n")
profile = profile_of(raw)
check("A-F7 canary: the rendered profile carries a list-shaped clientless group",
      isinstance(profile, dict) and profile.get("plugins", {}).get("clientless") == [],
      "the webhook's yaml.Unmarshal rejects this at startup (findings_claude_r5_test.go); the chart does not guard the spelling")

# 7. round-5 label rename: the webhook rule selects the renamed label.
selector_ok = False
rule_ok = False
for doc in render_doc(raw):
    if doc.get("kind") == "MutatingWebhookConfiguration":
        for wh in doc.get("webhooks", []):
            sel = ((wh.get("objectSelector") or {}).get("matchLabels") or {})
            if wh.get("name") == "clientless.fluid.io":
                rule_ok = "clientless.fluid.io/inject" in sel and sel.get("clientless.fluid.io/inject") == "true"
                selector_ok = selector_ok or rule_ok
check("clientless rule selects the clientless.fluid.io/inject label",
      rule_ok and selector_ok,
      "the objectSelector follows the label rename of commit 9c8d0e9fc")

print()
if failures:
    print("FAILED: " + ", ".join(failures))
    sys.exit(1)
print("all chart checks green (A-F5 template semantics fixed; A-F7 canary holding)")
