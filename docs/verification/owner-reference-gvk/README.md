# Verification — owner reference GVK recovery (fluid PR #6139)

- **PR under review:** https://github.com/fluid-cloudnative/fluid/pull/6139
- **PR head verified:** `ee7e5ffd` ("fix(transformer): recover the GroupVersionKind of an owner whose TypeMeta is empty")
- **True merge base:** `44cfae9fb` (note: `origin/master` at `e18a6d08` is *not* an ancestor of the PR head)
- **Code under review:** `pkg/utils/transformer/owner_reference.go`
- **Production code modified by this harness:** none. The only additive file is
  `pkg/utils/transformer/owner_reference_verify_test.go`.

## What the PR does

`GenerateOwnerReferenceFromObject` used to build the owner `APIVersion` as
`Group + "/" + Version` unconditionally, so an object handed back by a typed client with an
empty `TypeMeta` produced `APIVersion: "/"` and an empty `Kind`. The PR adds a package-level
`fluidScheme` and falls back to `apiutil.GVKForObject` when `gvk.Empty()`:

```go
gvk := obj.GetObjectKind().GroupVersionKind()
if gvk.Empty() {
    if resolved, err := apiutil.GVKForObject(obj, fluidScheme); err == nil {
        gvk = resolved
    }
}
```

The value flows into Helm values as `common.OwnerReference` and is rendered as a literal
`ownerReferences` entry by `charts/*/templates/**`, so a malformed `apiVersion`/`kind` is
genuinely visible to the API server. The fix is real and effective for the fully-empty case.

## Observed vs expected

Layer: **L1 unit** (`pkg/utils/transformer`). No integration/live layer was run — see
"Layers not run" below.

| id | claim | file:line | sev | polarity | PR head `ee7e5ffd` | merge base `44cfae9fb` | verdict |
|----|-------|-----------|-----|----------|--------------------|------------------------|---------|
| H0 | Fully-empty `TypeMeta` must be recovered from the scheme | `owner_reference.go:39-44` | high | contract | **PASS** | **FAIL** (`APIVersion:"/"`, `Kind:""`) | Fix works — bug it targets was real |
| H1 | Partial `TypeMeta` skips recovery entirely | `owner_reference.go:40` | medium | contract | **FAIL** | FAIL | **Confirmed real, still present** |
| H1c | Canary pinning the actual partial-`TypeMeta` output | — | info | canary | PASS | FAIL | Documents today's behavior |
| H1m | Upstream `Empty()` predicate semantics | `vendor/.../group_version.go:149` | info | contract | PASS | PASS | Mechanism audited |
| H2 | `GVKForObject` error swallowed with zero diagnostics | `owner_reference.go:41` | low | contract | **FAIL** (0 log records) | FAIL | **Confirmed real** |
| H2c | Canary pinning the unusable ref for an unregistered type | — | info | canary | PASS | FAIL (`"/"`) | Documents today's behavior |
| H2b | Function cannot signal failure at all (no `error` return) | `owner_reference.go:38` | low | contract | Confirmed by inspection | same | **Pre-existing design limit** (`mutation` layer, not auto-runnable) |
| H3 | `BlockOwnerDeletion:false` with `Controller:true` | `owner_reference.go:52-53` | info | canary | PASS | **PASS (identical)** | **Pre-existing — NOT a regression of this PR** |

### H1 — the actual observed values (correcting the stage ① prediction)

Stage ① predicted `APIVersion: ""` / `Kind: "DataLoad"`. That is right for the Kind-only case,
but the failure is asymmetric and the reverse case behaves differently. Observed on PR head:

```
H1 observed: input TypeMeta{Kind:"DataLoad" APIVersion:""}               -> OwnerReference{Kind:"DataLoad" APIVersion:""}
H1 observed: input TypeMeta{Kind:"" APIVersion:"data.fluid.io/v1alpha1"} -> OwnerReference{Kind:"" APIVersion:"data.fluid.io/v1alpha1"}
H1 observed: input TypeMeta{Kind:"" APIVersion:"v1alpha1"}               -> OwnerReference{Kind:"" APIVersion:"v1alpha1"}
```

Same three inputs on the merge base (note `"/"` and `"/v1alpha1"`, which the PR *did* change):

