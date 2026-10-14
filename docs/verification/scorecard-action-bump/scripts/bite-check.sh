#!/usr/bin/env bash
# Harness-bites check (review-finding-verifier skill, step 4).
# Runs check_action_pin.py against mutated workflow copies and a local
# fixture action repo; every mutation must flip the corresponding check to
# FAIL, otherwise the harness proves nothing. Production code untouched.
#
# Usage: bash docs/verification/scorecard-action-bump/scripts/bite-check.sh
#        (from a checkout whose worktree is at the PR head)
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../../../.." && pwd)"
H="$HERE/check_action_pin.py"
W="$ROOT/.github/workflows/sonarcloud.yaml"
T="$(mktemp -d)"
R="${1:-$HERE/../results}"
mkdir -p "$R"
OUT="$R/l1-bite-check.txt"

FAILURES=0
check_bite() { # name expected_exit actual_exit
  if [ "$2" = "$3" ]; then
    echo "bite $1: exit $3 (expected $2) OK"
  else
    echo "bite $1: exit $3 (expected $2) MISMATCH"
    FAILURES=$((FAILURES+1))
  fi
}

{
  echo "# Harness-bites check — $(date -u +%FT%TZ)"
  echo "# Mutated workflow copies + local fixture repo; production code untouched."
  echo

  echo "## bite A: pin SHA replaced with 0x111... (expect C2 FAIL)"
  sed 's/fa7117db83c52d787ae5a89f350999701505d78e/1111111111111111111111111111111111111111/' "$W" > "$T/wa.yaml"
  python3 "$H" --workflow "$T/wa.yaml" --report "$T/a.json" 2>&1; A=$?
  check_bite A 1 $A

  echo
  echo "## bite B: required input results_file removed from with: (expect C4 FAIL)"
  grep -v "results_file" "$W" > "$T/wb.yaml"
  python3 "$H" --workflow "$T/wb.yaml" --report "$T/b.json" 2>&1; B=$?
  check_bite B 1 $B

  echo
  echo "## bite C: --strict-inputs on the real workflow (expect C3 FAIL — documents the inert exclude_* inputs)"
  python3 "$H" --workflow "$W" --strict-inputs --report "$T/c.json" 2>&1; C=$?
  check_bite C 1 $C

  echo
  echo "## bite D: local fixture action repo whose new pin drops exclude_checks/exclude_paths (expect C5 FAIL, C6 FAIL)"
  # fixture: two commits — prev declares exclude_*, head does not.
  FAKE="$T/fake-action.git"; WK="$T/fixture-src"
  git init -q --bare "$FAKE"
  git init -q "$WK"
  cat > "$WK/action.yaml" <<'EOF'
name: fake
inputs:
  results_file: {required: true}
  results_format: {required: true}
  exclude_checks: {required: false, default: ""}
  exclude_paths: {required: false, default: ""}
runs:
  using: docker
  image: docker://ghcr.io/ossf/scorecard-action:v2.5.0
EOF
  ( cd "$WK" && git add . && git -c user.email=t@t -c user.name=t commit -qm prev )
  PREV=$(git -C "$WK" rev-parse HEAD)
  cat > "$WK/action.yaml" <<'EOF'
name: fake
inputs:
  results_file: {required: true}
  results_format: {required: true}
runs:
  using: docker
  image: docker://ghcr.io/ossf/scorecard-action:v2.5.0
EOF
  ( cd "$WK" && git add . && git -c user.email=t@t -c user.name=t commit -qm head )
  HEADSHA=$(git -C "$WK" rev-parse HEAD)
  git -C "$WK" push -q "$FAKE" "+refs/heads/master:refs/tags/v9.9.9" 2>/dev/null \
    || git -C "$WK" push -q "$FAKE" "+refs/heads/main:refs/tags/v9.9.9"
  sed "s|fa7117db83c52d787ae5a89f350999701505d78e # v2.5.0|$HEADSHA # v9.9.9|" "$W" > "$T/wd.yaml"
  python3 "$H" --workflow "$T/wd.yaml" --action-repo "$FAKE" --prev-pin "$PREV" --report "$T/d.json" 2>&1; D=$?
  cp "$T/d.json" "$R/l1-bite-d-fixture.json"
  check_bite D 1 $D

  echo
  echo "bites with unexpected exit codes: $FAILURES"
} | tee "$OUT"

rm -rf "$T"
[ "$FAILURES" = "0" ] || exit 1
exit 0
