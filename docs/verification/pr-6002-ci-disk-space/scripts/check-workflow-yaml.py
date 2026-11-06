#!/usr/bin/env python3
"""check-workflow-yaml.py — L1 structural check of the PR's workflow change.

Parses .github/workflows/backward-compatibility-e2e.yml in a given repo dir and
asserts what PR #6002 claims to add:

  workflow_yaml_parses          file is valid YAML with the expected job
  free_disk_step_present        a "Free disk space" step exists
  free_disk_step_ordering       Create-kind < Free-disk-space < Build-images
  free_disk_step_contents       step removes the big preinstalled toolchains and
                                prunes the host docker image store

Output: TAB-separated "name<TAB>pass|fail" lines on stdout.

Usage: check-workflow-yaml.py <repo-dir>
"""
import sys

def verdict(name, ok):
    print("%s\t%s" % (name, "pass" if ok else "fail"))

def main():
    repo = sys.argv[1]
    path = repo.rstrip("/") + "/.github/workflows/backward-compatibility-e2e.yml"
    try:
        import yaml
    except ImportError:
        print("PyYAML required", file=sys.stderr)
        for t in ("workflow_yaml_parses", "free_disk_step_present",
                  "free_disk_step_ordering", "free_disk_step_contents"):
            verdict(t, False)
        return 0
    try:
        with open(path) as f:
            doc = yaml.safe_load(f)
        verdict("workflow_yaml_parses", True)
    except Exception as e:
        print("yaml parse failed: %s" % e, file=sys.stderr)
        for t in ("workflow_yaml_parses", "free_disk_step_present",
                  "free_disk_step_ordering", "free_disk_step_contents"):
            verdict(t, False)
        return 0

    steps = ((doc.get("jobs") or {}).get("backward-compat-test") or {}).get("steps") or []
    names = [s.get("name", "") for s in steps if isinstance(s, dict)]

    verdict("free_disk_step_present", "Free disk space" in names)

    def idx(name):
        return names.index(name) if name in names else -1

    i_kind = idx("Create k8s Kind Cluster")
    i_free = idx("Free disk space")
    i_build = idx("Build current fluid docker images")
    verdict("free_disk_step_ordering",
            i_kind != -1 and i_free != -1 and i_build != -1 and i_kind < i_free < i_build)

    body = ""
    for s in steps:
        if isinstance(s, dict) and s.get("name") == "Free disk space":
            body = s.get("run", "") or ""
    need = ["/usr/share/dotnet", "/usr/local/lib/android", "/opt/ghc", "docker image prune"]
    missing = [n for n in need if n not in body]
    if missing:
        print("free_disk step missing: %s" % ", ".join(missing), file=sys.stderr)
    verdict("free_disk_step_contents", not missing)
    return 0

if __name__ == "__main__":
    sys.exit(main())
