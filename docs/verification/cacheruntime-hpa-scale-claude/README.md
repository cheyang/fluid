# cacheruntime-hpa-scale — bug verification (Reviewer A / Claude)

Reproducible evidence for the review of PR #6223
("feat(cache): support HPA on CacheRuntime via scale subresource and status selector").

Layers run against the **code under review** (`3f39501e7b99c1e4fd925f332ee7e34faec45171`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `CacheEngine.getWorkerSelectors`, `CheckAndUpdateRuntimeStatus` selector population, component label derivation | `go test ./pkg/ddc/cache/engine/ -run 'TestHarness' -v` |
| 2. Integration (fake-client) | the selector string vs. the real worker workload creation path (`component.NewComponentHelper(...).Reconciler`), and the worker-disabled lifecycle | same command (ginkgo specs of the PR itself: `go test ./pkg/ddc/cache/engine/ ./pkg/ddc/cache/component/`) |
| 3. Live | real API server scale subresource — covered by the repo's kind e2e in CI (curvine case), **not** run here (debate setup skips the live layer) | see "Live run notes" |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP TO RED when fixed.

## Problem premise (P0)

Run against the **base** branch (`fe074af38434fc4b618686de6459a9173570b48b`) without the patch.

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

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | base leaves `Status.Selector` empty (issue #6063 symptom) | 1 | Confirmed (fails on base, passes on head) | `results/base-selector.out`, `results/head-selector.out` |
| F1 | PR populates `Status.Selector` with the exact worker label selector | 1 | Confirmed (contract passes on head; fails on base) | `results/head-selector.out` |
| F1b | the selector string matches the worker workload's `spec.selector` and pod labels (HPA pod discovery works) | 1+2 | Confirmed | `results/head-selector.out` (`TestHarnessSelectorMatchesWorkerWorkload`) |
| F2 | `Status.Selector` is populated even when the worker component is disabled (canary) | 1 | Confirmed (documented behavior) | `results/head-selector.out` (`TestHarnessSelectorSetEvenWhenWorkerDisabled`) |

Additional evidence used by the review but produced by CI (not this harness):
- kind e2e (curvine) in CI run 40716621753 **passed on all 5 k8s versions** for the PR's own
  testcase: `status.selector` set and matching worker pods, scale subresource GET reporting
  `spec.replicas` / `status.replicas` / string `selector`, scale-up through the scale
  subresource and scale-down via spec patch, pod counts verified both ways.
- The kind-e2e workflow overall is red on this PR, but the same `jindoruntime secret e2e`
  warmup failure occurs on an unrelated dependabot branch (run 40644839213) — pre-existing
  flake, not this PR.

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
the diff vs the PR head is only the harness test files + this directory), so it grafts onto
whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cacheruntime-hpa-scale-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- pkg/ddc/cache/engine/status_selector_harness_test.go \
     pkg/ddc/cache/engine/selector_workload_consistency_harness_test.go \
     docs/verification/cacheruntime-hpa-scale-claude
   ```
2. Prereqs: Layer 1/2 = Go toolchain only (fake client, no cluster). Layer 3 = a kind
   cluster with the fluid CRDs applied.
3. Re-run: `bash docs/verification/cacheruntime-hpa-scale-claude/scripts/re-verify.sh`
   (no argument — it resolves the current PR head from the manifest's `pr` URL).
4. Read results via the polarity table: contract tests (`TestHarnessStatusSelectorPopulated`,
   `TestHarnessSelectorMatchesWorkerWorkload`) should be GREEN; the bug-canary
   (`TestHarnessSelectorSetEvenWhenWorkerDisabled`) will be RED and must be inverted if its
   behavior is deliberately changed.
5. Harness-bites: re-graft `status_selector_harness_test.go` onto the merge-base
   `fe074af38434` and confirm it goes red (empty selector) — already recorded in
   `results/base-selector.out`.

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
