# dataflow-runafter-release-codex — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6254
(`fix(dataflow): release a waiting operation whose runAfter was removed`, head `50933d0b` (round 2; round 1 reviewed `cc3947c0`), base `f432bdc8`).

Layers run against the **code under review** (`cc3947c0`), with the premise additionally run against **base** (`f432bdc8`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `DataFlowReconciler.Reconcile` / `reconcileOperationDataFlow` with a fake client (`runafter_removed_verify_test.go`) | `KUBECONFIG=/dev/null FLUID_UNIT_TEST=true CGO_ENABLED=0 go test -count=1 ./pkg/controllers/v1alpha1/dataflow/ -ginkgo.focus='verify PR6254'` |
| 2. Integration | the real reconciler against a real API server (envtest, shipped CRDs, status subresource) (`dataflowenvtest/`) | `KUBEBUILDER_ASSETS=$(ls -d ~/.local/share/kubebuilder-envtest/k8s/1.33.0-linux-amd64) CGO_ENABLED=0 go test -count=1 ./pkg/controllers/v1alpha1/dataflow/dataflowenvtest/` |
| 3. Live | skipped per round policy; the PR's own curvine e2e covers it (`verify_dataload_runafter_removed`, green on 5 k8s versions at head) | see PR body run 43818103797 |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. All harness specs are now CONTRACTS: the F1 canary flipped red when head `50933d0b`
> added the waiting guard, and was inverted per polarity rules (see the round-2 section).

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "a user can remove [spec.runAfter] while the operation is still waiting. `reconcileOperationDataFlow` then dereferences the nil `runAfter` (`runAfter.Namespace`) and panics ... the panic kills dataset-controller ... the controller crash-loops and Dataset and data-operation reconciliation stops for the whole cluster." (PR body) |
| Linked issue | none (`NONE` in PR body); supersedes PR #5527 (same root cause, stale) |
| Reported component | DataFlow reconciler in dataset-controller (`pkg/controllers/v1alpha1/dataflow`) |
| Patched component | `pkg/controllers/v1alpha1/dataflow/operations.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** (real problem) |
| Evidence | base run of the P0 contract specs panics with nil pointer dereference (`results/l1-unit-base-p0-focus.txt`, `results/l2-envtest-base.txt`); replaying the PR's own new unit tests on base fails exactly as the PR body claims, 35 Passed \| 6 Failed (`results/l1-unit-base-pr-tests-replay.txt`). Mechanism confirmed by code reading: base `operations.go` does `len(runAfter.Namespace)` with no nil check, and `cmd/dataset/app/dataset.go` sets no `RecoverPanic` (vendored controller-runtime v0.17.5 recovers only when `RecoverPanic` is explicitly `*bool` true). |

Extra reachability evidence beyond the PR's scenario: the panic does not even require the
user to remove `runAfter`. `DataFlowReconciler.Reconcile` runs all four per-kind reconcile
funcs for one request name, so a waiting DataLoad named `x` makes the reconciler fetch a
same-named DataBackup that has no `runAfter` at all and panic on it (`P0-xkind-contract`,
red on base, `results/l1-unit-base-p0-focus.txt`). The fix removes the whole bug class.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | waiting op with nil runAfter panics the reconciler on base | 1, 2 | Confirmed | panic on base, clean release on head; `results/l1-unit-base-p0-focus.txt`, `results/l2-envtest-base.txt` vs `results/l1-unit-head.txt`, `results/l2-envtest-head.txt` |
| F1 | nil-runAfter early return also fires for *non-waiting* same-name cross-kind objects → spurious `waitingFor.operationComplete=false` status write + misleading "stop waiting" log | 1, 2 | **Fixed** at `50933d0b` (was Confirmed minor at `cc3947c0`) | round 1: canary green on old head (`results/l1-unit-head.txt`, `results/l2-envtest-head.txt`). Round 2: canary flipped red on the new head, inverted to `F1-contract` (untouched: flag stays nil, no status write, no event); green at unit + envtest (`results/reverify/unit.parsed`, `results/reverify/integration.parsed`); bites on `cc3947c0`'s operations.go (`results/l1-unit-bites-harness-prevhead-operations.txt`) |
| F2 | release via runAfter removal (and via preceding-op completion) recorded no Kubernetes event | 1 | **Fixed** at `50933d0b` | `DataOperationWaitingEnded` now recorded on both release paths, only for operations that were waiting; pinned by `F2-contract` specs (removal path, completion path, non-waiting control), green on head, red on `cc3947c0`'s operations.go (`results/reverify/unit.parsed`, `results/l1-unit-bites-harness-prevhead-operations.txt`) |

## Per-finding detail

### P0 (premise) — contract

- Expected on fixed code: no panic, `waitingFor.operationComplete` cleared to false, phase stays Pending, no requeue.
- Observed on base `f432bdc8`: `runtime error: invalid memory address or nil pointer dereference` in all 3 specs (fake client and real envtest apiserver alike).
- Observed on head `cc3947c0`: 44/44 unit specs green (41 PR + 3 harness); 2/2 envtest specs green.
- Author's own claim independently replayed: head test files grafted onto base fail 35/6 (9 with the 3 harness specs included), exactly the numbers in the PR body.

### F1 (canary) — spurious release of non-waiting cross-kind objects

Mechanism: `Reconcile` loops `reconcileFuncs` over all four kinds for one request name and
each kind func calls `reconcileOperationDataFlow` unconditionally for any object that exists
with that name; the `waitingFor` filter lives only in the watch predicate. The new
`if runAfter == nil { return releaseWaitingOperation(...) }` therefore also fires for objects
that are not waiting at all (e.g. a Complete DataBackup sharing namespace/name with a waiting
DataLoad), writing `waitingFor.operationComplete=false` into their status and logging
"spec.runAfter is no longer set, stop waiting" for an object that never waited.

Impact: one spurious status update + one confusing log line per collide-and-reconcile; the
written value (`false`) is semantically inert for a non-waiting object (its own controller
only waits while the flag is `true`, `pkg/ddc/base/operation.go` reconcilePending). On base
this same collision panics, so the PR is strictly an improvement — hence minor, not a blocker.

Fix sketch (NOT applied to production here): gate the release on the fetched status —
`if runAfter == nil && opStatus.WaitingFor.OperationComplete != nil && *opStatus.WaitingFor.OperationComplete` — else return without writing. Harness-bites proof: with that guard,
`F1-canary` flips to red while both P0 contract specs stay green
(`results/l1-unit-head-f1-guard-bites.txt`: 2 Passed | 1 Failed).

## Round 2 (head `50933d0b`, delta `cc3947c0..50933d0b`)

The new head is a fast-forward single commit (`fix(dataflow): only release and record events
for waiting operations`) that addresses the round-1 findings:

- **F1 / MT2 fixed**: the nil-`runAfter` release is gated on the fetched waiting status
  (`isWaitingForPrecedingOperation(opStatus)`), which also consumes the previously unused
  `opStatus` parameter (N2). PR unit + controller tests now pin that non-waiting operations
  are left untouched.
- **F2 fixed**: `releaseWaitingOperation` records `DataOperationWaitingEnded` (new constant in
  `pkg/common/constants.go`) on both release paths, only for operations that were waiting.
- **Q1 fixed**: the `runAfter` field documents the mutability semantics on all four kinds
  (openapi + en/zh api docs regenerated; CRDs carry no descriptions, `crd:maxDescLen=0`, so
  `config/crd` is untouched).
- **N1 fixed**: the e2e fails if no dataset-controller pod restart count can be read.

Re-verification at `50933d0b` (`scripts/re-verify.sh`, unit + envtest layers):

| ID | Verdict | Detail |
|----|---------|--------|
| P0 | FIXED (still green) | release without panic at both layers (`results/reverify/unit.parsed`, `results/reverify/integration.parsed`) |
| F1 | FIXED (canary flipped → inverted to contract) | first re-verify against the new head: canary FAIL = spurious write gone; inverted `F1-contract` now green at both layers |
| F2 | FIXED (new contracts) | 3 `F2-contract` specs green on head |

Harness-bites checks for the new/changed tests, run against `cc3947c0`'s `operations.go`
(grafted onto the head tree so `common.DataOperationWaitingEnded` still resolves):

- PR's own suite: 40 Passed | 8 Failed (the 4 removal entries, 2 never-waited entries,
  completion-path event spec, controller-level same-name spec) — matches the PR body
  (`results/l1-unit-bites-prevhead-operations.txt`).
- This harness: `F1-contract`, `F2-contract` (removal), `F2-contract` (completion) all red;
  P0 contracts stay green (`results/l1-unit-bites-harness-prevhead-operations.txt`).

Full pristine-head package suite: 48/48 green (`results/l1-unit-head-50933d0b.txt`).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/dataflow-runafter-release-codex` (remote `reviewfork`,
https://github.com/cheyang/fluid.git), production code untouched.

```bash
git fetch https://github.com/cheyang/fluid.git verify/dataflow-runafter-release-codex
git checkout verify/dataflow-runafter-release-codex
bash docs/verification/dataflow-runafter-release-codex/scripts/re-verify.sh   # auto-fetches current PR head
```

Layer 1 needs only the Go toolchain. Layer 2 needs envtest assets
(`setup-envtest use 1.33.0`, or point `KUBEBUILDER_ASSETS` at an existing
`.../kubebuilder-envtest/k8s/<ver>-linux-amd64`). Polarity: every harness spec is a CONTRACT
and must be GREEN on the PR head; a red contract is a regression of the merged behavior.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/dataflow-runafter-release-codex (remote
https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6254 confirmed premise P0 (nil runAfter
panics the DataFlow reconciler); round-1 findings F1 (spurious write on non-waiting
same-name objects) and F2 (no release event) were FIXED at head 50933d0b and the harness
is now all-contract. Read docs/verification/dataflow-runafter-release-codex/README.md
and run scripts/re-verify.sh against the current PR head (all contracts must stay green).
Report observed vs expected per finding.
```
