# refresh-capacity-labels-claude — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6244 (base `ff99142f`).

**Round 2 (head `7de88000e`): F1 is FIXED.** The PR added the proposed guard to
`syncCapacityInfoOnNode` (mirroring `addScheduleInfoToNode`) plus two entry-point tests
through `SyncScheduleInfoToCacheNodes` (repair + steady-state zero-patch, and
exclude-selector untouched). All 32 specs in the package pass on head, including the C3
contract that failed on the previous head `e0cba66e`. See
`results/reverify-head2/` for this round's raw output and the table below.

Reviewer: Claude (Reviewer A, PR-debate pipeline). **Additive only — production code is
untouched on this branch**; the harness is one new test file next to the code it exercises.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (fake client) | `SyncScheduleInfoToCacheNodes` entry point: capacity-label refresh, tier removal, exclude-selector handling, steady-state write behavior | `go test ./pkg/utils/dataset/lifecycle/ -run TestLifecycle -count=1 -ginkgo.no-color -ginkgo.json-report=/tmp/report.json` |
| 2. Integration | not needed — the claims are pure controller-client logic fully exercised by the fake client (label patch semantics are the real `client.Patch` path of the fake client) | — |
| 3. Live | skipped this round (pipeline policy: unit + integration only); see liveNote in the manifest | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when fixed.
> There are no bug-canaries in this harness.

## Problem premise (P0)

Run against the **base** branch (`ff99142f`) without the patch, because the question is whether
the problem exists today.

