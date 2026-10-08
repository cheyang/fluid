#!/usr/bin/env bash
# Unit layer runner for the urllib3-sec-bump harness.
#
# Runs P0 (premise, against the BASE branch), T1 (hash authenticity) and
# T3 (version/dependency compatibility) and emits one go-test-style JSON
# event per test on stdout:
#   {"Test":"<name>","Action":"pass"|"fail"}
# Diagnostics go ONLY to the results/ logs — stdout must stay pure JSON so
# scripts/re-verify.sh can parse it (it merges stderr into the same stream).
#
# Written 2026-10-08 by Reviewer A (Claude).
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS="${VERIFY_LOG_DIR:-$HERE/../results}"
mkdir -p "$RESULTS"

emit() { # $1=test name, $2=pass|fail
    printf '{"Test":"%s","Action":"%s"}\n' "$1" "$2"
}

# P0 — premise (runs against the base branch; see check_premise.sh)
if bash "$HERE/check_premise.sh" >"$RESULTS/premise.stdout" 2>&1; then
    emit "P0_premise_on_base" pass
else
    emit "P0_premise_on_base" fail
fi

# T1 — every pinned hash is a genuine PyPI digest
if python3 -I "$HERE/check_hashes.py" "$REPO_ROOT/test/gha-e2e/mooncake/image/requirements.txt" \
        >"$RESULTS/check_hashes.log" 2>&1; then
    emit "T1_pins_hash_authentic" pass
else
    emit "T1_pins_hash_authentic" fail
fi

# T3 — urllib3 2.8.0 compatible with the rest of the pin set and the image
if python3 -I "$HERE/check_compat.py" >"$RESULTS/check_compat.log" 2>&1; then
    emit "T3_urllib3_compat" pass
else
    emit "T3_urllib3_compat" fail
fi
