# cacheruntime-replicas-docs — verification harness

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6183
(docs: list the fields CacheRuntime can update in place; fixes #6182).

The PR is documentation + a stale-TODO-comment removal; it changes **no
executable code**. The harness therefore verifies the *behavior the docs now
assert* — both on the PR head (`6bcf1887`) and on the merge-base
(`f2785f84`) for the premise check.

Layers run against the code under review (`6bcf1887`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit / fake-client integration | `syncRuntimeSpec` replicas wiring, patch behavior, disabled-component boundary; `buildWorkerAffinity` placement modes | `go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus 'verification harness' -ginkgo.v` |
| 2. envtest | — (package has no envtest suite; fake-client layer decides all claims) | — |
| 3. Live | skipped this round (no KUBECONFIG by instruction); PR author recorded a kind run in the PR body | see `liveNote` in the manifest |

> Polarity: all harness specs are **contract** tests — they assert the intended
> behavior and pass on both base and head. There are no bug-canaries: no
> production-code defect was confirmed.

## Problem premise (P0)

Run against the **base** branch (`f2785f84`), without the patch.

| | |
|---|---|
| Claimed symptom | issue #6182: docs say only `runtimeVersion`/`resources` update in place and list `replicas` under “Unsupported Update Fields … you must redeploy the CacheRuntime for changes to take effect” — “This is wrong. `replicas` is synced in place for Master and Worker.” |
| Linked issue | #6182, OPEN, filed against exactly the two doc files this PR rewrites |
| Reported component | `docs/{zh,en}/samples/cacheruntime/cacheruntime_spec_update.md` + the `pkg/ddc/cache` behavior they describe |
| Patched component | the same two docs + stale TODO comment above the `syncRuntimeSpec` wiring in `pkg/ddc/cache/engine/sync.go` |
| Component match | **Yes** |
| **Verdict** | **Confirmed** |
| Evidence | On base: `spec.worker.replicas` 2→3 patches the worker ASTS `spec.replicas` in place — exactly 1 Patch call, pod template untouched (`results/l1-unit-base.json`); master 1→2 likewise. Issue's code-path citation (`sync.go` passes `&runtime.Spec.{Master,Worker}.Replicas`, always non-nil; `SyncComponentSpec` → `updateReplicas`) matches the code at `f2785f84`. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | `replicas` is synced in place on base (the docs were wrong) | 1 | **Confirmed** | `results/l1-unit-base.json`: 6/6 specs green on `f2785f84` |
| F1 | No engine-layer test pins the replicas propagation now documented publicly | 1 | Confirmed gap — filled by this harness | `sync_test.go` (1242 lines) covers resources/tieredStore/client for `syncRuntimeSpec`, never replicas; only the component layer (`sync_component_spec_test.go`) covers it. New specs green on head: `results/l1-unit-pr-head.json` |
| F2 | Doc claim “replicas > schedulable nodes ⇒ surplus Workers stay Pending” holds only under default/Exclusive placement | 1 | **Confirmed (doc claim is conditional)** | Exclusive/default: required hostname anti-affinity vs dataset-labeled pods present → one worker per node. Shared: no required term for same-dataset workers → co-location allowed. `results/l1-unit-pr-head.json` |
| F3 | “Scaling writes no RuntimeCondition” overstates: the readiness condition `RuntimeWorkersReady` does flip during scaling | — | Reasoning only (code reading) | `pkg/ddc/cache/engine/worker.go:88-92` + `status.go:58-62`: `RuntimeWorkersReady` is false while `readyReplicas < desiredReplicas` — observable during scale-out |

## Per-finding detail

### P0 — premise
Mechanism: `CacheEngine.Sync` → `syncRuntimeSpec` (`pkg/ddc/cache/engine/sync.go`)
passes `Replicas: &runtime.Spec.{Master,Worker}.Replicas` (always non-nil, unlike
`resources`) into `ComponentSpec`; `AdvancedStatefulSetManager.SyncComponentSpec`
(`pkg/ddc/cache/component/advanced_statefulset_manager.go:195`) applies it first via
`updateReplicas` (compares old/new, sets `asts.Spec.Replicas`) and patches with
`client.MergeFrom`. Unchanged values short-circuit (`needsUpdate == false`, no patch —
verified by the Patch-counting client). Disabled components are skipped entirely
(boundary spec).

Command: `go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus 'verification harness' -ginkgo.v`

Observed: 6/6 passed on base `f2785f84` (`results/l1-unit-base.json`) and on PR head
`6bcf1887` (`results/l1-unit-pr-head.json`). Expected: replicas propagate in place →
the base docs' “requires redeploy” claim was factually wrong → issue #6182 valid.

Harness-bites (both directions, then reverted, `git diff -- pkg/` empty):
- remove the worker `Replicas` wiring → exactly the worker-propagation spec fails
  (`results/bites.txt`);
- disable the Exclusive anti-affinity branch → exactly the default-placement spec
  fails (`results/bites2.txt`).

Full-suite regression check: the engine+component suites produce **identical**
failure sets on base and head (12 pre-existing failures, unrelated to the PR) —
`results/full-suite-base-vs-head.txt`.

### F1 — missing engine-layer test for the documented behavior
The docs now publicly promise that `spec.{master,worker}.replicas` propagates in
place. Nothing at the engine layer pins that wiring: if someone later drops the
`Replicas:` field from `ComponentSpec` (or gates it behind a flag), every existing
engine test stays green and the docs silently rot again — which is precisely how
issue #6182 happened. The component-layer test only proves the manager honors a
non-nil `Replicas`, not that the engine passes it. This harness's specs fill the
gap and are candidates for upstreaming (test-only diff).

### F2 — the “surplus Workers stay Pending” bullet is placement-dependent
`pkg/ddc/cache/engine/transform_worker.go:95-120`: with the default dataset
placement (`""` = `DefaultMode` = Exclusive, `api/v1alpha1/constant.go:41`),
worker pods get a **required** hostname anti-affinity against any pod carrying a
dataset label → one cache worker per node → replicas beyond the schedulable-node
count stay Pending (doc bullet correct). Under `placement: Shared`, the required
term only matches pods labeled `placement=Exclusive` (other datasets); same-dataset
workers may co-locate on one node, so the doc's unconditional statement is wrong
in that mode. Suggested doc fix: qualify the bullet with the default/exclusive
placement assumption (or mention `dataset.spec.placement: Shared`).

### F3 — “no RuntimeCondition” wording (nit, reasoning only)
No *scale-action* condition (`RuntimeWorkerScaledIn/Out`) or Event is written on
this path (verified: `SyncReplicas` is never called by the cache engine; the
Recorder is used only for UFS-mount and dataload failures). But the pre-existing
readiness condition `RuntimeWorkersReady` does flip (false while
`readyReplicas < desiredReplicas`) as scaling progresses, so “扩缩容不会写入
RuntimeCondition” is broader than reality. Optional rewording: “writes no
scale-specific RuntimeCondition/Event”.

## Proposed fixes (NOT applied to production here)
- **F1**: upstream the two harness test files (additive, test-only).
- **F2**: qualify the third §3.3 limitation bullet in both locales with the
  placement mode (default Exclusive ⇒ one worker per node; Shared allows
  co-location).
- **F3**: optional rewording as above.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cacheruntime-replicas-docs-claude`
(fork `cheyang/fluid`; production code untouched), so it grafts onto whatever
the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cacheruntime-replicas-docs-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cacheruntime-replicas-docs \
     pkg/ddc/cache/engine/sync_replicas_verify_test.go \
     pkg/ddc/cache/engine/worker_affinity_placement_verify_test.go
   ```
2. Prereqs: Go toolchain only (no envtest assets needed).
3. Re-run:
   ```bash
   bash docs/verification/cacheruntime-replicas-docs/scripts/re-verify.sh
   ```
   (no ref: fetches the current PR head from `manifest.pr`), or
   `re-verify.sh <fixed-ref>`.
4. Read results via the polarity table: all specs are contract tests — they
   should be GREEN. If the author changes the sync wiring or the affinity
   rules, the corresponding spec goes red: treat red as “behavior changed,
   re-check the docs”, not automatically “bug”.
5. Live layer: only if a cluster is available — see `liveNote` in the manifest.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cacheruntime-replicas-docs-claude
(fork cheyang/fluid). Background: a review of fluid PR #6183 (docs claiming
CacheRuntime replicas update in place) produced findings F1 (missing engine-layer
replicas test), F2 (Pending-on-overflow bullet is placement-dependent), F3 (no
RuntimeCondition wording); a fake-client harness reproduced the premise and both
mechanisms. Read docs/verification/cacheruntime-replicas-docs/README.md
("Continuing after the fix") and follow it: graft the harness onto the fixed ref,
re-run scripts/re-verify.sh, mind that all specs are contract tests (red means
the documented behavior changed — re-check the docs), and report an
observed-vs-expected table. Do not run cluster-wide destructive actions.
```
