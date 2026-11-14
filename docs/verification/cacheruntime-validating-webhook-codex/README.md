# Verification harness — PR #6243 (reviewer: Codex)

PR: https://github.com/fluid-cloudnative/fluid/pull/6243
"feat(webhook): reject CacheRuntime updates to fields that cannot be applied in place"
Reviewed head: 98d5eac0975116cb263afed4f13c47aa707d9cad (base ff99142f9ec5c32a0c9166de023cb8936f0e5ae5)
Branch: verify/cacheruntime-validating-webhook-codex (harness only; production code untouched)

## P0 — premise (runs against base): CONFIRMED

Claim (PR body): only master/worker `replicas`, `runtimeVersion`, `resources` are
synced in place; other spec updates are silently ignored, and `tieredStore` is
applied halfway (ConfigMap quota follows the spec, tmpfs `sizeLimit` does not).

Code-path evidence on base ff99142f:

- `pkg/ddc/cache/engine/sync.go:203-290` (`syncRuntimeSpec`): builds
  `component.ComponentSpec{Version, Resources, Replicas}` only, for master and
  worker only.
- `pkg/ddc/cache/component/advanced_statefulset_manager.go:199`
  (`SyncComponentSpec`): patches replicas, image and resources — nothing else.
- `pkg/ddc/cache/component/daemonset_manager.go:157-159`: client `SyncComponentSpec`
  returns "not supported" and is never called.
- `pkg/ddc/cache/engine/cm.go:159-190`: the runtime ConfigMap (incl.
  `TieredStoreLevels` from the *current* spec) is regenerated every reconcile,
  while the worker pod's tmpfs `sizeLimit` is set once at creation → tieredStore
  updates are indeed half-applied.
- Base had no validating webhook (`register.go` had `addHandlers(validating.HandlerMap)`
  commented out), so such updates were accepted.

The component matches: the PR installs a ValidatingWebhookConfiguration for
`cacheruntimes` UPDATE — the component that accepts-and-ignores those updates.

## Findings

| ID | Severity | Layer | Polarity | Status on head 98d5eac | Evidence |
|----|----------|-------|----------|--------|----------|
| F1 | blocker | unit (L1) + envtest (L2) + CI e2e | contract | CONFIRMED (tests red on head) | results/l1-unit.out, results/l2-envtest.out, results/ci-kind-e2e.out |
| F2 | blocker | script/L1 | contract | CONFIRMED (test red on head) | results/f2-check.out, results/l2-manifests-dirty.diff, results/ci-lint.out |

### F1 [blocker] webhook denies the controller's own metadata-only updates of client-less CacheRuntimes

Claim: for a CacheRuntime whose **stored** form has no `spec.client` (any
client-less topology applied from YAML, e.g. `test/gha-e2e/mooncake`), every
full-object Update — including the CacheRuntime controller's own finalizer and
owner-reference updates — is denied with `spec.client.cleanPolicy: Forbidden:
field is immutable`, so the runtime never reconciles (dataset stuck NotBound).

Mechanism (each step verified):

1. Stored form has no `spec.client` — CRD defaulting does not materialize
   absent parent objects. (L2 envtest, "fact 1".)
2. A typed Go client (the fluid controller) marshals the runtime with a
   materialized `client` object (encoding/json never omits zero-value structs).
   (L1 model assertion; payload captured in results/l1-unit.out.)
3. The API server applies CRD defaulting to the decoded update payload:
   `spec.client.cleanPolicy` gains `default: OnRuntimeDeleted`
   (charts/fluid/fluid/crds/data.fluid.io_cacheruntimes.yaml:46). The admission
   new object therefore differs from the old in an "immutable" field.
   (L2 envtest, "fact 2": after a typed metadata-only Update the stored object
   gained `spec.client.cleanPolicy="OnRuntimeDeleted"`.)
4. `ValidateSpecUpdate` forbids it. Observed: denial message identical to the
   CI controller log.

CI evidence (results/ci-kind-e2e.out): kind-e2e fails on **all five** Kubernetes
versions (v1.22.17–v1.33.2); the mooncake e2e times out after 600s with the
CacheRuntime controller logging:

```
Failed to add ownerreference ... admission webhook "update.cacheruntime.fluid.io"
denied the request: ... spec.client.cleanPolicy: Forbidden: field is immutable
```

Blast radius beyond mooncake: any user who created a CacheRuntime without a
`spec.client` section can no longer have it reconciled; the same phantom diff
also rejects user-driven full-object writes (`kubectl replace`, Go clients) that
only touch metadata/annotations. `kubectl patch`/`annotate` (merge patch) do not
materialize `client`, which is why the curvine e2e's annotation check passed.

Fix direction (verified by harness-bites check, then reverted): treat the
API-server-default materialization as unchanged, e.g. tolerate old `""` → new
`OnRuntimeDeleted` for `spec.client.cleanPolicy` (or normalize defaults before
comparison). A real change (`OnRuntimeDeleted`/`""` → `OnDemand`) must stay
denied — guarded by TestVerifyPR6243ClientlessCleanPolicyStillGuarded.

### F2 [blocker] config/webhook/manifests.yaml not regenerated (lint CI fails)

The PR adds a `+kubebuilder:webhook` marker in
`pkg/webhook/handler/validating/webhook.go` but does not commit the regenerated
`config/webhook/manifests.yaml`. CI lint runs `make update-crd` and fails:
`M config/webhook/manifests.yaml` → "CRD validation failed". Reproduced locally:
`make manifests` produces exactly the validating-webhook entry (see
results/l2-manifests-dirty.diff). Fix: run `make update-crd` and commit.

## How to run

Prereqs: go 1.26+, and for L2 a kubebuilder envtest asset dir
(KUBEBUILDER_ASSETS, or ~/.local/share/kubebuilder-envtest/k8s/* — auto-detected).

```bash
# L1 (unit): F1 handler contract tests + F2 manifests test — all three F1/F2
# contract tests FAIL on the reviewed head (bug present), pass once fixed.
go test ./pkg/webhook/handler/validating/cacheruntime/ -run 'TestVerifyPR6243' -v

# L2 (integration, real API server via envtest): proves the defaulting
# asymmetry and composes it with the validator.
go test ./pkg/webhook/handler/validating/cacheruntime/defaultingtest/ -v

# F2 cheap check (same as the CI lint gate's essence):
bash docs/verification/cacheruntime-validating-webhook-codex/scripts/check-webhook-manifests.sh

# Whole harness against the current PR head (auto-fetches it):
bash docs/verification/cacheruntime-validating-webhook-codex/scripts/re-verify.sh
```

## Harness-bites check (done 2026-11-15)

Temporarily applied a candidate F1 fix (tolerate `""` → `OnRuntimeDeleted` for
`spec.client.cleanPolicy` in `ValidateSpecUpdate`): L1 and L2 contract tests
flipped green; the anti-overfix guard (real `OnDemand` change) still failed
closed, i.e. still denied. Reverted; production diff empty again.

## Continuing after the fix / on another machine

1. `git fetch https://github.com/cheyang/fluid.git verify/cacheruntime-validating-webhook-codex`
   and check it out (harness is self-contained on the branch).
2. `bash docs/verification/cacheruntime-validating-webhook-codex/scripts/re-verify.sh`
   — fetches the current PR head, grafts the harness, runs L1+L2 and reports
   per finding Fixed / Still-broken. All findings are contract polarity: green
   means fixed.
3. Advance `.last-reviewed` after the round.
