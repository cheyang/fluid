# cacheruntime-immutable-spec-webhook — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6243
(head `98d5eac0975116cb263afed4f13c47aa707d9cad`, base `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`).

Layers run against the **code under review** (the PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `ValidateSpecUpdate` allowlist, `SyncComponentSpec` propagation, committed manifests | `go test ./pkg/ddc/cache/component/ ./pkg/webhook/handler/validating/cacheruntime/ -run 'TestSyncComponentSpecDoesNotPropagateImagePullPolicy\|TestValidateSpecUpdateAllowsImagePullPolicyChange\|TestCommittedWebhookManifestsIncludeValidatingWebhook'` |
| 2. Integration (envtest) | the real `SpecUpdateHandler` behind a real API server with the chart's ValidatingWebhookConfiguration rule | `go test ./pkg/webhook/handler/validating/cacheruntime/ -run TestValidatingWebhookAdmissionAgainstRealAPIServer` (envtest assets auto-discovered, or `export KUBEBUILDER_ASSETS=…`) |
| 3. Live | — skipped (unit + integration only, per the review setup) | `scripts/verify-manifests.sh` additionally reproduces the CI lint failure |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

Answered before the findings, and run against the **base** branch without the patch, because
the question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom | "the API server accepts the update and it is silently ignored, with no error shown to the user; `tieredStore`: the update is applied halfway. The quota in the runtime ConfigMap follows the spec, while the tmpfs `sizeLimit` of the Worker Pod keeps its creation-time value" (PR body) |
| Linked issue | none (the PR body states the problem itself) |
| Reported component | CacheRuntime controller sync (`CacheEngine.Sync` / `AdvancedStatefulSetManager.SyncComponentSpec`) |
| Patched component | validating admission webhook for CacheRuntime UPDATEs |
| Component match | Yes — the patch intercepts exactly the writes whose effect the report measures |
| **Verdict** | **Confirmed** |
| Evidence | `results/premise-base.out` — on base, a `spec.worker` change to tieredStore/nodeSelector/args is accepted, the runtime value ConfigMap picks up the new quota (4Gi) while the worker ASTS keeps its creation-time tmpfs sizeLimit (1Gi), nodeSelector and args |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | spec updates beyond replicas/runtimeVersion/resources are silently ignored / half-applied | 1 (base) | Confirmed | `results/premise-base.out` |
| F1 | the webhook denies the controller's own metadata-only updates for a CacheRuntime created without `spec.client` | 2 | Confirmed (red) | `results/f1-envtest-head.out` |
| F2 | the kubebuilder marker's generated manifest is not committed → CI `lint` fails | 1 | Confirmed (red) | `results/f2-unit-head.out`, `results/f2-manifests-head.out` |
| F3 | `runtimeVersion.imagePullPolicy` is allowed by the webhook but never synced | 1 | Confirmed (canary, green) | `results/f3-admission-head.out`, `results/f3-unit-head.out` |

## Per-finding detail

### F1 — blocker: defaulting asymmetry denies the controller's own writes

**Mechanism.** The CRD defaults `spec.client.cleanPolicy: OnRuntimeDeleted`, but
structural-schema defaulting only fills defaults inside objects that are *present*.
A manifest without a `client:` key (e.g. `test/gha-e2e/mooncake/cacheruntime.yaml`) is
stored with no client object. When the controller does a typed
Get → mutate metadata (ownerReference / finalizer:
`RuntimeReconciler.AddOwnerAndRequeue`, `AddFinalizerAndRequeue`) → full-object
Update, the typed client always serializes the zero `CacheRuntimeClientSpec` struct as
`"client":{}` — an object that *is* present — so the API server applies the cleanPolicy
default to the request body. Admission then compares oldObject (cleanPolicy `""`) with
the defaulted newObject (cleanPolicy `"OnRuntimeDeleted"`) and denies
`spec.client.cleanPolicy: Forbidden: field is immutable`.

**User-visible contract.** The CacheRuntime can never gain its ownerReference/finalizer:
the reconcile loop errors forever, the Dataset stays `NotBound`, and — on deletion — the
finalizer can never be removed either. This is exactly the kind-e2e failure of this PR
on all five k8s versions (job `kind-e2e-test`, testcase "mooncake cache runtime
(client-less topology) e2e": `timeout 600s waiting for dataset mooncake-demo to be
Bound`, controller log `Failed to add ownerreference ... spec.client.cleanPolicy:
Forbidden: field is immutable`).

**Harness.** `envtest_admission_test.go` runs the real handler behind a real API server
with the chart's rule. Subtest F1 (contract, red on head) is flanked by a control
(with-client CacheRuntime — green) and a guard (tieredStore change is denied — green,
proving the webhook is wired), so the red cannot be a wiring false-negative.
**Bites check:** with a demo fix applied (skip the comparison when old is the zero value
and new is the CRD default), the whole test goes green
(`results/f1-envtest-demo-fix-green.out`); the fix was then reverted — production code
is untouched on this branch.

**Fix sketch (for the author).** Any of:
- exempt controller identity (`req.UserInfo`) from the spec comparison;
- treat "old is zero, new is the CRD default" as unchanged for defaulted fields;
- drop the `+kubebuilder:default` on `cleanPolicy` and default it in the controller;
- compare against a re-defaulted old object.
Note the demo fix in the bites check is *minimal*, not necessarily the best choice —
a general solution must cover any field that gains a CRD default later.

### F2 — major: CI `lint` fails because the regenerated webhook manifest is not committed

`make update-crd` (the CI lint gate) regenerates `config/webhook/manifests.yaml` from
the new `+kubebuilder:webhook` marker in
`pkg/webhook/handler/validating/webhook.go`; the PR did not commit the output, so the
gate exits 1 (`CRD validation failed. Please use 'make update-crd' to keep CRDs
latest` — job `lint`). Verified differentially:
`results/f2-manifests-head.out` (regeneration produces `M
config/webhook/manifests.yaml`) vs `results/f2-manifests-base.out` (no-op).
`manifests_commit_test.go` encodes the same check as a unit contract test.
Fix: run `make manifests` and commit the result.

### F3 — minor: `runtimeVersion.imagePullPolicy` is treated as updatable but is never synced

`validateComponentCommonSpecUpdate` allows the whole master/worker `runtimeVersion`
(`spec_update.go`), but `AdvancedStatefulSetManager.updateImage` patches only
`container.Image` — the comment there even claims ImagePullPolicy is synced, which it
is not. So a `spec.master.runtimeVersion.imagePullPolicy` change passes admission and
is silently ignored — the same class of no-op update the PR exists to reject, limited
to one subfield. Both canaries are green on head (they document the gap):
`TestValidateSpecUpdateAllowsImagePullPolicyChange` (admission side) and
`TestSyncComponentSpecDoesNotPropagateImagePullPolicy` (sync side; also asserts the
image tag *is* synced, so the harness demonstrably exercises the path). Fix: either
make `imagePullPolicy` immutable too, or actually propagate it in `updateImage`; then
invert the canaries.

## Live run notes

Skipped per the review setup (unit + integration layers only). The kind-e2e CI logs
(job 132432994134) serve as the live-layer corroboration for F1: the identical denial
string appears in the cacheruntime-controller logs and the mooncake dataset never
reaches Bound, while the curvine testcase (whose manifest *has* a `client:` block and
whose new `verify_immutable_spec_update_rejected` passed) stayed green — matching the
mechanism above.

## Proposed fixes (NOT applied to production here)

- **F1**: see the sketch above; prefer a general treatment of CRD defaults, not a
  cleanPolicy-specific special case.
- **F2**: `make manifests` + commit.
- **F3**: decide whether imagePullPolicy is immutable or synced; keep the allowlist and
  the sync code in agreement.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cacheruntime-immutable-spec-webhook-claude` of the
reviewer's fork (`https://github.com/cheyang/fluid.git`), production code untouched, so
it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cacheruntime-immutable-spec-webhook-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cacheruntime-immutable-spec-webhook \
     pkg/ddc/cache/component/imagepullpolicy_sync_gap_test.go \
     pkg/ddc/cache/engine/sync_premise_p0_test.go \
     pkg/webhook/handler/validating/cacheruntime/envtest_admission_test.go \
     pkg/webhook/handler/validating/cacheruntime/spec_update_gap_test.go \
     pkg/webhook/handler/validating/cacheruntime/manifests_commit_test.go \
     pkg/webhook/handler/validating/cacheruntime/testdata
   ```
2. Prereqs: Layer 1 = Go toolchain only; Layer 2 = envtest binaries
   (`KUBEBUILDER_ASSETS`, or installed under
   `~/.local/share/kubebuilder-envtest/k8s/<version>/`); `verify-manifests.sh` =
   controller-gen v0.19.0 (auto-installed to a temp dir if absent).
3. Re-run:
   ```bash
   bash docs/verification/cacheruntime-immutable-spec-webhook/scripts/re-verify.sh   # resolves the current PR head itself
   ```
   or the three layer commands in the table above.
4. Read results via the polarity table: contract tests (F1, F2) should be GREEN;
   bug-canaries (F3) will be RED if the author makes imagePullPolicy immutable and
   must then be inverted.
5. The premise layer (`go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo
   focus 'P0 premise'`) is a symptom-documentation canary: it is expected to keep
   passing — the webhook prevents the updates, it does not change the engine sync.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cacheruntime-immutable-spec-webhook-claude
(remote https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6243 produced findings F1 (defaulting
asymmetry makes the webhook deny the controller's own metadata-only updates on
client-less CacheRuntimes), F2 (regenerated webhook manifest not committed, CI lint
fails), F3 (imagePullPolicy allowed but never synced); a harness reproduced them. The
PR is now fixed at <ref>. Read
docs/verification/cacheruntime-immutable-spec-webhook/README.md ("Continuing after the
fix") and follow it: graft the harness onto the fixed code, re-run all layers (envtest
assets via KUBEBUILDER_ASSETS), mind the polarity table (invert the F3 canaries if they
flip), run the harness-bites check, and report an observed-vs-expected table. Clean up
scoped test resources; do not run cluster-wide destructive actions.
```
