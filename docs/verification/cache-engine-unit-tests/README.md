# cache-engine-unit-tests — test verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6264 (Reviewer A, Claude).

## Round 2 (head `ee7899ca55deea79eadc338053707da97c647005`, incremental)

The PR added one commit, `ee7899ca5` "test(cache): cover the DataLoad value file happy path"
(+69/−2, still tests-only). It addresses **all four** SHOULD-level follow-ups from round 1:

| Prior finding | How this round resolves it | Verified by |
|---|---|---|
| T1 (minor, missingTests) — no happy-path test for the DataLoad value-file generation | new spec `writes a DataLoad value file built from the runtime class DataOperationSpecs` (full fixture: runtime + runtime class + DataOperationSpecs + dataset + DataLoad; asserts file-name convention, unmarshalled `DataLoadValue` name/image/command/target dataset/target paths, and the runtime-config volume + mount) | **fixed** — mutation M5 (the exact round-1 gap mutation) now turns exactly that spec red: `results/mutation-round2-m5.txt`; `scripts/mutation-check-t1.sh` exits 0 with both PR specs and harness red |
| N1 (nit) — empty-topology spec asserts only `HaveOccurred()` | now `MatchError(ContainSubstring("at least one component"))` | **fixed** — `validate_test.go:39` at head |
| N2 (nit) — no worker-`RuntimePhaseNone` rows in the CheckRuntimeReady table | both suggested rows added: `(false, Ready, None, false)` and `(true, None, None, false)` | **fixed** — mutation M8 (master-disabled worker gate accepts `None`) turns exactly the new `master disabled, worker not set up yet` row red: `results/mutation-round2-m8.txt` |
| N3 (nit) — `fakeOperation.GetTargetDataset` returns `(nil, nil)` | now returns `errors.New("not implemented")` with a comment stating the stub's contract | **fixed** — `data_operation_test.go` at head; suite green, no exercised path calls it |

New content checks on the added spec (content assertions must actually bite):

- **M6** — drop `dataloadInfo.Image = opSpec.Image` from `genDataLoadValue`: happy-path spec red → the image assertion bites (`results/mutation-round2-m6.txt`).
- **M7** — `getRuntimeConfigDir` → `/etc/fluid/mutated`: happy-path spec + the pre-existing transform spec red → the volume-wiring assertions bite (`results/mutation-round2-m7.txt`).

Round-2 regression sweep at head:

- canonical invocation (`FLUID_UNIT_TEST=true go test -gcflags=all=-l`): **421 passed / 0 failed / 16 skipped** (`results/l2-full-suite-make-style.txt`) — was 418 at the round-1 head, +3 specs (2 table rows + 1 It), all green.
- plain `go test` (no gcflags): 421 ran / 409 passed / **12 failed** — the *same* 12 pre-existing `ufs_test.go`/`sync_test.go` gomonkey failures as base and round-1 head (`results/l2-plain-gotest-head.txt` vs `results/l1-plain-gotest-*.txt`); PRE1 stays refuted as a PR issue.
- `re-verify.sh` (harness grafted onto the new head): **all findings FIXED**, exit 0 (`results/reverify-round2.txt`).
- `scripts/mutation-check-round2.sh` (M5–M8 bites-check): every mutation turns the suite red, tree restored clean (`results/mutation-round2-bitescheck.txt`).
- `go vet` and `gofmt` clean on both touched test files; CI re-checked check-by-check via `gh pr checks 6264` at ee7899ca5: unittest (`make test`, -race), lint, staticcheck, build amd64+arm, 5× kind-e2e, 5× backward-compat, CodeQL, DCO, codecov — all green.