```
-> OwnerReference{Kind:"DataLoad" APIVersion:"/"}
-> OwnerReference{Kind:"" APIVersion:"data.fluid.io/v1alpha1"}
-> OwnerReference{Kind:"" APIVersion:"/v1alpha1"}
```

Root cause, from `vendor/k8s.io/apimachinery/pkg/runtime/schema/group_version.go:149-151`:

```go
func (gvk GroupVersionKind) Empty() bool {
	return len(gvk.Group) == 0 && len(gvk.Version) == 0 && len(gvk.Kind) == 0
}
```

`Empty()` needs **all three** empty, so a `TypeMeta` with any one field set bypasses the new
recovery block. And `GroupVersion.String()` returns just `Version` when `Group` is empty
(same file, line 178-183), which is why a Kind-only `TypeMeta` yields `APIVersion: ""` rather
than the `"/"` one might expect.

**Severity judgment:** medium, not high. All 16 production call sites pass concrete Fluid
`v1alpha1` structs; a partially-filled `TypeMeta` is not produced by the typed client path
those callers use (it hands back either a fully-populated or a fully-empty `TypeMeta`). H1 is
a latent robustness gap in a helper that is now advertised as GVK-recovering, not a live
production break. It is worth fixing because the new code's stated purpose is exactly to
guarantee a well-formed ownerReference.

### H2 — silent degradation, confirmed

```
H2 precondition: apiutil.GVKForObject returns an error that the caller drops:
    no kind is registered for the type v1.ConfigMap in scheme "..."
H2 observed: unregistered *v1.ConfigMap -> OwnerReference{Kind:"" APIVersion:"" Enabled:true Controller:true}; log records captured=0
```

The test installs a capturing `logr` sink as the controller-runtime global logger and asserts
at least one record is emitted. Zero were. Note the returned struct still has `Enabled:true`
and `Controller:true`, so a caller has no way to tell a usable reference from an unusable one.

**Severity judgment:** low. Every one of the 16 production call sites passes a type registered
via `SchemeBuilder.Register`, so the error branch is unreachable in production today. This is a
maintainability/diagnosability issue that would bite whoever next passes a non-Fluid type.

### H3 — pre-existing, reported as context only

`BlockOwnerDeletion:false` with `Controller:true` is byte-identical on the merge base and on
the PR head (`TestVerifyH3Canary_...` PASSES on both refs). It is **not** a regression
introduced by this PR and should not be raised as a finding against it.

## Harness-bites check (Step 4) — passed

The proposed fix (widen the predicate to `gvk.Kind == "" || gvk.Version == ""`, and log the
error instead of discarding it) was applied to production code temporarily, then reverted;
`git diff pr-6139` is empty afterwards.

