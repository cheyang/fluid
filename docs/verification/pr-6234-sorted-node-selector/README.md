# pr-6234-sorted-node-selector — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6234
(reviewer: codex). Production code is untouched on this branch; the harness is
five additive `zz_verify_pr6234_test.go` files plus this directory.

Layers run against the code under review (head `c9dfc9f43c4924f558e6a2328194ac1e89c1e11e`,
base `a01c038bcc115e46f0fd74faf5e5ca5e9c6aa840`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `getRequiredSchedulingTerm`, `RequireNodeWithFuse.Mutate`, tiered-locality term builders, `MergeNodeSelectorAndNodeAffinity` control | `go test -mod=vendor -count=1 -run 'TestVerifyPR6234' ./pkg/utils/kubeclient ./pkg/webhook/plugins/requirenodewithfuse ./pkg/webhook/plugins/nodeaffinitywithcache` |
| 2. Integration (fake client) | full PV create paths `CreatePersistentVolumeForRuntime` / `EFCEngine.createFusePersistentVolume` through a controller-runtime fake client | `go test -mod=vendor -count=1 -run 'TestVerifyPR6234' ./pkg/utils/dataset/volume ./pkg/ddc/efc` |
| 3. Live | skipped per debate policy (no KUBECONFIG layer) | — |

> Test polarity: ALL tests here are contract tests (assert intended, sorted/deterministic
> behavior). They FAIL where the unsorted behavior is present and PASS where it is fixed.
> C1 is a control: it passes on both base and head.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "several pre-existing sites iterate over Go maps without key sorting when building NodeSelectorTerms or MatchExpressions … map iteration order varies between admissions, resulting in non-deterministic term order in mutated pod specs" (issue #6220 body) |
| Linked issue | #6220, OPEN, created 2026-10-24, still valid |
| Reported component | `pkg/webhook/plugins/nodeaffinitywithcache`, `pkg/webhook/plugins/requirenodewithfuse`, `pkg/utils/dataset/volume/create.go`, `pkg/ddc/efc/create_volume.go` |
| Patched component | exactly those four sites + new `utils.ToNodeSelectorRequirements` helper + `kubeclient.MergeNodeSelectorAndNodeAffinity` refactor |
| Component match | Yes |
| **Verdict** | **Confirmed** — all four named sites reproduce non-deterministic ordering on base |
| Evidence | results/base-unit.txt: contract tests fail on base with observed unsorted orders `[zone arch region]`, `[region zone arch]`, preferred term[0]=`runtime-b` before `runtime-a`, etc. results/head-unit.txt: the same four tests pass on head. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0/S1 | `requirenodewithfuse.getRequiredSchedulingTerm` emits unsorted MatchExpressions on base | 1 | Confirmed on base; fixed on head | base: `got [zone arch region], want [arch region zone]` (results/base-unit.txt); head: PASS |
| P0/S2 | `nodeaffinitywithcache` tiered-locality term order non-deterministic on base | 1 | Confirmed on base; fixed on head | base: `term[0].key=…runtime-b, want …runtime-a`; head: PASS |
| P0/S3 | `CreatePersistentVolumeForRuntime` PV node affinity unsorted on base | 2 | Confirmed on base; fixed on head | base: `got [region zone arch]`; head: PASS |
| P0/S4 | `EFCEngine.createPersistentVolumeForRuntime` PV node affinity unsorted on base | 2 | Confirmed on base; fixed on head | base: `got [zone arch region]`; head: PASS |
| C1 | Control: `MergeNodeSelectorAndNodeAffinity` (already sorted by #6217) | 1 | PASS on base and head — harness is valid and the PR's refactor is behavior-preserving | both results files |
| F1 | `RequireNodeWithFuse.Mutate` still iterates the `runtimeInfos` map without ordering, so the ORDER of injected NodeSelectorTerms is non-deterministic for pods bound to multiple runtimes | 1 | **Confirmed — still present on head** | head: `got [sel-mmm sel-zzz sel-aaa], want sorted [sel-aaa sel-mmm sel-zzz]` (results/head-unit.txt) |

## Per-finding detail

### F1 — RequireNodeWithFuse.Mutate outer map iteration left unsorted (minor)

`pkg/webhook/plugins/requirenodewithfuse/require_node_with_fuse.go` `Mutate` loops
`for _, runtime := range runtimeInfos` (a `map[string]base.RuntimeInfoInterface`) and appends
one `NodeSelectorTerm` per runtime. The PR sorted the MatchExpressions *inside* each term via
`utils.ToNodeSelectorRequirements`, and sorted the analogous runtime-map loops in
`nodeaffinitywithcache` via `utils.OrderedKeys` — but this outer loop was not ordered, so for a
pod bound to multiple runtimes with global-fuse selectors the mutated pod spec still varies
between admissions (only in term order; OR-semantics, so scheduling behavior is unaffected).

- Mechanism: Go map iteration order is randomized per `range` execution.
- Command: `go test -mod=vendor -count=1 -run 'TestVerifyPR6234MutateTermOrderDeterministic' ./pkg/webhook/plugins/requirenodewithfuse`
- Observed (head): FAIL, first iteration already yields `sel-mmm,sel-zzz,sel-aaa`.
- Expected: deterministic (runtime-name sorted) term order, matching the sibling plugin.
- Harness-bites check: applying the one-line sketch fix `for _, name := range utils.OrderedKeys(runtimeInfos) { term, err := getRequiredSchedulingTerm(runtimeInfos[name]) … }`
  turns the test green (results/head-f1-fixcheck.txt); scratch fix reverted afterwards.

Impact is cosmetic (no reconcile loop re-diffs webhook-mutated pod specs; term order is
semantically irrelevant), hence minor — but it is the same class of non-determinism the PR
exists to eliminate, in a file the PR already touches, and issue #6220's stated goal is "fully
deterministic pod spec mutation".

## Proposed fixes (NOT applied to production here)

- **F1**: iterate `utils.OrderedKeys(runtimeInfos)` in `RequireNodeWithFuse.Mutate` (verified
  green against the harness; see above).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr-6234-sorted-node-selector-codex`
(fork `https://github.com/cheyang/fluid.git`), production code untouched.

1. Graft onto updated code: `bash docs/verification/pr-6234-sorted-node-selector/scripts/re-verify.sh`
   (no ref needed — resolves the current PR head from `manifest.pr`; grafts harnessPaths onto it).
2. Prereqs: Go toolchain only; the repo vendors its deps (`-mod=vendor`).
3. Read results via polarity: all six tests are contract tests — green means fixed. F1 red
   today is the expected open finding; S1–S4 green at head confirm the PR's own fix; C1 must
   stay green on any ref (harness control).
4. Harness-bites: `git stash` the harness onto base `a01c038b` (or checkout base in a worktree,
   copy the five `zz_verify_pr6234_test.go` files in, run the two layer commands) — S1–S4 and
   F1 must go red there, C1 must stay green.
5. Advance `.last-reviewed` to the reviewed head after each round.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/pr-6234-sorted-node-selector-codex
(remote https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6234 confirmed the premise (four
map-iteration sites produced non-deterministic NodeSelectorRequirements/terms on base) and
one open minor finding F1 (RequireNodeWithFuse.Mutate outer runtimeInfos loop still
unordered). Read docs/verification/pr-6234-sorted-node-selector/README.md, run
docs/verification/pr-6234-sorted-node-selector/scripts/re-verify.sh against the current PR
head, and report observed-vs-expected per finding (contract polarity: green = fixed).
```
