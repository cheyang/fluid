# ownerref-converge — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6199
("fix(utils): converge datasetControllerOwnerReference with transformer helper", fixes #6140).

Layers run against the **code under review** (6797b26f = PR #6199 head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `datasetControllerOwnerReference` output vs golden base behavior, 8 TypeMeta shapes | `go test ./pkg/utils/ -run 'TestVerifyDatasetControllerOwnerReferenceGolden' -count=1 -v` |
| 2. Integration | real API server (envtest 1.31): Dataset created via typed client (TypeMeta stripped), `CreateRuntimeForReferenceDatasetIfNotExist`, ownerRef accepted + second reconcile no-op | `KUBEBUILDER_ASSETS=<setup-envtest 1.31 path> go test ./pkg/utils/ -run TestVerifyCreateRuntimeForReferenceDatasetEnvtest -count=1 -v` |
| 3. Live | skipped per debate setup (no KUBECONFIG layer) | — |

> Polarity: both tests are **contract** tests — they PASS on correct code and FAIL on buggy
> code. There are no bug-canaries in this harness.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "Two independent implementations of 'recover the owner GroupVersionKind when `TypeMeta` is empty' are landing at the same time, and they do not agree on semantics." (issue #6140) |
| Linked issue | #6140, OPEN, created 2026-07-29, still valid as a dedup task |
| Reported component | `pkg/utils/dataset_runtime.go` (`datasetControllerOwnerReference`) + `pkg/utils/transformer/owner_reference.go` (`GenerateOwnerReferenceFromObject`) |
| Patched component | `pkg/utils/dataset_runtime.go` (delegates), tests in both packages |
| Component match | Yes |
| **Verdict** | **Confirmed, with a nuance**: the duplication exists on base e0fc4c18 (direct code evidence), but the *semantic divergence* half of the issue is stale — #6139 merged (95138fc7) with per-field recovery, so base local impl and the shared helper produce identical references for all 8 Dataset TypeMeta shapes (results/premise-base-diff.txt). The PR is therefore a behavior-preserving dedup, not a behavior fix. |
| Evidence | results/premise-base-diff.txt — `agree=true` for every shape on base |

Issue #6140's four proposals vs current tree: (1) per-field recovery — already in master via
#6139; (2) error logging on scheme-miss — already in master via #6139; (3) delegate
`datasetControllerOwnerReference` — **this PR**; (4) regression tests for partial TypeMeta —
DataLoad entries already in master, Dataset/AlluxioRuntime entries added by this PR. Merging
legitimately closes #6140.

## Findings and observed-vs-expected

No defects found in the PR. The harness instead proves the two claims that matter for a
delegation refactor:

| Claim | Layer | Expected | Observed | Verdict |
|-------|-------|----------|----------|---------|
| R1: delegation keeps `datasetControllerOwnerReference` output identical to base for every reachable TypeMeta shape (Kind/APIVersion/Name/UID/Controller/nil BlockOwnerDeletion) | unit | golden base values | golden base values, 8/8 subtests pass (results/l1-unit.txt) | Confirmed no regression |
| R2: real API server accepts the produced ownerReference on the production path, and a second reconcile is a no-op | integration | ThinRuntime created with `{data.fluid.io/v1alpha1, Dataset, controller:true}`; resourceVersion unchanged after 2nd call | exactly that; typed-client read confirmed to strip TypeMeta (`kind="" apiVersion=""`), so the scheme-recovery path is the one exercised (results/l2-envtest.txt) | Confirmed |

## Harness-bites check (mutation)

Production code temporarily mutated to a naive TypeMeta read with **no** scheme recovery and
**no** fallback (results/mutation-bites.txt), then reverted (`git diff` on production files: 0 lines):

- L1: 5/8 subtests fail (`expected "Dataset", got ""` etc.) — the golden test exercises the recovery/fallback.
- L2: real API server rejects the create: `metadata.ownerReferences.apiVersion: Invalid value: "": version must not be empty, metadata.ownerReferences.kind: Invalid value: "": kind must not be empty` — exactly the failure mode the recovery prevents.

Both layers bite. After revert, both are green again.

## Minor observations (not defects)

- The last-resort fallback (`datav1alpha1.Datasetkind` / `GroupVersion.String()`) in
  `datasetControllerOwnerReference` is unreachable in practice: `fluidScheme` registers all
  `datav1alpha1` types via `utilruntime.Must` at package init, so `apiutil.GVKForObject` on a
  `*datav1alpha1.Dataset` cannot fail. Keeping it matches issue #6140 proposal 3 verbatim, so
  this is per-spec defense in depth, not dead code to remove.
- `TestCheckMountPointBroken` in `pkg/utils` fails both on base and on the PR head in this
  environment (mount-point detection, unrelated to the PR). Pre-existing/environmental, not
  attributable to this PR.

## Continuing after the fix / on another machine

1. `git fetch https://github.com/cheyang/fluid.git verify/ownerref-converge-codex && git checkout verify/ownerref-converge-codex`
2. Re-run everything: `bash docs/verification/ownerref-converge/scripts/re-verify.sh` — it fetches the current PR head from `manifest.pr`, grafts the harness, and runs L1+L2. L2 needs envtest binaries (`setup-envtest use 1.31.0 -p path` or point `KUBEBUILDER_ASSETS` at an existing dir).
3. All tests are contract polarity: green = good. If the PR changes expected values (e.g. fallback semantics), update the golden table in `pkg/utils/dataset_runtime_ownerref_verify_test.go`.
4. `.last-reviewed` holds the reviewed head sha; advance it after each round.

Copy-paste kickoff for a fresh agent: "Check out verify/ownerref-converge-codex from
https://github.com/cheyang/fluid.git, run docs/verification/ownerref-converge/scripts/re-verify.sh,
and report per-claim Fixed/Still-broken per the polarity table in
docs/verification/ownerref-converge/README.md."