Still open after round 2 (both non-blocking): **Q1** (issue-#6263 accuracy note) and the
residual unasserted fields of the happy-path value file (`OwnerDatasetId`, the
`FLUID_RUNTIME_CONFIG_PATH` env) — nit-level, see the findings ledger.

---

Layers run against the **code under review** (round 1: `ef7ecf5f06fdc67ef950bfe2737e1e509be2bb5b`; round 2: `ee7899ca55deea79eadc338053707da97c647005`, PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (gotest) | reviewer harness contract tests for the DataLoad value-file happy path | `go test ./pkg/ddc/cache/engine/ -run 'TestDataOperationValueFileHappyPath\|TestDataLoadValueFileNotSupportedWhenSpecMissing' -v` |
| 2. Ginkgo (PR's own new specs) | the 26 specs the PR adds (23 in round 1 + 3 in round 2), in isolation | `go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus="CacheEngine data operations\|validateRuntimeClassTopology" -v` |
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

## Summary of results (round 2 unless noted)

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | premise: the four functions had no direct unit tests; new tests pass | 1/2 | Confirmed (round 1) | results/l1-pr-new-specs.txt, results/l2-full-suite-make-style.txt |
| T1 | the PR's routing tests only assert the early type-mismatch error, so a happy-path regression in `generateDataLoadValueFile` leaves all new specs green | 1 | **Fixed in round 2** — the PR's own happy-path spec now goes red under the round-1 gap mutation | results/mutation-round2-m5.txt (M5 red); round-1 evidence of the gap: results/mutation-t1.txt |
| N2 (was T2) | `CheckRuntimeReady` table lacks the worker-`RuntimePhaseNone` rows (worker never started) | 2 | **Fixed in round 2** — both rows added; M8 turns the new row red | results/mutation-round2-m8.txt |
| N3 (was T3) | `fakeOperation.GetTargetDataset()` returns `(nil, nil)` — nil dataset with nil error | — | **Fixed in round 2** — returns `errors.New("not implemented")` | data_operation_test.go at ee7899ca5 |
| N1 | empty-topology spec asserts only `HaveOccurred()` | 2 | **Fixed in round 2** — asserts the error message | validate_test.go:39 at ee7899ca5 |
| Q1 | issue #6263 says `validateRuntimeClassTopology` has "no test anywhere" — it had indirect negative coverage on base | — | Open (non-blocking accuracy note; no code change) | git grep on base dcb76167 (round 1) |
| PRE1 | 12 engine-package test failures seen under plain `go test` are pre-existing, not introduced by this PR | 1 | Refuted as a PR issue — re-confirmed identical at the round-2 head | results/l2-plain-gotest-head.txt vs results/l1-plain-gotest-{base,head}.txt |

## Per-finding detail

### T1 — routing tests do not guard the value-file happy path (minor, missingTests) — FIXED in round 2

`GetDataOperationValueFile` tests in the PR dispatch a *deliberately mismatched* object
(e.g. a `DataProcess` under `DataLoadType`) and assert each generator's early type-assertion
error. That proves dispatch, but not generation. Issue #6263's motivation is "a regression in
either one breaks DataLoad and friends without any test going red" — the value-file
generation itself is the part that breaks DataLoad.

Round-1 mechanism proof (`scripts/mutation-check-t1.sh`): mutate `generateDataLoadValueFile`
to return `"mutated-bogus.yaml", nil` right after the type assertion. Then the PR's 23 specs
stayed green (results/mutation-t1.txt, section a) while the harness went red (section b).

**Round 2:** commit `ee7899ca5` adds the spec
`writes a DataLoad value file built from the runtime class DataOperationSpecs` — a full
happy-path contract test (fixture runtime/runtime-class-with-DataOperationSpecs/dataset/
DataLoad, fake client, value file read back and unmarshalled, image/command/target-dataset/
target-paths/runtime-config volume+mount asserted, temp file cleaned up via `DeferCleanup`).
The exact round-1 gap mutation now turns exactly that spec red
(results/mutation-round2-m5.txt), and the content-level mutations M6 (image dropped) and
M7 (mount dir changed) are caught too. The PR independently wrote the test this harness
proposed; the harness tests remain as an extra guard. T1 is closed.

### N2 (round-1 id T2) — missing worker-None rows (nit) — FIXED in round 2

`data_operation.go:44-57`: the master-worker branch requires
`Worker.Phase == Ready || PartialReady`. Round 1's table covered NotReady but never
`RuntimePhaseNone` (worker never started), for either architecture. Round 2 added both
suggested rows; mutation M8 proves the new `master disabled, worker not set up yet` row
is the one that pins the worker gate against accepting `None`
(results/mutation-round2-m8.txt).

### N3 (round-1 id T3) — fakeOperation.GetTargetDataset returns (nil, nil) (nit) — FIXED in round 2

`GetTargetDataset()` now returns `errors.New("not implemented")` with a comment stating
the stub's contract, exactly as suggested. Never called on the exercised paths; the suite
stays green, confirming no path depends on the old `(nil, nil)`.

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

- ~~**T1**: adopt an equivalent of `pkg/ddc/cache/engine/data_operation_harness_test.go`
  (a happy-path test) into the PR~~ — done by the author in round 2 (ee7899ca5).
- ~~**N2/T2**: add the worker-`RuntimePhaseNone` rows~~ — done by the author in round 2.
- ~~**N3/T3**: make the fake's `GetTargetDataset` return an explicit error~~ — done by the
  author in round 2.
- **N4 (new, round 2, nit)**: the happy-path spec could additionally assert
  `OwnerDatasetId` (the dataset UID wiring through `utils.GetDatasetId`) and the injected
  `FLUID_RUNTIME_CONFIG_PATH` env; a regression in either leaves the suite green today.
  Optional — the core T1 contract (image/command/target path/runtime-config volume) is
  covered and mutation-verified.
- **PRE2 (new, round 2, nit, pre-existing)**: `generateDataLoadValueFile` never closes the
  temp file handle it creates (`os.CreateTemp` → `os.WriteFile` → return). Harmless on
  Linux (the PR's own test comment notes removal is only blocked on Windows), a
  pre-existing production wart, not introduced by this tests-only PR.

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
   bash docs/verification/cache-engine-unit-tests/scripts/mutation-check-t1.sh        # T1 guard: PR specs AND harness must go red
   bash docs/verification/cache-engine-unit-tests/scripts/mutation-check-round2.sh    # M5-M8 bites-check for the round-2 specs
   ```
4. Read results via the polarity table: everything here is a **contract** test — green is
   the good state; red means a regression. No canaries to invert.
5. Harness-bites: `scripts/mutation-check-round2.sh` is the standing bites-check — every
   mutation must turn the suite red. `scripts/mutation-check-t1.sh` doubles as the T1
   regression guard: since round 2 it expects the PR's specs to go red as well; a green
   run reopens T1.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/cache-engine-unit-tests-claude of
https://github.com/cheyang/fluid.git. Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6264 (tests-only PR for
pkg/ddc/cache/engine) produced findings T1/N1/N2/N3/Q1/PRE1 plus premise P0 in round 1;
round 2 (PR head ee7899ca5) fixed T1/N1/N2/N3 and the harness re-verified all of them
fixed (re-verify.sh exits 0). Read
docs/verification/cache-engine-unit-tests/README.md ("Continuing after the fix") and
follow it: graft the harness onto the current PR head (resolve it from the manifest's pr
field), re-run both layers, re-run scripts/mutation-check-t1.sh and
scripts/mutation-check-round2.sh (both must exit 0 — every mutation red), and report an
observed-vs-expected table per finding. Do not modify production code; do not
push anywhere but verify/* on the cheyang fork.
```
