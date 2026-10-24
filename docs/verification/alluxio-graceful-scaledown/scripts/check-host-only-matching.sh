#!/bin/bash
# F4 risk check (documentation-of-limitation contract): Alluxio's
# decommissionWorker matches the target worker by HOST ONLY
# (DecommissionWorkerCommand.findMatchingWorkerAddress compares
# worker.getNetAddress().getHost() and ignores the port, v2.9.4). The
# alluxio chart sets no pod anti-affinity on workers, so two workers of the
# same cluster can be scheduled on one node (this always happens in the
# single-node kind e2e). In that case the decommission may drain the
# SURVIVING worker while the actual victim is killed undrained.
#
# Contract (fails while unaddressed): fluid must either document/guard the
# co-located-workers limitation, or verify the decommission hit the intended
# worker (e.g. by checking the decommissioned worker list rather than just
# the live count).
set -u
if grep -q "host-only\|same node\|co-located\|colocated" docs/verification/alluxio-graceful-scaledown/README.md 2>/dev/null \
   && grep -rq "Decommissioned" pkg/ddc/alluxio/operations/decommission.go; then
  echo "PASS: co-location limitation acknowledged and decommission result verified per-worker"
  exit 0
fi
echo "FAIL: no guard or verification that decommission targeted the intended worker when workers share a node (Alluxio matches by host only)"
exit 1
