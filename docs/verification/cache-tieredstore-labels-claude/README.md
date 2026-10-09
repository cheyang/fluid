# cache-tieredstore-labels — bug verification (PR #6181)

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6181
(`fix(cache): build RuntimeInfo from the worker tiered store`).

Reviewer: Claude (round 1, 2026-10-10). Harness branch:
`verify/cache-tieredstore-labels-claude`, based on PR head `6ea38789`.
Production code is untouched — the diff is two additive test files plus this docs tree.

Layers run against the **code under review** (`6ea38789`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `convertToLegacyTieredStore` quota string round-trip into `base.BuildRuntimeInfo` → `tieredstore.GetLevelStorageMap` | `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -count=1 -ginkgo.focus 'quota string round-trip'` |
| 2. Integration (fake client) | full production path `CacheEngine.getRuntimeInfo` → `lifecycle.SyncScheduleInfoToCacheNodes` → node labels | `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -count=1 -ginkgo.focus 'Verification harness: PR #6181 tiered store'` |
| 3. Live | real cluster, real CacheRuntime | **skipped this round by debate-pipeline configuration** — see "Live run notes" |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

Answered before the findings, run against the **base** branch `f2785f847` (merge-base) without
the patch, because the question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom | issue #6174: "`fluid.io/s-h-cache-m-<ns>-<name>` and `fluid.io/s-h-cache-d-<ns>-<name>` are never added at all, while `totalRequirement` keeps its initial `resource.MustParse("0Gi")` and `fluid.io/s-h-cache-t-<ns>-<name>` is written unconditionally as `0B`" |
| Linked issue | #6174, OPEN, created 2026-08-28 (~6 weeks old), no labels, still valid — `runtime.go:100` on base still passes `datav1alpha1.TieredStore{}` |
| Reported component | `pkg/ddc/cache/engine/runtime.go` `getRuntimeInfo` → node capacity labels |
| Patched component | exactly that: `runtime.go` + new `convertToLegacyTieredStore` in `transform_tiered_store.go` |
| Component match | **Yes** |
| **Verdict** | **Confirmed** |
| Evidence | the 4 L2 contract specs grafted onto merge-base `f2785f847` fail with the node label map literally showing `"fluid.io/s-h-cache-t-default-demo": "0B"` and no `m`/`d` keys; the same specs pass on PR head `6ea38789`. results/l2-base-premise.log, results/focused-runs.log |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | premise: CacheRuntime with a declared worker tiered store advertises 0B / no capacity labels | 2 | **Confirmed** | red on base, green on head; results/focused-runs.log |
| F1 | the fix populates total/mem/disk labels correctly for emptyDir (disk), emptyDir Memory + processMemory (mem), hostPath multi-path (per-path quotas summed, not averaged), and multi-level tiered stores | 2 | **Confirmed** | 4 contract specs red on base → green on head; results/focused-runs.log |
| F2 | QuotaList `.String()` round-trip: every quota format a CRD user can write (BinarySI, DecimalSI, decimal mantissa + binary suffix, plain bytes, Ki/Mi/Gi/M) re-parses in `convertToTieredstoreInfo` without error and sums exactly | 1 | **Confirmed** (risk retired) | 7 table/contract specs green on head; results/focused-runs.log |
| F3 | upgrade path: a node already labelled by the pre-fix code keeps its stale `0B` total and missing m/d labels forever (labels are written once, when the node first enters the cache node set) | 2 | **Confirmed as a limitation, not a regression** — bug-canary passes on both base and head | results/focused-runs.log |

Pre-existing failures, not the PR's: `pkg/ddc/cache/engine` has 12 failing specs
(1× `sync_test.go`, 11× `ufs_test.go`, all `pods "test-runtime-master-0" not found`
fake-client issues) that are byte-for-byte identical on merge-base and PR head.
`go test ./pkg/ddc/base/... ./pkg/utils/tieredstore/... ./pkg/utils/dataset/lifecycle/...`
is fully green on head.

## Per-finding detail

### P0 / F1 — mechanism

`getRuntimeInfo` (pkg/ddc/cache/engine/runtime.go:100) passes
`base.WithTieredStore(convertToLegacyTieredStore(runtime.Spec.Worker.TieredStore, e.Log))`.
The conversion maps `ProcessMemory`→MEM at `/dev/shm`, `EmptyDir(medium:Memory)`→MEM,
`EmptyDir(default)`→HDD, `HostPath`→HDD with per-path `QuotaList`; skipped levels
(no medium, or paths/quotas length mismatch) are logged and dropped so
`convertToTieredstoreInfo` can never error out of `BuildRuntimeInfo` and fail the reconcile.

L2 harness drives the exact production chain used by `sync.go`:
`getRuntimeInfo` → `lifecycle.SyncScheduleInfoToCacheNodes` → `addScheduleInfoToNode` →
`labelNodeWithCapacityInfo` → `tieredstore.GetLevelStorageMap`, against a fake client
holding a CacheRuntime, an AdvancedStatefulSet worker and a worker pod on `node-0`.

- base (no patch): `"fluid.io/s-h-cache-t-default-demo": "0B"`, no `m`/`d` labels —
  exactly the issue's observed symptom.
- head: `t=1GiB,d=1GiB` (emptyDir default), `t=4GiB,m=4GiB` (processMemory),
  `d=4GiB` not `2GiB` (hostPath 1Gi+3Gi — proves per-path quotas survive rather than
  being averaged by `convertToTieredstoreInfo`'s single-Quota path),
  `m=4GiB,d=1GiB,t=5GiB` (two levels).

### F2 — QuotaList round-trip

`convertToLegacyTieredStore` serializes host-path quotas with `resource.Quantity.String()`
and downstream `convertToTieredstoreInfo` re-parses them with `resource.ParseQuantity`.
The table covers `100Gi/50Gi`, `1500M/500M`, `1.5Gi`, `1.5Gi+1500M+1024Mi` (=4184354560
bytes), `512Ki+512Ki`, `1024` and multi-level host paths summing to 6Gi. All parse, all
sum exactly, no error from `BuildRuntimeInfo`.

### F3 — upgrade path (bug-canary)

`calculateNodeDifferences` (pkg/utils/dataset/lifecycle/node.go:131) only computes
nodes-to-add = desired − actual, and `addScheduleInfoToNode` (node.go:168) returns early
when the node already carries the runtime label. So nodes labelled while the bug was live
keep `t=0B` and no m/d labels until the runtime is recreated. The PR author states this
explicitly in "Special notes for reviews" and defers the healing decision. The canary
asserts the stale state persists through `SyncScheduleInfoToCacheNodes`; it passes on both
base and head. If label healing is implemented later the canary flips red and must be
inverted into a contract test.

## Live run notes

Not run. The probe found a reachable cluster (`kubectl get no`: 2 Ready nodes,
`cn-hongkong.10.215.180.124/125`, v1.36.2-aliyun.1), but this debate round is configured
for unit + integration layers only, and the cluster is a shared production-adjacent
environment not vouched for as a test cluster. The live signal to check if the layer is
ever run: the PR body's own steps — create the CacheRuntime from issue #6174, then
`kubectl get node <node> -o json | jq '.metadata.labels | with_entries(select(.key | contains("fluid.io")))'`
and expect `fluid.io/s-h-cache-t-...=1GiB` plus the `d` (or `m`) label.

## Proposed fixes (NOT applied to production here)

- None required for the PR's own scope: the mechanism is correct and verified.
- F3 (optional follow-up, author already flagged): heal stale capacity labels for
  already-labelled nodes, or document that upgrading requires recreating the CacheRuntime.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cache-tieredstore-labels-claude` (fork
`cheyang/fluid`; production code untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-tieredstore-labels-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cache-tieredstore-labels \
     pkg/ddc/cache/engine/verify_pr6181_labels_test.go \
     pkg/ddc/cache/engine/verify_pr6181_unit_test.go
   ```
   Or just: `bash docs/verification/cache-tieredstore-labels/scripts/re-verify.sh` from a
   checkout of the harness branch — it resolves the current PR head from
   `manifest.pr`, grafts, runs both layers, and prints per-finding verdicts.
2. Prereqs: Layer 1+2 = Go toolchain only (fake client; `FLUID_UNIT_TEST=true`).
   Layer 3 = a real cluster, skipped this round.
3. Re-run:
   ```bash
   FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -count=1 \
     -ginkgo.focus 'Verification harness: PR #6181' -ginkgo.json-report /tmp/r.json
   ```
4. Read results via polarity: the F1 contract specs should be GREEN; the F3 canary stays
   green while the limitation persists and FLIPS if healing lands (then invert it).
5. Harness-bites (already done this round): the same contract specs were run against
   merge-base `f2785f847` and went red with `0B` in the observed label map — the harness
   demonstrably exercises the buggy path, not a vacuous one.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cache-tieredstore-labels-claude
(fork cheyang/fluid). Background: a review of fluid PR #6181 (cache runtime tiered
store -> node capacity labels) produced findings P0/F1/F2/F3; a harness reproduced
them (P0 confirmed on merge-base f2785f847; F1/F2 green on head 6ea38789; F3 is a
canary for the stale-label upgrade limitation). Read
docs/verification/cache-tieredstore-labels/README.md ("Continuing after the fix")
and follow it: graft the harness onto the current PR head, re-run both ginkgo layers
with FLUID_UNIT_TEST=true, mind the polarity table (invert the F3 canary if it
flips), and report an observed-vs-expected table. The 12 ufs/sync test failures in
pkg/ddc/cache/engine are pre-existing and out of scope. Do not run cluster-wide
destructive actions.
```
