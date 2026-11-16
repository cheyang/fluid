#!/usr/bin/env bash
# C5 — consistency sweep for PR #6255 (Go 1.26.10 -> 1.26.11).
#
# The bump must be complete: no stale 1.26.10 references outside vendor/,
# the go.mod toolchain directive updated, the 3 GitHub workflows and Travis
# updated, and no leftover old image digest (23ce713...).
#
# Emits "CHECK C5-consistency pass|fail"; exit 0 iff all sub-checks pass.
set -uo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
rc=0

stale="$(grep -rn "1\.26\.10" --exclude-dir=vendor --exclude-dir=.git --exclude-dir=verification . 2>/dev/null || true)"
if [ -n "$stale" ]; then
  echo "CHECK C5-consistency fail  stale 1.26.10 references:"
  echo "$stale" | head -10
  rc=1
else
  echo "CHECK C5-consistency-stale pass  no 1.26.10 references outside vendor/"
fi

if grep -q "^toolchain go1.26.11$" go.mod && grep -q "^go 1.26.0$" go.mod; then
  echo "CHECK C5-consistency-gomod pass  go 1.26.0 / toolchain go1.26.11"
else
  echo "CHECK C5-consistency-gomod fail  go.mod directives unexpected:"
  grep -E "^go |^toolchain" go.mod
  rc=1
fi

wf="$(grep -h "GO_VERSION: 1.26.11" .github/workflows/project-check.yml .github/workflows/kind-e2e.yml .github/workflows/backward-compatibility-e2e.yml 2>/dev/null | wc -l)"
if [ "$wf" -eq 3 ]; then
  echo "CHECK C5-consistency-workflows pass  GO_VERSION: 1.26.11 in 3 workflows"
else
  echo "CHECK C5-consistency-workflows fail  GO_VERSION: 1.26.11 found in ${wf}/3 workflows"
  rc=1
fi

if grep -q '^\s*- "1.26.11"$' .travis.yml; then
  echo "CHECK C5-consistency-travis pass  .travis.yml go 1.26.11"
else
  echo "CHECK C5-consistency-travis fail  .travis.yml go version unexpected:"
  grep -n "1.26" .travis.yml
  rc=1
fi

if grep -rq "23ce71337" . --exclude-dir=vendor --exclude-dir=.git --exclude-dir=verification 2>/dev/null; then
  echo "CHECK C5-consistency-olddigest fail  old 1.26.10 index digest still referenced somewhere"
  rc=1
else
  echo "CHECK C5-consistency-olddigest pass  old image digest fully replaced"
fi

[ "$rc" -eq 0 ] && echo "CHECK C5-consistency pass  all sub-checks green"
exit $rc
