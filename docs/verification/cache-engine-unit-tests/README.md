# cache-engine-unit-tests — test verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6264 (Reviewer A, Claude).

Layers run against the **code under review** (`ef7ecf5f06fdc67ef950bfe2737e1e509be2bb5b`, PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (gotest) | reviewer harness contract tests for the DataLoad value-file happy path | `go test ./pkg/ddc/cache/engine/ -run 'TestDataOperationValueFileHappyPath\|TestDataLoadValueFileNotSupportedWhenSpecMissing' -v` |
| 2. Ginkgo (PR's own new specs) | the 23 specs the PR adds, in isolation | `go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus="CacheEngine data operations\|validateRuntimeClassTopology" -v` |
| 3. Live | not applicable — tests-only PR, no runtime behavior change | — |

> Test polarity: all tests in this harness are **contract** tests. They assert intended
> behavior: PASS on correct code, FAIL when the DataLoad value-file generation regresses.
> There are no bug-canaries.

## Problem premise (P0)

Tests-only PR (no production code changes), so the full premise check is carved out per the
review rubric. Recorded anyway for the record, run against the **base** branch
(`dcb76167b0143a239addc6bed83ed50b45267631`):

| | |
|---|---|
| Claimed symptom | PR body: "Adds unit tests for the CacheRuntime engine functions that had none, as listed in #6263"; issue #6263: "a regression in either one breaks DataLoad and friends without any test going red" |
| Linked issue | #6263 "[FEATURES] Add unit tests for untested CacheRuntime engine functions", OPEN, created 2026-11-17 (one day before the PR) |
| Reported component | `pkg/ddc/cache/engine` — `CheckRuntimeReady`, `GetDataOperationValueFile`, `findDataOperationSpec`, `validateRuntimeClassTopology`, `Shutdown` |
| Patched component | same package, test files only (`data_operation_test.go`, `validate_test.go`); `Shutdown` explicitly deferred to a follow-up, matching the issue's own suggestion |
| Component match | Yes |
| **Verdict** | **Confirmed** with one nuance (see T4/notes): `git grep` on the merge base finds zero test references to `CheckRuntimeReady`, `GetDataOperationValueFile`, `findDataOperationSpec`. `validateRuntimeClassTopology` had *indirect* negative coverage on base (transform_test.go asserts its "at least one component should be defined" error 4×, cm_test.go 1×), but no direct unit tests and no positive (accepting) cases. |
| Evidence | results/l1-pr-new-specs.txt (23/23 pass on head); results/l1-plain-gotest-base.txt (base suite state) |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | premise: the four functions had no direct unit tests; new tests pass | 1/2 | Confirmed | results/l1-pr-new-specs.txt, results/l1-full-suite-make-style.txt |
| T1 | the PR's routing tests only assert the early type-mismatch error, so a happy-path regression in `generateDataLoadValueFile` leaves all 23 new specs green | 1 | Confirmed (mutation experiment) | results/mutation-t1.txt: PR specs stay 23/23 green under a happy-path mutation, harness tests go red |
| T2 | `CheckRuntimeReady` table lacks the worker-`RuntimePhaseNone` rows (worker never started) | — | Not-reproduced as a defect (nit: coverage nicety) | code reading of `data_operation.go:41-57` |
| T3 | `fakeOperation.GetTargetDataset()` returns `(nil, nil)` — nil dataset with nil error | — | Confirmed (style nit, never exercised) | code reading of `data_operation_test.go` |
| PRE1 | 12 engine-package test failures seen under plain `go test` are pre-existing, not introduced by this PR | 1 | Confirmed (identical on base and head) | results/l1-plain-gotest-base.txt vs results/l1-plain-gotest-head.txt |

## Per-finding detail

### T1 — routing tests do not guard the value-file happy path (minor, missingTests)

`GetDataOperationValueFile` tests in the PR dispatch a *deliberately mismatched* object
(e.g. a `DataProcess` under `DataLoadType`) and assert each generator's early type-assertion
error. That proves dispatch, but not generation. Issue #6263's motivation is "a regression in
either one breaks DataLoad and friends without any test going red" — the value-file
generation itself is the part that breaks DataLoad.

Mechanism proof (`scripts/mutation-check-t1.sh`): mutate `generateDataLoadValueFile` to
return `"mutated-bogus.yaml", nil` right after the type assertion. Then:

- the PR's 23 new specs: **still 23/23 green** (results/mutation-t1.txt, section a);
- the one pre-existing test that touches this function (`TestGenerateDataLoadValueFileWithNilTopology`
  in cm_test.go, a nil-topology negative case) catches *this particular* mutation, but a
  subtler happy-path regression — e.g. dropping the `DataOperationSpecs` image or command
  from the generated file — would not change any currently-asserted outcome;
- the reviewer harness tests go red (results/mutation-t1.txt, section b).

Proposed remediation is the two harness contract tests in
`pkg/ddc/cache/engine/data_operation_harness_test.go`: a real `DataLoad` object routed
through `GetDataOperationValueFile` with a fully-populated fixture (runtime, runtime class
with `DataOperationSpecs`, dataset), asserting the generated file carries the
DataOperationSpec image, command, target path and runtime-config volume name. That test
file is graftable onto the PR as-is if the author wants to adopt it.

### T2 — missing worker-None rows (nit)

`data_operation.go:44-57`: the master-worker branch requires
`Worker.Phase == Ready || PartialReady`. The PR's table covers NotReady but never
`RuntimePhaseNone` (worker never started), for either architecture. Behavior is trivially
correct by equality; purely a coverage nicety. No harness test added.

### T3 — fakeOperation.GetTargetDataset returns (nil, nil) (nit)

`data_operation_test.go`: `GetTargetDataset() (*datav1alpha1.Dataset, error)` returns
`nil, nil`. Never called on the exercised paths, but a future routing test that grows into
dataset lookup would nil-deref confusingly. A `errors.New("not implemented")` or a comment
would make the stub's contract explicit.

### PRE1 — pre-existing plain-`go test` failures (not a PR finding)

12 specs in `ufs_test.go` / `sync_test.go` fail under plain `go test` because their
gomonkey patches are defeated by inlining. Identical 12 on the merge base and on the PR
head; the project's canonical invocation (`FLUID_UNIT_TEST=true go test
-gcflags="all=-l"`, per the Makefile) is fully green: 418 passed / 0 failed / 16 skipped
(results/l1-full-suite-make-style.txt). Recorded so future rounds don't misattribute these
to this PR.

## Live run notes

Not applicable — the PR changes no production code, so there is no runtime behavior to
observe on a cluster.

## Proposed fixes (NOT applied to production here)

- **T1**: adopt `pkg/ddc/cache/engine/data_operation_harness_test.go` (or an equivalent
  happy-path test) into the PR so the DataLoad value-file generation is actually guarded.
- **T2**: add `Entry(..., false, RuntimePhaseReady, RuntimePhaseNone, false)` and
  `Entry(..., true, RuntimePhaseNone, RuntimePhaseNone, false)` rows.
- **T3**: make the fake's `GetTargetDataset` return an explicit error instead of `(nil, nil)`.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cache-engine-unit-tests-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-engine-unit-tests-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cache-engine-unit-tests pkg/ddc/cache/engine/data_operation_harness_test.go
   ```
2. Prereqs: Go toolchain only (Go 1.27 used for this round). No cluster, no envtest.
3. Re-run:
   ```bash
   go test ./pkg/ddc/cache/engine/ -run 'TestDataOperationValueFileHappyPath|TestDataLoadValueFileNotSupportedWhenSpecMissing' -v
   go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus="CacheEngine data operations|validateRuntimeClassTopology" -v
   bash docs/verification/cache-engine-unit-tests/scripts/mutation-check-t1.sh   # only while the T1 gap is open
   ```
4. Read results via the polarity table: everything here is a **contract** test — green is
   the good state; red means a regression. No canaries to invert.
5. Harness-bites: `scripts/mutation-check-t1.sh` is the standing bites-check — it must show
   the harness red under the mutation. Once the author adopts an equivalent happy-path
   test, T1 is closed and the mutation-check becomes a plain regression guard.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/cache-engine-unit-tests-claude of
https://github.com/cheyang/fluid.git. Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6264 (tests-only PR for
pkg/ddc/cache/engine) produced findings T1-T3 plus premise P0; the harness proved T1
(routing tests don't guard the DataLoad value-file happy path) via a mutation experiment.
Read docs/verification/cache-engine-unit-tests/README.md ("Continuing after the fix") and
follow it: graft the harness onto the current PR head (resolve it from the manifest's pr
field), re-run both layers, re-run scripts/mutation-check-t1.sh while the gap is open, and
report an observed-vs-expected table per finding. Do not modify production code; do not
push anywhere but verify/* on the cheyang fork.
```