| test | polarity | PR head | proposed fix | discriminates? |
|------|----------|---------|--------------|----------------|
| `TestVerifyH0_...` | contract | PASS | PASS | yes (FAILs on merge base) |
| `TestVerifyH1_PartialTypeMetaStillYieldsMalformedRef` | contract | **FAIL** | **PASS** | yes |
| `TestVerifyH1Canary_...` | canary | PASS | **FAIL (flipped)** | yes |
| `TestVerifyH1_EmptyPredicateSemantics` | contract | PASS | PASS | n/a (documents upstream) |
| `TestVerifyH2_UnregisteredTypeLogsNoDiagnostic` | contract | **FAIL** (0 records) | **PASS** (1 record) | yes |
| `TestVerifyH2Canary_...` | canary | PASS | PASS | correct — a log does not make the ref usable |
| `TestVerifyH3Canary_...` | canary | PASS | PASS | yes (pre-existing) |
| `TestTransformer` (the PR's own Ginkgo suite) | — | PASS | PASS | fix causes no regression |

Raw output: `results/unit-prhead.out`, `results/unit-mergebase.out`, `results/unit-proposedfix.out`.

## How to run

```bash
# L1 unit layer (the primary and only automated layer)
go test -p 1 -count=1 -v ./pkg/utils/transformer/ -run 'TestVerify'

# Re-verify against the current PR head, with polarity applied
bash docs/verification/owner-reference-gvk/scripts/re-verify.sh
```

`re-verify.sh` refuses to run on a dirty tree (it does `git checkout -f`). It resolves the PR
head from `verify-manifest.json`'s `pr` field, so no sha needs to be typed.

### H2b (mutation-only)

`H2b` sits on a deliberately **non-runnable `mutation` layer** so `re-verify.sh` reports it as
`SKIPPED`/`HARNESS-UPDATE` rather than falsely reporting `FIXED`. Verify by inspection:

```bash
git grep -n 'func GenerateOwnerReferenceFromObject' -- pkg/utils/transformer/
```

The claim is closed only when the signature gains a failure channel (an `error` return, or a
logger/recorder parameter) — no unit spec can prove that, which is why it is not on the unit
layer.

## Layers not run (stated honestly)

- **L2 integration (envtest): NOT built.** Per the review brief, an envtest harness was only to
  be used if one already reached this code. It does not: the two existing envtest suites
  (`pkg/controllers/v1alpha1/alluxio/suite_test.go`,
  `pkg/controllers/v1alpha1/dataprocess/suite_test.go`) are scaffolded bootstraps that never
  call `GenerateOwnerReferenceFromObject`. Building one from scratch was out of scope.
  *What would settle it:* an envtest that renders a chart's child object with the malformed ref
  and confirms the API server's rejection message. This would upgrade H1 from "malformed value"
  to "API server rejects it", which is currently an inference from Kubernetes validation rules
  rather than something observed here.
- **L3 live: NOT run, by instruction.** The only kubeconfig available targets a *shared, real*
  3-node ACK cluster; the brief forbids any state-mutating `kubectl`. See `liveNote` in the
  manifest for the exact signal to check on a disposable cluster.

## Proposed fix (for the PR author — not applied here)

```go
gvk := obj.GetObjectKind().GroupVersionKind()
// gvk.Empty() is true only when Group AND Version AND Kind are all empty, so a
// partially populated TypeMeta would skip recovery and still yield a reference the
// API server rejects.
if gvk.Kind == "" || gvk.Version == "" {
	resolved, err := apiutil.GVKForObject(obj, fluidScheme)
	if err != nil {
		// Do not degrade silently: the caller cannot distinguish a usable
		// ownerReference from an unusable one.
		log.Error(err, "failed to resolve the GroupVersionKind of the owner object",
			"name", obj.GetName(), "namespace", obj.GetNamespace(), "type", fmt.Sprintf("%T", obj))
	} else {
		gvk = resolved
	}
}
```

Both H1 and H2 are addressed by this; H2b (no failure channel in the signature) is not, and is
arguably fine to leave as-is given all real callers pass registered types.

## Continuing after the fix lands

1. `git fetch` the verification branch and check it out on any machine (L1 is deterministic —
   no envtest assets, no cluster, no credentials needed).
2. `bash docs/verification/owner-reference-gvk/scripts/re-verify.sh` — it fetches the current
   PR head from the manifest's `pr` URL, grafts the harness on, and prints a per-finding verdict.
3. Expected on a correct fix: `H0`/`H1`/`H2`/`H1m` = `FIXED`; `H1c` = `FIXED(flipped)`;
   `H2c` = `STILL-PRESENT` (correct — a log does not make the ref usable; only a signature
   change would flip it); `H3` = `STILL-PRESENT` (correct — pre-existing and intentionally
   unchanged); `H2b` = `SKIPPED` (non-runnable layer, verify by inspection).
4. **When `H1c` flips to fail, invert or delete it** — that is the signal H1 was actually fixed.
5. Advance the marker for the next round:
   `echo <new-head> > docs/verification/owner-reference-gvk/.last-reviewed && git commit -s -am "review: advance last-reviewed"`

### Kickoff prompt for a fresh agent

> Resume stage ② verification of https://github.com/fluid-cloudnative/fluid/pull/6139 from the
> `verify/owner-reference-gvk` branch on the `cheyang/fluid` fork. Read
> `docs/verification/owner-reference-gvk/README.md`, then run
> `bash docs/verification/owner-reference-gvk/scripts/re-verify.sh`. Findings H1 (partial
> TypeMeta skips the `gvk.Empty()` recovery) and H2 (swallowed `GVKForObject` error) were
> confirmed real and unfixed at head `ee7e5ffd`; H3 is pre-existing and must not be reported
> against this PR. Apply canary polarity: `H1c` is fixed only when it FLIPS to fail. Do not
> publish anything to GitHub.
