#!/usr/bin/env bash
# verify-quotalist-doc.sh — round-4 pin for finding F13 (doc line on QuotaList).
# Contract: the QuotaList field doc must (a) recommend binary units (e.g. 512Mi)
# for API input and (b) describe a lowercase "m" suffix as Jindo's internal
# notation for Mi — consistently in api/v1alpha1/common.go and the four
# regenerated copies — and the round-3 sentence teaching raw "m" as API input
# ("is read as Mi (not milli)") must be gone everywhere.
# Usage: run from a checkout of the ref under test. Exit 0 iff the contract holds.
set -euo pipefail
FILES="api/v1alpha1/common.go api/v1alpha1/openapi_generated.go api/v1alpha1/swagger.json docs/en/dev/api_doc.md docs/zh/dev/api_doc.md"
fail=0
for f in $FILES; do
  if grep -q 'Use binary units such as .\\\?"512Mi' "$f" 2>/dev/null || grep -q 'Use binary units such as "512Mi"' "$f" 2>/dev/null; then
    echo "PASS  $f: recommends binary units (512Mi) for API input"
  else
    echo "FAIL  $f: missing the binary-unit recommendation"; fail=1
  fi
  if grep -q "Jindo's internal notation for Mi" "$f"; then
    echo "PASS  $f: describes lowercase m as Jindo's internal notation for Mi"
  else
    echo "FAIL  $f: missing the 'Jindo's internal notation' description"; fail=1
  fi
done
if grep -rEl 'is read as Mi \(not milli\)' $FILES >/dev/null 2>&1; then
  echo "FAIL  stale round-3 sentence ('is read as Mi (not milli)') still present:"; grep -rEl 'is read as Mi \(not milli\)' $FILES; fail=1
else
  echo "PASS  stale round-3 sentence absent from all copies"
fi
[ "$fail" -eq 0 ] && { echo "RESULT: QuotaList doc contract holds."; exit 0; } || { echo "RESULT: QuotaList doc contract VIOLATED."; exit 1; }
