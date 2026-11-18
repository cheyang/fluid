#!/usr/bin/env bash
# Unit layer: pure checks (no heavy deps). Emits go-test-json lines only.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
HERE="docs/verification/vineyard-sklearn-bump-codex"
. "$HERE/scripts/emit.sh"

bash "$HERE/scripts/check_premise_advisory.sh" "$VERIFY_RESULTS/P0.log" \
  && emit P0_SecurityAdvisoryMotivatesBump pass "$VERIFY_RESULTS/P0.log" \
  || emit P0_SecurityAdvisoryMotivatesBump fail "$VERIFY_RESULTS/P0.log"

python3 "$HERE/scripts/check_hashes.py" samples/vineyard/requirements.txt \
  >"$VERIFY_RESULTS/C1.log" 2>&1 \
  && emit C1_SklearnHashesMatchPyPI pass "$VERIFY_RESULTS/C1.log" \
  || emit C1_SklearnHashesMatchPyPI fail "$VERIFY_RESULTS/C1.log"

python3 "$HERE/scripts/check_header_consistency.py" samples/vineyard/requirements.txt \
  >"$VERIFY_RESULTS/F1.log" 2>&1 \
  && emit F1_HeaderRegenPinMatchesLockfile pass "$VERIFY_RESULTS/F1.log" \
  || emit F1_HeaderRegenPinMatchesLockfile fail "$VERIFY_RESULTS/F1.log"
