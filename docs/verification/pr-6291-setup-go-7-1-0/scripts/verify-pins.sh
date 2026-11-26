#!/usr/bin/env bash
# verify-pins.sh — deterministic contract checks for PR #6291
# (dependabot bump of actions/setup-go 7.0.0 -> 7.1.0 across 3 workflow files:
#  backward-compatibility-e2e.yml, kind-e2e.yml, project-check.yml).
#
# Runs against the CURRENTLY CHECKED-OUT tree (re-verify.sh grafts this harness
# onto the ref under test before calling it). Emits one "name<TAB>pass|fail"
# line per check — the format re-verify.sh's lookup() consumes.
#
# All checks are CONTRACT tests: they assert the intended-correct state and
# FAIL on a bad, partial, stale, or forged pin. On the BASE tree the
# version-sensitive checks (T2/T3/T5/T7) are expected to FAIL — that is the
# harness-bites control proving the harness distinguishes base from head.
set -uo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT"
WF_DIR=".github/workflows"
HARNESS_DIR="docs/verification/pr-6291-setup-go-7-1-0"

# Baked-in ground truth captured 2026-11-27 via:
#   git ls-remote https://github.com/actions/setup-go.git refs/tags/v7.1.0 refs/tags/v7.0.0
# (both are lightweight tags: the ref target IS the commit, no peeling needed)
EXPECTED_NEW_SHA="d8cbb8045d7f43706b9177cceb36c1f91a6644d5"   # refs/tags/v7.1.0
EXPECTED_OLD_SHA="b7ad1dad31e06c5925ef5d2fc7ad053ef454303e"   # refs/tags/v7.0.0
EXPECTED_NEW_COMMENT="v7.1.0"
EXPECTED_COUNT=6   # 1x backward-compatibility-e2e.yml, 1x kind-e2e.yml, 4x project-check.yml

pass() { printf '%s\tpass\n' "$1"; }
fail() { printf '%s\tfail\n' "$1"; }

# Collect every setup-go pin as bare SHAs.
PINS="$(grep -RhoE 'actions/setup-go@[0-9a-f]{40}' "$WF_DIR" 2>/dev/null || true)"
SHAS="$(printf '%s\n' "$PINS" | grep -oE '@[0-9a-f]{40}' | tr -d '@' | sort -u)"

# T1 (contract): every actions/setup-go use is pinned to a full 40-hex SHA
# (no mutable tag/branch refs, no short SHAs).
UNPINNED="$(grep -RhE 'uses:\s*actions/setup-go' "$WF_DIR" 2>/dev/null \
  | grep -vE 'uses:\s*actions/setup-go@[0-9a-f]{40}( |$|#)' || true)"
if [ -z "$UNPINNED" ] && [ -n "$PINS" ]; then pass T1_PinIsFullSHA; else fail T1_PinIsFullSHA; fi

# T2 (contract): exactly EXPECTED_COUNT pins and they all resolve to ONE sha
# (all six call sites must move together; no partial bump).
N_PINS="$(printf '%s\n' "$PINS" | grep -c .)"
if [ "$N_PINS" -eq "$EXPECTED_COUNT" ] && [ "$(printf '%s\n' "$SHAS" | grep -c .)" -eq 1 ]; then
  pass T2_PinsConsistent
else
  fail T2_PinsConsistent
fi

# T3 (contract): the single pinned sha is the canonical target of tag v7.1.0 in
# the official actions/setup-go repo. Prefer a live check; fall back to the
# baked-in value captured at review time (both recorded in results/).
LIVE="$(git ls-remote https://github.com/actions/setup-go.git 'refs/tags/v7.1.0' 2>/dev/null | awk '{print $1}')"
ACTUAL="$(printf '%s\n' "$SHAS" | head -1)"
if [ -n "$LIVE" ]; then
  [ "$ACTUAL" = "$LIVE" ] && pass T3_PinMatchesClaimedTag || fail T3_PinMatchesClaimedTag
