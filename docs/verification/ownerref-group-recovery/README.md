# Verification — PR #6206: recover group for group-less apiVersion in owner reference

- **Reviewer:** Claude (Reviewer A, debate pipeline)
- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6206
- **PR head:** `0daf2ba051fc25aa05dc68b2c42ea083f4f3c593` (round 2; round-1 head was `0fa3229416df66784dcf5b853dd636449d265a02`)
- **Base:** `e4a10bf49a1e8068d901e6f146ee4e435db7e785` (= merged PR #6199)
- **Branch:** `verify/ownerref-group-recovery-claude` (harness only; production code untouched)

## Premise (P0) — CONFIRMED

Issue #6205 (OPEN) claims: `GenerateOwnerReferenceFromObject` only recovers the
GVK from the scheme when `Kind` or `Version` is empty, so a group-less
`apiVersion` (e.g. `TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"}`) parses
with both non-empty and skips recovery, producing an ownerReference with
`apiVersion: "v1alpha1"` that GC and owner-based watches cannot resolve.

Reproduced on **base** `e4a10bf` (see `results/base-run.txt`):

```
--- FAIL: TestOwnerRefGroupRecoveryGrouplessAPIVersionWithKind
    F1: APIVersion = "v1alpha1", want "data.fluid.io/v1alpha1"
```

The consumer-side mechanism named by the issue exists in-tree:
`resolveControllerRef` (`pkg/utils/kubeclient/metadata.go:65`) parses the
controllerRef apiVersion and rejects it when `controllerRefGV.Group != group`,
so a group-less reference would never match its owner. The generated
`common.OwnerReference` is also rendered verbatim as `owner.apiVersion` into
`ownerReferences` on statefulsets/daemonsets/jobs/cronjobs across ~20 charts.

Component match: the issue names `pkg/utils/transformer/owner_reference.go`
exactly — the only production file the PR touches. Premise status:
**Confirmed**.

## Hypotheses → tests (polarity)

All tests below are **contract** tests (assert the intended behavior; red on
base = the reproduction; green on the PR head = fixed). Files are additive.

| id | claim | test | layer |
| --- | --- | --- | --- |
| P0 | group-less apiVersion with kind set → group recovered | `TestOwnerRefGroupRecoveryGrouplessAPIVersionWithKind` (`pkg/utils/transformer/owner_reference_group_recovery_test.go`) | unit |
| F1 | group-less apiVersion with kind *missing* → kind AND group recovered (not covered by the PR's own table entry) | `TestOwnerRefGroupRecoveryGrouplessAPIVersionWithoutKind` (same file) | unit |
| F3 | core k8s type with a complete group-less TypeMeta (`apiVersion: "v1"`) → output unchanged, recovery failure is log-only (no regression from the widened trigger condition) | `TestOwnerRefGroupRecoveryCoreTypeCompleteTypeMetaUnchanged` (same file) | unit |
| F4 | consumer path: `datasetControllerOwnerReference` (pkg/utils/dataset_runtime.go, converged in #6199) with a group-less apiVersion dataset → full `data.fluid.io/v1alpha1` on the Runtime ownerReference | `TestDatasetControllerOwnerReferenceGrouplessAPIVersion` (`pkg/utils/dataset_runtime_groupless_ownerref_test.go`) | integration (fake-client package `pkg/utils`) |

## Observed vs expected

| finding | base e4a10bf | head 0fa32294 | verdict |
| --- | --- | --- | --- |
| P0 | FAIL (`APIVersion = "v1alpha1"`) | PASS | confirmed, fixed by PR |
| F1 | FAIL (kind recovered, group still missing) | PASS | confirmed, fixed by PR |
| F3 | PASS | PASS | no regression from widened trigger |
| F4 | FAIL (`APIVersion = "v1alpha1"` on Runtime ref) | PASS | confirmed, fixed by PR |

Harness-bites check (`results/bites-check-reverted.txt`): with the production
fix reverted on the head worktree (only `owner_reference.go` checked out from
base), F1/F2 go red again; after restoring, green; production diff empty.

Broader sweep on head (`results/head-pkg-utils.txt`):
`go test ./pkg/utils/...` — transformer, dataset-runtime and all sibling
packages pass. Two failures occur (`pkg/utils` `TestCheckMountPointBroken`,
`pkg/utils/helm` `TestHelm`) but they fail **identically on base**
(environment: no suitable mount/root; local helm binary version) and are in
packages this PR does not touch — not PR-attributed. `go build ./pkg/...`
clean. `gofmt` clean.

envtest/live layers: not run (no kubebuilder-envtest assets on this machine;
debate policy restricts stage ② to unit + integration).

## Round 2 — delta `0fa3229..0daf2ba` (evidence: `results/round2/`)

The author pushed two commits addressing all three round-1 SHOULD items:

1. `75851117` — gate the recovery-failure error log on the emitted reference
   being actually incomplete (`len(gvk.Kind) == 0 || len(gvk.Version) == 0`),
   and update the function header comment (kind/version/**group** fall back).
2. `0daf2ba0` — lift both missing tests into the PR's own suite: the
   kind-missing + group-less table entry in
   `owner_reference_test.go:259`, and the group-less case in
   `TestDatasetControllerOwnerReference` (`dataset_runtime_test.go:316`).

New harness contracts for the log-gating change (added this round,
`pkg/utils/transformer/owner_reference_group_recovery_test.go`):

| id | claim | test |
| --- | --- | --- |
| F2-log-fix | a complete core-group TypeMeta (`apiVersion: "v1"`) emits **no** recovery-failure error log | `TestOwnerRefGroupRecoveryCoreTypeCompleteTypeMetaNoRecoveryErrorLog` |
| F2-log-keep | a genuinely incomplete TypeMeta (kind empty) **still** logs the recovery failure | `TestOwnerRefGroupRecoveryIncompleteTypeMetaStillLogs` |

| contract | base e4a10bf | prev head 0fa32294 | head 0daf2ba | verdict |
| --- | --- | --- | --- | --- |
| F2-log-fix | PASS (branch never entered) | **FAIL** (misleading error logged) | PASS | log-noise fixed by `75851117` |
| F2-log-keep | PASS | PASS | PASS | diagnostic preserved |
| P0/F1/F3/F4 (round 1) | as above | PASS | PASS | no regression from the delta |

Bites check for the new contract: on the round-1 head `0fa32294`
(the verify-branch base) `F2-log-fix` goes red — the misleading
`failed to recover the GroupVersionKind` error fires for `ConfigMap/v1` —
which is the reproduction; green on `0daf2ba`.

The PR's own new tests are genuine differential tests: grafted onto base
`e4a10bf` (`results/round2/base-pr-tests-*.txt`), both new entries fail
(transformer: 33 passed / 2 failed; consumer:
`expected APIVersion data.fluid.io/v1alpha1, got v1alpha1`) and pass on the
head (`results/round2/head-pr-tests-*.txt`). Full suites on head:
`go test ./pkg/utils/transformer/` ok (35 specs), consumer suite PASS
(7 subtests), `go build ./pkg/...` clean, `gofmt` clean.

CI on head `0daf2ba` (GitHub statusCheckRollup, read at round start):
unittest, lint, staticcheck, build (2 arch), kind-e2e across 6 k8s versions,
backward-compat across 6 versions, CodeQL, SonarCloud, DCO, codecov, tide —
all SUCCESS.

## Review verdict summary

No blocker/major findings in either round. The fix is minimal, correct, and
consistent with the scheme-recovery design introduced by #6199. Round 2
addressed every round-1 SHOULD item: both missing tests now live in the PR's
own suite, the header comment names the group, and the misleading log for
complete core-group types is gated (differentially verified here). The
remaining round-1 question (no known production producer of the group-less
shape) is pre-existing contract hardening, not a defect.

## Continuing after the fix

```bash
git clone https://github.com/cheyang/fluid.git && cd fluid
git checkout verify/ownerref-group-recovery-claude
bash docs/verification/ownerref-group-recovery/scripts/re-verify.sh   # auto-resolves PR head
```

Polarity table for future rounds: all six tests are contract tests — after a
developer fix they must PASS on the PR head. If any goes FAIL again, the
finding is Still-broken. No canaries, nothing to invert.

Kickoff prompt for a fresh agent: "You are resuming the review of
fluid-cloudnative/fluid PR #6206 from the branch
`verify/ownerref-group-recovery-claude` in the cheyang/fluid fork. Read
`docs/verification/ownerref-group-recovery/README.md`, run
`scripts/re-verify.sh`, report per finding Fixed / Still-broken / Partial /
Harness-update, review the `.last-reviewed..head` delta, then advance
`.last-reviewed` and commit."
