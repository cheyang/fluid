#!/usr/bin/env bash
# base-premise.sh — P0 premise check, run against the BASE branch without the
# patch. Creates a throwaway git worktree at the frozen base sha, drops the
# premise test into api/v1alpha1/, runs it, and captures the output into
# results/base-premise.out. Idempotent; cleans the worktree up afterwards.
set -euo pipefail

TOPIC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_SHA="82e490e47b7116aaca6e3b7d9abc772111260e4d"
WT="$(mktemp -d /tmp/base-premise-wt.XXXXXX)"

git worktree add --detach "$WT" "$BASE_SHA" >/dev/null
trap 'git worktree remove --force "$WT" >/dev/null 2>&1 || true; git worktree prune' EXIT

cp "$TOPIC_DIR/premise/base_premise_verify_claude_test.go.txt" \
   "$WT/api/v1alpha1/premise_verify_claude_test.go"

cd "$WT"
go test ./api/v1alpha1/ -run "TestVerifyBasePremise" -count=1 -v \
  | tee "$TOPIC_DIR/results/base-premise.out"
