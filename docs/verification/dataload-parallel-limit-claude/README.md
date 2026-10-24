# dataload-parallel-limit-claude — bug verification

Reproducible evidence for the findings raised by Reviewer A (Claude) while reviewing
https://github.com/fluid-cloudnative/fluid/pull/5754 (head `20b7e5ed3c5bfc9844061eed496e75b8f25e23cb`, base `82e490e47b7116aaca6e3b7d9abc772111260e4d`).

Layers run against the **code under review** (`20b7e5ed`, branch `verify/dataload-parallel-limit-claude`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `dataMigrateOperation.GetParallelTaskNumber` + `Dataset.CanStartDataOperation` (limit semantics) | `go test ./pkg/controllers/v1alpha1/datamigrate/ -run 'TestVerifyClaude' -count=1 -v` |
| 2. Integration (fake client) | full `updateDatasetDataOperation` / `removeDatasetDataOperation` round trip: `utils.GetDataset` + status update + `retry.RetryOnConflict` | `go test ./pkg/ddc/base/ -run 'TestVerifyClaude' -count=1 -v` |
| 3. Live | real controller + real cluster | not run this round (debate setup skips the live layer) |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

Answered before the findings, and run against the **base** branch without the patch, because the
question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom | PR body: "Previously, `SetDataOperationInProgress` would always append new operation names to a comma-separated string, effectively allowing multiple tasks to run on the same dataset simultaneously." Linked issue #1138: "what would happen if multiple same DataLoad task start running?" |
| Linked issue | #1138, OPEN since 2021-11-19 — a *question-style* issue; see "Issue linkage" below |
| Reported component | Dataset `OperationRef` lock used by data operation controllers |
| Patched component | `api/v1alpha1/dataset_types.go` (`CanStartDataOperation`) + `pkg/ddc/base/operation_lock.go` (`updateDatasetDataOperation`) |
| Component match | Yes (for the "concurrent same DataLoad" half of the issue; the issue's primary cold-read question is NOT covered) |
| **Verdict** | **Confirmed** — the unlimited-append behavior exists on base |
| Evidence | On base, `SetDataOperationInProgress("DataLoad","load-2")` appends unconditionally → `"load-1,load-2"`, and no limit check exists anywhere on the path (`CanStartDataOperation` absent on base). `results/base-premise.out`; re-run via `scripts/base-premise.sh` |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| V1 | At limit 1, a second operation is refused with a "reached the maximum number of parallel" error and does not register | 2 | Confirmed (contract test green on head; red when the gate is removed) | `results/integration-harness.out`, `results/harness-bites-gate-removed.out` |
| V2 | Re-entrant registration of the same operation is allowed (cron/OnEvent re-execution must not self-block) | 2 | Confirmed | `results/integration-harness.out` |
| V3 | After `removeDatasetDataOperation`, the slot frees and the next operation can register | 2 | Confirmed | `results/integration-harness.out` |
| V4 | With limit > 1, exactly that many operations register; limit+1 refused | 2 | Confirmed | `results/integration-harness.out` |
| F1 | The dataset-level concurrency limit for DataMigrate is wired to `DataMigrate.Spec.Parallelism` (job-internal worker count), not to a fixed 1 — so `Parallelism: 4` admits 4 concurrent DataMigrate CRs per dataset | 1 | Confirmed (canary: current behavior documented; flips red if the semantics are decoupled) | `results/unit-harness.out`, `results/harness-bites-canary-flip.out` |

Harness-bites checks (Step 4 of the skill): with the gate removed from `operation_lock.go`,
V1/V4 go red; with `dataMigrateOperation.GetParallelTaskNumber` changed to `return 1`
(the proposed F1 fix), the F1 canary flips red. Production code was restored after each
check — the committed diff contains only the harness.

## Per-finding detail

### V1/V2/V3/V4 — the PR's core contract (pkg/ddc/base)
`updateDatasetDataOperation` (renamed/extracted from `SetDataOperationInTargetDataset` by this
PR) now calls `dataset.CanStartDataOperation(type, maxParallel, name)` inside the
`retry.RetryOnConflict` closure, before the status update. Because the read-modify-write
sits inside the conflict-retry loop, two racing reconcilers both passing the check resolve
via the optimistic-concurrency conflict on `Status().Update` — the loser re-reads and is
refused. The fake-client layer exercises `utils.GetDataset` → gate → `SetDataOperationInProgress`
→ status update → (for V3) release and re-acquire.

`reconcilePending` (pkg/ddc/base/operation.go:123-127) turns the refusal into a 20s requeue
with the phase left Pending, matching the PR's described behavior. NOTE: no test (mine or
the PR's) exercises that controller-level requeue path — see finding F2 in
findings-claude.md (missing integration/e2e coverage of the Pending→requeue→proceed loop).

### F1 — DataMigrate limit semantics (canary)
`pkg/controllers/v1alpha1/datamigrate/implement.go:183` returns `r.dataMigrate.Spec.Parallelism`
for `GetParallelTaskNumber()`. Per `api/v1alpha1/datamigrate_types.go:92`, `Parallelism` is the
*job-internal* worker count ("If the value is greater than 1, the job acts as a launcher, and
users should define the WorkerSpec") — it configures ssh workers *within one migration job*,
and the same getter is used elsewhere (pkg/ddc/base/operation.go:258,348) to scale the
parallel-worker StatefulSet. PR #5754 now also uses it as the number of concurrent DataMigrate
CRs allowed on one dataset. The PR body claims the limit "is 1 for DataLoad/DataMigrate" —
true only when `Parallelism` is left at its CRD default of 1. With `Parallelism: 4`, four
concurrent DataMigrate operations may hold the dataset lock — exactly the concurrent-same-operation
scenario the lock exists to prevent.

## Live run notes
Not run this round. Manual L3 recipe (if someone picks it up): create DataLoad A on dataset D,
while A is Executing create DataLoad B on D; B should stay Pending with 20s requeue and the
controller logs "reached the maximum number of parallel"; delete/complete A, B proceeds.

## Proposed fixes (NOT applied to production here)
- **F1**: either (a) decouple the dataset-level limit from job-internal parallelism — e.g. have
  `dataMigrateOperation.GetParallelTaskNumber()` return 1 for locking purposes and keep
  `Spec.Parallelism` for worker count, or (b) keep the wiring but document it explicitly in the
  PR/CRD docs and fix the PR description. Option (a) matches the PR's stated intent.
- **F2**: add a controller-level (envtest) or e2e test for the blocked-second-DataLoad
  Pending→requeue→proceed loop.

## Issue linkage (meta finding, no test)
Issue #1138's primary question — "what would happen if multiple ml training task read the same
fluid dataset without pre-heating?" — is about concurrent *cold reads*, which this PR does not
touch. Only the trailing "similar question" (concurrent same DataLoad) is addressed. `fixes #1138`
will therefore auto-close a still-open question issue whose main content is unanswered. The
author should drop the closing keyword (use `refs #1138`) or confirm scope.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/dataload-parallel-limit-claude` of the reviewer fork
(`https://github.com/cheyang/fluid.git`), production code untouched, so it grafts onto
whatever the fixed code is.

1. Re-run everything with one command from a checkout of this branch:
   ```bash
   bash docs/verification/dataload-parallel-limit-claude/scripts/re-verify.sh
   ```
   (no ref: it fetches the current PR head from `manifest.pr`)
2. Prereqs: Go toolchain only (layers 1–2 use the fake client; no cluster, no KUBECONFIG).
3. Read results via the polarity table: V1–V4 are contract tests — they should stay GREEN.
   F1 is a **canary**: if the maintainer fixes the DataMigrate limit semantics it goes RED
   ("FIXED(flipped)") — at that point invert it or promote the new behavior to a contract test.
4. P0 re-check: `bash docs/verification/dataload-parallel-limit-claude/scripts/base-premise.sh`
   (against the frozen base `82e490e4`).
5. Harness-bites: `results/harness-bites-*.out` show both bite checks; re-do them only if the
   tests are edited.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/dataload-parallel-limit-claude (fork
https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/5754 produced findings V1-V4 (contract,
core PR behavior) and F1 (canary: DataMigrate dataset-level limit == Spec.Parallelism).
Read docs/verification/dataload-parallel-limit-claude/README.md ("Continuing after the
fix") and follow it: run scripts/re-verify.sh (it grafts the harness onto the current PR
head), mind the polarity table (invert the F1 canary if it flips), optionally re-run
scripts/base-premise.sh, and report an observed-vs-expected table. Clean up scoped test
resources; do not run cluster-wide destructive actions.
```
