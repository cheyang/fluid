# Verification — PR #6206: recover group for group-less apiVersion in owner reference

- **Reviewer:** Claude (Reviewer A, debate pipeline)
- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6206
- **PR head:** `0fa3229416df66784dcf5b853dd636449d265a02`
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

## Review verdict summary

No blocker/major findings. The fix is minimal, correct, and consistent with
the scheme-recovery design introduced by #6199; the PR's own table entry plus
this harness cover the contract. Minor gaps: F1-style combined case and the
F4 consumer case are not in the PR's tests (covered here), and the
function-level comment still says only "kind and the apiVersion" fall back.

## Continuing after the fix

```bash
git clone https://github.com/cheyang/fluid.git && cd fluid
git checkout verify/ownerref-group-recovery-claude
bash docs/verification/ownerref-group-recovery/scripts/re-verify.sh   # auto-resolves PR head
```

Polarity table for future rounds: all four tests are contract tests — after a
developer fix they must PASS on the PR head. If any goes FAIL again, the
finding is Still-broken. No canaries, nothing to invert.

Kickoff prompt for a fresh agent: "You are resuming the review of
fluid-cloudnative/fluid PR #6206 from the branch
`verify/ownerref-group-recovery-claude` in the cheyang/fluid fork. Read
`docs/verification/ownerref-group-recovery/README.md`, run
`scripts/re-verify.sh`, report per finding Fixed / Still-broken / Partial /
Harness-update, review the `.last-reviewed..head` delta, then advance
`.last-reviewed` and commit."
