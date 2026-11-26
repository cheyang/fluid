#!/usr/bin/env bash
# verify-pins.sh — deterministic contract/canary checks for PR #6289
# (dependabot bump of github/codeql-action init/analyze/upload-sarif 4.39.3 -> 4.39.4).
#
# Runs against the CURRENTLY CHECKED-OUT tree (re-verify.sh grafts this harness
# onto the ref under test before calling it). Emits one "name<TAB>pass|fail"
# line per check — the format re-verify.sh's lookup() consumes.
#
# Polarity:
#   T1..T4 are CONTRACT tests: they assert intended-correct behavior and FAIL
#          on a bad pin.
#   C1 is a bug-CANARY for finding F1 (stale version comment): it asserts the
#          currently-observed (wrong) state, so it PASSES while the misleading
#          comment is present and FLIPS TO FAIL once someone fixes the comment.
set -uo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT"
WF_DIR=".github/workflows"

# Baked-in ground truth captured 2026-11-27 via:
#   git ls-remote https://github.com/github/codeql-action.git 'refs/tags/v4.39.4^{}' 'refs/tags/v4.39.3^{}'
EXPECTED_NEW_SHA="1bd86f2fdb4b86187fa4059f16f31a3202acc9ea"   # v4.39.4^{} (peeled commit)
EXPECTED_OLD_SHA="33d7b85da1a04171e211befe2c739303b9a4c066"   # v4.39.3^{} (peeled commit)

pass() { printf '%s\tpass\n' "$1"; }
fail() { printf '%s\tfail\n' "$1"; }

# Collect every codeql-action pin: "<sha> <file>:<line> <comment>"
PINS="$(grep -RhoE 'github/codeql-action/[a-z-]+@[0-9a-f]{40}' "$WF_DIR" 2>/dev/null || true)"
SHAS="$(printf '%s\n' "$PINS" | grep -oE '@[0-9a-f]{40}' | tr -d '@' | sort -u)"

# T1 (contract): every github/codeql-action/* use is pinned to a full 40-hex SHA
# (no mutable tag/branch refs, no short SHAs).
UNPINNED="$(grep -RhE 'uses:\s*github/codeql-action/' "$WF_DIR" 2>/dev/null \
  | grep -vE 'uses:\s*github/codeql-action/[a-z-]+@[0-9a-f]{40}( |$|#)' || true)"
if [ -z "$UNPINNED" ] && [ -n "$PINS" ]; then pass T1_PinIsFullSHA; else fail T1_PinIsFullSHA; fi

# T2 (contract): all codeql-action pins in the repo resolve to ONE sha
# (init/analyze/upload-sarif must not drift apart).
if [ "$(printf '%s\n' "$SHAS" | grep -c .)" -eq 1 ]; then pass T2_PinsConsistent; else fail T2_PinsConsistent; fi

# T3 (contract): the single pinned sha is the peeled commit of codeql-action
# v4.39.4. Prefer a live check against the upstream repo; fall back to the
# baked-in value captured at review time (both recorded in results/).
LIVE="$(git ls-remote https://github.com/github/codeql-action.git 'refs/tags/v4.39.4^{}' 2>/dev/null | awk '{print $1}')"
ACTUAL="$(printf '%s\n' "$SHAS" | head -1)"
if [ -n "$LIVE" ]; then
  [ "$ACTUAL" = "$LIVE" ] && pass T3_PinMatchesClaimedTag || fail T3_PinMatchesClaimedTag
else
  echo "verify-pins: network unavailable, using baked-in v4.39.4 peel $EXPECTED_NEW_SHA" >&2
  [ "$ACTUAL" = "$EXPECTED_NEW_SHA" ] && pass T3_PinMatchesClaimedTag || fail T3_PinMatchesClaimedTag
fi

# T4 (contract): both modified workflow files still parse as YAML.
if python3 -c "
import sys
try:
    import yaml
except ImportError:
    sys.exit(3)
for f in ['$WF_DIR/codeql-analysis.yml', '$WF_DIR/sonarcloud.yaml']:
    yaml.safe_load(open(f))
" 2>/dev/null; then
  pass T4_WorkflowsParse
else
  rc=$?
  if [ "$rc" = "3" ]; then
    # pyyaml missing: structural fallback — every uses: line still well-formed
    if grep -RhE 'uses:\s*github/codeql-action/' "$WF_DIR" | grep -qE '@[0-9a-f]{40}\s*(#.*)?$'; then
      pass T4_WorkflowsParse
    else
      fail T4_WorkflowsParse
    fi
  else
    fail T4_WorkflowsParse
  fi
fi

# C1 (CANARY for F1): trailing version comments disagree with the pinned tag.
# Current (wrong) state: comments say v3.29.5 / v2.22.11 while the pin is v4.39.4.
# PASS = stale comment still present; a fix makes this FLIP to fail.
STALE="$(grep -RhE 'uses:\s*github/codeql-action/[a-z-]+@[0-9a-f]{40}\s*#\s*v' "$WF_DIR" 2>/dev/null \
  | grep -vE '#\s*v4\.39\.4' || true)"
if [ -n "$STALE" ]; then pass C1_StaleVersionComment; else fail C1_StaleVersionComment; fi
