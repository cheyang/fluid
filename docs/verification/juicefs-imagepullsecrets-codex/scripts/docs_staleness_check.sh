#!/usr/bin/env bash
# docs_staleness_check.sh — contract check for F-docs-recreate-note-stale.
# Round 3 of PR #6245 wires imagePullSecrets into SyncRuntime, so changing the
# secrets on an existing runtime propagates on the next reconcile (pods pick
# them up when recreated; worker/fuse update strategy is OnDelete). The sample
# docs still state the opposite ("changing them on an existing runtime needs
# the runtime to be recreated" / zh equivalent). This script exits 1 while the
# stale sentence is present in docs/{en,zh}/samples/image_pull_secrets.md.
# Usage: docs_staleness_check.sh <repo-root>
set -euo pipefail
ROOT="${1:-$(git rev-parse --show-toplevel)}"
rc=0
if grep -q "needs the runtime to be recreated" "$ROOT/docs/en/samples/image_pull_secrets.md"; then
  echo "STALE: docs/en/samples/image_pull_secrets.md still claims changing secrets needs runtime recreation (SyncRuntime propagates them since round 3)"
  rc=1
fi
if grep -q "需要重新创建 runtime" "$ROOT/docs/zh/samples/image_pull_secrets.md"; then
  echo "STALE: docs/zh/samples/image_pull_secrets.md still claims changing secrets needs runtime recreation"
  rc=1
fi
if [ "$rc" -eq 0 ]; then
  echo "OK: no stale recreate-to-change note in either language sample"
fi
exit "$rc"
