# ownerref-gvk-delegation — verification of PR #6199 (fixes #6140)

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6199
(head `1bad02a9`, base/merge-base `e0fc4c18`). Production code is untouched by this branch; the
harness is additive (`pkg/utils/ownerref_convergence_verify_test.go`,
`pkg/utils/ownerref_envtest_verify_test.go`, plus this docs tree).

Layers:

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `datasetControllerOwnerReference` vs the shared helper, 9-shape TypeMeta truth table, exact old-vs-new behavior delta | `go test ./pkg/utils/ -run 'TestVerifyOwnerRefConvergenceContract\|TestVerifyOwnerRefBehaviorDeltaVsOldImpl' -v` |
| 2. Integration (envtest, real API server 1.36.2) | API-server validation of the old (malformed) vs new (converged) ownerReference output | `KUBEBUILDER_ASSETS=$HOME/.local/share/kubebuilder-envtest/k8s/1.36.2-linux-amd64 go test ./pkg/utils/ -run TestVerifyOwnerRefAPIServerValidation -v` |
| 3. Live | skipped this round (no KUBECONFIG layer); see `liveNote` in verify-manifest.json | — |

Polarity: all three harness tests are CONTRACT tests (green on the converged code, red on the
pre-#6199 implementation). The premise canary (P0, embedded below) is a CANARY: passes on base,
must flip to red once the implementations converge.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | Issue #6140: "Two independent implementations of 'recover the owner GroupVersionKind when TypeMeta is empty' are landing at the same time, and they do not agree on semantics… `gvk.Empty()` is true only when Group, Version and Kind are all empty. An object whose TypeMeta is partially populated therefore skips recovery entirely and still produces a malformed reference." Scope note in the issue: "not a blocker for either PR. Neither gap is reachable from the current call sites". |
| Linked issue | #6140, OPEN (created 2026-07-29), still valid: #6139 landed the scheme-based per-field helper on master (commit 95138fc7), #6138 landed the local helper (f7067fd6); the divergence the issue describes exists on base `e0fc4c18` |
| Reported component | `pkg/utils/dataset_runtime.go` (`datasetControllerOwnerReference`) and `pkg/utils/transformer/owner_reference.go` (`GenerateOwnerReferenceFromObject`) |
| Patched component | `pkg/utils/dataset_runtime.go` (delegates to the shared helper) + tests in both packages |
| Component match | Yes |
| **Verdict** | **Confirmed** (real divergence, reproduced on base; issue's own scope applies: contract-level, not reachable from current call sites) |
| Evidence | results/layer1-base-premise-canary.txt (base: local helper → `APIVersion="data.fluid.io/"`, shared helper → `"data.fluid.io/v1alpha1"` for the same object); results/layer1-head-canary-flip.txt (same canary FAILS on the PR head — convergence achieved); results/layer2-envtest-apiserver-validation.txt (real API server rejects the old output: `metadata.ownerReferences.apiVersion: Invalid value: "data.fluid.io/": version must not be empty`) |

P0 canary source (run on base; intentionally not committed to this branch — it is designed to
fail once convergence lands): `TestBasePremise_TwoImplementationsDiverge` builds a
`*datav1alpha1.Dataset` with `TypeMeta{Kind:"Dataset", APIVersion:"data.fluid.io/"}` and asserts
`datasetControllerOwnerReference` keeps the malformed apiVersion while
`transformer.GenerateOwnerReferenceFromObject` repairs it. Full source: `scripts/base_premise_canary_test.go` (copy it into `pkg/utils/` to run it; it is
designed to pass on base and fail once convergence lands — the graft onto the PR head produced
the flip recorded in `results/layer1-head-canary-flip.txt`).

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Two divergent GVK-recovery implementations exist on base | 1+2 | Confirmed | results/layer1-base-premise-canary.txt, results/layer2-envtest-apiserver-validation.txt |
| C1 | After delegation, `datasetControllerOwnerReference` agrees with the shared helper (+ documented fallback) for every TypeMeta shape, and never emits empty Kind/APIVersion | 1 | Confirmed | results/layer1-head-contract.txt (9/9 shapes pass) |
| C2 | The delegation's only behavior delta vs the old implementation is repairing malformed empty-version apiVersion shapes; all well-formed shapes are byte-identical | 1 | Confirmed | results/layer1-head-contract.txt (`TestVerifyOwnerRefBehaviorDeltaVsOldImpl`, 9/9) |
| C3 | The old output shape is rejected by a real API server; the converged output is accepted | 2 | Confirmed | results/layer2-envtest-apiserver-validation.txt |
| PR's own tests | `TestDatasetControllerOwnerReference`, `TestCreateRuntimeForReferenceDatasetIfNotExist`, transformer Ginkgo suite | 1 | Pass on PR head | results/pr-tests-utils.txt, results/pr-tests-transformer.txt |

Harness-bites: with `pkg/utils/dataset_runtime.go` temporarily reverted to base, the PR's new
`TestDatasetControllerOwnerReference` fails only on the malformed shape
(`expected APIVersion data.fluid.io/v1alpha1, got data.fluid.io/`) and both harness contract
tests fail only on the two malformed shapes — 8 red subtests, all others green
(results/layer1-harness-bites-prefixed.txt). Production file restored afterwards; diff empty.

## Per-finding detail

No defects were found in the PR. The review findings are two nits (see findings-codex.md in the
review workspace): the post-delegation fallback in `datasetControllerOwnerReference` is
unreachable by construction and would mask a scheme regression silently if it ever fired; and
the augmented `TestCreateRuntimeForReferenceDatasetIfNotExist` asserts the created and adopted
runtimes' ownerReferences but not the pre-owned "ThinRuntimeExists" case.

## Proposed fixes (NOT applied to production here)

- nit 1: optionally log when the last-resort fallback in `datasetControllerOwnerReference`
  actually fires (it indicates `fluidScheme` lost the Dataset registration).
- nit 2: extend the post-loop assertions to the pre-owned `ThinRuntimeExists` runtime.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/ownerref-gvk-delegation-codex` (remote `reviewfork`,
https://github.com/cheyang/fluid.git), based on the PR head with production code untouched.

1. Graft onto updated code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/ownerref-gvk-delegation-codex
   git checkout <fixed-ref>
   git checkout FETCH_HEAD -- docs/verification/ownerref-gvk-delegation \
     pkg/utils/ownerref_convergence_verify_test.go pkg/utils/ownerref_envtest_verify_test.go
   ```
   or run `bash docs/verification/ownerref-gvk-delegation/scripts/re-verify.sh` from a checkout
   of the verify branch (it auto-fetches the current PR head from `manifest.pr`).
2. Prereqs: Go toolchain for L1; envtest assets under `~/.local/share/kubebuilder-envtest/` for
   L2 (or any `KUBEBUILDER_ASSETS` pointing at kube-apiserver+etcd).
3. Re-run the L1/L2 commands above. All three are contract tests: green = convergence intact.
   If a future change reintroduces a local copy, `TestVerifyOwnerRefConvergenceContract` goes red.
4. Harness-bites: run L1 once against base `e0fc4c18` — the two malformed shapes must be red.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/ownerref-gvk-delegation-codex (remote
https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6199 (converge owner-reference GVK recovery,
fixes #6140) found no defects; a contract harness pins the convergence and the exact behavior
delta. Read docs/verification/ownerref-gvk-delegation/README.md ("Continuing after the fix"),
graft the harness onto the current PR head, re-run L1+L2 (L2 needs KUBEBUILDER_ASSETS pointing
at envtest binaries), and report an observed-vs-expected table. All harness tests are contract
polarity (green = good). Do not modify production code.
```
