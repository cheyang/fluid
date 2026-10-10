# ownerref-gvk-converge — verification harness (PR #6199, Reviewer B / Codex)

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6199
("fix(utils): converge datasetControllerOwnerReference with transformer helper", fixes #6140).

Layers run against the **code under review** (`dc3f33ca`, PR head, 2026-10-11):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `datasetControllerOwnerReference` across all TypeMeta shapes; convergence invariant vs `transformer.GenerateOwnerReferenceFromObject` | `go test ./pkg/utils/ -run 'TestVerifyPR6199' -count=1 -v` |
| 2. Integration | real API server (envtest): create Dataset -> `CreateRuntimeForReferenceDatasetIfNotExist` -> ThinRuntime ownerRef complete, controller=true, RESTMapper-resolvable; API-server rejection of empty-GVK ownerRefs | `KUBEBUILDER_ASSETS=<envtest bin dir> go test ./pkg/utils/ -run 'TestVerifyPR6199EnvtestOwnerReference' -count=1 -v` |
| 3. Live | skipped per debate setup (no KUBECONFIG layer) | — |

All tests are **contract** polarity: they assert the intended behavior, FAIL on the base
branch for the divergent shape, PASS on the PR head.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "Two independent implementations of 'recover the owner GroupVersionKind when TypeMeta is empty' are landing at the same time, and they do not agree on semantics." (issue #6140) |
| Linked issue | #6140, OPEN, filed 2026-07-29, still valid |
| Reported component | `pkg/utils/dataset_runtime.go` (#6138's helper) and `pkg/utils/transformer/owner_reference.go` (#6139's helper) |
| Patched component | `pkg/utils/dataset_runtime.go` (delegation) + tests in both packages |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | On base (`e0fc4c18`), both implementations exist and diverge: for `TypeMeta{Kind:"Dataset", APIVersion:"data.fluid.io/"}` (empty version segment) the dataset helper keeps `data.fluid.io/` verbatim while the transformer helper repairs it to `data.fluid.io/v1alpha1`. Harness-bites run: results/unit-base-harness-bites.txt (red on exactly that shape). Separately verified against a real API server: a typed client Get returns the Dataset with empty TypeMeta (the production condition), and the API server rejects ownerReferences with empty kind/apiVersion — so the completeness of the generated ref is load-bearing. results/integration-pr-head.txt |

Note on issue scope: #6140's proposals 1 (per-field recovery) and 2 (log on scheme-lookup
failure) describe an *earlier revision* of #6139; the merged #6139 (`95138fc7`) already
implements both. This PR completes the remaining proposals 3 (delegate) and 4 (regression
coverage), so merging it fully resolves #6140. The auto-close is appropriate.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Two divergent GVK-recovery implementations exist on base and disagree | 1 (vs base) | Confirmed | results/unit-base-harness-bites.txt |
| C1 | Post-PR, `datasetControllerOwnerReference` emits a complete GVK for every TypeMeta shape (incl. group-only apiVersion) | 1 | Confirmed | results/unit-pr-head.txt |
| C2 | Post-PR, the dataset helper and the transformer helper agree on Kind/APIVersion for every shape (convergence invariant) | 1 | Confirmed | results/unit-pr-head.txt |
| C3 | End-to-end vs real API server: created/adopted ThinRuntime carries complete, controller=true, RESTMapper-resolvable ownerRef | 2 | Confirmed | results/integration-pr-head.txt |
| C4 | (premise sub-claim) API server rejects ownerReferences with empty kind/apiVersion | 2 | Confirmed | "metadata.ownerReferences.apiVersion: Invalid value: \"\": version must not be empty ..." in results/integration-pr-head.txt |

No finding reproduced as a defect on the PR head; no blockers. The PR's own tests pass
(results/pr-own-tests.txt, results/transformer-pr-head.txt) and the PR introduces no new
failures in `./pkg/utils` (results/pkg-utils-{base,pr}-failures.txt are identical:
`TestCheckMountPointBroken` fails on both, a pre-existing host-environment failure unrelated
to this PR).

## Harness-bites check

`pkg/utils/verify_pr6199_ownerref_converge_test.go` was copied onto base (`e0fc4c18`,
production code untouched there): the group-only-apiVersion cases FAIL
(`expected ... "data.fluid.io/v1alpha1", got "data.fluid.io/"`), all other shapes PASS —
the harness is red on base for exactly the divergent behavior and nothing else.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/ownerref-gvk-converge-codex` (remote: reviewer's fork),
production code untouched, so it grafts onto any updated PR head.

```bash
bash docs/verification/ownerref-gvk-converge/scripts/re-verify.sh
```

(with no ref it fetches the current PR head via the manifest's `pr` URL). Layer 2 needs
envtest assets (`KUBEBUILDER_ASSETS`, e.g. via `setup-envtest`); it self-skips without them.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/ownerref-gvk-converge-codex
(fork https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6199 confirmed the premise (divergent
owner-reference GVK recovery) and produced no defects; the harness lives in
docs/verification/ownerref-gvk-converge/ plus pkg/utils/verify_pr6199_*_test.go.
Read docs/verification/ownerref-gvk-converge/README.md and follow "Continuing after the
fix": re-run unit + integration layers (set KUBEBUILDER_ASSETS for envtest), all tests are
contract polarity (green = good), then advance .last-reviewed and push.
```