| | |
|---|---|
| Claimed symptom | "an existing CacheRuntime still shows fluid.io/s-h-cache-t-\*: 0B with no mem/disk labels" (issue #6204), i.e. capacity labels written from an outdated tiered store are never corrected on already-labelled nodes |
| Linked issue | #6204, OPEN, created 2026-10-09, labeled `bug`; the follow-up to #6181 which itself documents "the capacity labels are written once ... A CacheRuntime that existed before this change therefore keeps its 0B label until it is recreated" |
| Reported component | cache runtime node capacity labels — written via `lifecycle.SyncScheduleInfoToCacheNodes` → `labelNodeWithCapacityInfo` |
| Patched component | `pkg/utils/dataset/lifecycle/node.go` — the generic sync path used by **all** engines including `pkg/ddc/cache/engine/sync.go` (CacheRuntime) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | On base, spec "should refresh outdated capacity labels on a node that already has the runtime label" FAILS: node labelled `runtime=true`, `total=0B`, worker pod on it → after `SyncScheduleInfoToCacheNodes` the labels are still `total=0B` with no mem/disk labels. `results/base.out`, `results/base-report.json` |

## Summary of results

Code under review = PR head `e0cba66e`. Base = `ff99142f` (premise + differential control).

| ID | Claim | base | head | Verdict | Evidence |
|----|-------|------|------|---------|----------|
| P0 | stale capacity labels are never refreshed on already-labelled nodes | FAIL (bug present) | — | **Confirmed (premise)** | results/base.out |
| PR6244-fix-regression | `SyncScheduleInfoToCacheNodes` refreshes outdated labels (C1), drops labels of removed tiers (C2), and does not write on steady state (C4) | FAIL | PASS | **Fix works as claimed** | results/head.out; C4 shows 0 Patch calls when labels already match (counting-client), C4b proves the counter bites |
| F1-exclude-selector-bypass | nodes matching `nodeExcludeSelector` (FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR) must not be written to by the schedule-info sync | PASS (already-labelled nodes never touched) | FAIL (labels patched) | **Confirmed, introduced by this PR** | results/head.out — the node ends with m=1GiB/d=2GiB/t=3GiB although it matches the exclude selector |

### Round 2 re-verification (head `7de88000e`, delta `e0cba66e..7de88000e`)

Delta reviewed: commit "fix(lifecycle): skip excluded nodes when refreshing capacity labels"
adds the `nodeExcludeSelector` guard to `syncCapacityInfoOnNode` (exact mirror of
`addScheduleInfoToNode`: same position after the node `Get`, same predicate, same V(1) log) and
two new entry-point specs in `node_test.go` driven through `SyncScheduleInfoToCacheNodes` with
a patch-counting interceptor client.

| ID | Claim | base | previous head (`e0cba66e`) | head (`7de88000e`) | Verdict | Evidence |
|----|-------|------|---------------------------|--------------------|---------|----------|
| F1-exclude-selector-bypass | excluded nodes untouched by the refresh | PASS | FAIL | **PASS** | **Fixed** | results/reverify-head2/ginkgo-report.json — "should leave nodes matching nodeExcludeSelector untouched" green; the PR's own "should leave nodes matching the exclude selector untouched" also green |
| PR6244-fix-regression | refresh (C1) / tier removal (C2) / steady-state no-write (C4) | FAIL | PASS | **PASS** | **Still fixed (no regression)** | results/reverify-head2/ginkgo-report.json — all three green; C4b bites-check green |
| F2-missing-entry-point-test | PR's own tests cover the wiring through the entry point | — | absent | **present & green** | **Fixed** | node_test.go "Test SyncScheduleInfoToCacheNodes refreshing capacity labels": repair + second-sync zero-patch, plus the exclude-selector entry test; both pass on head |

Full package suite on head: **32/32 specs pass** (30 from round 1 + the PR's two new
entry-point specs), `gofmt`/`go vet` clean, `results/reverify-head2/unit.out`.

Note on the guard's blast radius: the early `return nil` for excluded nodes skips only the
capacity-label write; no cleanup, retry, or status update is skipped (the caller
`syncCapacityInfoOnNodes` only logs errors, and the removal path is unchanged). Excluded
nodes keep their runtime label and stale capacity labels — the same opt-out contract
`addScheduleInfoToNode` already gives the operator.

## Per-finding detail

### F1 — `syncCapacityInfoOnNode` bypasses `nodeExcludeSelector`

- **Mechanism.** `addScheduleInfoToNode` (pkg/utils/dataset/lifecycle/node.go) skips nodes matching
  `nodeExcludeSelector` (feature #5285, documented in `charts/fluid/fluid/values.yaml`:
  "exclude nodes that should be skipped when syncing schedule info", canonical example
  `type=virtual-kubelet`). The new capacity-refresh loop runs on the *kept* nodes
  (`desired ∩ actual`) and never consults the selector, so a node that already carries the
  runtime label and matches the exclude selector gets its `fluid.io/s-h-*` labels patched on
  every reconcile while they mismatch.
- **Trigger conditions.** Operator sets FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR (e.g.
  `type=virtual-kubelet`) on a cluster where excluded nodes already carry a runtime label
  (labelled before the selector was configured), with a worker pod still scheduled there and a
  tiered store that doesn't match the on-node labels.
- **Observed vs expected.** Expected: excluded node untouched (`total` stays `0B`, no mem/disk
  labels). Observed on head: `m=1GiB, d=2GiB, t=3GiB` written. On base the same spec passes
  (already-labelled nodes were never touched at all) — a clean differential showing the PR
  introduced the bypass.
- **Proposed fix (not applied here).** In `syncCapacityInfoOnNode`, after the `Get`, mirror
  `addScheduleInfoToNode`: `if nodeExcludeSelector != nil && nodeExcludeSelector.Matches(labels.Set(node.Labels)) { return nil }`.
  (Note for the debate: `removeScheduleInfoFromNode` also lacks this check — a pre-existing,
  separate gap worth a tracking issue, not a reason to keep the new gap.)

### PR6244-fix-regression — the fix itself

- C1 (wiring through the public entry point): stale node `runtime=true, t=0B` + worker pod →
  `m=1GiB, d=2GiB, t=3GiB` after one `SyncScheduleInfoToCacheNodes` call. This is exactly the
  upgrade scenario in the PR's "how to verify it" section, and is the test the PR itself is
  missing (its own tests call the unexported helper directly).
- C2: node has `m` label for a tier the runtime no longer has → `m` deleted, `t` recomputed.
- C4: when labels already match, **0 Patch calls** are issued (counting client) — the PR's
  "on a steady cluster this is just a cached Get per node" claim holds.
- All 25 pre-existing specs in the package still pass on head (no regression).

## Proposed fixes (NOT applied to production here)

- **F1**: see above — consult `nodeExcludeSelector` in `syncCapacityInfoOnNode`.

## Continuing after the fix

The harness is on branch `verify/refresh-capacity-labels-claude` (production code untouched),
so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/refresh-capacity-labels-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/refresh-capacity-labels-claude pkg/utils/dataset/lifecycle/node_capacity_verify_test.go
   ```
2. Prereqs: Go toolchain (go 1.27 used here); `go test` resolves modules from the normal module cache.
3. Re-run:
   ```bash
   bash docs/verification/refresh-capacity-labels-claude/scripts/re-verify.sh
   ```
   (no argument: it fetches the current PR head from `manifest.pr` itself), or
   `bash .../re-verify.sh <fixed-ref>`.
4. Polarity: all specs are contract tests. F1 is Fixed exactly when
   "should leave nodes matching nodeExcludeSelector untouched" goes green; PR6244-fix-regression
   must stay green (a flip there is a regression of the PR's own fix).
5. Harness-bites: C4b ("should observe a patch when labels are outdated") proves the
   patch-counting client actually counts — it fails on any code where the refresh stops happening.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/refresh-capacity-labels-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6244 confirmed finding F1
(syncCapacityInfoOnNode bypasses nodeExcludeSelector) via a contract test that fails on the
PR head. The PR is now updated at <ref>. Read
docs/verification/refresh-capacity-labels-claude/README.md ("Continuing after the fix") and
follow it: run scripts/re-verify.sh, confirm F1's spec went green while the
PR6244-fix-regression specs stayed green, and report an observed-vs-expected table.
```
