# ownerref-delegate — verification harness for PR #6199

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6199
(`fix(utils): converge datasetControllerOwnerReference with transformer helper (#6140)`).

Layers run against the **code under review** (`1f8666fc`, PR head at review time):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `datasetControllerOwnerReference` delegation equivalence, transformer GVK recovery | `go test ./pkg/utils/ ./pkg/utils/transformer/ -run 'TestVerify' -count=1 -v` |
| 2. Integration | `CreateRuntimeForReferenceDatasetIfNotExist` against a fake client, plus the PR's own suites | `go test ./pkg/utils/ -run 'TestCreateRuntimeForReferenceDatasetIfNotExist\|TestDatasetControllerOwnerReference\|TestVerifyThinRuntimeCreationCarriesOwnerReference' -count=1 -v; go test ./pkg/utils/transformer/ -count=1` |
| 3. Live | skipped this round (debate setup: unit + integration only) | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

Answered before the findings, run against the **base** branch (`e0fc4c18`) without the patch.

| | |
|---|---|
| Claimed symptom | issue #6140: “The two code paths do not overlap — `datasetControllerOwnerReference` does not go through the transformer — so neither fix covers the other's callers.” |
| Linked issue | #6140, OPEN, opened 2026-07-29, still valid |
| Reported component | `pkg/utils/dataset_runtime.go` + `pkg/utils/transformer/owner_reference.go` |
| Patched component | `pkg/utils/dataset_runtime.go` (delegates) + tests in both packages — matches |
| Component match | **Yes** |
| **Verdict** | **Confirmed** |
| Evidence | Base `dataset_runtime.go:49` implements its own per-field GVK fallback, no transformer import (`results/premise-base-inspection.txt`). Note: #6140's items 1+2 (per-field fill, failure logging) already landed on base via the merged #6139 (95138fc7) — the issue text predates that merge shape — so this PR's delegation (item 3) + regression coverage (item 4) are precisely the remainder. Merging legitimately closes #6140. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Two divergent GVK-recovery copies exist on base; PR converges them | base | Confirmed | `results/premise-base-inspection.txt` |
| F1 | Transformer leaves Group unrecovered when Version is set (`APIVersion:"v1alpha1"` → ownerRef `APIVersion:"v1alpha1"`); the delegated Dataset path inherits the gap | 1 | **Reproduced (contract red)** | `results/unit-transformer-f1.json`: “group not recovered for group-less apiVersion — got APIVersion=\"v1alpha1\", want \"data.fluid.io/v1alpha1\"” |
| F1-canary | Delegation reproduces legacy output byte-for-byte, including the malformed group-less shapes (i.e. the PR itself is behavior-preserving; F1 is pre-existing) | 1 | Confirmed (canary green) | `results/unit-pkgutils.json`: 11/11 pass |
| F2 | Delegation preserves the pre-PR behavior on every well-formed TypeMeta shape | 1 | Confirmed (contract green) | `results/unit-pkgutils.json` `TestVerifyOwnerReferenceDelegationEquivalence` |
| F3 | End-to-end: ThinRuntime created/adopted for a reference dataset carries a well-formed controller ownerReference; idempotent on re-run | 2 | Confirmed (green) | `results/unit-pkgutils.json` + `results/pr-own-pkgutils.json` (12/12) + `results/integration-transformer-full.json` (ginkgo 32/32) |

## Per-finding detail

**P0 (premise).** On base, `datasetControllerOwnerReference` (pkg/utils/dataset_runtime.go) and
`GenerateOwnerReferenceFromObject` (pkg/utils/transformer/owner_reference.go) are two
independent GVK-recovery implementations. Issue #6140 asked to converge them. The PR makes the
dataset helper delegate to the transformer and keeps the well-known Dataset values only as an
(unreachable-for-registered-types) last-resort fallback. Components match; premise valid.

**F1 (minor, pre-existing — reproduced).**
`GenerateOwnerReferenceFromObject` recovers `(Group, Version)` only **as a pair and only when
`gvk.Version` is empty**. A partial TypeMeta carrying a version but no group
(`metav1.TypeMeta{APIVersion: "v1alpha1"}` → `GVK{Group:"", Version:"v1alpha1"}`) therefore
passes through with the group still empty, and the minted ownerReference is
`{Kind:"Dataset", APIVersion:"v1alpha1"}` — the same malformed class #6140 proposal 1 targeted
(“fill Kind and APIVersion independently … keeping the scheme lookup as the source of truth for
both fields”). The PR's delegation routes the Dataset owner path through this logic and adds
regression entries for kind-only / apiVersion-only shapes, but not for the group-less shape,
which stays both uncovered and mishandled.
- Command: `go test ./pkg/utils/transformer/ -run TestVerifyGroupRecoveredWhenVersionIsSet -count=1 -v`
- Observed: `APIVersion="v1alpha1"` — Expected: `"data.fluid.io/v1alpha1"` (red contract test).
- Reachability caveat (why this is minor, not blocking): a typed client hands back empty
  TypeMeta, and the API server rejects objects whose own `apiVersion` doesn't resolve, so the
  group-less shape is only constructible from malformed in-memory input; the OLD
  datasetControllerOwnerReference produced the identical malformed output, so this is **not a
  regression introduced by the PR** — it is a gap the convergence could have closed and didn't.

