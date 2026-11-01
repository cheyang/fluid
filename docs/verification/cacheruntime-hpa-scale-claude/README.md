# cacheruntime-hpa-scale — bug verification (Reviewer A / Claude)

Reproducible evidence for the review of PR #6223
("feat(cache): support HPA on CacheRuntime via scale subresource and status selector").

Round 3 (incremental, 2026-11-02): the PR head moved `9a265eb3 → 99e35af0` with one PR
commit, `99e35af0e fix(cache): count all pods in component status during mixed-revision
rollouts` — it addresses two ledger findings: `C4420756561-currentreplicas-undercount`
(`constructStatus` now exposes `asts.Status.Replicas`, all observed pods, instead of the
revision-scoped `asts.Status.CurrentReplicas`) and `F-scale-write-e2e-skipped` (the e2e
scale-write assertions now run unconditionally on single-node clusters; only the pod-count
checks stay gated). The merge-base is unchanged (`96a3765e`). A **new contract test**
(`pkg/ddc/cache/component/asts_status_mixed_revision_harness_test.go`) was added to pin the
mixed-revision behavior; it is red on the previous head and green on this head, and the
whole harness was re-run via `scripts/re-verify.sh` (all layers). The harness branch was
rebased onto the new head, so the branch diff vs the PR head is again harness-only.

Round 2 (incremental, 2026-10-31): the PR head moved `3f39501e → 9a265eb3` by merging
master (`96a3765ea`, "ci: pin kind-e2e runner to ubuntu-24.04" — CI-only, **all PR code
files byte-identical**, verified with `git diff 3f39501 9a265eb -- . ':(exclude).github'` →
empty). The merge-base moved `fe074af → 96a3765e`. The whole harness was re-run against the
new head and the premise re-attested against the new base; the envtest probe (previously an
untracked phase-2 scratch dir) is now committed here so it travels with the branch.

