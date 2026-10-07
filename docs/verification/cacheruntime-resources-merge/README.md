# Verification: PR #6177 — merge CacheRuntime resources onto the CacheRuntimeClass baseline

Reviewer: Codex (Reviewer B). Reviewed head: `c1c303e8b2dc0a237ab9abe289be4e64c4445970`
(`.last-reviewed`). Base (merge-base with master): `e0fc4c189a6e45ee17a5e812abf3fcbecf566f09`.

## Premise (P0) — verdict: CONFIRMED

Issue #6173 (open) claims: a CacheRuntime that names only some resource keys loses every
requirement it does not restate — setting just `limits.memory` clears `requests.cpu`,
`limits.cpu` and `requests.memory` from the workload on the next reconcile.

Reproduced on the **base** commit with the harness below: the three contract tests fail with

```
desiredComponentResources: requests.cpu = "0", want "1" (template value dropped)
desiredComponentResources: requests.memory = "0", want "2Gi" (template value dropped)
desiredComponentResources: limits.cpu = "0", want "2" (template value dropped)
```

on the resolver (`sync.go`), the creation path (`transformComponentPodTemplate`) and the full
sync path through a fake client (`syncRuntimeSpec` → `SyncComponentSpec` → `updateResources`).
Component match: the issue names `updateResources`, which only applies the desired state it is
handed; the PR fixes the two places that compute that state. Symptom matches exactly.

## Hypotheses and polarity

| id | claim | polarity | layer | test |
|----|-------|----------|-------|------|
| P0 | partial `resources` drops unstated template keys | contract (red on base, green on head) | unit | `TestVerifyPR6177_DesiredComponentResources_OverlaysOntoTemplate`, `TestVerifyPR6177_TransformComponentPodTemplate_OverlaysOntoTemplate` |
| P0 | same, through the whole reconcile-side sync | contract | integration (fake client) | `TestVerifyPR6177_SyncRuntimeSpec_KeepsUnnamedRequirements` (incl. convergence: 2nd sync must not patch) |
| G1 | neither side declaring resources leaves the workload untouched (#6165 contract) | guard, green both | integration | `TestVerifyPR6177_NeitherSideDeclares_GuardWorkloadUntouched` |
| G2 | runtime sets nothing → resolves to template, template not mutated | guard, green both | unit | `TestVerifyPR6177_TemplateOnly_GuardResolvesToTemplate` |

## How to run

```bash
# unit + integration layers (no cluster needed)
FLUID_UNIT_TEST=true go test -gcflags="all=-N -l" ./pkg/ddc/cache/engine/ -run 'TestVerifyPR6177' -v

# whole package (PR's own tests included)
FLUID_UNIT_TEST=true go test -gcflags="all=-N -l" ./pkg/ddc/cache/...
```

## Observed vs expected

| run | expected | observed | artifact |
|-----|----------|----------|----------|
| harness @ base e0fc4c1 | P0 contract tests FAIL (bug reproduces), guards PASS | 3 contract FAIL with the exact dropped keys, 2 guards PASS | `results/unit-base.txt` |
| harness @ PR head c1c303e8 | all PASS | 5/5 PASS | `results/unit-head.txt` |
| full package @ PR head | PASS | `ok pkg/ddc/cache/component`, `ok pkg/ddc/cache/engine` | `results/suite-head.txt` |

Harness-bites check: the polarity flip base→head *is* the apply-fix/revert-fix cycle — the same
unmodified harness file is red on the unpatched code and green on the patched code, and the two
guard tests prove the file exercises real behavior rather than vacuously passing.

No defects found in the diff itself: the merge deep-copies both sides (no informer-cache or
template mutation — covered by assertions), preserves nil vs empty maps so reconciles converge
(covered by the convergence assertion), keeps master/worker/client and the tiered-store memory
quota accounting consistent between creation and sync, and updates both docs files to match.

## Continuing after the fix

`bash docs/verification/cacheruntime-resources-merge/scripts/re-verify.sh` from a checkout of
this branch fetches the current PR head (from `manifest.pr`), grafts the harness onto it, runs
the unit + integration layers, and prints Fixed / Still-broken per finding. All findings are
contract polarity: green means good. If a future change *intentionally* restores wholesale
replace semantics, the P0 contract tests flip red — invert or delete them then.
