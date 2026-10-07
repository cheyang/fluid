# ownerref-gvk-converge — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6199
(fixes #6140: converge the two owner-reference GVK recovery implementations).

Layers run against the **code under review** (`6797b26f4b6aaadb55e81c2ee862c8956d896db1`, PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `datasetControllerOwnerReference` differential equivalence (legacy oracle vs converged), incl. exotic TypeMeta shapes | `go test ./pkg/utils/ -run 'TestVerify' -count=1` |
| 2. Integration (fake client) | `CreateRuntimeForReferenceDatasetIfNotExist` persists a well-formed ownerReference on the created ThinRuntime | same command (part of `TestVerify*`) |
| 3. Live | — | **skipped**: a cluster was reachable (2 nodes) but this review round is configured unit+integration only |

> Test polarity: all harness tests are **contract** tests — they assert the intended
> correct behavior (equivalence + well-formedness) and FAIL if the convergence regresses it.

## Problem premise (P0)

Run against the **base** branch (`e0fc4c18`) without the patch, because the question is
whether the problem exists today.

| | |
|---|---|
| Claimed symptom | "The two code paths do not overlap — `datasetControllerOwnerReference` does not go through the transformer — so neither fix covers the other's callers." (issue #6140) |
| Linked issue | #6140, OPEN, created 2026-07-29 — still valid |
| Reported component | `pkg/utils/dataset_runtime.go` + `pkg/utils/transformer/owner_reference.go` |
| Patched component | exactly those two (production change confined to `dataset_runtime.go`) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | Base `dataset_runtime.go` carries its own per-field GVK fallback and zero references to `utils/transformer` (results/premise-base-inspection.txt). Issue items 1 (per-field fill) and 2 (failure diagnostic) were already landed on base via #6139 (`95138fc7`); the PR implements the remaining items 3 (delegation) and 4 (regression coverage for partial-TypeMeta Dataset/AlluxioRuntime shapes). Merging legitimately closes #6140. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Divergent duplicate GVK-recovery implementations exist on base | — (base inspection) | Confirmed | results/premise-base-inspection.txt |
| B1 | The converged `datasetControllerOwnerReference` is observably identical to the legacy implementation for every well-formed TypeMeta shape (no behavioral regression) | 1 | Confirmed (equivalence holds) | 11/11 shapes equal; results/l1-l2-tests.txt |
| B1′ | The only divergence is a malformed apiVersion (`"data.fluid.io/"`), where the new code normalizes to the registered version instead of copying the garbage — strictly better | 1 | Confirmed | `TestVerifyMalformedAPIVersionIsNormalized` passes |
| B2 | `CreateRuntimeForReferenceDatasetIfNotExist` persists a well-formed ownerReference (`Kind`/`APIVersion`/`Controller` populated) through the fake client | 2 | Confirmed | results/l1-l2-tests.txt |
| — | PR's own verification commands (§Ⅳ of the PR body) pass | 1 | Pass | results/pr-commands-and-attribution.txt |
| — | Blast radius: consumer package `pkg/controllers/v1alpha1/dataset` green; `go build ./...` and `go vet` green | 2 | Pass | results/blast-radius-dataset-controller.txt, results/build-vet.txt |

Pre-existing failures **not caused by this PR** (verified by running them on base `e0fc4c18`):
`TestCheckMountPointBroken` (pkg/utils — ginkgo mount-check assertion + pprof port 6060 collision)
and `TestHelm` (pkg/utils/helm). See results/pr-commands-and-attribution.txt.

## Per-finding detail

### B1 — behavioral equivalence of the convergence (the core risk of this refactor)

Mechanism: the PR replaces the wrapper's direct per-field TypeMeta reads with
`transformer.GenerateOwnerReferenceFromObject`, whose scheme recovery has different trigger
conditions (`len(gvk.Kind)==0 || len(gvk.Version)==0` vs the legacy per-field emptiness checks
on the raw strings).

Test: `pkg/utils/dataset_runtime_verify_test.go` —
`TestVerifyOwnerReferenceEquivalenceLegacyVsConverged` embeds a byte-for-byte copy of the base
implementation as oracle (`legacyDatasetControllerOwnerReference`) and compares outputs via
`reflect.DeepEqual` over 11 shapes: complete, empty, kind-only, apiVersion-only (current and
custom-beta version), wrong-kind, foreign-group, foreign-group+custom-kind, version-without-group.

Observed: 11/11 identical (Kind, APIVersion, Name, UID, Controller, BlockOwnerDeletion).
Expected: identical — the convergence is behavior-preserving. Confirmed.

One genuine divergence, pinned separately in `TestVerifyMalformedAPIVersionIsNormalized`:
TypeMeta `{Kind: "Dataset", APIVersion: "data.fluid.io/"}` (group set, version empty — malformed
input no real client produces) — legacy copied `"data.fluid.io/"` through (an API-server-invalid
reference); the new code resolves the version from the scheme and emits
`data.fluid.io/v1alpha1`. An improvement, not a regression.

### B2 — well-formed ownerReference persisted end-to-end

Mechanism: the whole point of the GVK recovery is that the ThinRuntime created for a reference
dataset carries an ownerReference the API server accepts and the owner-based watch can resolve.
No pre-existing test asserted the persisted Kind/APIVersion through the fake client.

Test: `TestVerifyCreatedThinRuntimeCarriesWellFormedOwnerReference` — empty-TypeMeta dataset
(the shape a typed client hands back), create via `CreateRuntimeForReferenceDatasetIfNotExist`,
read the ThinRuntime back, assert the persisted reference.

Observed: `{Kind: "Dataset", APIVersion: "data.fluid.io/v1alpha1", Controller: true}`.
Expected: exactly that. Confirmed. (This also fills the missingTests gap F2 in the review.)

## Harness-bites check

Sabotage applied: `datasetControllerOwnerReference` made to drop `Kind`/`APIVersion` from its
return value → all three contract tests FAIL (equivalence, normalization, persisted-reference).
Sabotage reverted (`git checkout --`), all green again, production diff empty.
See results/harness-bites.txt.

## Live run notes

Skipped this round: a live cluster is reachable from this machine (2 nodes), but the review
round was configured for unit + integration layers only. L3 signal worth checking later if the
function is refactored again: create a reference dataset on a real cluster and confirm the
auto-created ThinRuntime's ownerReference carries `Kind: Dataset` / `APIVersion: data.fluid.io/v1alpha1`
and is garbage-collected when the dataset is deleted.

## Proposed fixes (NOT applied to production here)

- **F1 (minor)**: the "last resort" fallback in `datasetControllerOwnerReference` is unreachable
  for its only input type — `*datav1alpha1.Dataset` is always registered in the transformer's
  scheme, and recovery triggers exactly when Kind or Version is empty, so `ref.Kind` /
  `ref.APIVersion` can never come back empty. Keep it as defense-in-depth or drop it, but the
  doc comment + test naming should not imply the fallback path is exercised (it cannot be,
  through this wrapper). Optionally log if it ever fires.
- **F2 (minor, missingTests)**: no test asserted the persisted ownerReference through
  `CreateRuntimeForReferenceDatasetIfNotExist` (existing test only checked error/termination
  behavior). The harness adds exactly that test; upstreaming it would close the gap.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/ownerref-gvk-converge-claude` (fork `cheyang/fluid`;
production code untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/ownerref-gvk-converge-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/ownerref-gvk-converge pkg/utils/dataset_runtime_verify_test.go
   ```
2. Prereqs: Layer 1/2 = Go toolchain only (no envtest, no cluster).
3. Re-run: `bash docs/verification/ownerref-gvk-converge/scripts/re-verify.sh` (no ref needed —
   it fetches the current PR head from `manifest.pr`), or directly
   `go test ./pkg/utils/ -run 'TestVerify' -count=1`.
4. Polarity: all contract tests — they should be GREEN on correct code; any red is a real
   regression of the convergence. No canaries to invert.
5. Harness-bites: re-apply the sabotage from results/harness-bites.txt once to confirm red.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/ownerref-gvk-converge-claude (fork cheyang/fluid).
Background: a review of https://github.com/fluid-cloudnative/fluid/pull/6199 produced claims
P0/B1/B2 (owner-reference GVK convergence); a harness reproduced/confirmed them. The PR may
have been updated since. Read docs/verification/ownerref-gvk-converge/README.md
("Continuing after the fix") and follow it: graft the harness onto the current PR head,
re-run the unit+integration layers via scripts/re-verify.sh, all tests are contract-polarity
(green = correct), run the harness-bites check once, and report an observed-vs-expected table.
Do not run the live layer unless explicitly asked. Clean up scoped test resources; no
cluster-wide destructive actions.
```
