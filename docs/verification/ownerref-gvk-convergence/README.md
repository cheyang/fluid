# ownerref-gvk-convergence — verification harness for PR #6199

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6199
("fix(utils): converge datasetControllerOwnerReference with transformer helper", fixes #6140),
head `1f8666fc`. Production code is untouched; this branch adds test files and this docs tree only.

Layers (live layer skipped per review setup):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | legacy-vs-delegated differential over 14 TypeMeta shapes; helper partial-recovery probe | `go test ./pkg/utils/ -run 'TestVerifyOwnerRefConvergenceEquivalence\|TestVerifyHelperPartialTypeMetaRecovery' -count=1 -v` |
| 2. Integration | real API server (envtest): persisted ownerRef validity, RESTMapper resolvability, counterfactual rejections | `KUBEBUILDER_ASSETS=<envtest bin dir> go test ./pkg/utils/ -run TestVerifyOwnerRefAgainstRealAPIServer -count=1 -v` |

> Polarity: all tests are contract tests (assert intended behavior; FAIL on buggy code).
> On the **base** branch the three `normalized/*` subtests of the differential FAIL by design —
> they pin the one deliberate behavior change of the PR (malformed apiVersion normalization).

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | Issue #6140: "Two independent implementations of 'recover the owner GroupVersionKind when TypeMeta is empty' ... do not agree on semantics." (structural divergence, no live user symptom; the issue itself says the gap is unreachable from current call sites) |
| Linked issue | #6140, OPEN, filed 2026-07-29, still valid |
| Reported component | `pkg/utils/dataset_runtime.go` (`datasetControllerOwnerReference`) vs `pkg/utils/transformer/owner_reference.go` (`GenerateOwnerReferenceFromObject`) |
| Patched component | exactly those two: delegation in `dataset_runtime.go` + tests |
| Component match | Yes |
| **Verdict** | **Confirmed** (with one nuance below) |
| Evidence | base @e0fc4c18 carries both implementations; `results/L2-envtest-pr-head.out` shows a typed-client Get returns empty TypeMeta, so recovery is the production path |

Nuance: the issue's "Why the difference matters" section describes the shared helper as gated on
`gvk.Empty()` (skipping recovery for partial TypeMeta). On the current base the helper already
recovers per-field and logs lookup failures (landed with the merged form of #6139; verified by
`results/L1-transformer-suite-base.out` — the base transformer suite, including partial-TypeMeta
DataLoad entries, passes). So the semantic divergence was already fixed; what remained — and what
this PR removes — is the duplicate implementation.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | premise (see above) | 1+2 | Confirmed | `results/L1-differential-base.out`, `results/L2-envtest-pr-head.out` |
| F1 | The convergence is behavior-preserving on every reachable TypeMeta shape; the only divergence is normalization of malformed apiVersions (group set / version empty, unparseable), and the old output was API-server-rejected | 1 (+2) | Confirmed | 11/11 equivalent on PR head (`results/L1-differential-pr-head.out`); 3 `normalized/*` fail on base (ibid. `-base.out`); base shape `"data.fluid.io/"` rejected by real API server (`L2-envtest-pr-head.out`, counterfactual cf-empty-version) |
| F2 | The full production path works against a real API server: empty-TypeMeta dataset → ThinRuntime created with complete, RESTMapper-resolvable controller ownerReference | 2 | Confirmed | `results/L2-envtest-pr-head.out` (all assertions pass; both counterfactuals rejected) |

Review-level finding recorded in the debate document (not a harness claim): the PR's committed
unit tests pin the behavior contract, not the delegation — they also pass against the pre-PR
implementation (`results/L1-pr-tests-on-base-impl.out`). The differential test on this branch is
the guard a future de-delegation would trip.

## Harness-bites (proof the tests exercise the path)

| Check | Setup | Result |
|-------|-------|--------|
| L1 catches lost recovery | PR head mutated to naive `dataset.TypeMeta` passthrough (no delegation, no fallback) | 8/11 `equivalent/*` subtests FAIL (`results/L1-bite-mutation.out`) |
| L2 catches lost recovery | same mutation | `CreateRuntimeForReferenceDatasetIfNotExist` fails: real API server rejects the empty-GVK ownerReference (`results/L2-bite-mutation.out`) |
| first-draft harness bug | struct compare on `metav1.OwnerReference` (fresh `*bool` per call) | red for the wrong reason; fixed to compare dereferenced values — recorded here per honest-reporting policy |

Note: an earlier, weaker mutation (TypeMeta-kind + fallback) passed L1 *correctly* — it is
behaviorally equivalent on all reachable shapes, so the harness was right not to flag it.

## Environment notes

- Go 1.27.0; envtest binaries k8s 1.36.2 at `~/.local/share/kubebuilder-envtest/k8s/1.36.2-linux-amd64`.
- Pre-existing, environment-dependent failure on this host (fails identically on base @e0fc4c18,
  unrelated to the PR): `TestCheckMountPointBroken`. Full package suite otherwise green
  (`results/full-suite-pr-head.out`, run with `-skip TestCheckMountPointBroken`).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/ownerref-gvk-convergence-codex` (remote: the reviewer's fork),
production code untouched. Re-run after any new push to the PR:

```bash
git checkout verify/ownerref-gvk-convergence-codex   # or fetch it into any clone
bash docs/verification/ownerref-gvk-convergence/scripts/re-verify.sh          # auto-resolves current PR head
# or against an explicit ref:
bash docs/verification/ownerref-gvk-convergence/scripts/re-verify.sh <sha>
```

Prereqs: Layer 1 = Go toolchain only. Layer 2 = envtest binaries (set `KUBEBUILDER_ASSETS`;
the manifest cmd defaults to the path above). Exit 0 iff every finding reports Fixed.

Interpretation after a fix round: all tests are contract polarity — green means good. The three
`normalized/*` subtests pin current (post-PR) behavior; they fail on pre-PR code by design.
`.last-reviewed` (next to this file) marks the reviewed head `1f8666fc`; after reviewing a new
delta, advance it as re-verify.sh prints.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/ownerref-gvk-convergence-codex (fork remote:
https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6199 (converge datasetControllerOwnerReference
with transformer.GenerateOwnerReferenceFromObject) produced no defects; the harness proves the
convergence is behavior-preserving (differential vs the pre-PR oracle) and that the persisted
ownerReference is valid against a real API server. Read
docs/verification/ownerref-gvk-convergence/README.md and run
docs/verification/ownerref-gvk-convergence/scripts/re-verify.sh to re-check the current PR head
(needs Go; integration layer needs KUBEBUILDER_ASSETS pointing at envtest binaries). All tests
are contract polarity. Review the delta in .last-reviewed..head, advance .last-reviewed, commit
and push the branch.
```
