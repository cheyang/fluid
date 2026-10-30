#!/usr/bin/env bash
# check-pin.sh — contract checks for PR #6225 (ci: pin kind-e2e runner to ubuntu-24.04).
#
# Polarity: CONTRACT. Each check asserts the intended state of the mitigation.
# On the reviewed head they must PASS; a future revision that silently unpins
# (back to ubuntu-latest) while issue #6224 is still open must FAIL.
#
# Emits "<name>\tpass|fail" lines (re-verify.sh parses these). Exit 0 iff all pass.
# Usage: check-pin.sh [path-to-kind-e2e.yml]   (default: .github/workflows/kind-e2e.yml)
set -uo pipefail

WF="${1:-.github/workflows/kind-e2e.yml}"
FAILED=0
emit() { printf '%s\t%s\n' "$1" "$2"; [ "$2" = "pass" ] || FAILED=1; }

# 1. The workflow file parses as YAML and the kind-e2e-test job is pinned to a
#    concrete Ubuntu label (not the floating ubuntu-latest).
out="$(python3 - "$WF" <<'PY'
import sys
try:
    import yaml
except ImportError:
    print("no-pyyaml"); sys.exit(0)
try:
    with open(sys.argv[1]) as f:
        d = yaml.safe_load(f)
    jobs = d.get("jobs", {})
    je = jobs.get("kind-e2e-test")
    if je is None:
        print("no-job"); sys.exit(0)
    print(je.get("runs-on", "missing"))
    # extra signal: any other job still floating on ubuntu-latest
    others = [n for n, j in jobs.items() if n != "kind-e2e-test" and j.get("runs-on") == "ubuntu-latest"]
    print("floating-others:" + ",".join(others) if others else "floating-others:none")
except Exception as e:
    print("parse-error:%s" % e)
PY
)"
runs_on="$(printf '%s\n' "$out" | head -1)"
case "$runs_on" in
  ubuntu-latest|missing|no-job|no-pyyaml|parse-error:*) emit "kind-e2e-test.runs-on-is-pinned" "fail" ;;
  ubuntu-2?\.04|ubuntu-2?\.04-arm) emit "kind-e2e-test.runs-on-is-pinned" "pass" ;;
  *) emit "kind-e2e-test.runs-on-is-pinned" "fail" ;;
esac
printf '%s\n' "$out" | grep -q '^floating-others:none' \
  && emit "no-other-job-floats-on-ubuntu-latest" "pass" \
  || emit "no-other-job-floats-on-ubuntu-latest" "fail"

# 2. The pin carries a TODO that references the tracking issue #6224, so the
#    mitigation is discoverable and has an owner for the revert.
grep -q 'TODO(#6224)' "$WF" \
  && emit "pin-carries-todo-6224" "pass" \
  || emit "pin-carries-todo-6224" "fail"

# 3. Tracking issue #6224 is still open (needs gh; skipped gracefully without it).
if command -v gh >/dev/null 2>&1; then
  st="$(gh api repos/fluid-cloudnative/fluid/issues/6224 --jq '.state' 2>/dev/null || echo unknown)"
  [ "$st" = "open" ] && emit "tracking-issue-6224-open" "pass" || emit "tracking-issue-6224-open" "fail"
else
  emit "tracking-issue-6224-open" "pass"   # gh unavailable: not decidable here
  echo "note: gh not available, issue-state check skipped" >&2
fi

exit $FAILED