Layers run against the **code under review** (`9a265eb349b6fc0dd8bc91aec57beea41001c232`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `CacheEngine.getWorkerSelectors`, `CheckAndUpdateRuntimeStatus` selector population, component label derivation | `go test ./pkg/ddc/cache/engine/ -run 'TestHarness' -v` |
| 2. Integration (fake-client) | the selector string vs. the real worker workload creation path (`component.NewComponentHelper(...).Reconciler`), and the worker-disabled lifecycle | same command (ginkgo specs of the PR itself: `go test ./pkg/ddc/cache/engine/ ./pkg/ddc/cache/component/`) |
| 2b. envtest | real API server + the PR CRD: `/scale` behavior for worker-less payloads (findings F-workerless-scale-docs, Q-blockless-worker-scale-target) | `KUBEBUILDER_ASSETS=$HOME/.local/share/kubebuilder-envtest/k8s/1.31.0-linux-amd64 go test ./zz-critique-probe/ -run TestCritiqueProbe -v` |
| 3. Live | real API server scale subresource — covered by the repo's kind e2e in CI (curvine case), **not** run here (debate setup skips the live layer) | see "Live run notes" |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP TO RED when fixed.

## Problem premise (P0)

Run against the **base** branch (`fe074af38434fc4b618686de6459a9173570b48b`, round 1; round 2
re-attested against the new merge-base `96a3765eaae0f23140aef9b30152538396ed7ec2` — the two
bases differ only in `.github/workflows/kind-e2e.yml`) without the patch.

| | |
|---|---|
| Claimed symptom | issue #6063: "CacheEngine does not populate Status.Selector, breaking HPA and worker pod discovery" — `status.selector` always empty; also (from the PR body) HPA cannot target CacheRuntime at all because the CRD has no scale subresource |
| Linked issue | #6063, OPEN, created 2026-06-25, still valid (TODO comments present on base) |
| Reported component | `pkg/ddc/cache/engine` (`status.go` `CheckAndUpdateRuntimeStatus`, `master.go`) |
| Patched component | exactly those files + `cacheruntime_types.go`/CRD + component label helper |
| Component match | Yes |
| **Verdict** | **Confirmed** (real problem) |
| Evidence | `results/base-selector.out`: the contract test grafted onto base fails — `Status.Selector = ""` where the head populates the worker selector; base CRD `subresources:` has only `status: {}` (no scale). Both fixed by the PR. |

## Summary of results

### Round 3 (head `99e35af0`, re-verify via `scripts/re-verify.sh`)

| ID (ledger) | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | base leaves `Status.Selector` empty (issue #6063 symptom) | 1 | Confirmed (unchanged; round-2 attestation carries, base and engine code untouched by the delta) | `results/head-9a265eb/base-96a3765-premise.out` |
| F1-selector-populated (AC-selector-populated) | PR populates `Status.Selector` with the exact worker label selector | 1 | FIXED (contract passes on head) | `results/head-99e35af/unit.parsed` |
| F1b-selector-matches-workload (AC-selector-matches-pods) | the selector matches the worker workload's `spec.selector` and pod labels | 1+2 | FIXED (contract passes on head) | `results/head-99e35af/unit.parsed`, `results/head-99e35af/integration.parsed` |
| F-selector-when-worker-disabled | selector populated even when the worker component is disabled (canary, minor open finding) | 1 | STILL-PRESENT (canary green; `status.go` untouched by the delta) | `results/head-99e35af/unit.parsed` |
| F-workerless-scale-docs | `/scale` worker-less behavior payload-dependent (canary, minor open finding) | 2b | STILL-PRESENT (probe re-run on head) | `results/head-99e35af/envtest.parsed` |
| Q-blockless-worker-scale-target | no defaulting of `.spec.worker.replicas` when block absent (canary, open question) | 2b | STILL-PRESENT | `results/head-99e35af/envtest.parsed` |
| C4420756561-currentreplicas-undercount | during a rolling update `status.worker.currentReplicas` (scale `status.replicas`, the HPA's observed metric) undercounts, reporting only current-revision pods | 1 | **FIXED** by `99e35af0e`: new contract test red on previous head `9a265eb3`, green on head `99e35af0` | `results/prev-9a265eb/mixed-revision-contract.out` (red) vs `results/head-99e35af/mixed-revision-contract.out` (green) |

No `Harness-update` outcomes; every test compiled and ran against the new head. The
harness-bites check for the new contract test is the red-on-previous-head run above (the
round-1/2 harness-bites check for the selector tests is unchanged).

CI (run 41297697216, head `99e35af0`, all 22 checks SUCCESS — kind-e2e on all five k8s
versions, backward-compat ×5, unittest, lint, build ×2, staticcheck, CodeQL, DCO, codecov,
SonarCloud): the curvine testcase now **executes the scale-WRITE path on single-node
clusters** — `>>> Scaling worker to 2 replicas (method=scale, verify_pods=false)` →
`cacheruntime.data.fluid.io/curvine-demo scaled` → `>>> Worker scale write to 2 verified on
the CR`, and back down to 1 via patch. This closes `F-scale-write-e2e-skipped` (the write
assertions — CR readback + scale-subresource GET readback — run in every job); the
pod-count checks remain gated on node_count≥2, which is a scheduling constraint, not a
coverage hole for the write path. Excerpts captured in
`results/head-99e35af/ci-curvine-scale-write-v1.33.2.log` and
`ci-curvine-scale-write-v1.22.17.log`.

### Round 2 (head `9a265eb3`, re-verify via `scripts/re-verify.sh`)

| ID (ledger) | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | base leaves `Status.Selector` empty (issue #6063 symptom) | 1 | Confirmed on the **new** base too | `results/head-9a265eb/base-96a3765-premise.out` (red on 96a3765e) |
| F1-selector-populated (AC-selector-populated) | PR populates `Status.Selector` with the exact worker label selector | 1 | FIXED (contract passes on head) | `results/reverify/unit.parsed` |
| F1b-selector-matches-workload (AC-selector-matches-pods) | the selector string matches the worker workload's `spec.selector` and pod labels (HPA pod discovery works) | 1+2 | FIXED (contract passes on head) | `results/reverify/unit.parsed` |
| F-selector-when-worker-disabled | `Status.Selector` is populated even when the worker component is disabled (canary, minor open finding) | 1 | STILL-PRESENT (canary green = behavior persists, code unchanged) | `results/reverify/unit.parsed` |
| F-workerless-scale-docs | `/scale` for worker-less payloads is payload-dependent: block-absent → GET fails; block-present/disabled → GET/PUT succeed against a phantom target (canary, minor open finding) | 2b | STILL-PRESENT (probe re-run on head, byte-identical outcome) | `results/head-9a265eb/envtest-probe.out` |
| Q-blockless-worker-scale-target | no defaulting/backfill materializes `.spec.worker.replicas` when the worker block is absent; scale PUT creates the path (canary, open question) | 2b | STILL-PRESENT (T1/T3/T4 of the probe) | `results/head-9a265eb/envtest-probe.out` |

No `Harness-update` outcomes: every test compiled and ran against the new head unchanged.

### Round 1 (head `3f39501e`) — carried forward

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | base leaves `Status.Selector` empty (issue #6063 symptom) | 1 | Confirmed (fails on base, passes on head) | `results/base-selector.out`, `results/head-selector.out` |
| F1 | PR populates `Status.Selector` with the exact worker label selector | 1 | Confirmed (contract passes on head; fails on base) | `results/head-selector.out` |
| F1b | the selector string matches the worker workload's `spec.selector` and pod labels (HPA pod discovery works) | 1+2 | Confirmed | `results/head-selector.out` (`TestHarnessSelectorMatchesWorkerWorkload`) |
| F2 | `Status.Selector` is populated even when the worker component is disabled (canary) | 1 | Confirmed (documented behavior) | `results/head-selector.out` (`TestHarnessSelectorSetEvenWhenWorkerDisabled`) |

Additional evidence used by the review but produced by CI (not this harness):
- **Round 2 (run 41179758839, head `9a265eb3`, all checks green):** kind e2e (curvine)
  **SUCCEEDED on all five k8s versions** (v1.22.17–v1.33.2) for the PR's own testcase:
  `status.selector` set, matching worker pods, and the scale-subresource GET reporting
  `spec.replicas=1, status.replicas=1, selector=<worker selector>` — logs captured in
  `results/head-9a265eb/v1.33.2.log` and `v1.22.17.log`. The `jindoruntime secret e2e`
  warmup flake that made round 1's kind-e2e red is gone (the PR merged master's runner pin,
  #6225); the jindo testcase now succeeds. All other checks green (unittest, lint, build ×2,
  staticcheck, CodeQL, backward-compat on all five versions, DCO, codecov, SonarCloud).
- Round 1 (run 40716621753): curvine testcase passed on all five versions while the overall
  kind-e2e workflow was red on the `jindoruntime secret e2e` warmup — pre-existing flake
  (same failure on unrelated dependabot run 40644839213), now resolved by the runner pin.
- Both rounds: `>>> Skipping scale test: single-node cluster (1 node)` appears in every
  kind-e2e job, so the scale-**write** path (the HPA write path) remains unexercised in CI —
  finding `F-scale-write-e2e-skipped` (minor, open).

## Per-finding detail

### P0 / F1 — `Status.Selector` population
Mechanism: on base, `CheckAndUpdateRuntimeStatus` had a TODO and never touched
`Status.Selector`; the head derives it from the same labels the component manager stamps on
the worker AdvancedStatefulSet (`component.GetComponentMatchLabels`), so no extra API read.
Command: `go test ./pkg/ddc/cache/engine/ -run TestHarnessStatusSelectorPopulated -v`
Observed (base): `Status.Selector = ""` — FAIL. Expected (and observed on head):
`cacheruntime.fluid.io/component-name=curvine-demo-worker,cacheruntime.fluid.io/name=curvine-demo`.
This same red-on-base run is the harness-bites check for F1.

### F1b — selector selects the right pods
Mechanism: `getWorkerSelectors()` duplicates the label construction of the workload creation
path; if the two drift, HPA would silently match nothing (or the wrong component). The
contract test creates the worker workload through the real `Reconciler` and asserts the
status selector matches both `asts.Spec.Selector.MatchLabels` and the pod template labels.
Observed: PASS on head (selector matches both).

### F2 — worker-disabled runtime still gets a selector (canary)
Mechanism: `CheckAndUpdateRuntimeStatus` sets `Status.Selector` unconditionally
(`pkg/ddc/cache/engine/status.go:147-151`), before/without consulting
`value.Worker.Enabled`. For a runtime whose topology has no worker, the selector matches no
pods, yet (with the CRD change) `GET .../scale` succeeds and reports it. Minor review
finding; the canary records the current behavior and flips if it is changed.

### C4420756561 — mixed-revision undercount (round 3, fixed by the PR)
Mechanism: `AdvancedStatefulSetManager.constructStatus` mapped
`RuntimeComponentStatus.CurrentReplicas` from `asts.Status.CurrentReplicas`, which counts
only pods on the **current** revision; mid-rolling-update the fleet is split across
revisions, so `.status.worker.currentReplicas` — the scale subresource's `statusReplicasPath`
and therefore the HPA's observed-replicas metric — undercounts. `99e35af0e` switches it to
`asts.Status.Replicas` (all observed pods) and derives `UnavailableReplicas` from the same
base, matching `pkg/ctrl/worker.go:128` (`CurrentWorkerNumberScheduled =
workers.Status.Replicas`) for the pre-existing runtimes; `pkg/ddc/thin/referencedataset/sync.go`
maps the same field to `CurrentWorkerNumberScheduled`, so the fix also aligns that surface.
Consumers checked: nothing gates logic on this field (status reporting only).
Contract test: `go test ./pkg/ddc/cache/component/ -run TestHarnessConstructStatus -v` —
red on `9a265eb3` (`CurrentReplicas = 2, want 3`), green on `99e35af0`.

## Live run notes
Not run in this debate phase (no cluster layer). CI kind e2e covers the API-server-side
contract (see above). If re-run manually: apply the CRD from `config/crd/bases`, create the
curvine demo, then `kubectl get --raw .../cacheruntimes/<name>/scale` and
`kubectl scale cacheruntime/<name>`.

## Proposed fixes (NOT applied to production here)
- **F2** (optional, minor): only publish the selector when the worker component is enabled
  (`value.Worker != nil && value.Worker.Enabled`), so a worker-less runtime reports an empty
  selector and `statusReplicasPath`/`labelSelectorPath` do not advertise a
  worker that does not exist.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cacheruntime-hpa-scale-claude` (production code untouched:
the diff vs the PR head is only the harness test files, the `zz-critique-probe/` package and
this directory), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cacheruntime-hpa-scale-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- pkg/ddc/cache/engine/status_selector_harness_test.go \
     pkg/ddc/cache/engine/selector_workload_consistency_harness_test.go \
     pkg/ddc/cache/component/asts_status_mixed_revision_harness_test.go \
     zz-critique-probe \
     docs/verification/cacheruntime-hpa-scale-claude
   ```
2. Prereqs: Layer 1/2 = Go toolchain only (fake client, no cluster). Layer 2b (envtest) =
   Go toolchain plus kubebuilder envtest assets
   (`$HOME/.local/share/kubebuilder-envtest/k8s/1.31.0-linux-amd64`; the CRD path in the
   probe is relative, `../config/crd/bases`). Layer 3 = a kind
   cluster with the fluid CRDs applied.
3. Re-run: `bash docs/verification/cacheruntime-hpa-scale-claude/scripts/re-verify.sh`
   (no argument — it resolves the current PR head from the manifest's `pr` URL; default
   layers are unit,integration,envtest).
4. Read results via the polarity table: contract tests (`TestHarnessStatusSelectorPopulated`,
   `TestHarnessSelectorMatchesWorkerWorkload`) should be GREEN; the bug-canaries
   (`TestHarnessSelectorSetEvenWhenWorkerDisabled`, `TestCritiqueProbe`) will be RED and must
   be inverted if their behavior is deliberately changed.
5. Harness-bites: re-graft `status_selector_harness_test.go` onto the merge-base
   (`fe074af38434` round 1; `96a3765eaae0` since round 2) and confirm it goes red (empty
   selector) — recorded in `results/base-selector.out` and
   `results/head-9a265eb/base-96a3765-premise.out`.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cacheruntime-hpa-scale-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6223 produced findings P0/F1/F1b/F2; a
harness reproduced them. The PR is now fixed at <ref>. Read
docs/verification/cacheruntime-hpa-scale-claude/README.md ("Continuing after the fix") and
follow it: graft the harness onto the fixed code, re-run all layers, mind the polarity
table (invert the F2 bug-canary if it flips), run the harness-bites check, and report an
observed-vs-expected table. Clean up scoped test resources; do not run cluster-wide
destructive actions.
```
