#!/bin/bash
# F3 contract check: pkg/ddc/alluxio/operations/decommission.go passes
# `--wait 0s` claiming the CLI "returns immediately" and does not block the
# reconcile. Upstream (Alluxio v2.9.4 and master):
#   DecommissionWorkerCommand.waitForWorkerToBecomeIdle builds
#     new TimeoutRetry(startWaiting.toEpochMilli() + waitTimeMs, 1000)
#   and TimeoutRetry interprets the first constructor argument as a DURATION
#   (attempt() compares (now - mStartMs) <= mRetryTimeoutMs), so an absolute
#   epoch timestamp (~1.7e12 ms) makes the wait bound never fire: --wait is
#   effectively ignored and the command polls until every target worker has
#   been quiet for 20 consecutive 1s checks (WORKER_QUIET_THRESHOLD=20), or
#   forever on a busy worker. Before that, verifyFromMasterAndWait()
#   unconditionally sleeps alluxio.user.worker.list.refresh.interval
#   (default 2min). So each DecommissionWorkers call blocks the reconcile
#   >= ~2.5min on the happy path and up to fluid's FileUtilsExecTimeout
#   (1500s) on a busy worker.
#
# Contract (fails while the finding is unfixed): decommission.go must not
# pass `--wait 0s` under the claim that it returns immediately.
set -u
SRC="pkg/ddc/alluxio/operations/decommission.go"
if grep -q -- '"--wait", "0s"' "$SRC"; then
  echo "FAIL: decommission.go still invokes decommissionWorker with --wait 0s (comment claims immediate return; upstream ignores the wait bound and blocks until workers are quiet)"
  exit 1
fi
echo "PASS: --wait 0s no longer used"
exit 0
