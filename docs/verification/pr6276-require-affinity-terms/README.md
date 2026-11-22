# Verification — PR #6276: fix(utils): inject node selector requirements into existing terms

Reviewer: codex (Reviewer B). Branch: `verify/pr6276-require-affinity-terms-codex`.
Head reviewed: `add9a8faa03f4c8aec610efe9b49a4cd79f56792` (base/merge-base: `896d21e945e6513ef4dd0e452ea9ec154acb0414`).
Production code under review is untouched by this harness (additive test files + this doc only).

## Premise (P0) — CONFIRMED

Issue #6275 claims `utils.InjectNodeSelectorRequirements` drops the
`runAfter.affinityStrategy` (`policy: Require`) requirement whenever the data
operation already has `requiredDuringSchedulingIgnoredDuringExecution` terms,
because it appends to the `range` loop variable (a per-iteration copy).

Reproduced on base `896d21e` with the grafted harness: the requirement is
dropped for existing terms (also when the existing `MatchExpressions` slice has
spare capacity), and dropped for an empty-but-non-nil `NodeSelectorTerms` slice
(base leaves an API-invalid empty NodeSelector behind). The author's own added
test cases also fail on base, as the PR body claims. The component matches: the
patch touches exactly the function the issue names, on the code path the issue
names (`InjectAffinityByRunAfterOp` → `injectRequiredAffinity`).

## Observed vs expected

| ID | Claim (contract) | Base 896d21e | Head add9a8fa | Evidence |
|----|------------------|--------------|---------------|----------|
| F1 | Requirement appended to every existing term (unit + dataflow chain) | FAIL (dropped, incl. spare-capacity slice) | PASS | results/unit-base.txt, results/unit-head.txt, results/dataflow-base.txt, results/dataflow-head.txt |
| F2 | Empty non-nil terms get a synthesized term instead of dropping | FAIL (0 terms) | PASS | results/unit-base.txt, results/unit-head.txt, results/dataflow-base.txt |
| F3 | Merged affinity is API-valid and persists with the requirement; base's empty-terms output is rejected by the apiserver | FAIL (persisted pod lost the requirement) | PASS (merged accepted; empty-terms shape rejected: "must have at least one node selector term") | results/envtest-base.txt, results/envtest-head.txt |

Sanity controls that pass on BOTH base and head (guards against a vacuous
harness): nil-affinity input still synthesizes a single term; empty requirement
input is a no-op identity return.

Harness-bites check: the same files fail on base and pass on head — the base→head
differential is the fix, so the tests provably exercise the changed code path.
Author's claim re-verified independently: the PR's own three new cases fail on
base with the production fix reverted (results/pr-tests-on-base.txt).

## How to run

Unit layer (pkg/utils + pkg/dataflow, fake client for the full
`InjectAffinityByRunAfterOp` chain):

```
go test -gcflags=all=-l ./pkg/utils/ ./pkg/dataflow/ -run 'TestVerifyInject' -v
```

Integration layer (envtest, real kube-apiserver validation of the resulting Pod spec):

```
KUBEBUILDER_ASSETS=<envtest k8s bin dir> go test -gcflags=all=-l ./pkg/utils/ -run TestVerifyInjectedAffinityAPIValidation -v
```

(Skips cleanly when KUBEBUILDER_ASSETS is unset. This machine used
/home/ecs-user/envtest-assets/k8s/1.31.0-linux-amd64.)

One-line re-verification of a future revision (from a checkout of this branch):

```
bash docs/verification/pr6276-require-affinity-terms/scripts/re-verify.sh   # auto-fetches current PR head
```

All harness tests are CONTRACT tests: they pass when the fix is correct. There
are no bug-canaries in this harness.

## Notes / non-blocking observations

- In-place mutation semantics of `InjectNodeSelectorRequirements` (mutates the
  passed affinity, returns it) are unchanged by the PR; the fix only makes the
  intended mutation actually land. `append` on the indexed term may share the
  backing array with the caller's slice when spare capacity exists — covered by
  TestVerifyInjectRequirementsWithSpareCapacity; acceptable because the engine
  callers pass spec affinities obtained from client reads (controller-runtime
  0.17 cache deep-copies on Get) and immediately serialize the result into the
  rendered chart/job.
- `pkg/utils` suite on this machine has one unrelated pre-existing failure,
  `TestCheckMountPointBroken` (fails identically on base and head; mount/port
  environment noise, not attributable to the PR).

## Continuing after changes

Fresh machine: clone https://github.com/cheyang/fluid.git, checkout
`verify/pr6276-require-affinity-terms-codex`, run the two commands above (or
`re-verify.sh`). If the PR changes the function shape (rename/signature), adjust
the harness (Harness-update) — the contract to preserve is: requirement lands in
every existing term; empty/nil terms synthesize a single term.
