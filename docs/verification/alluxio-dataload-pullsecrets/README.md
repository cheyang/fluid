# alluxio-dataload-pullsecrets — PR #6250 verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6250
(feat(alluxio): use runtime imagePullSecrets for DataLoad and DataBackup).

Harness branch: `verify/alluxio-dataload-pullsecrets-claude`, based on PR head `34a7318627`
(base / merge-base `ff99142f9`). Production code is untouched; the only additions are the two
`*_pullsecrets_verify_test.go` test files and this directory.

Layers run against the **code under review** (head `34a7318627`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (fake client + real value-file generation) | `AlluxioEngine.generateDataLoadValueFile` / `generateDataBackupValueFile` end-to-end into the marshaled helm values | `go test ./pkg/ddc/alluxio/ -run 'TestPR6250' -count=1 -v` |
| 2. Integration (envtest/cluster) | not needed — the claim is fully decided at the value-file layer; the chart templates already render `imagePullSecrets` (charts/fluid-dataloader/alluxio/templates/job.yaml:75, charts/fluid-databackup/alluxio/templates/databackup.yaml:14) | — |
| 3. Live | skipped for this round (unit + integration policy); see `verify-manifest.json` `liveNote` | — |

> Test polarity: all tests here are **contract** tests (assert the intended behavior). They
> FAIL on the merge-base and PASS on the PR head. No bug-canaries in this set.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is whether the
problem exists today.

| | |
|---|---|
| Claimed symptom | "a DataLoad on a runtime whose images live in its own private registry couldn't pull the image" (PR body); #6247: "a DataLoad on that runtime still runs with the controller wide secrets, so it fails to pull if the image lives in the same private registry" |
| Linked issue | #6247 (OPEN, feature request; referenced as "Part of", not `fixes` — no auto-close). #6245 (OPEN, JuiceFS spec field) is a dependency for the deferred follow-up. |
| Reported component | Alluxio (and JuiceFS) DataLoad/DataMigrate job pods — Alluxio is this PR's scope |
| Patched component | `pkg/ddc/alluxio/load_data.go`, `backup_data.go`, `utils.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** (real problem on base) |
| Evidence | On base with `runtime.spec.imagePullSecrets=[{runtime-secret}]` and `IMAGE_PULL_SECRETS=env-secret-should-be-replaced`, both value files contain the **env** secret, not the runtime's — `results/base-unit.out` (2 FAIL), `results/head-unit.out` (all PASS). The gap matches the base code: `docker.GetImagePullSecretsFromEnv(common.EnvImagePullSecretsKey)` was the only source (removed lines in the diff). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | On base, DataLoad/DataBackup value files ignore `AlluxioRuntime.spec.imagePullSecrets` | 1 | **Confirmed** | `results/base-unit.out`: value file has `[{env-secret-should-be-replaced}]`, expected `[{runtime-secret}]` |
| F1 | No test in the PR pins "runtime secrets land in the value file" (only the helper and an empty-list pass-through are tested) | 1 | Confirmed (gap demonstrated by contract tests the PR lacks) | `TestPR6250DataLoadValueFileUsesRuntimePullSecrets` / `TestPR6250DataBackupValueFileUsesRuntimePullSecrets` assert file contents; PR's `TestGenerateDataLoadValueFile` only checks the file-name pattern with a secrets-less fixture |
| F2 | New hard dependency: DataLoad value-file generation now errors when the AlluxioRuntime object is missing | 1 | Confirmed (behavior change, documented as contract) | `TestPR6250DataLoadValueFileRequiresRuntime` passes on head, fails on base (`results/base-unit.out`) |
| — | Env fallback unchanged (no runtime secrets → controller env) | 1 | Confirmed (no regression) | fallback tests pass on **both** base and head |

## Per-finding detail

### P0 — premise
Mechanism on base: `generateDataLoadValueFile`/`genDataLoadValue` (and `generateDataBackupValueFile`)
read image pull secrets exclusively from the controller env. Runtime-scoped secrets were ignored,
so data-operation pods on private-registry runtimes could not pull. Differential harness:
fake client with dataset + runtime carrying `spec.imagePullSecrets`, env var set to a decoy;
read the marshaled value file and compare.

### F1 — missing behavioral assertion in the PR's own tests
The PR adds `TestDataOperationImagePullSecrets` (helper, in isolation), updates
`TestGenerateDataLoadValueFile` (only asserts the temp-file **name**; the fixture runtime has no
secrets) and `Test_genDataLoadValue` (pass-through with an **empty** secret list).
`TestGenerateDataBackupValueFile` (which already has an `expectValueCheck` hook that reads the
file contents) gained no ImagePullSecrets case. Nothing in the PR fails if the wiring
`runtime.spec.imagePullSecrets → value file` regresses. The contract tests in this harness are
the missing coverage; they should be uplifted (minus reviewer-private wrapping) into the PR.

### F2 — new hard failure mode in generateDataLoadValueFile
`load_data.go:82` now does `utils.GetAlluxioRuntime(r.Client, dataload.Spec.Dataset.Name,
dataload.Spec.Dataset.Namespace)` and returns the error, so a missing runtime object blocks value
generation and the DataLoad loops in PhaseExecuting (20s requeue + warning events, operation.go).
Reachability: the operation controller only reaches the engine after
`targetDataset.Status.Runtimes` lists the accelerate runtime, which the runtime controller sets
only after the runtime object exists — so in the normal lifecycle the lookup cannot miss.
Residual window: runtime deleted while its entry in `dataset.Status.Runtimes` is stale and the
engine is still cached in the controller's engine map. In that window the DataLoad previously
created a job that failed anyway (master gone); now it fails earlier with a clearer error.
Equivalent failure outcome, different surface — recorded as minor, not a regression.
Note the engine already holds the runtime (`e.runtime`, set in `Build`) and `e.getRuntime()`
(backup_data.go:49) fetches by the same key; the fresh lookup is redundant but harmless.

## Harness-bites check

The "bite" was verified by construction: every contract test here FAILS on the merge-base
`ff99142f9` (see `results/base-unit.out`) and PASSES on the PR head `34a7318627`
(`results/head-unit.out`). No fix was applied or reverted — the PR head itself is the "fix".
Production diff on this branch is empty apart from the harness files
(`git diff 34a7318627..HEAD -- ':!docs/verification' ':!pkg/ddc/alluxio/*_verify_test.go'` is empty).

## Known local-env noise (not PR-attributed)

`go test ./pkg/ddc/alluxio/ -count=1` (full package) fails one pre-existing ginkgo spec on **both**
base and head identically: `metadata_test.go` "should successfully sync metadata and update dataset
status" → `GetMetadataInfoFailed` (149 passed / 1 failed on both refs; see
`results/alluxio-package-{base,head}.out`). Same failure on base ⇒ not introduced by this PR;
GitHub CI on the PR reports success. Do not count it against the PR.

## Proposed fixes (NOT applied to production here)

- **F1**: extend the PR's own tests — give the `TestGenerateDataLoadValueFile` fixture runtime
  `spec.imagePullSecrets` and assert the value file contents; add an ImagePullSecrets
  `expectValueCheck` case to `TestGenerateDataBackupValueFile`. The two contract test files on
  this branch can be copied nearly verbatim.

## Continuing after the fix

The harness lives on branch `verify/alluxio-dataload-pullsecrets-claude` (fork
`cheyang/fluid`), production code untouched, so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/alluxio-dataload-pullsecrets-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/alluxio-dataload-pullsecrets \
     pkg/ddc/alluxio/load_data_pullsecrets_verify_test.go \
     pkg/ddc/alluxio/backup_data_pullsecrets_verify_test.go
   ```
   Or just `bash docs/verification/alluxio-dataload-pullsecrets/scripts/re-verify.sh` from a
   checkout of the harness branch (it resolves the current PR head itself).
2. Prereqs: Go toolchain (≥ go 1.26 per go.mod) — Layer 1 needs nothing else.
3. Re-run: `go test ./pkg/ddc/alluxio/ -run 'TestPR6250' -count=1 -v`
4. Read results via the polarity table: all tests are **contract** tests, so all should be
   GREEN on fixed code. If any go red, the runtime-secrets wiring regressed.
5. Harness-bites: run the same command on the merge-base `ff99142f9` to see the contract
   tests fail (3 of 5 fail there; the two env-fallback tests legitimately pass on both).

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/alluxio-dataload-pullsecrets-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6250 produced findings P0/F1/F2; a harness
reproduced them (PR #6250 head 34a7318627, base ff99142f9). Read
docs/verification/alluxio-dataload-pullsecrets/README.md ("Continuing after the fix") and follow
it: run scripts/re-verify.sh (or graft the two *_pullsecrets_verify_test.go files onto the fixed
ref), re-run the unit layer, mind the polarity table (all contract tests), and report an
observed-vs-expected table. The full alluxio package has one pre-existing local-env ginkgo
failure (metadata_test.go syncMetadataInternal) on both refs — ignore it. Clean up scoped test
resources; do not run cluster-wide destructive actions.
```
