# tiered-locality-nodeselector — bug verification (PR #6281, issue #6280)

Reviewer: **Claude (Reviewer A)** in the two-reviewer debate pipeline.
Harness branch: `verify/tiered-locality-nodeselector-claude` on the reviewer's fork
(`cheyang/fluid`), based on the PR head `e6d2213` (production code untouched — the only
code change on this branch is the additive test file
`pkg/webhook/plugins/nodeaffinitywithcache/tiered_locaity_claude_verify_test.go`).

Layers run (unit + package sweep; no live/KUBECONFIG layer, per debate setup):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `TieredLocality.hasRepeatedLocality` table incl. the bug cases + `NodeAffinityWithCache.Mutate` on the fake client | `go test ./pkg/webhook/plugins/nodeaffinitywithcache/ -run 'TestClaudeVerify' -v` |
| 1b. Package sweep | whole `pkg/webhook/...` tree on the PR head (regression check) | `go test ./pkg/webhook/...` |

> All tests are **contract** polarity: they assert the documented behavior
> (docs/en/operation/tiered_locality_schedule.md §3.3 note 1) and FAIL on the
> unfixed base, PASS on the PR head. No canaries.

## Problem premise (P0)

Run against the **base** branch (`253f6bb`) without the patch, because the question is
whether the problem exists today.

| | |
|---|---|
| Claimed symptom | pod pinning its own locality label via `nodeSelector` (with a preferred-only nodeAffinity on another label) still gets the locality affinity injected on top: "The pod now needs rack-b (nodeSelector) and rack-a (injected affinity) at the same time and stays Pending." (issue #6280) |
| Linked issue | #6280, OPEN, created 2026-11-20 (2 days before this review), no labels |
| Reported component | `pkg/webhook/plugins/nodeaffinitywithcache` — `TieredLocality.hasRepeatedLocality` |
| Patched component | `pkg/webhook/plugins/nodeaffinitywithcache/tiered_locaity.go` (only production file touched) |
| Component match | **Yes** — exact file |
| Docs contract | §3.3: "If the application Pod specifies the affinity about tiered locality information (defined in 'spec.affinity' or 'spec.nodeselector'), webhook will not inject the relevant location affinity, and the user's configuration will be kept." |
| **Verdict** | **Confirmed** |
| Evidence | `results/base-unit.txt` — on base, `TestClaudeVerifyMutateSkipWhenNodeSelectorHasLocality` fails with the *exact* reported injection: `NodeSelectorTerms: [{rack In [rack-a]}]` injected onto a pod whose nodeSelector pins `rack-b`; the two fallback subtests fail (`hasRepeatedLocality` = false, want true). Controls on the same run pass, proving the harness targets the reported path, not an unrelated breakage. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Premise: locality affinity injected on top of a locality nodeSelector when nodeAffinity has no required terms | 1 | **Confirmed on base; fixed on head** | `results/base-unit.txt` (red) vs `results/head-unit.txt` (green) |
| C1 | `hasRepeatedLocality` must fall through to the nodeSelector check when nodeAffinity has no required terms (preferred-only or empty), and `Mutate` must not inject in that case | 1 | **Confirmed, fixed by PR** | contract tests red on base, green on head |
| C2 | Fix must not over-skip: pods *without* a locality nodeSelector must still get the required locality affinity injected | 1 | **Confirmed — no regression** | `TestClaudeVerifyMutateStillInjectsWithoutLocalityNodeSelector` green on both base and head; whole `pkg/webhook/...` tree green on head (`results/head-webhook-pkg.txt`) |

## Harness-bites check

Done by construction of the two runs above: the same harness commit is red on base
`253f6bb` (bug present) and green on head `e6d2213` (fix applied) — the tests exercise
the changed path, not a tautology. Additionally the base run shows the *control*
subtests (non-locality required affinity → nodeSelector fall-through; nil node
affinity; non-locality nodeSelector → no skip) passing on base, i.e. red comes only
from the bug cases.

## Per-finding detail

### P0 / C1 — early `return false` skips the nodeSelector check
Mechanism (base, `tiered_locaity.go:65-67`): inside
`if pod.Spec.Affinity != nil && pod.Spec.Affinity.NodeAffinity != nil`, when
`RequiredDuringSchedulingIgnoredDuringExecution == nil` the function returns `false`
immediately, so the `for name := range pod.Spec.NodeSelector` loop at the bottom is
never reached. `Mutate` therefore injects. For a `sched: required` pod the injection
ANDs with the user's nodeSelector (rack-a ∩ rack-b = ∅ → Pending).

The PR replaces the early return with a guarded loop — behavior delta is exactly
{NodeAffinity != nil, Required == nil, no locality key in preferred terms, locality key
in nodeSelector}: false → true. Nothing else changes; I traced the single caller
(`node_affinity_with_cache.go:111`) and the `utils.InjectNodeSelectorTerms` merge path.

### C2 — no over-skip
`TestClaudeVerifyMutateStillInjectsWithoutLocalityNodeSelector` asserts the required
rack affinity (`rack In [rack-a]` from the runtime's CacheAffinity) is still injected
for a pod without a locality nodeSelector; passes on base and head.

### Observed behavior change beyond the issue (review finding, non-blocking)
A preferred-policy pod (no `fluid.io/dataset.<name>.sched: required` label) with a
locality nodeSelector + preferred-only affinity used to get the preferred tiers
injected on base (fluid.io/node + zone + user's own term = 3 terms); on head it gets
nothing but its own term (1). This is the documented contract and matches what already
happened on base for the same pod *without* any nodeAffinity, so it is a consistency
fix, not a regression — but the PR's tests only cover the `sched: required` variant.
Throwaway A/B run (not committed): base `preferred=3`, head `preferred=1`.

## Proposed fixes (NOT applied to production here)

None needed — the PR's own fix is correct and minimal. Optional hardening (review
nits, not applied): add a Mutate-level test for the preferred-policy pod with a
locality nodeSelector to pin the skip in that variant too.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/tiered-locality-nodeselector-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/tiered-locality-nodeselector-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/tiered-locality-nodeselector \
     pkg/webhook/plugins/nodeaffinitywithcache/tiered_locaity_claude_verify_test.go
   ```
2. Prereqs: Go toolchain only (go 1.27 used here; the repo's go.mod governs).
3. Re-run:
   ```bash
   bash docs/verification/tiered-locality-nodeselector/scripts/re-verify.sh   # auto-resolves the current PR head
   # or explicitly:
   go test ./pkg/webhook/plugins/nodeaffinitywithcache/ -run 'TestClaudeVerify' -v
   go test ./pkg/webhook/...
   ```
4. Polarity: all contract tests — they should be GREEN on fixed code; any red means
   the bug is back (or a Harness-update if the fixtures moved).
5. Harness-bites: graft onto base `253f6bb` and confirm C1's tests go red.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/tiered-locality-nodeselector-claude
(fork cheyang/fluid). Background: a review of fluid PR #6281 (issue #6280, tiered
locality nodeSelector fallback in the NodeAffinityWithCache webhook plugin) produced
a confirmed premise plus contract tests in
pkg/webhook/plugins/nodeaffinitywithcache/tiered_locaity_claude_verify_test.go.
Read docs/verification/tiered-locality-nodeselector/README.md ("Continuing after the
fix") and follow it: run scripts/re-verify.sh (it fetches the current PR head itself),
mind the polarity table (all contract tests; no canaries), and report an
observed-vs-expected table. Clean up scoped test resources; do not run cluster-wide
destructive actions.
```
