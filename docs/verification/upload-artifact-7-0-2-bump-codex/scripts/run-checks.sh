#!/usr/bin/env bash
# Verification harness for fluid PR #6210 (dependabot bump of actions/upload-artifact
# 7.0.1 -> 7.0.2 across three workflow files).
#
# Usage: run-checks.sh [unit|integration|all]
#
# STDOUT carries ONLY go-test-JSON-shaped events: {"Test":"<name>","Action":"pass|fail"}
# so the stock review-finding-verifier scripts/re-verify.sh (kind "gotest") can parse
# results. All narrative logging goes to results/checks-run.log (never stdout/stderr —
# re-verify.sh merges stderr into the parsed stream, and jq slurps the whole file).
#
# All checks are CONTRACT tests: they assert what a correct bump must satisfy.
# A FAIL = the bump is malformed (wrong pin, stale pin elsewhere, unsupported input,
# broken workflow YAML).
set -uo pipefail

LAYER="${1:-all}"
EXPECTED_TAG="v7.0.2"
EXPECTED_SHA=""   # resolved from the upstream tag below
UPSTREAM="https://github.com/actions/upload-artifact.git"
RAW="https://raw.githubusercontent.com/actions/upload-artifact"
WORKFLOWS=".github/workflows/backward-compatibility-e2e.yml .github/workflows/kind-e2e.yml .github/workflows/sonarcloud.yaml"

TOPIC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG="$TOPIC_DIR/results/checks-run.log"
mkdir -p "$TOPIC_DIR/results"
: > "$LOG"
log() { printf '%s\n' "$*" >> "$LOG"; }
emit() { printf '{"Test":"%s","Action":"%s"}\n' "$1" "$2"; }

repo_root() { git rev-parse --show-toplevel; }

resolve_tag_sha() {
  # Prefer the peeled ref (annotated tags); fall back to the tag ref itself
  # (upload-artifact uses lightweight tags).
  local peeled direct
  peeled=$(git ls-remote "$UPSTREAM" "refs/tags/${EXPECTED_TAG}^{}" 2>>"$LOG" | cut -f1)
  direct=$(git ls-remote "$UPSTREAM" "refs/tags/${EXPECTED_TAG}" 2>>"$LOG" | cut -f1)
  EXPECTED_SHA="${peeled:-$direct}"
  log "upstream tag $EXPECTED_TAG -> ${EXPECTED_SHA:-<unresolved>} (peeled='${peeled:-}' direct='${direct:-}')"
}

# ---------------- unit layer (fully local; needs only the git worktree) -------------

check_A2_no_stale_pins() {
  # Every actions/upload-artifact pin under .github/ must be the new SHA, and there
  # must be exactly the 3 known call sites (guards against a stale 4th usage).
  local root pins unique count
  root=$(repo_root)
  pins=$(grep -rhoP 'actions/upload-artifact@\K[0-9a-f]{40}' "$root/.github" 2>/dev/null | sort)
  unique=$(printf '%s\n' "$pins" | sort -u)
  count=$(printf '%s\n' "$pins" | grep -c . || true)
  log "A2: pins under .github/ (n=$count):"; printf '%s\n' "$pins" | sed 's/^/    /' >> "$LOG"
  if [ "$count" -eq 3 ] && [ "$(printf '%s\n' "$unique" | grep -c .)" -eq 1 ] \
     && [ "$unique" = "$EXPECTED_SHA" ]; then
    emit A2_NoStalePinsRepoWide pass
  else
    log "A2 FAIL: count=$count unique={$unique} expected=$EXPECTED_SHA"
    emit A2_NoStalePinsRepoWide fail
  fi
}

check_A4_yaml_parses() {
  local root f ok=0
  root=$(repo_root)
  for f in $WORKFLOWS; do
    if python3 -c "import yaml,sys; yaml.safe_load(open(sys.argv[1]))" "$root/$f" 2>>"$LOG"; then
      log "A4: $f parses as YAML"
    else
      log "A4 FAIL: $f does not parse as YAML"; ok=1
    fi
  done
  [ "$ok" -eq 0 ] && emit A4_WorkflowYamlParses pass || emit A4_WorkflowYamlParses fail
}

