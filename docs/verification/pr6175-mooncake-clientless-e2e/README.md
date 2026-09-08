# Verification — PR #6175 (Mooncake client-less topology e2e)

Reviewer-private harness. Proves/disproves the PR premise and each finding with a
layered, reproducible setup. Production code is **not** modified; everything lives
under `docs/verification/` and `scripts/`.

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6175
- **Head reviewed:** `01b19d16` (`btxu-db/fluid:test/mooncake-client-less-e2e`)
- **Cluster:** ACK cn-hongkong, k8s v1.36.1, 3 nodes, containerd
- **Deployed Fluid at test time:** `fluidcloudnative/*:v1.1.0-36f0467` (pre-#6157)

## Premise verdict

**CONFIRMED.** The PR adds an e2e test for a *client-less* CacheRuntimeClass
(only `master` + `worker`, no `client`), which is exactly the topology that
`generateRuntimeConfigData` (pkg/ddc/cache/.../cm.go) dereferenced a nil `client`
on before the #6157 fix.

- **L0 (canary, against pre-fix `36f0467`):** applying `cacheruntimeclass.yaml` +
  `dataset.yaml` + `cacheruntime.yaml` drives `cacheruntime-controller` into a nil
  pointer panic (`Observed a panic in reconciler`, restartCount 4→6→8) and leaves
  the Dataset `NotBound`. This reproduces the bug the test is meant to guard.
- **L4 (positive, against master):** with master `cacheruntime-controller` **and**
  `dataset-controller`, the full `test/gha-e2e/mooncake/test.sh` **PASSES** end to
  end in ~2m50s — all six assertions green (see below).

The PR is a *test-only* change: it does not touch controller logic, so it cannot
itself fix #6157 — it relies on #6157 already being in master. Confirmed: #6157 is
merged and present on master, so the new test is valid there and would have caught
the original regression.

## Observed vs expected

| # | Layer / assertion | Expected | Observed | Verdict |
|---|-------------------|----------|----------|---------|
| P0 | L0 canary: client-less vs pre-fix controller | panic + Dataset NotBound | nil-pointer panic at cm.go, restarts 4→6→8, phase=NotBound | ✅ CONFIRMED |
| P1 | L4: full test.sh on master Fluid | all assertions pass | 6/6 green, ~2m50s | ✅ CONFIRMED |
| A1 | Dataset Bound, controller not panicked | Bound, no panic | Bound; no panic in current logs | ✅ pass |
| A2 | Cache worker ready, no client component | worker Ready, no client | worker Ready; no client pod/svc/asts | ✅ pass |
| A3 | cacheStates reflect worker capacity | cacheCapacity==ufsTotal | cacheCapacity=1.00GiB, ufsTotal=1.00GiB | ✅ pass |
| A4 | rw_job put/get md5 round-trip | job Completed, md5 verified | Completed; put_md5==get_md5 | ✅ pass |
| A5 | cached bytes / fileNum after write | cached=4.00MiB, fileNum=1 | cached=4.00MiB, fileNum=1 | ✅ pass |
| A6 | PVC not client-mountable (FailedMount) | "timeout waiting for FUSE mount point" | exact message observed | ✅ pass |
| A7 | GC: Dataset/Runtime delete cleans ASTS/SVC/PV/PVC | resources removed | removed (see F10 caveat) | ⚠️ passes but weakly |
| F1 | L3: reportSummary.sh master-only (capacity 0B) | graceful output | `set -e` + `grep -oE "\([0-9.]+%\)"` exit 1 → script crashes | ✅ CONFIRMED (minor) |
| F2 | L3: worker entrypoint jq parsing | correct svc/quota/segment | MASTER_SVC/WORKER_SVC/QUOTA/SEGMENT parsed correctly | ✅ no bug (validated) |
| F4 | L3: rw_job python put/get | md5 round-trip | verified against real master/worker in docker | ✅ no bug (validated) |
| F10 | L5: GC selector `-l fluid.io/managed-by=fluid` | selects the ASTS/SVC it must wait on | selector is **empty**; controller labels are `cacheruntime.fluid.io/{name,component-name}` | ✅ CONFIRMED (major) |
| F15 | L4: `check_controller_not_panicked` log source | catch panics across restarts | uses `--tail=200` with **no `--previous`** → misses pre-restart panic | ✅ CONFIRMED (minor) |

## Findings

**F10 — vacuous GC assertion (major, but inherited & non-blocking).**
`wait_runtime_deleted` in `test.sh` polls
`kubectl get advancedstatefulset,daemonset,svc -l fluid.io/managed-by=fluid -n default`
and breaks when that is empty. But the resources the controller creates carry
`cacheruntime.fluid.io/name` and `cacheruntime.fluid.io/component-name` (from
`getCommonLabelsFromComponent`, pkg/ddc/cache/component/component_manager.go:64),
**not** `fluid.io/managed-by=fluid`. So the selector matches nothing from the start
and the "GC succeeded" branch is trivially true — L5 shows the test selector empty
while the synthetic ASTS+SVC (with the real controller labels) still exist. In L4
the overall delete still passed because the Dataset/Runtime teardown does remove
the objects; the assertion is simply not what proves it. Same pattern is inherited
from the curvine e2e test, so this is a pre-existing weakness the PR copies, not one
it introduces. Severity: major (test could go green on a GC regression) but
non-blocking for this test-only PR.

**F1 — reportSummary.sh crashes master-only (minor).**
`PERCENT_RAW=$(echo "$MEM_LINE" | grep -oE "\([0-9.]+%\)" | tr -d "()%")` under
`set -euo pipefail`: when a master has no worker segment registered the summary is
`Mem Storage: 0 B / 0 B` with **no** `(N%)` group, `grep` exits 1, and the script
aborts. Only reachable in the master-only window before a worker registers (the
real deployment always has a worker, and the test queries after readiness), so it
does not break the PR test — but the script is not robust standalone.

**F15 — panic check ignores previous container logs (minor).**
`check_controller_not_panicked` reads `kubectl logs ... --tail=200` without
`--previous`. A panic that triggered a container restart (exactly the L0 failure
mode) is only in the *previous* log; the fresh post-restart log is clean, so the
check can pass while a panic did occur. L0 uses `--previous` and catches it. In the
passing L4 path this is moot (no panic), but it weakens the guard that is the whole
point of the test.

## Validated (no bug) — Copilot findings addressed

- **F2** worker `custom-entrypoint.sh` jq parsing (`.master.service.name`,
  `.worker.service.name`, `.worker.tieredStoreLevels[0].quotas[0] // "1GiB"`,
  Gi→GB/Mi→MB): correct (L3).
- **F4** `rw_job.yaml` hardcoded `MASTER=mooncake-demo-master-0.svc-mooncake-demo-master`
  matches controller naming; put/get md5 round-trips (L3, L4).
- Copilot gating fix in `.github/scripts/build-all-images.sh`
  (`WITH_E2E_TEST_IMAGES=="true"` builds oss-emulator + mooncake; kind-e2e sets it,
  backward-compatibility-e2e does not and only exercises Alluxio): correct.
- `check_cached_after_write` requires `-n "$file_num" && "$file_num" != "0"`
  (Copilot fix applied): correct.
- cheyang `[[ ]]` style comment: addressed.

## Layout

    scripts/
      layer0-premise.sh      L0 canary vs pre-fix controller (expects panic)
      layer3-image-scripts.sh L2 build + L3 reportSummary/entrypoint/rw_job in docker
      layer4-live-e2e.sh     L4 full test.sh on master controllers (swap+restore)
      layer5-gc-selector.sh  L5 synthetic labels vs test GC selector (F10)
      re-verify.sh           resume entry point; resolves head from manifest.pr,
                             delta from .last-reviewed; --live adds L0+L4
    verify-manifest.json     pr/topic/layers/findings
    .last-reviewed           sha of last reviewed head (01b19d16)

## Re-run

    scripts/re-verify.sh           # static + docker layers (no cluster swap)
    scripts/re-verify.sh --live    # + L0 premise + L4 live e2e on ACK

L4 pushes master controllers to Docker Hub `rolebasedgroup/*:verify-6175`, swaps
both `cacheruntime-controller` and `dataset-controller`, and **restores** the
original images on exit. `dataset-controller` must be swapped too: master added
`FileNum`/`UfsTotal` to `api/v1alpha1/common.go`, so cacheStates (A3/A5) need both
controllers at master. CRDs, mount.go FUSE message, and webhook were verified
unchanged between `36f0467` and master.
