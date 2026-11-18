# Verification — PR #6250: alluxio DataLoad/DataBackup use runtime imagePullSecrets

Reviewer: codex (Reviewer B). PR: https://github.com/fluid-cloudnative/fluid/pull/6250
Base: `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5` · Head (round 2): `bf85265045c6391cb9a1d62cc20ae4d1ac186342` (round 1: `34a7318627aeab595f5f7ed53d2f2eb1ed291787`)
Production code untouched — the harness is one additive test file:
`pkg/ddc/alluxio/dataop_imagepullsecrets_verify_test.go`.

## Premise (P0): CONFIRMED

Claim (issue #6247 / PR body): alluxio DataLoad and DataBackup value files always took
the controller's `IMAGE_PULL_SECRETS` env and ignored
`AlluxioRuntime.spec.imagePullSecrets`, so a data operation on a runtime whose images
live in a private registry could not pull its image.

Reproduced on base `ff99142f`: with the runtime's `spec.imagePullSecrets` set and the
env unset, the generated DataLoad and DataBackup value files contain **no**
imagePullSecrets (see `results/base-harness.txt`, 4 red contract tests). The same tests
pass on the PR head (`results/head-harness.txt`). The linked issue is OPEN, its symptom
and named component (`data_load.go`) match the patch.

## Claims and polarity

| id | claim | polarity | base | head |
|----|-------|----------|------|------|
| A1 | DataLoad value file carries runtime `spec.imagePullSecrets` when set | contract | FAIL (premise) | PASS |
| A2 | DataLoad falls back to controller env secrets when runtime sets none | contract (no-regression) | PASS | PASS |
| A3 | Runtime secrets replace (not merge with) the env default | contract | FAIL | PASS |
| A4 | DataBackup value file carries runtime `spec.imagePullSecrets` when set | contract | FAIL (premise) | PASS |
| A5 | DataBackup falls back to controller env secrets when runtime sets none | contract (no-regression) | PASS | PASS |
| A6 | Missing AlluxioRuntime now makes `generateDataLoadValueFile` return an error | contract (documents deliberate behavior change) | FAIL (no error) | PASS |
| A7 (round 2) | runtime lookup is fresh (stale `e.runtime` must not leak) and keyed on the engine/dataset identity (`e.getRuntime()` switch in bf852650) | contract | FAIL (no feature) | PASS |

All tests call only `generateDataLoadValueFile` / `generateDataBackupValueFile`, whose
signatures are identical on base and head, so the same file grafts onto either tree.

## How to run

```bash
# on the PR head (or this branch): all six should PASS
go test ./pkg/ddc/alluxio/ -run 'TestVerify' -v

# on base (premise / harness-bites check):
git worktree add /tmp/fluid-base ff99142f9ec5c32a0c9166de023cb8936f0e5ae5
cp pkg/ddc/alluxio/dataop_imagepullsecrets_verify_test.go /tmp/fluid-base/pkg/ddc/alluxio/
cd /tmp/fluid-base && go test ./pkg/ddc/alluxio/ -run 'TestVerify' -v
# expect: A1/A3/A4/A6 FAIL, A2/A5 PASS
```

Re-verification after a fix revision: `bash docs/verification/alluxio-dataop-imagepullsecrets-codex/scripts/re-verify.sh`
(no argument needed; resolves the current PR head from `manifest.pr`).

## Observed vs expected

- Head: 6/6 PASS (`results/head-harness.txt`).
- Base: A1/A3/A4/A6 red, A2/A5 green (`results/base-harness.txt`) — exactly the expected
  polarity flip; the harness bites for the right reason (assertion on value-file content,
  not compile or setup errors).
- Full package sweep on head: only pre-existing flake
  `metadata_test.go` "should successfully sync metadata and update dataset status" fails;
  the identical spec fails on the clean base tree too (`results/head-full-package.txt`),
  so it is NOT attributable to this PR. `go build ./...` clean.

## Notes for future rounds

- The DataLoad path now hard-requires the AlluxioRuntime object named after the target
  dataset. In the real reconcile flow the operation controller
  (`pkg/controllers/operation_controller.go` `ReconcileInternal`) already fetched the
  runtime into `ctx.Runtime` and returns early when it is missing, so the new error
  branch is reachable only in a narrow delete race or direct test calls. Deliberate,
  documented by the author; low risk.
- Out of scope here (tracked by the author under #6247, JuiceFS part waits on #6245):
  JuiceFS DataLoad/DataMigrate, and alluxio DataProcess value files carry no
  imagePullSecrets at all (pre-existing).

## Round 2 (head bf852650, 2026-11-17)

Delta `34a73186..bf852650`: one commit (`test(alluxio): check that runtime imagePullSecrets
reach the value files`) answering round 1's findings — plus a production-code tweak:

1. `load_data.go`: `utils.GetAlluxioRuntime(r.Client, dataload.Spec.Dataset.Name/Namespace)`
   → `e.getRuntime()` (round-1 [SHOULD], symmetry with `backup_data.go`). Traced the key
   source: the operation controller (`pkg/controllers/operation_controller.go`
   `ReconcileInternal`) sets `ctx.NamespacedName` from the target dataset fetched via
   `dataload.Spec.Dataset`, and `alluxio.Build` sets `e.name/e.namespace` from
   `ctx.Name/Namespace` — the lookup key is byte-identical on the reconcile path, and
   both old and new code do a fresh cache-backed read with the same client. Pinned by A7.
2. PR's own tests now assert value-file contents: `TestGenerateDataLoadValueFileImagePullSecrets`
   (replace + env fallback) and a new `runtime imagePullSecrets reach the value file` case in
   `TestGenerateDataBackupValueFile`. **Harness-bites check**: reverting the wiring on head
   (env-only secrets) turns exactly the two runtime-secrets cases red
   (`results/round2-head-revert-bites.txt`) — the round-1 coverage gap (F1/AC5) is genuinely
   closed. `TestGenerateDataLoadValueFile` was updated for the new lookup (engine now carries
   Client + dataset identity), matching the fixture.

### Harness update (expected, not a fix signal)

The round-1 harness engine had no `name`/`namespace`; under bf852650 the DataLoad tests
failed with `alluxioruntimes.data.fluid.io "" not found` and A6 false-passed. Fixed the
harness engine to carry the dataset identity (mirrors the PR's own test update) and added A7.
Also switched the manifest's unit cmd to `go test -json` so `re-verify.sh` can parse it.

### Round-2 results

- `re-verify.sh bf852650`: A1–A7 all pass → all findings FIXED
  (`results/reverify/unit.parsed`; raw `unit.out` is `*.out`-gitignored locally).
- Base polarity re-confirmed with the updated harness: A1/A3/A4/A6/A7 red, A2/A5 green
  (`results/round2-base-harness.txt`).
- PR's own tests on head: all pass (`results/round2-head-pr-own-tests.txt`).
- Full package sweep on head: same single pre-existing flake as round 1
  (`metadata_test.go` GetMetadataInfoFailed), fails identically on base
  (`results/round2-head-full-package.txt`); not PR-attributed. `go build ./...` and
  `go vet ./pkg/ddc/alluxio/` clean on head.
- GitHub checks on bf852650: all green (unittest, lint, build, kind-e2e × 5 k8s versions,
  backward-compat × 5, CodeQL, SonarCloud, codecov).

## Round 3 (head 9667daf6, base moved ff99142f → dcb76167, 2026-11-18)

The PR was **rebased** (old commits 34a731862/bf8526504 → 416beca92/3b72b5174, tree-identical
in `pkg/ddc/alluxio/`) plus **one new commit** `9667daf6b`
(`test(alluxio): cover the env imagePullSecrets fallback for DataBackup`) answering F5.

Delta `bf852650..9667daf6` in PR files: `backup_data_test.go` +66 only:

1. New subtest `env imagePullSecrets without runtime secrets` in `TestGenerateDataBackupValueFile`:
   runtime without secrets + `t.Setenv(common.EnvImagePullSecretsKey, "env-secret")` → asserts
   `values.DataBackup.ImagePullSecrets == [{env-secret}]` (F5's ask).
2. The existing `runtime imagePullSecrets reach the value file` case now also sets
   `envSecrets: "env-secret"`, so it pins **replace-not-merge for DataBackup** too — more than
   F5 asked.

Base delta `ff99142f..dcb76167` review: vendored `sigs.k8s.io/yaml` 1.4.0 → 1.6.0 (impl swap
goyaml.v2 → go.yaml.in/yaml/v2 — the value-file marshal path in `load_data.go` uses it),
Go 1.26.10→1.26.11, #6257 CI/script/Dockerfile hardening, plus `pkg/common/constants.go`
(additive const), `pkg/ddc/cache/engine/*`, `pkg/utils/dataset/lifecycle/*`. None of it touches
the PR's files, the caller chain (`pkg/ddc/base`, `pkg/databackup`, `pkg/controllers/operation*`),
or `pkg/utils/docker` — verified by name-diff; the harness re-run compiles and runs against the
new vendored yaml, behaviorally covering the marshal path.

### Round-3 results

- `re-verify.sh` (auto-resolved head 9667daf6): A1–A7 all PASS → all findings FIXED
  (`results/reverify/unit.parsed`).
- Base polarity re-confirmed on the NEW base dcb76167: A1/A3/A4/A6/A7 red, A2/A5 green —
  same shape as round 2 (`results/round3-base-polarity.txt`); premise P0 re-confirmed.
- PR's own tests on head: all pass, incl. the new backup env-fallback subtest
  (`results/round3-head-pr-own-tests.txt`).
- New mutation bite-proofs for the round-3 test additions (`results/round3-head-mutations.txt`):
  - M1 merge-instead-of-replace in `dataOperationImagePullSecrets` → fails BOTH the DataLoad and
    the updated DataBackup runtime-secrets cases (`got [{runtime-secret} {env-secret}]`).
  - M2 broken env fallback → fails exactly the new backup env-fallback subtest
    (`backup_data_test.go:236`) and the load env-fallback subtest. Control run on unmodified
    head: green.
- Full package sweep on head: 149/150, the same pre-existing `metadata_test.go:205` flake,
  reproduced identically on the new base dcb76167 (`results/round3-head-full-package.txt`);
  `go build ./...` + `go vet ./pkg/ddc/alluxio/` clean (`results/round3-head-build-vet.txt`).
- GitHub checks on 9667daf6: all SUCCESS (23 contexts incl. kind-e2e ×5, backward-compat ×5);
  PR OPEN/MERGEABLE.
- #6247 follow-up: author's tracking comment stands; author later narrowed DataProcess
  ("jobProcessor.podSpec is user-supplied and can carry imagePullSecrets") — verified true for
  that path (`job.yaml:46-47`), but the `scriptProcessor` path still renders a required user
  image with no imagePullSecrets anywhere in the chart. Pre-existing, out of scope.
