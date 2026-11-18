#!/usr/bin/env python3
"""Contract test (layer: lockfile/docs): the regeneration recipe in the header
comment of samples/vineyard/requirements.txt stays in sync with the pins in the
file itself.

The header documents how to regenerate the whole lockfile:

    printf "pandas\\nrequests\\nnumpy\\nvineyard\\nscikit-learn==1.4.0\\njoblib==1.3.2\\n" \\
        | uv pip compile ...

If a version bump changes the pin but not the recipe, the documented command no
longer reproduces the file — a maintainer following it silently reverts the bump.

PASS  = every `pkg==ver` named in the header recipe equals the lockfile pin.
FAIL  = at least one recipe pin diverges (F1 in the review findings).

Usage: check_header_recipe.py <requirements.txt>
"""
import re
import sys


def main():
    path = sys.argv[1]
    lines = open(path, encoding="utf-8").read().splitlines()

    recipe = ""
    for ln in lines:
        if ln.lstrip().startswith("#"):
            recipe += ln.lstrip().lstrip("#").strip() + "\n"
        else:
            break  # header comment block only

    # the recipe is a printf line whose separators are literal backslash-n
    recipe = recipe.replace("\\n", " ")
    recipe_pins = dict(re.findall(r"([A-Za-z0-9_-]+)==([0-9][^\s\\\"]*)", recipe))
    if not recipe_pins:
        print("HEADER_RECIPE_CONTRACT: FAIL")
        print("  no `pkg==ver` pins found in the header recipe — recipe missing?")
        return 1

    lock_pins = {}
    for ln in lines:
        m = re.match(r"^([A-Za-z0-9_.-]+)==(\S+)", ln)
        if m:
            lock_pins[m.group(1).lower()] = m.group(2)

    failures = []
    for pkg, ver in sorted(recipe_pins.items()):
        actual = lock_pins.get(pkg.lower())
        if actual is None:
            failures.append("recipe names %s==%s but the lockfile has no %s pin" % (pkg, ver, pkg))
        elif actual != ver:
            failures.append("recipe says %s==%s but lockfile pins %s==%s" % (pkg, ver, pkg, actual))

    print("header recipe pins: " + ", ".join("%s==%s" % kv for kv in sorted(recipe_pins.items())))
    if failures:
        print("HEADER_RECIPE_CONTRACT: FAIL")
        for f in failures:
            print("  " + f)
        return 1
    print("HEADER_RECIPE_CONTRACT: PASS")
    return 0


if __name__ == "__main__":
    sys.exit(main())