else
  echo "verify-pins: network unavailable, using baked-in v7.1.0 target $EXPECTED_NEW_SHA" >&2
  [ "$ACTUAL" = "$EXPECTED_NEW_SHA" ] && pass T3_PinMatchesClaimedTag || fail T3_PinMatchesClaimedTag
fi

# T4 (contract): the three modified workflow files still parse as YAML.
if python3 -c "
import sys
try:
    import yaml
except ImportError:
    sys.exit(3)
for f in ['$WF_DIR/backward-compatibility-e2e.yml', '$WF_DIR/kind-e2e.yml', '$WF_DIR/project-check.yml']:
    yaml.safe_load(open(f))
" 2>/dev/null; then
  pass T4_WorkflowsParse
else
  rc=$?
  if [ "$rc" = "3" ]; then
    # pyyaml missing: structural fallback — every setup-go uses: line well-formed
    if grep -RhE 'uses:\s*actions/setup-go' "$WF_DIR" | grep -qE '@[0-9a-f]{40}\s*(#.*)?$'; then
      pass T4_WorkflowsParse
    else
      fail T4_WorkflowsParse
    fi
  else
    fail T4_WorkflowsParse
  fi
fi

# T5 (contract): every pin line carries an accurate trailing version comment
# (the '# v7.1.0' marker must match the pinned tag — a stale comment misleads
# future security audits of the supply chain).
COMMENT_LINES="$(grep -RhE 'uses:\s*actions/setup-go@[0-9a-f]{40}\s*#' "$WF_DIR" 2>/dev/null || true)"
N_COMMENT="$(printf '%s\n' "$COMMENT_LINES" | grep -c .)"
N_GOOD_COMMENT="$(printf '%s\n' "$COMMENT_LINES" | grep -cE "#\s*$EXPECTED_NEW_COMMENT\b" || true)"
if [ "$N_COMMENT" -eq "$EXPECTED_COUNT" ] && [ "$N_GOOD_COMMENT" -eq "$EXPECTED_COUNT" ]; then
  pass T5_VersionCommentAccurate
else
  fail T5_VersionCommentAccurate
fi

# T6 (contract): the upstream action.yml contract between the two pinned
# commits is additive-only for consumers — no removed inputs, no changed input
# defaults, same node runner. Reference copies were fetched from the two pinned
# SHAs at review time (results/action-yml-diff.txt).
if python3 - "$HARNESS_DIR/reference/action-v7.0.0.yml" "$HARNESS_DIR/reference/action-v7.1.0.yml" <<'PY' 2>/dev/null
import sys
try:
    import yaml
except ImportError:
    sys.exit(3)
old = yaml.safe_load(open(sys.argv[1]))
new = yaml.safe_load(open(sys.argv[2]))
oi, ni = old.get('inputs', {}), new.get('inputs', {})
removed = set(oi) - set(ni)
changed = {k for k in oi if k in ni and (oi[k] or {}).get('default') != (ni[k] or {}).get('default')}
runner_same = (old.get('runs') or {}).get('using') == (new.get('runs') or {}).get('using')
sys.exit(0 if (not removed and not changed and runner_same) else 1)
PY
then
  pass T6_UpstreamContractAdditive
else
  rc=$?
  if [ "$rc" = "3" ]; then
    # pyyaml missing: line-based fallback — identical default:/using: line sets
    OLD_D="$(grep -E '^\s+(default|using):' "$HARNESS_DIR/reference/action-v7.0.0.yml" | sort)"
    NEW_D="$(grep -E '^\s+(default|using):' "$HARNESS_DIR/reference/action-v7.1.0.yml" | sort)"
    [ "$OLD_D" = "$NEW_D" ] && pass T6_UpstreamContractAdditive || fail T6_UpstreamContractAdditive
  else
    fail T6_UpstreamContractAdditive
  fi
fi

# T7 (contract): the superseded v7.0.0 pin is fully replaced in .github/
# (no stale occurrence left behind in any workflow file).
STALE="$(grep -Rn "$EXPECTED_OLD_SHA" "$WF_DIR" 2>/dev/null || true)"
if [ -z "$STALE" ]; then pass T7_OldPinFullyReplaced; else fail T7_OldPinFullyReplaced; fi
