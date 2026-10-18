#!/usr/bin/env python3
"""Checksum-stability check for the plugins-profile upgrade path (B-F6, PR #6197).

`charts/fluid/fluid/templates/webhook/webhook.yaml` annotates the webhook
Deployment with `checksum/plugins-profile: sha256(rendered plugins-profile.yaml)`,
so any byte change in that manifest rolls the webhook once more. Round 2 found
(B-F6, Reviewer B's probe on 6d3a6d7) that the verbatim branch re-indented the
stored profile together with its trailing newline, so the SECOND upgrade
re-rendered the same parsed profile with different bytes — one spurious
rollout. Fixed at head by `| trimSuffix "\\n"` before `indent 4`
(commits 16c5509e + 760f5369).

This script models the template's existing-profile branches with the exact
function semantics helm/sprig use:

  - helm `toYaml`        : yaml dump (map keys sorted, like Go yaml) with the
                           trailing newline stripped;
  - sprig `indent n`     : pad + s.replace("\\n", "\\n" + pad)  (a trailing
                           newline therefore yields a trailing pad line);
  - sprig `trimSuffix`   : Go strings.TrimSuffix (one occurrence);
  - block scalar `|` clip: the stored ConfigMap value regains exactly one
                           trailing newline when the API server parses it.

Scenario (default values, no forceReplacePluginsProfile):

  fresh install : no existing profile      -> values branch;
  upgrade 1     : legacy profile without clientless -> MERGE branch;
  upgrade 2     : stored merged profile (has clientless) -> VERBATIM branch.

Contract: upgrade 1 and upgrade 2 render byte-identical blocks, so the
checksum — and the webhook rollout — do not change a second time.
`--show-prefix` models the pre-fix template (no trimSuffix) and reports the
churn the round-2 probe observed, demonstrating the check bites.

When run from the repo root with helm available, the fresh-install `plugins`
sub-block predicted by the model is compared against the real `helm template`
render, which validates the toYaml/indent modeling on the content the merge
touches. (The full profile contains a long literal-block string whose exact
serialization differs between Go and PyYAML; it is irrelevant to the churn
invariant, which holds for any toYaml-produced profile.)
"""
import hashlib
import subprocess
import sys

import yaml


def to_yaml(v):
    """helm's toYaml: yaml dump with sorted map keys, trailing newline stripped."""
    return yaml.safe_dump(v, default_flow_style=False, sort_keys=True).rstrip("\n")


def indent(n, s):
    """sprig's indent: pad + replace internal newlines with newline+pad."""
    pad = " " * n
    return pad + s.replace("\n", "\n" + pad)


def trim_suffix(s, suffix):
    """Go strings.TrimSuffix: strip one trailing occurrence."""
    return s[: -len(suffix)] if s.endswith(suffix) else s


def load_values_plugins(values_path):
    with open(values_path) as f:
        values = yaml.safe_load(f)
    return values["webhook"]["pluginsProfile"]["plugins"]


def merge_branch(stored_profile, clientless):
    """fromYaml -> guard -> set plugins.clientless -> toYaml | indent 4."""
    profile = yaml.safe_load(stored_profile)
    profile["plugins"]["clientless"] = clientless
    return indent(4, to_yaml(profile))


def verbatim_branch(stored_profile, fix=True):
    """`$existingProfile | [trimSuffix "\\n" |] indent 4`."""
    s = stored_profile if not fix else trim_suffix(stored_profile, "\n")
    return indent(4, s)


def main():
    show_prefix = "--show-prefix" in sys.argv
    failures = []

    values_path = "charts/fluid/fluid/values.yaml"
    try:
        plugins = load_values_plugins(values_path)
    except OSError:
        print("FAIL: run from the repo root (cannot read %s)" % values_path)
        return 1
    clientless = plugins["clientless"]
    legacy = {k: v for k, v in plugins.items() if k != "clientless"}

    # fresh install: values branch (model)
    fresh = indent(4, to_yaml({"plugins": plugins}))

    # upgrade 1 over the legacy stored profile (toYaml + the block's newline)
    legacy_stored = to_yaml({"plugins": legacy}) + "\n"
    upgrade1 = merge_branch(legacy_stored, clientless)

    # upgrade 2 over what upgrade 1 stored: the rendered block parsed as a
    # block scalar (de-indent, then clip re-adds exactly one trailing newline)
    value_lines = [ln[4:] if ln.startswith("    ") else ln
                   for ln in upgrade1.split("\n")]
    stored_after_1 = "\n".join(value_lines) + "\n"
    upgrade2 = verbatim_branch(stored_after_1, fix=True)

    if upgrade1 != upgrade2:
        failures.append(
            "fixed template still re-renders: upgrade1 != upgrade2\n"
            "  u1 tail: %r\n  u2 tail: %r" % (upgrade1[-40:], upgrade2[-40:]))

    if show_prefix:
        pre2 = verbatim_branch(stored_after_1, fix=False)
        churn = "CHURN (byte diff)" if pre2 != upgrade1 else "no churn?"
        sha1 = hashlib.sha256(upgrade1.encode()).hexdigest()[:12]
        sha2 = hashlib.sha256(pre2.encode()).hexdigest()[:12]
        print("pre-fix model: second upgrade render %s; block sha %s -> %s"
              % (churn, sha1, sha2))

    # helm cross-check: the model's fresh-install plugins sub-block vs the
    # real render (validates toYaml/indent on the merge-relevant content)
    try:
        out = subprocess.run(["helm", "template", "fluid", "charts/fluid/fluid"],
                             capture_output=True, text=True, timeout=120).stdout
        lines = out.split("\n")
        start = None
        for i, ln in enumerate(lines):
            if ln == "    plugins:":
                start = i
                break
        if start is not None:
            block = []
            for ln in lines[start:]:
                if ln.startswith("      ") or ln.startswith("    "):
                    block.append(ln)
                else:
                    break
            real_block = "\n".join(block)
            model_block = indent(4, to_yaml({"plugins": plugins}))
            if real_block != model_block:
                failures.append(
                    "toYaml/indent model does not match the real helm render:\n"
                    "  model: %r\n  real : %r" % (model_block[:80], real_block[:80]))
            else:
                print("helm cross-check: fresh-install model matches the real render")
    except (OSError, subprocess.SubprocessError):
        print("helm not available; skipped the fresh-install cross-check")

    if failures:
        print("FAIL")
        for f in failures:
            print(" - " + f)
        return 1
    print("PASS: the second upgrade renders byte-identical bytes to the first,")
    print("      so checksum/plugins-profile does not roll the webhook a second time.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
