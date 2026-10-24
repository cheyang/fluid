#!/bin/bash
# F2 contract check: the alluxio-scaledown e2e must be able to prove that
# data cached ONLY on a removed worker survives the scale-down. With the
# current fixtures this is impossible:
#   - the fixture is a single 32-byte file => exactly one Alluxio block
#     (default block size 64MB), cached on ONE worker, picked at random
#     among node-local workers (LocalFirstPolicy shuffles candidates; fluid
#     sets alluxio.user.ufs.block.read.location.policy=LocalFirstPolicy in
#     pkg/ddc/alluxio/transform_optimization.go)
#   - Alluxio's decommissionWorker does NOT evacuate blocks: the master
#     removes the decommissioned worker's block locations immediately and
#     marks last-copy blocks lost (DefaultBlockMaster.
#     processDecommissionedWorkerBlocks -> processWorkerRemovedBlocks ->
#     mLostBlocks.add, v2.9.4); the replication checker only restores blocks
#     up to file.replicationMin (default 0) and skips lost blocks
#     (ReplicationChecker REPLICATE branch, v2.9.4)
# => when the fixture block lands on the removed worker (~50% per run on the
#    single-node kind cluster), the post-scale-down read with minio down must
#    FAIL even if the decommission machinery works perfectly. The test passes
#    exactly in the runs that prove nothing.
#
# Contract (fails while the finding is unfixed): the e2e must either write a
# multi-block fixture (more bytes than one block, so every worker holds some
# blocks) AND/OR explicitly assert block placement / drain success signals
# (e.g. fail if the WorkerDecommissioning condition ever shows
# DecommissionFailed, or if scale-down takes ~the 10m deadline).
set -u
TDIR="test/gha-e2e/alluxio-scaledown"
fail=0

# 1) fixture must exceed one 64MB block so every worker deterministically
#    holds some blocks
fixture_gen=$(grep -o "printf '%s' '[^']*'" "$TDIR/minio_create_bucket.yaml" || true)
echo "fixture generator: ${fixture_gen:-<none>}"
if echo "${fixture_gen}" | grep -q "fluid-alluxio-scaledown-e2e-fixture"; then
  echo "FAIL: fixture is a single small constant string (one block, random single-worker placement)"
  fail=1
fi

# 2) the test must fail if the decommission path silently degraded to the
#    forced-proceed deadline fallback
if grep -q "DecommissionFailed" "$TDIR/test.sh"; then
  echo "OK: test.sh inspects the DecommissionFailed condition"
else
  echo "FAIL: test.sh never asserts on the WorkerDecommissioning condition / DecommissionFailed, so a silently broken decommission (e.g. command missing on the default alluxio-dev:2.9.0 image) passes the test after a 10-minute stall"
  fail=1
fi

exit $fail
