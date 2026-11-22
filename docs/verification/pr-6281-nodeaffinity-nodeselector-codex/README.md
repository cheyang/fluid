# pr-6281-nodeaffinity-nodeselector-codex — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6281
(head `e6d22131915b4fa99c139d396bb0a058a47bd788`, base `253f6bb84f26a906fba301550b5c81ebdd73af35`).
Reviewer: codex (debate pipeline, Reviewer B). This branch is **additive only** — the only
production-tree addition is one Go test file; no production code is modified.

Layers run against the code under review (head `e6d2213`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | pure logic (`TieredLocality.hasRepeatedLocality`) | `go test ./pkg/webhook/plugins/nodeaffinitywithcache/ -run 'TestVerifyPR6281HasRepeatedLocality' -count=1 -v` |
| 2. Integration | plugin `Mutate` against a fake K8s client with a real AlluxioRuntime status accessor (YAML args → runtime-info lookup → injection helpers) | `go test ./pkg/webhook/plugins/nodeaffinitywithcache/ -run 'TestVerifyPR6281Mutate' -count=1 -v` |
| 3. Live | skipped by review policy for this round (see `liveNote` in verify-manifest.json) | — |

> Test polarity: all harness tests are **contract** tests (assert the documented behavior).
> They FAIL on the base branch (bug present) and PASS on the fixed head. No canaries.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom | "when the pod has a `nodeAffinity` that has no required terms (for example only a preferred term on some other label), it does `return false` before it ever looks at `spec.nodeSelector`. The plugin then injects the locality affinity on top of the nodeSelector. … The pod now needs rack-b (nodeSelector) and rack-a (injected affinity) at the same time and stays Pending." (issue #6280) |
| Linked issue | #6280, OPEN, filed 2026-11-20 against master at 253f6bb — still valid (the code it names is unchanged on base) |
| Reported component | `pkg/webhook/plugins/nodeaffinitywithcache` (`TieredLocality.hasRepeatedLocality`) |
| Patched component | `pkg/webhook/plugins/nodeaffinitywithcache/tiered_locaity.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** — the bug reproduces on base exactly as reported |
| Evidence | On base, `TestVerifyPR6281MutateSkipsInjectionWhenPodNodeSelectorHasLocality` fails: `Mutate` injects `RequiredDuringSchedulingIgnoredDuringExecution: {topology.kubernetes.io/rack In [rack-a]}` onto a pod whose own `nodeSelector` is `{topology.kubernetes.io/rack: rack-b}` (results/base-reviewer-harness.txt, results/base-new-tests.txt). The documented behavior (docs/en/operation/tiered_locality_schedule.md §3.3 note 1: "If the application Pod specifies the affinity about tiered locality information (defined in 'spec.affinity' or 'spec.nodeselector'), webhook will not inject the relevant location affinity") is violated on base. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Bug exists on base: early `return false` when `NodeAffinity.Required == nil` skips the `nodeSelector` locality check | 2 | **Confirmed** | base: contract tests fail with the exact injected contradiction from issue #6280; head: same tests pass (results/base-*.txt, results/head-*.txt) |
| B1 | `hasRepeatedLocality` must reach the `nodeSelector` check when required terms are absent (preferred-only or empty nodeAffinity) | 1 | **Fixed on head** | base: 2 contract subtests FAIL, 2 guard subtests pass; head: all 4 pass (results/base-reviewer-harness.txt, results/head-reviewer-harness.txt) |
| B2 | `Mutate` must skip injection (and leave the pod's own affinity/nodeSelector untouched) when the pod pins a locality label via `nodeSelector` despite a preferred-only affinity | 2 | **Fixed on head** | base: FAIL — `rack In [rack-a]` injected onto a `rack-b` pod; head: PASS, nothing injected, pod's own settings untouched |
| G1 | No over-correction: a pod with a preferred-only affinity on an unrelated label and no locality setting must still get the required locality injected | 2 | Holds on base **and** head | `TestVerifyPR6281MutateStillInjectsWhenPodHasNoLocality` passes on both |

## Per-finding detail

### B1 — unit layer
Command: `go test ./pkg/webhook/plugins/nodeaffinitywithcache/ -run 'TestVerifyPR6281HasRepeatedLocalityNodeSelector' -count=1 -v`
- Base: subtests "preferred-only affinity on unrelated label + locality nodeSelector" and "empty node affinity + locality nodeSelector" return `false`, expected `true` → FAIL.
- Head: all four subtests PASS, including the over-correction guard ("no locality anywhere" still returns `false`).

### B2 — integration
Command: `go test ./pkg/webhook/plugins/nodeaffinitywithcache/ -run 'TestVerifyPR6281MutateSkipsInjectionWhenPodNodeSelectorHasLocality' -count=1 -v`
- Base: injected `RequiredDuringSchedulingIgnoredDuringExecution = {rack In [rack-a]}` on top of `nodeSelector {rack: rack-b}` → the Pending deadlock from issue #6280 → FAIL.
- Head: no required affinity injected; the pod's own preferred term (1) and nodeSelector (`rack-b`) untouched → PASS.

### Harness-bites check
The production diff between base and head is exactly the fix, so "red on base / green on head"
*is* the bites check; no temporary manual fix was needed. Guards G1 (Mutate still injects when
the pod sets no locality) and the B1 guard subtests pass on both revisions, proving the tests
distinguish the bug from healthy behavior rather than failing for unrelated reasons.
Full package on base with the harness grafted shows no other failures (results/base-full-package.txt);
full `./pkg/webhook/...` tree on head is green (results/head-webhook-tree.txt).

## Proposed fixes (NOT applied here)
None needed — the PR itself is the fix and it is verified correct.

## Continuing after future revisions (possibly on another machine)

The harness is on branch `verify/pr-6281-nodeaffinity-nodeselector-codex` of the reviewer's fork
(https://github.com/cheyang/fluid.git). Production code untouched.

```bash
git fetch https://github.com/cheyang/fluid.git verify/pr-6281-nodeaffinity-nodeselector-codex
git checkout verify/pr-6281-nodeaffinity-nodeselector-codex
bash docs/verification/pr-6281-nodeaffinity-nodeselector-codex/scripts/re-verify.sh   # auto-fetches the current PR head
```

- All tests are contract polarity: green = fixed, red = still broken.
- `.last-reviewed` pins `e6d22131915b4fa99c139d396bb0a058a47bd788`; the incremental review delta
  for the next round is `e6d2213..<new head>`.
- Kickoff prompt for a fresh agent: "Re-run the PR #6281 verification harness on branch
  verify/pr-6281-nodeaffinity-nodeselector-codex (fork https://github.com/cheyang/fluid.git)
  with `bash docs/verification/pr-6281-nodeaffinity-nodeselector-codex/scripts/re-verify.sh`
  and report per-finding Fixed / Still-broken / Harness-update."
