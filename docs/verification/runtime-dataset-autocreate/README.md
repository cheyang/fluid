# Verification harness — PR #5674 "feat(runtime): support dataset auto-create policy"

Reviewer: Codex (Reviewer B). Branch: `verify/runtime-dataset-autocreate-codex`, based on PR head
`63f43e262a769691bb1458bef96aeb423d6aad63` (base `25531595e9233cb9340a3c544eb284b400b82d50`).
**Production code is untouched** — the branch adds only test files and this documentation.

## Premise (P0) — Confirmed

Issue #4518 (OPEN): "Support creating Runtime without Dataset dependency" — users must currently
pre-define a Dataset; the runtime controller otherwise waits forever. Reproduced on base
`25531595`: an AlluxioRuntime without a Dataset requeues every 5s with "No dataset can be bound
to the runtime, waiting." and nothing is created; the `fluid.io/dataset-policy: auto-create`
annotation literal is ignored on base (`results/layer0-base-premise.txt`; base-only test file in
`premise/`). On head, the annotation auto-creates a stub Dataset (feature test green).

Note: the issue proposed `fluid.io/dataset-check-policy: "ignore"` (skip binding entirely); the PR
implements auto-create of a stub Dataset instead. Same component (`RuntimeReconciler`), different
mechanism — see finding F6.

## Findings and harness

| ID | Sev | Polarity | Layer | Test anchor | State on head |
|----|-----|-----|-------|-------------|----------------|
| P0 | premise | contract | unit(base) | `runtime without the annotation still waits and creates no dataset` + base-only premise file | limitation confirmed on base; compat green on head |
| F0 | feature | contract | unit | `annotated runtime auto-creates a stub dataset bound to itself` | green (feature works at unit level) |
| F1 | major | contract | unit | `terminating runtime with the annotation must not auto-create a dataset` | **RED — bug reproduced** |
| F2 | major | contract | unit | `engine ClusterRoles with dataset access grant owner finalizers update` | **RED — gap reproduced** (static RBAC) |
| F3 | minor | canary | integration (envtest) | `I2 [canary]: first auto-create reconcile errors on empty-GVK ownerRef, second heals` | green (bug present); flips red when fixed |
| F5 | minor | characterization | unit | `same-named foreign dataset is adopted via the normal bind path` | green (documents that the AlreadyExists guard is cosmetic) |

### F1 — auto-create fires for terminating runtimes (major, PR-introduced)

`ReconcileInternal` runs step 5 (GetDataset + auto-create) before step 6 (deletion check).
A runtime with the annotation that is terminating while its Dataset is missing gets a brand-new
Dataset created for it, owned by the terminating runtime. GC eventually collects the orphan, but
creating resources for a terminating object is wrong (and with foreground deletion it transiently
adds a dependent the GC must wait on). Proven by the red contract test; a one-line guard
(`&& objectMeta.GetDeletionTimestamp().IsZero()`) flips it green with all other specs passing
(harness-bites verified, then reverted).

### F2 — blockOwnerDeletion=true without `<engine>runtimes/finalizers: update` (major, conditional)

`controllerutil.SetControllerReference` sets `controller=true, blockOwnerDeletion=true`. On clusters
running the `OwnerReferencesPermissionEnforcement` admission plugin (**default-off upstream**
— verified against kubernetes/kubernetes v1.32.1 `pkg/kubeapiserver/options/plugins.go`; enabled on
hardened distros, e.g. OpenShift), creating such a Dataset requires `update` on the OWNER's
`<engine>runtimes/finalizers` subresource (`plugin/pkg/admission/gc/gc_admission.go`: rejects with
"cannot set blockOwnerDeletion if an ownerReference refers to a resource you can't set finalizers
on"). No engine controller ClusterRole grants it (all six: alluxio/efc/jindo/juicefs/thin/vineyard —
`results/rbac-finalizers-check.txt`, re-check: `scripts/check-rbac-finalizers.sh`). On those clusters
auto-create fails with 403 and requeues every 5s forever. Fix: add `<engine>runtimes/finalizers
update` per engine role (+ kubebuilder marker), or drop blockOwnerDeletion.

