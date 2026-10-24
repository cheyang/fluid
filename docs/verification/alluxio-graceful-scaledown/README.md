# Verification harness — PR #6061 (re-land graceful scale-down for AlluxioRuntime)

Reviewer: Codex (Reviewer B). Base: `a6d461c721d43d602a4bd17ca998fa444040e069`,
head under review: `69ed16fdd6962bf13dc674dcb0ad617c60ccbe48`.

## P0 — premise

The PR re-lands #5805 (reverted in #6059 for missing e2e coverage) and adds
`test/gha-e2e/alluxio-scaledown/`. Claimed mechanism (pkg/features/features.go
godoc): decommissioning workers ahead of scale-in "giv[es] the Alluxio master
... time to migrate their cached blocks to the surviving workers".

Premise verdict: **Partially refuted.**

* Alluxio's `fsadmin decommissionWorker` does NOT migrate/evacuate cached
  blocks: the master immediately removes the decommissioned worker's block
  locations and marks last-copy blocks lost (`DefaultBlockMaster.
  processDecommissionedWorkerBlocks` -> `processWorkerRemovedBlocks` ->
  `mLostBlocks.add`, v2.9.4; replication checker only restores up to
  `replicationMin` (default 0) and skips lost blocks). What it does do:
  exclude the worker from new I/O and wait for in-flight operations to
  idle. Evidence: `results/DecommissionWorkerCommand-v2.9.4.java`,
  `results/DefaultBlockMaster-v2.9.4.java`, `results/ReplicationChecker-v2.9.4.java`.
* On the default deployment the command does not even exist: the fluid chart
  pins `alluxio/alluxio-dev:2.9.0` (charts/fluid/fluid/values.yaml), an image
  last pushed 2023-01-11, while the decommission feature landed 2023-03-30
  and is documented first in v2.9.4 (`results/AdminCLI-v2.9.0.md` vs
  `results/AdminCLI-v2.9.4.md`).
* The PR body's claim that the gate "was never exposed as a flag on the
  alluxioruntime-controller binary ... Fixed that too" is inaccurate: the
  base already wires `utilfeature.DefaultMutableFeatureGate.AddFlag(
  alluxioCmd.Flags())` (cmd/alluxio/app/alluxio.go:101 at base), the gate is
  registered via the `pkg/features` import chain, and the PR diff does not
  touch `cmd/alluxio/` at all (`results/evidence-summary.txt`).

## Findings and observed-vs-expected

| ID | claim | polarity | layer | observed on head | expected after fix |
|----|-------|----------|-------|------------------|--------------------|
| F1 | default Alluxio image (2.9.0) lacks `decommissionWorker`; gate on + default image => every scale-down stalls 10 min then force-proceeds ungracefully | contract | L1 script | FAIL (exit 1): imageTag 2.9.0 < 2.9.4 | PASS when default image >= 2.9.4 or version-gated |
| F2 | e2e cannot prove data survival: single 32-byte fixture = 1 block on a random local worker (LocalFirstPolicy shuffles); victim placement (~50%/run) must fail even with a perfect decommission; test.sh never checks the DecommissionFailed condition | contract | L1 script | FAIL (exit 1) | PASS when fixture is multi-block AND drain success is asserted |
| F3 | `--wait 0s` does not "return immediately": upstream passes an absolute epoch into TimeoutRetry which expects a duration, so the wait bound never fires; the CLI sleeps user.worker.list.refresh.interval (2 min) and then polls until 20 consecutive quiet seconds (up to the 1500s exec timeout) | contract | L1 script + upstream source | FAIL (exit 1): `--wait 0s` still passed | PASS when the invocation/comment match upstream behavior |
| F4 | upstream matches decommission targets by host only; chart sets no worker anti-affinity; co-located workers (always the case in the single-node e2e) can decommission the surviving worker instead of the victim | contract | L1 script + upstream source | FAIL (exit 1): no guard/doc | PASS when guarded or documented and handled |
| F5 | during a drain (up to 10 min) every reconcile returns an error: controller emits Warning "Failed to sync the ddc" events every 20s and skips SyncRuntime/health/dataset-status updates | reasoning only (needs envtest/live) | not run | code path read: pkg/ddc/base/syncs.go returns early; pkg/controllers/runtime_controller.go:301-304 emits Warning event | design choice to confirm with maintainers |
| F6 | PR body claims a cmd/alluxio flag fix that is not in the diff and was not needed | evidence script | git | confirmed inaccurate | n/a (description-only) |

Unit regression layer (the PR's own tests, all passing on head):
`go test -gcflags="all=-N -l" ./pkg/ddc/alluxio/operations/` and the ginkgo
suites `drainScalingDownWorkers|getWorkerWebPort|workerWebPortFromPod|decommission deadline`
(21 specs) — see `results/unit-tests-head.txt`.

## How to re-run

```bash
# from the repo root of this branch
bash docs/verification/alluxio-graceful-scaledown/scripts/re-verify.sh
```

`re-verify.sh` grafts nothing (harness is already on top of the code under
review), runs the unit regression layer and the contract scripts, and prints
per-finding Fixed / Still-broken / Harness-update. Exit 0 iff all findings
are fixed.

## Harness-bites check

`results/harness-bites.txt`: each contract script was re-run against a
simulated fix (image tag bumped to 2.9.4 in a temp copy; `--wait 0s` removed;
multi-block fixture + DecommissionFailed assertion added) and flips to PASS.
Production code in this branch is untouched.

## Live-layer note (not run in this environment)

The decisive end-to-end evidence requires a cluster: deploy with the default
chart, enable the gate, scale down, and inspect (a) master pod logs for
`decommissionWorker` unrecognized-command errors (F1), (b) whether the block
location count for the fixture survives decommission (F2), (c) decommission
timing vs the 10m deadline. CI logs/artifacts for the July run are expired
(HTTP 410), so this remains open for a live re-check.
