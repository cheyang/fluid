#!/usr/bin/env bash
# P0: the pinned scikit-learn 1.4.0 is affected by a published security advisory
# that 1.5.0 fixes (this is a Dependabot security update; the repo's
# dependabot.yml configures no pip ecosystem, so only security alerts trigger
# pip PRs). Verdict is decided by the GitHub Advisory Database, not by us.
set -u
OUT="${1:-/dev/stdout}"
JSON="$(curl -sf "https://api.github.com/advisories/GHSA-jw8x-6495-233v")" || {
  echo "FAIL: could not fetch GHSA-jw8x-6495-233v from GitHub advisory API" >"$OUT"; exit 1; }
echo "$JSON" | python3 -c '
import json, sys
d = json.load(sys.stdin)
v = d["vulnerabilities"][0]
ok = (d.get("cve_id") == "CVE-2024-5206"
      and v["package"]["name"] == "scikit-learn"
      and v["first_patched_version"] == "1.5.0"
      and v["vulnerable_version_range"] == "< 1.5.0")
print("advisory: %s / %s severity=%s package=%s vulnerable=%r patched=%s"
      % (d["ghsa_id"], d.get("cve_id"), d.get("severity"),
         v["package"]["name"], v["vulnerable_version_range"],
         v["first_patched_version"]))
if ok:
    print("PASS: scikit-learn 1.4.0 is in the vulnerable range; 1.5.0 is the fix")
    sys.exit(0)
print("FAIL: advisory shape not as expected")
sys.exit(1)
' >"$OUT"
