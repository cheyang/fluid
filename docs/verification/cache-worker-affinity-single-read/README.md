# Verification — PR #6200: derive worker node affinity during status construction (single read)

- PR: https://github.com/fluid-cloudnative/fluid/pull/6200
- Linked issue: https://github.com/fluid-cloudnative/fluid/issues/5879 (OPEN, 2026-05-14)
- Reviewed head: `1d66cffe96cb327274f029d922e338af0b7e6f13` (base/merge-base: `d8b37f28d645ae5cf3f0d853a79fc72ad222f968`)
- Reviewer: Reviewer B (Codex). Harness is additive test-only; production diff is empty.

## Premise (P0)

Claim (issue #5879 + maintainer comment on PR #5836): `GetNodeAffinity` calls
`kubeclient.GetStatefulSet` **for every status update cycle**, i.e. the worker
workload is read twice per `CheckAndUpdateRuntimeStatus` cycle (once in
`ConstructComponentStatus`, once in `GetNodeAffinity`).

**Verdict: CONFIRMED.** `TestVerifyWorkerGetCountPerStatusCycle` runs unchanged
against the base branch and fails with `observed 2` Gets for the worker
AdvancedStatefulSet in a single status cycle (see `results/unit-p0-base.txt`).
Component match: the issue names the cache-runtime worker affinity path
(`pkg/ddc/cache/component/*statefulset*manager.go` →
`pkg/ddc/cache/engine/status.go`); the PR touches exactly those files.

## Claims and findings

| id | claim | layer | polarity | test | result on PR head |
|----|-------|-------|----------|------|-------------------|
| P0 | base reads the worker workload twice per status cycle | unit (fake client) | contract ("1 Get/cycle") | `TestVerifyWorkerGetCountPerStatusCycle` | PASS on PR head; FAILs on base with observed=2 → premise confirmed |
| B1 | PR head reads the worker workload exactly once per status cycle | unit | contract | `TestVerifyWorkerGetCountPerStatusCycle` | PASS (results/unit-pr-head.txt) |
| B2 | affinity/status from `ConstructComponentStatusAndAffinity` are identical to the legacy two-read path (nodeSelector + node affinity merge) | unit | contract | `TestVerifyAffinityParityWithLegacyGetNodeAffinity` | PASS |
| B3 | against a real API server: 1 worker Get/cycle, `status.CacheAffinity` round-trips via the status subresource, out-of-band nodeSelector change is visible next cycle ("zero staleness") | integration (envtest, k8s 1.36.2) | contract | `TestVerifyWorkerAffinitySingleReadEnvtest` | PASS (results/integration-envtest-pr-head.txt) |

No refuted claims. No code-level defect reproduced; residual review notes are in
the findings document of the debate run (dead `GetNodeAffinity` left on the
interface; `fixes #5879` only halves the per-cycle reads instead of eliminating
the per-cycle fetch the issue literally asks about).

## How to run

```bash
# unit layer (no external deps)
go test ./pkg/ddc/cache/engine/ -run 'TestVerifyWorkerGetCountPerStatusCycle|TestVerifyAffinityParityWithLegacyGetNodeAffinity' -v

# integration layer (envtest; needs kubebuilder assets, e.g. setup-envtest)
export KUBEBUILDER_ASSETS=$(setup-envtest use -p path)   # or an existing bin dir
go test ./pkg/ddc/cache/engine/ -run 'TestVerifyWorkerAffinitySingleReadEnvtest' -v
```

The envtest test skips cleanly when `KUBEBUILDER_ASSETS` is unset.

## Harness-bites check (done this round)

Temporarily reverted the 4 production files to base (`git checkout <merge-base> -- <files>`),
re-ran the contract tests: both FAIL with `observed 2` (see
`results/harness-bites-fix-reverted.txt`), then restored the PR head and
confirmed green. Production diff is empty afterwards.

## Continuing after the fix / on another machine

1. Check out this branch (`verify/cache-worker-affinity-single-read-codex`).
2. `bash docs/verification/cache-worker-affinity-single-read/scripts/re-verify.sh`
   (no args: fetches the current PR head from `manifest.pr`, grafts the harness,
   runs unit + integration layers, prints per-finding Fixed/Still-broken).
   Integration layer needs `KUBEBUILDER_ASSETS` exported; without it the envtest
   test skips (counts as neutral, re-run with assets for the full signal).
3. Polarity: all tests are contract tests — green = behavior correct. If a future
   fix round intentionally changes behavior, flip expectations accordingly.
4. `.last-reviewed` records the reviewed head; advance it after each round.

Copy-paste kickoff for a fresh agent:
> Review state for PR https://github.com/fluid-cloudnative/fluid/pull/6200 lives on
> branch verify/cache-worker-affinity-single-read-codex of https://github.com/cheyang/fluid.git.
> Read docs/verification/cache-worker-affinity-single-read/README.md, then run
> docs/verification/cache-worker-affinity-single-read/scripts/re-verify.sh.
