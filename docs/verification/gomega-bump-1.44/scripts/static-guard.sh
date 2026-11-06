#!/usr/bin/env bash
# static-guard.sh — blast-radius guard for the gomega 1.38.2 -> 1.44.0 bump
# (fluid PR #6231).
#
# The review's blast-radius conclusion rests on these invariants of fluid's
# own test code:
#   1. MatchJSON / MatchYAML / MatchXML / HaveExactElements are NOT used
#      (their 1.44.0 semantics changed: large-int exactness, multi-document
#      streams, namespace prefixes, index reporting).
#   2. HaveKey / HaveKeyWithValue are only called with literal keys, never a
#      key matcher (so the #926/#929 error-handling and iteration-order fixes
#      cannot change any assertion's outcome).
#   3. BeNumerically is only called with 2 operands (comparator + operand),
#      never the 3-operand threshold form, and never mixes signedness
#      beyond int/int64 comparisons (so #925/#927 cannot change outcomes).
#
# If any of these become false in a later revision, the bump's risk analysis
# must be redone — this script fails loudly so re-verify.sh surfaces it.
#
# The harness package pkg/gomegacontract deliberately exercises the changed
# semantics and is excluded from every check.
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"

HARNESS='/pkg/gomegacontract/'
status=0

# strip: harness files, pure comment lines
strip() { grep -v "$HARNESS" | grep -vE '^[^:]+:[0-9]+:\s*//' || true; }

check() { # $1 = description, stdin = offending lines
  local hits
  hits="$(strip)"
  if [ -n "$hits" ]; then
    echo "STATIC-GUARD FAIL: $1"
    echo "$hits" | sed 's/^/    /'
    status=1
  else
    echo "static-guard ok: $1"
  fi
}

# 1. matchers with changed semantics must remain unused in fluid's own code
for m in MatchJSON MatchYAML MatchXML HaveExactElements; do
  echo "--- $m ---"
  grep -rn --include="*.go" -E "\b$m\(" --exclude-dir=vendor . | check "$m is now used outside the verification harness — redo the semantic blast-radius analysis for the gomega bump"
done

# 2. HaveKey / HaveKeyWithValue must use literal keys (no key matchers)
echo "--- HaveKey/HaveKeyWithValue key matchers ---"
grep -rnE --include="*.go" 'HaveKey(WithValue)?\((ContainSubstring|MatchRegexp|HavePrefix|HaveSuffix|And|Or|Not|WithTransform)\(' --exclude-dir=vendor . \
  | check "HaveKey/HaveKeyWithValue now receives a key matcher — the #926/#929 semantics apply; verify affected assertions"

# 3. BeNumerically must not use the 3-operand threshold form
echo "--- BeNumerically 3-operand threshold form ---"
grep -rnE --include="*.go" 'BeNumerically\("[^"]+",\s*[^,)]+,\s*[^,)]+\)' --exclude-dir=vendor . \
  | check "BeNumerically now uses the threshold operand — the #927 float-threshold semantics apply; verify affected assertions"

exit $status