**F1-canary (confirms the PR is behavior-preserving).**
`TestVerifyDelegationMatchesLegacyForMalformedShapes` and
`TestVerifyDatasetOwnerRefGrouplessVersionShape` assert the delegated helper reproduces the
legacy per-field fallback output for the malformed group-less shapes too — i.e. the refactor
changes nothing for any input shape, malformed or not.

**F2 (confirmed).** `TestVerifyOwnerReferenceDelegationEquivalence` compares
`datasetControllerOwnerReference` against a verbatim copy of the pre-PR implementation
(`legacyDatasetControllerOwnerReference` in the harness test) across empty / complete /
kind-only / version-only / stale-version shapes: identical output everywhere.

**F3 (confirmed).** `TestVerifyThinRuntimeCreationCarriesOwnerReference` drives the real
`CreateRuntimeForReferenceDatasetIfNotExist` path with a fake client and a typed-client-shaped
(empty TypeMeta) Dataset: the created ThinRuntime carries
`{Kind:"Dataset", APIVersion:"data.fluid.io/v1alpha1", Controller:true}`, and a second
(adoption) pass leaves it intact. The PR's own suites all pass on the head as well.

## Harness-bites check (Step 4 of the skill)

The proposed F1 fix (recover `Group` whenever it is empty, independently of `Version`) was
applied **temporarily** to `pkg/utils/transformer/owner_reference.go`:

- `TestVerifyGroupRecoveredWhenVersionIsSet` (contract): red → **green** ✔
- `TestVerifyDatasetOwnerRefGrouplessVersionShape` (canary): green → **red** ✔ (flip)
- `TestVerifyDelegationMatchesLegacyForMalformedShapes` (canary): green → **red** ✔ (flip)
- `TestVerifyOwnerReferenceDelegationEquivalence` (contract): stays **green** ✔ (well-formed
  shapes unaffected by the fix)
- Full transformer suite incl. ginkgo (32 specs) and the PR's own tests: **still green** under
  the fix — so the fix is compatible with everything this PR shipped.

The fix was then reverted; `git diff` on production code is empty (only the two additive
harness test files and this docs tree are committed).

## Live run notes

Not run this round (debate setup limits stage ② to unit + integration). No cluster involved.

## Proposed fixes (NOT applied to production here)

- **F1**: in `GenerateOwnerReferenceFromObject`, gate the recovery on
  `len(gvk.Kind)==0 || len(gvk.Group)==0 || len(gvk.Version)==0` and fill Kind, Group, Version
  each independently from the scheme lookup. Verified compatible with the full existing suite
  (see harness-bites above). Add a table entry for the group-less shape to
  `owner_reference_test.go`.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/ownerref-delegate-claude` (production code untouched), so it
grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/ownerref-delegate-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/ownerref-delegate \
     pkg/utils/ownerref_delegate_verify_test.go \
     pkg/utils/transformer/owner_reference_gvk_verify_test.go
   ```
   Or simply run `bash docs/verification/ownerref-delegate/scripts/re-verify.sh` from a
   checkout of this branch — with no argument it resolves the current PR head from
   `manifest.pr` and prints the per-finding table.
2. Prereqs: Layer 1+2 = Go toolchain only (fake client, no envtest, no cluster).
3. Re-run the two commands from the layer table.
4. Read results via the polarity table:
   | Test | Polarity | Now (PR head) | After F1 fix |
   |---|---|---|---|
   | `TestVerifyGroupRecoveredWhenVersionIsSet` | contract | RED (repro) | GREEN |
   | `TestVerifyDatasetOwnerRefGrouplessVersionShape` | canary | GREEN | RED → invert |
   | `TestVerifyDelegationMatchesLegacyForMalformedShapes` | canary | GREEN | RED → invert |
   | `TestVerifyOwnerReferenceDelegationEquivalence` | contract | GREEN | GREEN |
   | `TestVerifyThinRuntimeCreationCarriesOwnerReference` | contract | GREEN | GREEN |
5. Harness-bites: re-run Layer 1 once against pre-fix code to confirm the contract test still
   goes red.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/ownerref-delegate-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6199 produced finding F1 (the transformer
owner-reference GVK recovery never recovers the Group when the Version is already set, so a
group-less apiVersion passes through malformed); a harness reproduced it. The PR is now fixed
at <ref>. Read docs/verification/ownerref-delegate/README.md ("Continuing after the fix") and
follow it: graft the harness onto the fixed code, re-run all layers, mind the polarity table
(invert the two bug-canaries), run the harness-bites check, and report an observed-vs-expected
table. Clean up scoped test resources; do not run cluster-wide destructive actions.
```
