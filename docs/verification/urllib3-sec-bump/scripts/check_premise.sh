#!/usr/bin/env bash
# P0 — premise check for PR #6201 (urllib3 2.7.0 -> 2.8.0 in the mooncake e2e image).
#
# Claim under test (from the PR, a Dependabot security bump):
#   "master pins urllib3 2.7.0, which is inside the vulnerable range of published
#    urllib3 security advisories fixed in 2.8.0."
#
# This check runs against the BASE branch (origin/master), not the PR head:
# the question is whether the problem exists *today*. It is a fact-check, not a
# code-path reproduction — the "symptom" is the presence of a known-vulnerable
# pin in the repo's only urllib3 manifest.
#
# PASS (exit 0)  = premise CONFIRMED  (base ships a pin inside a vulnerable range)
# FAIL (exit 1)  = premise REFUTED    (base pin is not in any vulnerable range,
#                                      or the advisories do not exist)
#
# Polarity: contract. Written 2026-10-08 by Reviewer A (Claude).
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
BASE_REF="${VERIFY_BASE_REF:-origin/master}"
REQ_PATH="test/gha-e2e/mooncake/image/requirements.txt"
LOG="${VERIFY_LOG_DIR:-$REPO_ROOT/docs/verification/urllib3-sec-bump/results}/premise.log"
mkdir -p "$(dirname "$LOG")"
: > "$LOG"

fail() { echo "PREMISE-FAIL: $1" | tee -a "$LOG"; exit 1; }
note() { echo "$1" | tee -a "$LOG"; }

# --- 1. what does the BASE branch pin? -------------------------------------
BASE_PIN="$(git -C "$REPO_ROOT" show "$BASE_REF:$REQ_PATH" 2>/dev/null | grep -oE '^urllib3==[0-9.]+' | head -1 | cut -d= -f3)"
note "base ref               : $BASE_REF"
note "base pin of urllib3    : ${BASE_PIN:-<not pinned>}"
[ -n "$BASE_PIN" ] || fail "no urllib3 pin found on base branch"

# --- 2. is that pin inside a published advisory's vulnerable range? --------
# The three GHSAs cited by the PR body are untrusted text; query the GitHub
# Advisory API ourselves instead of trusting the bump message.
ADVISORIES="GHSA-8988-9cw3-xx77 GHSA-vxq7-64xx-v4gw GHSA-gh4c-6fx4-qh6g"
VULN_COUNT=0
for ghsa in $ADVISORIES; do
    json="$(curl -s "https://api.github.com/advisories/$ghsa")"
    # range looks like ">= 1.26.0, < 2.8.0"; only the upper bound matters here.
    upper="$(printf '%s' "$json" | python3 -I -c "
import json,sys
try:
    d=json.load(sys.stdin)
    for v in (d.get('vulnerabilities') or []):
        if v.get('package',{}).get('name')=='urllib3':
            print(v.get('vulnerable_version_range',''))
except Exception:
    pass
")"
    if [ -z "$upper" ]; then
        note "advisory $ghsa        : NOT FOUND on the GitHub Advisory API"
        continue
    fi
    # crude range check: split into clauses, evaluate the '<' bound against the pin
    in_range="$(python3 -I - "$BASE_PIN" "$upper" <<'EOF'
import sys, re
pin, rng = sys.argv[1], sys.argv[2]
def vt(s):
    return tuple(int(x) if x.isdigit() else x for x in re.split(r'[.\-]', s))
ok = True; hit = False
for m in re.finditer(r'(>=|<=|>|<|==|!=)\s*([0-9][0-9a-zA-Z.\-]*)', rng):
    op, ver = m.group(1), m.group(2)
    hit = True
    a, b = vt(pin), vt(ver)
    try:
        if   op == '>=': ok &= a >= b
        elif op == '<=': ok &= a <= b
        elif op == '>':  ok &= a >  b
        elif op == '<':  ok &= a <  b
        elif op == '==': ok &= a == b
        elif op == '!=': ok &= a != b
    except TypeError:
        ok = False
print("yes" if (hit and ok) else "no")
EOF
)"
    if [ "$in_range" = "yes" ]; then
        VULN_COUNT=$((VULN_COUNT+1))
        note "advisory $ghsa        : EXISTS, base pin $BASE_PIN in vulnerable range ($upper)"
    else
        note "advisory $ghsa        : EXISTS, base pin NOT in range ($upper)"
    fi
done

[ "$VULN_COUNT" -gt 0 ] || fail "base pin $BASE_PIN is not inside any advisory's vulnerable range"

# --- 3. is this manifest the only urllib3 pin in the repo (component match)?
COUNT="$(git -C "$REPO_ROOT" grep -l "urllib3" "$BASE_REF" -- 2>/dev/null | wc -l | tr -d ' ')"
note "files mentioning urllib3 on base: $COUNT"
[ "$COUNT" -eq 1 ] || note "NOTE: $COUNT files mention urllib3 on base (expected 1)"

note ""
note "VERDICT: premise CONFIRMED — base pins urllib3 $BASE_PIN, inside the vulnerable"
note "range of $VULN_COUNT published urllib3 advisory(ies) fixed in 2.8.0."
echo "PREMISE-OK"
exit 0