### F3 — first auto-create reconcile always errors on the empty-GVK ownerRef (minor, self-healing)

envtest with the production wiring (cache-backed manager client, k8s 1.31) shows
(`results/layer2-envtest.txt`):

- Reconcile 1: stub Dataset created (valid ownerRef on the Dataset — scheme GVK lookup works), but
  the in-memory object returned by `client.Create` has TypeMeta stripped, so `AddOwnerAndRequeue`
  writes `ownerReferences: [{apiVersion: "", kind: ""}]` onto the runtime and the API server
  rejects the update: `metadata.ownerReferences.apiVersion: Invalid value: "": version must not
  be empty`. Reconcile returns error.
- Reconcile 2 (cache caught up): `GetDataset` returns the informer-cached object with TypeMeta
  populated, ownerRef write succeeds, binding proceeds.

So the impact is one guaranteed error + error log per auto-create, then self-heal — but every
fake-client unit test (incl. the PR's own) is blind to it. Fix: after a successful Create in
`ensureDatasetForRuntime`, `dataset.SetGroupVersionKind(datav1alpha1.GroupVersion.WithKind(datav1alpha1.Datasetkind))`
(or reuse the scheme-backed owner-ref helper from #6139). Canary verified to flip when this is
applied (harness-bites), then reverted.

Related established context: #6138/#6139/#6140/#6199 fixed the same empty-GVK owner-ref class for
the dataset→runtime direction (`pkg/utils/transformer/owner_reference.go`); this PR introduces a
new instance of it in the runtime→dataset direction.

### F5 — the AlreadyExists adoption guard is cosmetic (minor)

The guard refuses to adopt a same-named Dataset that is not owned/annotated, but only inside the
stale-cache race window: on the very next reconcile `GetDataset` finds the same Dataset and the
normal bind path adopts it anyway (`AddOwnerAndRequeue` makes the runtime owned by it — green
characterization test). The comment ("must not be silently taken over") overstates the protection;
either drop the guard or move the check into the main bind path.

## How to run

```bash
# L1 unit (fake client) — no env var needed
go test ./pkg/controllers/ -run TestControllers -count=1

# L2 integration (envtest, real API server)
VERIFY_PR5674_ENVTEST=1 KUBEBUILDER_ASSETS=$HOME/.local/share/kubebuilder-envtest/k8s/1.31.0-linux-amd64 \
  go test ./pkg/controllers/ -run TestControllers -count=1

# F2 static tripwire
bash docs/verification/runtime-dataset-autocreate/scripts/check-rbac-finalizers.sh

# Full re-verify against a fixed ref (or current PR head if no arg)
bash docs/verification/runtime-dataset-autocreate/scripts/re-verify.sh [fixed-ref]
```

L2 requires envtest binaries (`setup-envtest` / `~/.local/share/kubebuilder-envtest/k8s/<ver>` or
`KUBEBUILDER_ASSETS`). Without `VERIFY_PR5674_ENVTEST=1` the envtest specs skip and plain
`go test ./pkg/controllers` is unaffected.

## Observed vs expected (head 63f43e26)

- L1: 69 passed / 2 failed — failures are exactly the F1 and F2 contract tests (`results/layer1-unit.txt`).
- L2: 71 passed / 1 failed (+2 skipped without the env var on the L1 run) — the failure is F1; the
  F3 canary passes by documenting the transient error (`results/layer2-envtest.txt`).
- Base premise: all green — limitation confirmed, annotation ignored on base
  (`results/layer0-base-premise.txt`).

## Continuing after the fix

Expected flips when fixed: F1 contract → green once the auto-create branch is guarded by
`deletionTimestamp.IsZero()`; F2 contract → green once engine roles carry
`<engine>runtimes/finalizers: update` (or blockOwnerDeletion is dropped); F3 canary → flips RED
once the created Dataset's GVK is restored — invert the r1Err assertion (or promote to a contract)
when that lands. `.last-reviewed` = 63f43e262a769691bb1458bef96aeb423d6aad63.
