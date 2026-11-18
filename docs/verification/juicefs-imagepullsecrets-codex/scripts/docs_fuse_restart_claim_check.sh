#!/usr/bin/env bash
# docs_fuse_restart_claim_check.sh - contract check for
# F-docs-fuse-restart-overstated (PR #6245 round 5).
#
# The round-5 docs commit replaced the stale "needs the runtime to be
# recreated" sentence with a claim that on a secrets change "the fuse pods are
# restarted" (en) / "fuse pod 会重启" (zh). On the default configuration that
# overstates the behavior: the fuse DaemonSet is OnDelete (chart default, and
# syncFuseSpec flips any other strategy back), so a template update never
# restarts running pods; the fuse generation bump recycles fuse pods only
# lazily via CSI NodeUnstage and only under cleanPolicy OnFuseChanged/OnDemand
# (the default is OnRuntimeDeleted). Running fuse pods keep the old secrets
# until recreated - same as the worker pods the same sentence describes.
# Exits 1 while the overstatement is present in either language sample.
# Usage: docs_fuse_restart_claim_check.sh <repo-root>
set -euo pipefail
ROOT="${1:-$(git rev-parse --show-toplevel)}"
rc=0
if grep -qi "fuse pods are restarted" "$ROOT/docs/en/samples/image_pull_secrets.md"; then
  echo "OVERSTATED: docs/en/samples/image_pull_secrets.md claims the fuse pods are restarted on a secrets change (fuse ds is OnDelete and the default clean policy is OnRuntimeDeleted; running fuse pods keep the old secrets until recreated)"
  rc=1
fi
if grep -q "fuse pod 会重启" "$ROOT/docs/zh/samples/image_pull_secrets.md"; then
  echo "OVERSTATED: docs/zh/samples/image_pull_secrets.md claims the fuse pods restart on a secrets change (same mechanism)"
  rc=1
fi
if [ "$rc" -eq 0 ]; then
  echo "OK: no immediate-fuse-restart claim in either language sample"
fi
exit "$rc"