# ------------- integration layer (real upstream: github.com over https) -------------

check_A1_pin_matches_tag() {
  # The pinned SHA in each changed workflow must be exactly the upstream
  # refs/tags/v7.0.2 commit, and the trailing comment must agree (# v7.0.2).
  local root f pin comment ok=0
  root=$(repo_root)
  if [ -z "$EXPECTED_SHA" ]; then log "A1 FAIL: tag unresolvable (network?)"; emit A1_PinMatchesOfficialTag fail; return; fi
  for f in $WORKFLOWS; do
    pin=$(grep -oP 'actions/upload-artifact@\K[0-9a-f]{40}' "$root/$f" | head -1)
    comment=$(grep -oP 'actions/upload-artifact@[0-9a-f]{40}[[:space:]]*#[[:space:]]*\Kv[0-9.]+' "$root/$f" | head -1)
    log "A1: $f pin=$pin comment=$comment"
    if [ "$pin" != "$EXPECTED_SHA" ]; then log "A1 FAIL: $f pin $pin != $EXPECTED_TAG commit $EXPECTED_SHA"; ok=1; fi
    if [ "$comment" != "$EXPECTED_TAG" ]; then log "A1 FAIL: $f comment '$comment' != '$EXPECTED_TAG'"; ok=1; fi
  done
  [ "$ok" -eq 0 ] && emit A1_PinMatchesOfficialTag pass || emit A1_PinMatchesOfficialTag fail
}

check_A3_inputs_supported() {
  # The workflows pass name/path/retention-days under with:. Every input actually
  # used by an upload-artifact step must still be declared in action.yml at the pin.
  local action_yml root f used req ok=0
  root=$(repo_root)
  if [ -z "$EXPECTED_SHA" ]; then log "A3 FAIL: tag unresolvable (network?)"; emit A3_InputsStillSupported fail; return; fi
  action_yml=$(curl -sfL "$RAW/$EXPECTED_SHA/action.yml" 2>>"$LOG") || {
    log "A3 FAIL: cannot fetch action.yml at $EXPECTED_SHA"; emit A3_InputsStillSupported fail; return; }
  printf '%s\n' "$action_yml" > "$TOPIC_DIR/results/action-yml-at-pin.yml"
  # Extract with:-inputs of the upload-artifact step only: the block starting at the
  # step's `with:` (8-space indent) and ending when indentation drops below 10.
  used=$(for f in $WORKFLOWS; do
           awk '
             /actions\/upload-artifact@/ {instep=1; inwith=0; next}
             instep && /^        with:[[:space:]]*$/ {inwith=1; next}
             instep && inwith && /^          [a-zA-Z-]+:/ {sub(/:.*/, "", $1); print $1; next}
             instep && inwith {instep=0; inwith=0}
           ' "$root/$f"
         done | sort -u)
  log "A3: with:-inputs used by upload-artifact steps: $(printf '%s ' $used)"
  if [ -z "$used" ]; then log "A3 FAIL: no with:-inputs extracted (awk pattern broken?)"; emit A3_InputsStillSupported fail; return; fi
  for req in $used; do
    if printf '%s\n' "$action_yml" | grep -qE "^  $req:"; then
      log "A3: input '$req' declared in action.yml@$EXPECTED_TAG"
    else
      log "A3 FAIL: input '$req' NOT declared in action.yml@$EXPECTED_TAG"; ok=1
    fi
  done
  [ "$ok" -eq 0 ] && emit A3_InputsStillSupported pass || emit A3_InputsStillSupported fail
}

case "$LAYER" in
  unit)
    resolve_tag_sha   # A2 compares pins against the upstream tag commit too
    check_A2_no_stale_pins
    check_A4_yaml_parses
    ;;
  integration)
    resolve_tag_sha
    check_A1_pin_matches_tag
    check_A3_inputs_supported
    ;;
  all)
    resolve_tag_sha
    check_A1_pin_matches_tag
    check_A2_no_stale_pins
    check_A3_inputs_supported
    check_A4_yaml_parses
    ;;
  *) echo "usage: $0 [unit|integration|all]" >>"$LOG"; exit 2;;
esac
exit 0
