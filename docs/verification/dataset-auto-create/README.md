# Verification harness — PR #5674 "feat(runtime): support dataset auto-create policy"

Reviewer: A (Claude). Branch: `verify/dataset-auto-create-claude` (based on PR head
`63f43e262a769691bb1458bef96aeb423d6aad63`, base `25531595e9233cb9340a3c544eb284b400b82d50`).
Production code is untouched — the diff against the PR head is harness-only
(`git status --porcelain pkg/ | grep -v _test.go` → empty).

## Premise (P0) — Confirmed

Issue #4518 (OPEN, feature request): "In some corner cases, users need to create Runtime
without pre-defining a Dataset resource."

Differential evidence (`results/base-premise-p0.out`): a Runtime annotated
`fluid.io/dataset-policy=auto-create` with no Dataset gets **no** Dataset created on the
base branch (it waits forever in the "No dataset can be bound to the runtime, waiting."
5s requeue loop); the same input auto-creates the Dataset on the PR head. The component
matches: the patch lands in the shared `RuntimeReconciler`
(`pkg/controllers/runtime_controller.go`) that every runtime controller uses, which is
where the dataset-missing wait lives. Note: the annotation API implemented
(`fluid.io/dataset-policy: auto-create`) differs from the one proposed in the issue
(`fluid.io/dataset-check-policy: "ignore"`) — a design divergence to surface, not a
component mismatch.

## Findings and observed-vs-expected table

| ID | Claim (condition → expected vs suspected) | Layer | Polarity | Verdict | Evidence |
| --- | --- | --- | --- | --- | --- |
| P0 | Feature absent on base (annotated runtime + no dataset → dataset created) | unit (base) | contract | **Confirmed** (differential: red on base, green on head) | `results/base-premise-p0.out` |
| F1 | JuiceFSRuntime + auto-create + empty auto-created Dataset → transform must return an error, suspected panic on `dataset.Spec.Mounts[0]` | unit | contract | **Confirmed** — panics `index out of range [0] with length 0` on head; also panics on base (pre-existing guard gap the PR now triggers) | `results/unit-f1-juicefs-panic-head.out`, `results/base-f1-attribution.out` |
| F4 | Runtime with deletionTimestamp + auto-create + dataset gone → must NOT create a new Dataset, suspected it does | unit | contract | **Confirmed** — a phantom Dataset is created during the deletion reconcile on head | `results/unit-f4-deletion-churn-head.out` (65 passed / 1 failed = the reproduction) |
| F5 | Dataset Create with `blockOwnerDeletion=true` ownerRef rejected for an identity with only the shipped runtime-controller ClusterRole permissions | integration (envtest, k8s 1.29) | contract (guard) | **Refuted** — the Create succeeds; RBAC enforced (negative control: same user cannot create a ConfigMap) | `results/integration-f5-rbac-envtest-head.out` |

### F1 mechanism (blocker)

`ReconcileInternal` auto-creates an empty Dataset (no `spec.mounts`), then proceeds:
step 7 owner-ref + finalizer, then step 9 `ReconcileRuntime` → `engine.Validate` (passes:
placement mode/owner UID come from the now-bound dataset) → `engine.Setup` →
`ShouldSetupMaster` (WorkerPhase None → yes) → `SetupMaster` → `installJuiceFS` →
`generateJuicefsValueFile` → `transform` → `j.genEdition(dataset.Spec.Mounts[0], ...)`
(`pkg/ddc/juicefs/transform.go:69`) — **no empty-mounts guard before the indexing**.
Fluid's managers do not set `RecoverPanic`, and controller-runtime v0.17.5 re-panics when
it is unset (`pkg/internal/controller/controller.go`), so this kills the juicefs
controller process → CrashLoopBackOff → cluster-wide JuiceFSRuntime reconciliation outage.
`transformFuse` has the "do not assign mount point" guard, but it runs *after* `genEdition`.
The same class exists in `pkg/ddc/jindofsx/transform.go:886` (fuse-only mode). EFC, Thin
and Jindo guard empty mounts explicitly.

Attribution: the unguarded index is pre-existing (reachable on base only if a user
hand-creates an empty-mounts Dataset); the PR makes the *controller itself* create the
empty Dataset and drive reconciliation into it for its documented primary scenario
("Create a JuiceFSRuntime ... with fluid.io/dataset-policy: auto-create"), so the
regression is introduced by this PR.

### F4 mechanism (major)

In `ReconcileInternal`, the dataset lookup/auto-create (step 5) runs **before** the
deletion-timestamp check (step 6). While a runtime is terminating, its auto-created
Dataset is removed by GC first (it is controller-owned by the runtime), so each deletion
reconcile re-creates a phantom Dataset, then unbinds it and waits for GC to remove it
again. If the create fails persistently, runtime deletion stalls in a 5s
requeue+event loop.

### F5 refutation detail

An envtest API server with RBAC enforcement, an identity holding exactly the shipped
`alluxioruntime-controller` ClusterRole permissions (create/get/... on `datasets`,
nothing on `alluxioruntimes/finalizers`), performing the exact
`SetControllerReference`-shaped Create: **succeeds**. So the "grant finalizers permission
or the feature is dead" concern does not reproduce on k8s 1.29 CRs.

## Harness-bites check

Each red contract test was checked against the proposed fix, then the fix was reverted
(production diff empty again):

- F1: guard `len(dataset.Spec.Mounts) == 0` before `genEdition` → test goes green
  (`results/harness-bites-f1-fix-applied.out`).
- F4: gate auto-create on `objectMeta.GetDeletionTimestamp().IsZero()` → full controller
  suite (incl. this test and the PR's own tests) green
  (`results/harness-bites-f4-fix-applied.out`).

## How to re-run

Unit layer (L1):

```bash
FLUID_UNIT_TEST=true go test ./pkg/ddc/juicefs/ ./pkg/controllers/ \
  -run 'TestTransformWithEmptyMountsDataset|TestControllers' -v
```

Integration layer (L2, envtest — needs `setup-envtest use 1.29.0`):

```bash
KUBEBUILDER_ASSETS=<path> FLUID_UNIT_TEST=true go test ./pkg/controllers/ \
  -run 'TestAutoCreateDatasetCreateRBACContract' -v
```

Everything, per finding, after the author pushes a fix:

```bash
bash docs/verification/dataset-auto-create/scripts/re-verify.sh   # auto-resolves current PR head
```

Expected after both fixes: F1 `TestTransformWithEmptyMountsDataset` green,
F4 spec green, F5 stays green (it is a guard for a refuted claim).

## Continuing after the fix

Graft this branch's harness onto the fixed code on any machine:

```bash
git clone https://github.com/cheyang/fluid.git && cd fluid
git checkout verify/dataset-auto-create-claude
bash docs/verification/dataset-auto-create/scripts/re-verify.sh   # or <fixed-ref>
```

Polarity table: both tracked findings are **contract** tests — they are red while the
bug is present and must go green when fixed. Nothing needs inverting. `F5` is a green
guard documenting the refutation; if it ever flips red on a newer k8s, that is a real
signal (the blockOwnerDeletion admission behavior changed), not noise.

Not covered by this harness (needs a live cluster): actual GC cascades for the
runtime↔dataset ownership cycle (L3 note in the manifest). The unit reproduction of the
panic is faithful to the real call chain up to `transform`, which is the exact function
that panics.
