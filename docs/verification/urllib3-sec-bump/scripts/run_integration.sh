#!/usr/bin/env bash
# Integration layer runner for the urllib3-sec-bump harness.
#
# Runs T2 (lockfile resolution for the image's target env, the pip half of the
# Dockerfile build) and T4 (requests+urllib3 runtime round trip) and emits one
# go-test-style JSON event per test on stdout. Diagnostics go ONLY to the
# results/ logs — stdout must stay pure JSON so scripts/re-verify.sh can parse
# it (it merges stderr into the same stream).
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

# T2 — the hash-locked file resolves under --require-hashes for cp312/manylinux
if bash "$HERE/lockfile_resolve.sh" >"$RESULTS/lockfile_resolve.stdout" 2>&1; then
    emit "T2_lockfile_resolves_cp312" pass
else
    emit "T2_lockfile_resolves_cp312" fail
fi

# T4 — the pinned requests/urllib3 pair does a real HTTP round trip
if bash "$HERE/runtime_smoke.sh" >"$RESULTS/runtime_smoke.stdout" 2>&1; then
    emit "T4_requests_roundtrip" pass
else
    emit "T4_requests_roundtrip" fail
fi

# Harness-bites check (not a finding; guards the harness's own validity).
# Reported for the human log only — deliberately NOT a JSON test event.
if bash "$HERE/harness_bites.sh" >"$RESULTS/harness_bites.stdout" 2>&1; then
    echo "harness-bites: T2 turns red on planted bad pins (see results/harness_bites.log)" >>"$RESULTS/harness_bites.stdout"
fi
