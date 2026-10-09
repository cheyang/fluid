# ownerref-helper-converge — bug verification

Reproducible evidence for the findings raised while reviewing
[PR #6199](https://github.com/fluid-cloudnative/fluid/pull/6199) (fix(utils): converge
datasetControllerOwnerReference with transformer helper, fixes #6140).

Layers run against the **code under review** (`1bad02a9`, PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | pure logic (`datasetControllerOwnerReference`, `transformer.GenerateOwnerReferenceFromObject`) | `go test ./pkg/utils/ -run 'TestVerify' -count=1 -v` |
| 2. Integration | fake-client flows through `CreateRuntimeForReferenceDatasetIfNotExist` and the dataset controller that calls it | `go test ./pkg/utils/... ./pkg/controllers/v1alpha1/dataset/... -count=1` |
| 3. Live | not applicable — pure in-process logic; skipped per debate setup (no KUBECONFIG layer probed/needed) | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

Answered before the findings, and run against the **base** branch (`1bad02a9^`) without the
patch, because the question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom | issue #6140: "The two code paths do not overlap — datasetControllerOwnerReference does not go through the transformer — so neither fix covers the other's callers." Two divergent copies of GVK recovery in the tree. |
| Linked issue | #6140, OPEN, filed as a follow-up to #6138/#6139; still valid. Items 1–2 of its proposal (per-field recovery + diagnostics) already landed via #6139; this PR delivers items 3–4 (delegation + regression coverage). |
| Reported component | `pkg/utils/dataset_runtime.go` + `pkg/utils/transformer/owner_reference.go` |
| Patched component | same — `pkg/utils/dataset_runtime.go` delegates; tests in both packages |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | `git show 1bad02a9^:pkg/utils/dataset_runtime.go` — a self-contained per-field fallback with no call into the transformer; the differential oracle in the harness shows the base function and the helper disagree on malformed apiVersion shapes ("data.fluid.io/", "a/b/c", "/"): base's function passes them through verbatim, the helper recovers them. results/unit-base-red.txt |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| B1 | Delegation to `GenerateOwnerReferenceFromObject` is behavior-preserving for every well-formed TypeMeta shape, and recovers (rather than passes through) malformed apiVersion shapes | 1 | Confirmed (intended fix works; no regression) | TestVerifyDelegationEquivalence green on head, red on base; results/unit-head-green.txt, results/unit-base-red.txt |
| B2 | The well-known Dataset fallbacks in `datasetControllerOwnerReference` are unreachable for a scheme-registered type (the "unreachable by design" comments are accurate) | 1 | Confirmed | TestVerifyWellKnownFallbackUnreachable green on head and base (helper invariant, unchanged by the PR) |
| B3 | A group-less apiVersion (`"v1alpha1"`, no `data.fluid.io` group) still passes through unrecovered — residual gap in the GVK-recovery contract, pre-existing from #6139, not introduced or fixed by this PR | 1 | Confirmed (gap exists; delegation does not regress it) | TestVerifyGrouplessAPIVersionPassesThrough (canary) green on head and base |
| — | Full utils + dataset-controller suites pass on head | 2 | Pass | results/integration-*.txt |

## Per-finding detail

### B1 — delegation equivalence (contract)

`pkg/utils/dataset_runtime_verify_test.go` carries a verbatim copy of the base
implementation (`legacyDatasetControllerOwnerReference`) as a differential oracle and
compares it with the PR's delegating version over a 12-shape TypeMeta matrix (empty,
complete, kind-only, apiVersion-only, custom version, group-less, foreign kind, malformed ×3).

- Observed on head: identical output for all 9 well-formed shapes; the 3 malformed shapes
  ("data.fluid.io/", "a/b/c", "/") now resolve to `data.fluid.io/v1alpha1` instead of being
  passed through verbatim — the direction issue #6140 asks for.
- Observed on base: the same test is RED (`--- FAIL: TestVerifyDelegationEquivalence`),
  because the delegating behavior is absent — this is the harness-bites check.
- Comparison is field-wise (`ownerRefsEqual`): `metav1.OwnerReference.Controller` is a `*bool`
  from separate `ptr.To` allocations, so struct `==` would compare pointer identity.

### B2 — fallback unreachability (contract)

For every matrix shape, `GenerateOwnerReferenceFromObject` returns a non-empty Kind and
APIVersion for a registered `Dataset`, so the `datav1alpha1.Datasetkind` /
`datav1alpha1.GroupVersion` fallbacks in `datasetControllerOwnerReference` can never fire.
This guards the PR's "unreachable by design" comments; it also passes on base (helper
invariant), so it is a guard, not a bug repro.

