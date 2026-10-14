#!/usr/bin/env python3
# Unit-layer contract checks for PR fluid-cloudnative/fluid#6208
# (helm/kind-action v1.15.0 -> v1.15.1 pinned-SHA bump in two e2e workflows).
#
# Every check is a CONTRACT test: asserts the intended-correct state of the pin.
# On code that still pins v1.15.0 (the base branch) check `pin-integrity` FAILS —
# that is the reproduction, and the bump is what turns it green.
#
# Report mode (--report) prints one `NAME<TAB>pass|fail` line per check so
# re-verify.sh can consume it (custom layer kind).
#
# Checks:
#   pin-integrity      all helm/kind-action steps pin the same 40-hex SHA == expected
#   tag-identity       the pinned SHA is exactly the commit the upstream v1.15.1 tag points at
#   input-compat       every `with:` key used by these steps exists in the action.yml at the pinned SHA
#   explicit-overrides every kind-action step sets `version` and `kubectl_version` explicitly,
#                      so the v1.15.0->v1.15.1 default changes (kubectl v1.37.0->v1.37.1,
#                      cloud-provider-kind 0.6.0->0.12.0) cannot alter workflow behavior
#   yaml-valid         all workflow files still parse as YAML
import argparse
import json
import re
import sys
import urllib.request

RAW_ACTION_YML = (
    "https://raw.githubusercontent.com/helm/kind-action/{sha}/action.yml"
)
API_TAG_REF = (
    "https://api.github.com/repos/helm/kind-action/git/ref/tags/{tag}"
)
DEFAULT_EXPECTED_SHA = "1544676c07bb3570fcb70166beaa07c4528fd4aa"
DEFAULT_TAG = "v1.15.1"


def http_json(url):
    req = urllib.request.Request(url, headers={"User-Agent": "verify-harness"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def http_text(url):
    req = urllib.request.Request(url, headers={"User-Agent": "verify-harness"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.read().decode("utf-8")


def load_workflows(workflows_dir):
    import os
    files = sorted(
        os.path.join(workflows_dir, f)
        for f in os.listdir(workflows_dir)
        if f.endswith((".yml", ".yaml"))
    )
    out = []
    for path in files:
        with open(path, "r", encoding="utf-8") as fh:
            out.append((path, fh.read()))
    return out


def find_kind_steps(doc):
    """Yield (path, step) for every step using helm/kind-action."""
    for path, text in doc:
        import yaml
        data = yaml.safe_load(text)
        jobs = (data or {}).get("jobs") or {}
        # composite/ reusable workflows also allowed
        if not jobs and "steps" in (data or {}):
            jobs = {"<top>": data}
        for job_name, job in (jobs or {}).items():
            for step in (job or {}).get("steps") or []:
                uses = step.get("uses", "") if isinstance(step, dict) else ""
                if uses.startswith("helm/kind-action@"):
                    yield path, step, uses


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--workflows-dir", default=".github/workflows")
    ap.add_argument("--expected-sha", default=DEFAULT_EXPECTED_SHA)
    ap.add_argument("--tag", default=DEFAULT_TAG)
    ap.add_argument("--report", action="store_true",
                    help="machine-readable NAME<TAB>pass|fail output")
    args = ap.parse_args()

    checks = {}  # name -> (ok, detail)
    log = []

    def record(name, ok, detail):
        checks[name] = (ok, detail)
        log.append("[%s] %s: %s" % ("PASS" if ok else "FAIL", name, detail))

    try:
        docs = load_workflows(args.workflows_dir)
    except Exception as exc:  # noqa: BLE001
        print("cannot read workflows: %s" % exc, file=sys.stderr)
        sys.exit(2)

    steps = list(find_kind_steps(docs))

    # C1 pin-integrity: exactly one pinned SHA everywhere, equal to expected.
    shas = set()
    for path, _step, uses in steps:
        shas.add(uses.split("@", 1)[1].split()[0])
    if not steps:
        record("pin-integrity", False, "no helm/kind-action step found")
    elif len(shas) != 1:
        record("pin-integrity", False,
               "inconsistent pins across workflows: %s" % sorted(shas))
    else:
        sha = shas.pop()
        ok = re.fullmatch(r"[0-9a-f]{40}", sha) and sha == args.expected_sha
        record("pin-integrity", ok,
               "pinned %s (expected %s) in %d step(s)" %
               (sha, args.expected_sha, len(steps)))

    # C2 tag-identity: the pinned SHA is the commit the v1.15.1 tag points to.
    try:
        ref = http_json(API_TAG_REF.format(tag=args.tag))
        tagged = ref["object"]["sha"]
        # annotated tags: dereference to the commit
        if ref["object"]["type"] == "tag":
            tag_obj = http_json(
                "https://api.github.com/repos/helm/kind-action/git/tags/"
                + tagged)
            tagged = tag_obj["object"]["sha"]
        ok = tagged == args.expected_sha
        record("tag-identity", ok,
               "%s tag -> %s (pinned %s)" %
               (args.tag, tagged, args.expected_sha))
    except Exception as exc:  # noqa: BLE001
        record("tag-identity", False, "could not resolve tag: %s" % exc)

    # C3 input-compat: every `with:` key exists in the action.yml at the pinned SHA.
    try:
        action_yml_text = http_text(
            RAW_ACTION_YML.format(sha=args.expected_sha))
        import yaml
        action_inputs = set((yaml.safe_load(action_yml_text) or {})
                            .get("inputs", {}).keys())
        missing = []
        for path, step, _uses in steps:
            for key in (step.get("with") or {}).keys():
                if key not in action_inputs:
                    missing.append("%s: %s" % (path, key))
        record("input-compat", not missing,
               "unknown input(s): %s" % (missing or "none"))
    except Exception as exc:  # noqa: BLE001
        record("input-compat", False,
               "could not fetch/parse action.yml: %s" % exc)

    # C4 explicit-overrides: version + kubectl_version set on every step.
    implicit = []
    for path, step, _uses in steps:
        withs = step.get("with") or {}
        for key in ("version", "kubectl_version"):
            if key not in withs:
                implicit.append("%s: %s" % (path, key))
    record("explicit-overrides", not implicit,
           "relying on action defaults for: %s" % (implicit or "nothing"))

    # C5 yaml-valid: every workflow file parses.
    bad = []
    try:
        import yaml
        for path, text in docs:
            try:
                yaml.safe_load(text)
            except yaml.YAMLError as exc:
                bad.append("%s: %s" % (path, exc))
        record("yaml-valid", not bad, "unparseable: %s" % (bad or "none"))
    except ImportError:
        record("yaml-valid", False, "PyYAML not available")

    for line in log:
        print(line)
    if args.report:
        for name, (ok, _detail) in checks.items():
            print("%s\t%s" % (name, "pass" if ok else "fail"))
    sys.exit(0 if all(ok for ok, _ in checks.values()) else 1)


if __name__ == "__main__":
    main()
