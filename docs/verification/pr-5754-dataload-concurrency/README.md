# Verification — PR #5754 "fix: enforce concurrency limits for data operations"

Reviewer: B (Codex). Head: `20b7e5ed3c5bfc9844061eed496e75b8f25e23cb`. Merge-base: `965f661b73ae719f687ba82a3ed517352ac9617d`.
Harness is additive only (two `*_test.go` files + this dir); production code untouched.

## Premise (P0) — CONFIRMED

The PR claims: previously `SetDataOperationInProgress` always appended, so multiple
same-type data operations (issue #1138 asks about concurrent DataLoads) could run on
one dataset simultaneously — "is there a lock?" No, there wasn't.

Reproduced on the merge-base `965f661b` (both layers) and re-confirmed at the frozen
round baseSha `82e490e` (`results/base82-unit.txt`; #5798 changed the engine parameter
type there but added no gate):

- unit (fake client): `results/base-unit.txt` —
  `P0 reproduced: second DataLoad admitted while load-1 in progress; OperationRef[DataLoad]="load-1,load-2"`
- integration (envtest, real API server 1.31.0): `results/base-envtest.txt` — same admission
  through the real status subresource.
- frozen round base `82e490e`: `results/base82-unit.txt` — same admission.

On the PR head the same contract test passes: the second op is rejected with
"the dataset ds has reached the maximum number of parallel DataLoad operations (limit: 1), please wait"
and stays Pending (`reconcilePending` requeues after 20s, `pkg/ddc/base/operation.go:113-117`).

## How to run

```bash
# unit + fake-client layer (no prerequisites)
go test ./pkg/ddc/base/ -run 'TestVerify' -count=1 -v

# integration layer (envtest; needs kubebuilder assets)
export KUBEBUILDER_ASSETS=$(ls -d $HOME/envtest-assets/k8s/*-linux-amd64 | sort -V | tail -1)
go test ./pkg/ddc/base/ -run 'TestVerifyEnvtestOperationLock' -count=1 -v

# full re-verify against a newer PR head (from this branch's checkout)
bash docs/verification/pr-5754-dataload-concurrency/scripts/re-verify.sh
```

## Observed vs expected (head @ 20b7e5ed)

| id | claim | layer | expected (contract) | observed on head | verdict |
|----|-------|--------------------|---------------------|------------------|---------|
| P0 | base admits a 2nd concurrent DataLoad | unit+envtest | reproduce on base | reproduced (base), blocked on head | premise confirmed; main fix works |
| F1 | DataMigrate cap must be 1 per PR body; code uses `spec.parallelism` (worker count) as the cap | unit+envtest | 2nd DataMigrate blocked | 2nd DataMigrate admitted when parallelism=4 (`OperationRef="mig-1,mig-2"`) | **defect confirmed** |
| F3 | different op objects with the same name (other namespace) must not share the lock slot | unit | blocked | admitted via name-only key re-entrant branch | bypass confirmed |
| REENT | same op re-entering must be admitted exactly once | unit | pass | pass | ok |
| LIFE | op2 blocked while op1 runs; admitted after release | unit+envtest | pass | pass | ok |

Harness-bites check: with the `CanStartDataOperation` gate temporarily removed from
`pkg/ddc/base/operation_lock.go` on the head, `TestVerifyContractSecondDataLoadBlocked`
flips to FAIL (`results/head-gate-removed-bites.txt`); reverted afterwards, tree clean.

## F2 (minor, code-path evidence, no harness)

A blocked op produces no Kubernetes Event and no status condition; the only signal is an
Error-level controller log every 20s (`operation_lock.go:77` + `operation.go:115`).
Compare the runtime-not-ready path, which records a `RuntimeNotReady` event.

## Proposed fixes

- F1: decouple the inter-operation cap from worker parallelism — e.g. pass a constant 1
  (or a dedicated `MaxParallelOperations()`) to `CanStartDataOperation` instead of
  `GetParallelTaskNumber()`, or document+rename if `parallelism` is genuinely intended
  as the concurrency cap for DataMigrate.
- F3: key `OperationRef` entries by `namespace/name` (or store the UID) instead of name only.
- F2: record a Waiting-style event/condition when the gate blocks, and log at Info level.

## Continuing after the fix / on another machine

1. Fetch this branch (`verify/pr-5754-dataload-concurrency-codex` on the review fork).
2. `bash docs/verification/pr-5754-dataload-concurrency/scripts/re-verify.sh` — it fetches the
   current PR head via `manifest.pr`, grafts the two harness test files, runs unit+integration.
   Exit 0 iff every finding is fixed. F1/F3 are contract tests: they are "fixed" when they PASS.
3. Live layer intentionally skipped in this debate round.