### B3 — group-less apiVersion (canary)

`TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"}` parses to GVK `{Group:"", Version:"v1alpha1",
Kind:"Dataset"}`; since Kind and Version are non-empty the helper skips scheme recovery and
emits apiVersion `"v1alpha1"` — an ownerReference the API server accepts syntactically but an
owner-based watch / GC cannot resolve to the `data.fluid.io` group. Same output on base and
head: pre-existing helper semantics from #6139 (out of this PR's diff), documented here as a
non-blocking risk for the authors. The test asserts current behavior (canary polarity) and
must be inverted into a contract test if the helper ever learns to recover the group.

### Integration layer

`go test ./pkg/utils/... ./pkg/controllers/v1alpha1/dataset/...` on head — the caller of
`CreateRuntimeForReferenceDatasetIfNotExist` is `pkg/controllers/v1alpha1/dataset/dataset_controller.go:154`;
its suite plus the full utils suite (which includes the PR's new flow assertions in
`TestCreateRuntimeForReferenceDatasetIfNotExist`) all pass. Raw output in results/.

Two failures appear on head — `TestCheckMountPointBroken` (pkg/utils/mount_test.go:220,
expects nil error but the sandbox cannot check mounts) and `TestHelm` (7 ginkgo specs,
panics in the helm exec fakes). Both are **pre-existing environment failures**: rerunning
the exact same tests on the base commit `1bad02a9^` in a throwaway worktree reproduces them
identically (results/integration-base-preexisting-failures.txt), and the PR does not touch
`pkg/utils/helm` or the mount code. All packages related to the PR pass: `pkg/utils`
(owner-reference tests), `pkg/utils/transformer`, `pkg/controllers/v1alpha1/dataset`.

## Harness-bites check

`git checkout 1bad02a9^ -- pkg/utils/dataset_runtime.go` (temporarily reverting the PR's
production change), re-run Layer 1 → `TestVerifyDelegationEquivalence` FAILS; restore →
PASSES. Production diff confirmed empty afterwards (`git status` shows only harness files).
Raw output: results/unit-base-red.txt vs results/unit-head-green.txt.

## Proposed fixes (NOT applied to production here)

- **B3 (optional, for the maintainers, not this PR)**: in
  `GenerateOwnerReferenceFromObject`, also recover the group when the parsed GVK has an
  empty Group but the scheme's resolved GVK has one — or at least log it, mirroring the
  existing diagnostics. Out of scope for #6199; file under #6140 follow-ups.
- Optional nit: add a transformer-level table entry for the malformed
  apiVersion shapes ("data.fluid.io/") so the helper's own contract is pinned without going
  through the delegation test.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/ownerref-helper-converge-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/ownerref-helper-converge-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/ownerref-helper-converge pkg/utils/dataset_runtime_verify_test.go
   ```
2. Prereqs: Layer 1 and 2 = Go toolchain (≥ 1.25 per go.mod) only; Layer 3 = n/a.
3. Re-run:
   ```bash
   bash docs/verification/ownerref-helper-converge/scripts/re-verify.sh   # auto-resolves the PR head
   ```
   Exit 0 iff all findings are Fixed. Or run the layer commands from the table above.
4. Read results via the polarity table: B1/B2 are contract tests and should be GREEN; B3 is
   a bug-canary asserting current (group-less pass-through) behavior — it will FLIP RED if
   the helper starts recovering the group, which means the gap is fixed: invert it into a
   contract test then.
5. Harness-bites: run Layer 1 once against pre-fix code (`git checkout 1bad02a9^ --
   pkg/utils/dataset_runtime.go`) to confirm `TestVerifyDelegationEquivalence` still goes red.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/ownerref-helper-converge-claude
(github.com/cheyang/fluid fork). Background: a review of fluid PR #6199 produced findings
B1–B3 (see docs/verification/ownerref-helper-converge/README.md); a harness reproduced them.
The PR is now fixed at <ref>. Read docs/verification/ownerref-helper-converge/README.md
("Continuing after the fix") and follow it: graft the harness onto the fixed code, re-run
all layers, mind the polarity table (B3 is a canary — invert it if it flips), run the
harness-bites check, and report an observed-vs-expected table. No cluster is needed.
```
