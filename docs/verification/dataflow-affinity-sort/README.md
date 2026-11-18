# dataflow-affinity-sort — bug verification

Reproducible evidence for the review of
[PR #6258](https://github.com/fluid-cloudnative/fluid/pull/6258)
("fix(dataflow): sort generated node affinity expressions").

Layers run against the **code under review** (`79ba4d90aae5544101855982325975b72c8e4205`,
base `5e6473599f78263a49e521854d80f918a1a10bb0`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `GenerateNodeAffinity` ordering/stability + edge paths (`pkg/dataflow/affinity_sort_verify_test.go`) | `go test ./pkg/dataflow/ -run 'TestVerifyF1\|TestVerifyF2' -v` |
| 2. Integration (package suite) | whole `pkg/dataflow` package incl. the PR's own tests | `go test ./pkg/dataflow/` |
| 3. Live | **not applicable** — pure-function ordering fix, no cluster dependency; no live/KUBECONFIG layer was run by policy for this review | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

Answered before the findings, and run against the **base** branch without the patch, because
the question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom | "multiple injected affinity annotations can yield non-deterministic MatchExpressions ordering on the generated affinity" (issue #6238, OPEN, created 2026-11-11) |
| Linked issue | #6238 (open, 7 days old, feature request); `fixes #6238` in the PR body. (#15 also appears in `closesIssues` — that is the PR **template's literal example** inside an HTML comment, not a real claim; #15 is a closed demo PR.) |
| Reported component | `pkg/dataflow/helper.go` `GenerateNodeAffinity`, the `for key, value := range annotations` loop (line ~50 on base) |
| Patched component | exactly that loop (`utils.OrderedKeys(annotations)` + `value := annotations[key]`) |
| Component match | Yes |
| **Verdict** | **Confirmed** (real problem) |
| Evidence | base 5e64735: canary observed **4 distinct MatchExpressions orderings in 200 calls** (`results/premise-base.txt`); contract test C1 FAILS on base (the reproduction) |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| C1 | With several `affinity.dataflow.fluid.io.*` annotations, `MatchExpressions` come out in lexicographic key order and the affinity is identical across repeated calls | 1 | **Confirmed (fixed on head)** | F1 PASSES on head 79ba4d90, FAILS on base 5e64735 (`results/head-unit.txt`, `results/premise-base.txt`) |
| C2 | Surrounding behavior unchanged: inject gate, key trimming, no-affinity-label error, slash-form sibling annotations (`affinity.dataflow.fluid.io/inject`, `.../labels`) not collected, single/empty-value labels | 1 | **Confirmed** | F2 PASSES on head and on base (`results/head-unit.txt`, `results/premise-base.txt`) |
| C3 | PR's own tests + whole `pkg/dataflow` suite pass on head | 2 | **Confirmed** | `ok github.com/fluid-cloudnative/fluid/pkg/dataflow` (`results/head-package.txt`) |

## Per-finding detail

### C1 — ordering + stability (the PR's core claim)
Mechanism: `for _, key := range utils.OrderedKeys(annotations)` sorts all annotation keys
(`slices.Sort`), and only prefixed keys are appended, so `MatchExpressions` order equals
lexicographic order of the trimmed keys. `TestVerifyF1_MatchExpressionsSortedAndStable` uses 6
keys including the pair where full-key order is counter-intuitive (`k8s.rack` sorts **before**
`kubernetes.io/hostname`, because `'8' < 'e'`), and asserts exact equality across 50 calls.

- Observed on head: PASS (sorted, stable).
- Observed on base: FAIL — e.g. `got: {m-label}{topology.kubernetes.io/zone}{z-label}{a-label}{k8s.rack}{kubernetes.io/hostname}`.

### C2 — behavior preservation
The sort must not alter anything else. Subtests cover: nil job → `(nil, nil)`; inject annotation
absent / not `"true"` → `(nil, nil)`; inject `"true"` with only non-matching keys (including the
slash-form `affinity.dataflow.fluid.io/labels` and the bare stem without the trailing dot) → the
documented `"the affinity label is not set"` error and nil affinity; single label; empty value.
PASS on both base and head → no collateral behavior change.

### C3 — package suite
`go test ./pkg/dataflow/` → all 115 ginkgo specs + all tests pass on head, including the PR's
own `TestGenerateNodeAffinitySortsCustomizedAffinityAnnotations` and the tightened
`TestGenerateNodeLabels` (whose old "compare lengths only" fallback used to mask exactly this
nondeterminism; the PR correctly removed it).

### Harness-bites check
The fix was temporarily reverted in production code on the head worktree (range loop restored,
`utils` import dropped) and C1 re-run: **FAIL** with the ordering mismatch
(`results/bites-check.txt`) — the contract test genuinely bites. Production code was then
restored; the branch diff contains **only** harness files.

### Pre-existing, out-of-PR observation
`go test ./pkg/utils/` fails on **both** base and head with
`TestCheckMountPointBroken` (`Expected: nil, Actual: 'checking mounted failed: test'`).
Attributed to the environment/pre-existing code, not this PR. Filed in the review ledger as a
non-blocking, `introducedByPR: false` note.

## Downstream impact (code-path reading, not runtime)
The generated affinity flows into `OperationStatus.NodeAffinity` (dataload/datamigrate status
handlers) and is consumed by `InjectAffinityByRunAfterOp` → `injectRequiredAffinity` /
`injectPreferredAffinity` (`pkg/dataflow/affinity.go`), which scan `MatchExpressions` by key —
order-insensitive. So the reordering carries no scheduling-semantics risk; the value of the fix
is determinism (stable CR status across reconciles, diff-able/GitOps-friendly output, and
tests that can assert exact equality).

## Live run notes
Not applicable (see table).

## Proposed fixes (NOT applied to production here)
- None required: the PR's fix is correct as written.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/dataflow-affinity-sort-claude` (production code untouched), so
it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/dataflow-affinity-sort-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/dataflow-affinity-sort pkg/dataflow/affinity_sort_verify_test.go
   ```
2. Re-run (resolves the current PR head itself when no ref is given):
   ```bash
   bash docs/verification/dataflow-affinity-sort/scripts/re-verify.sh
   ```
   Exit 0 iff all contract claims pass. `TestVerifyP0_BaseMapIterationNondeterministic` skips
   by default; run it only on the base branch with `FLUID_VERIFY_PREMISE=1` (it is the premise
   canary — on patched code it flips to FAIL, which is expected and is why C1 replaces it).
3. Review delta: `re-verify.sh` prints the `last-reviewed..head` range from `.last-reviewed`;
   advance the marker after each round (`echo <head> > .last-reviewed`, commit, push).

Kickoff prompt for a fresh agent: *"You are resuming a review verification. Read
docs/verification/dataflow-affinity-sort/README.md on branch
verify/dataflow-affinity-sort-claude of https://github.com/cheyang/fluid.git, run
scripts/re-verify.sh, and report per-claim Fixed/Still-broken/Partial with the polarity rules
from the README."*
