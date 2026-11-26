#!/usr/bin/env bash
# verify-bump.sh — verification harness for PR #6290
# (chore(deps): bump ossf/scorecard-action from 2.5.0 to 2.6.0)
#
# Runs against a git ref of fluid-cloudnative/fluid and checks the claims a
# reviewer can check for a pinned GitHub Action bump:
#
#   CHECK pin-integrity   (contract, gates)  the SHA pinned in the workflow is the
#                         peeled commit of the tag named in its `# vX.Y.Z` comment
#                         (verified against the upstream repo, not the PR body).
#   CHECK inputs-compat   (contract, gates)  every `with:` input the workflow passes
#                         that was a DECLARED input at the old version (v2.5.0) is
#                         still declared at the new pinned version — i.e. the bump
#                         did not remove/rename an input this workflow relies on.
#   CHECK image-available (contract, gates)  the action is a docker action; the
#                         ghcr.io/ossf/scorecard-action:<tag> image manifest exists.
#   CHECK dead-inputs     (canary, INFO only, pre-existing on base, non-gating)
#                         `with:` keys that are NOT declared inputs at the new
#                         version. Today: exclude_checks, exclude_paths — silently
#                         ignored by every scorecard-action release. "Passes" while
#                         they remain undeclared; flips when the workflow drops them
#                         or the action starts honoring them.
#
# Usage: verify-bump.sh [ref]     (default ref: HEAD; must run inside a fluid clone)
# Exit:  0 iff no gating contract check FAILs.
set -uo pipefail

REF="${1:-HEAD}"
WORKFLOW_PATH=".github/workflows/sonarcloud.yaml"
OLD_TAG="v2.5.0"
OLD_SHA="fa7117db83c52d787ae5a89f350999701505d78e"
ACTION_REPO="https://github.com/ossf/scorecard-action.git"

FAILURES=0
note()  { printf 'CHECK %-15s %-4s %s\n' "$1" "$2" "$3"; }
fail()  { note "$1" FAIL "$2"; FAILURES=$((FAILURES+1)); }

git rev-parse --git-dir >/dev/null 2>&1 || { echo "verify-bump: not a git repo" >&2; exit 2; }
WF="$(git show "$REF:$WORKFLOW_PATH" 2>/dev/null)" || { echo "verify-bump: $WORKFLOW_PATH not found at $REF" >&2; exit 2; }

# --- parse pinned sha + version comment from the raw line (PyYAML strips
# --- comments), and the with: keys of the analysis step via PyYAML.
PIN_LINE="$(printf '%s\n' "$WF" | grep -E '^\s*uses:\s*ossf/scorecard-action@' | head -1)"
PIN_SHA="$(printf '%s' "$PIN_LINE" | sed -E 's/.*ossf\/scorecard-action@([0-9a-f]{40}).*/\1/')"
PIN_TAG="$(printf '%s' "$PIN_LINE" | sed -nE 's/.*#\s*(v[0-9][^ ]*).*/\1/p')"
WITH_KEYS="$(printf '%s' "$WF" | python3 -c '
import sys, yaml
doc = yaml.safe_load(sys.stdin)
steps = doc["jobs"]["analysis"]["steps"]
for s in steps:
    if s.get("uses", "").startswith("ossf/scorecard-action@"):
        print(",".join(sorted((s.get("with") or {}).keys())))
        break
')"
[ -n "${PIN_SHA:-}" ] || { echo "verify-bump: no ossf/scorecard-action step found at $REF" >&2; exit 2; }
[ -n "${PIN_TAG:-}" ] || { echo "verify-bump: pinned step carries no '# vX.Y.Z' version comment at $REF" >&2; exit 2; }
echo "verify-bump: ref=$REF pinned=$PIN_SHA comment=$PIN_TAG with=[$WITH_KEYS]"

# --- 1. pin integrity: pinned sha == peeled commit of the commented tag
PEELED="$(git ls-remote "$ACTION_REPO" "refs/tags/$PIN_TAG^{}" | awk '{print $1}')"
if [ "$PEELED" = "$PIN_SHA" ]; then
  note pin-integrity PASS "$PIN_TAG -> $PIN_SHA (matches upstream tag)"
else
  fail pin-integrity "workflow pins $PIN_SHA but $PIN_TAG peels to ${PEELED:-<tag not found>}"
fi

# --- 2. input compatibility: declared inputs at old vs new version
declared_at() { # $1 = sha/tag -> newline-separated declared input names
  curl -sf "https://raw.githubusercontent.com/ossf/scorecard-action/$1/action.yaml" \
  | python3 -c 'import sys, yaml; print("\n".join(sorted((yaml.safe_load(sys.stdin).get("inputs") or {}).keys())))'
}
NEW_DECL="$(declared_at "$PIN_SHA")"  || { fail inputs-compat "cannot fetch action.yaml at $PIN_SHA"; NEW_DECL=""; }
OLD_DECL="$(declared_at "$OLD_SHA")"  || { fail inputs-compat "cannot fetch action.yaml at $OLD_TAG"; OLD_DECL=""; }
if [ -n "$NEW_DECL" ] && [ -n "$OLD_DECL" ]; then
  MISSING=""
  IFS=',' read -ra KEYS <<< "$WITH_KEYS"
  for k in "${KEYS[@]}"; do
    if printf '%s\n' "$OLD_DECL" | grep -qx "$k" && ! printf '%s\n' "$NEW_DECL" | grep -qx "$k"; then
      MISSING="$MISSING $k"
    fi
  done
  if [ -z "$MISSING" ]; then
    note inputs-compat PASS "all workflow inputs declared at $OLD_TAG remain declared at $PIN_TAG"
  else
    fail inputs-compat "inputs honored at $OLD_TAG but no longer declared at $PIN_TAG:$MISSING"
  fi
fi

# --- 3. runtime image availability (docker action)
TOK="$(curl -sf "https://ghcr.io/token?scope=repository:ossf/scorecard-action:pull" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')" || TOK=""
CODE="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOK" \
        -H 'Accept: application/vnd.oci.image.index.v1+json' \
        "https://ghcr.io/v2/ossf/scorecard-action/manifests/$PIN_TAG")"
if [ "$CODE" = "200" ]; then
  note image-available PASS "ghcr.io/ossf/scorecard-action:$PIN_TAG manifest HTTP 200"
else
  fail image-available "ghcr.io/ossf/scorecard-action:$PIN_TAG manifest HTTP $CODE"
fi

# --- 4. dead inputs (canary, pre-existing on base branch -> INFO, non-gating)
if [ -n "$NEW_DECL" ]; then
  UNDECLARED=""
  IFS=',' read -ra KEYS <<< "$WITH_KEYS"
  for k in "${KEYS[@]}"; do
    printf '%s\n' "$NEW_DECL" | grep -qx "$k" || UNDECLARED="$UNDECLARED $k"
  done
  note dead-inputs INFO "with: keys not declared by action.yaml at $PIN_TAG (silently ignored):${UNDECLARED:- none}"
fi

echo "---"
if [ "$FAILURES" -eq 0 ]; then
  echo "verify-bump: ALL GATING CHECKS PASSED (ref=$REF)"; exit 0
else
  echo "verify-bump: $FAILURES GATING CHECK(S) FAILED (ref=$REF)"; exit 1
fi
