# webhook-deterministic-mutation — verification evidence

Reviewer: Claude (Reviewer A, PR-debate pipeline). PR under review:
https://github.com/fluid-cloudnative/fluid/pull/6262 (round-1 head `ac9e3e127e722fa23030e78993ccb9db9d250316`, round-2 head `08c111a019c5f840fa076561eee2c2df17db92dc`, base `dcb76167b0143a239addc6bed83ed50b45267631`).

The harness is **additive only** — two new test files, production code untouched
(`git diff ac9e3e127 -- pkg/` is empty apart from the test files below).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `assembleMountInfos`, `injectCheckMountReadyScript` (namespace pick), `FilePrefetcher.buildFilePrefetcherConfig` (default fileList) | `go test -count=1 -run 'TestV' ./pkg/application/inject/fuse/ ./pkg/webhook/plugins/fileprefetcher/` |
| 2. Integration | skipped — determinism of pure map-iteration order is fully decidable at unit layer; no envtest cluster adds signal | — |
| 3. Live | skipped per review setup (no KUBECONFIG) | — |

> Polarity: all harness tests are **contract** tests (assert the intended deterministic
> behavior). They FAIL on the base branch (that failure is the reproduction) and PASS on
> the PR head.

## Problem premise (P0) — verdict: **Confirmed**

Claimed symptom (PR body): *"replacing unordered Go map iterations with `utils.OrderedKeys`
… guarantee[s] deterministic pod spec mutations and eliminate[s] webhook mutation diffs /
test flakiness across all injection sites"* — i.e. pod mutations vary run-to-run today.

| | |
|---|---|
| Claimed symptom | pod spec mutation output (postStart command args, chosen ConfigMap namespace, prefetcher fileList env) varies across webhook invocations |
| Linked issue | none closing; follow-up to merged #6217 / #6234 (same determinism series) |
| Reported component | webhook plugins + FUSE application injector |
| Patched component | exactly those files |
| Component match | Yes |
| **Verdict** | **Confirmed** — reproduced on base `dcb76167` |
| Evidence | `results/base-differential.log`: 3 distinct outputs from `assembleMountInfos` over 200 invocations; ConfigMap created in `ns-a:3 / ns-m:6 / ns-z:31` across 40 invocations; 3 distinct `GlobPaths` values over 100 invocations. All three contract tests FAIL on base, PASS on head (`results/head-harness.log`). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0-a | `assembleMountInfos` output varies across invocations on base | 1 | Confirmed (fixed on head) | `results/base-differential.log` vs `results/head-harness.log` |
| P0-b | `injectCheckMountReadyScript` picks a random runtimeInfo's namespace on base → check-mount ConfigMap lands in a random namespace | 1 | Confirmed (fixed on head) | same logs; 3 namespaces observed on base, always `ns-a` on head |
| P0-c | fileprefetcher default `fileList` (built from `runtimeInfos` keys) varies on base | 1 | Confirmed (fixed on head) | same logs; 3 distinct values on base, 1 on head |
| F1 | the PR's new namespace-determinism test invokes the code path only once, so it cannot distinguish deterministic from random selection (passes on base ~1/3 of runs) | 1 | Confirmed as a test-robustness gap (harness supplies the repeated version, which passes on head) | `TestVInjectCheckMountReadyScriptNamespaceDeterministic` |

Full-package sweep on head (12 packages, `-count=1`): all `ok` — `results/head-full-package-tests.log`;
5 sequential repeat runs stable — `results/head-vet-fmt-repeat.log`; `go vet` and `gofmt -l` clean.

## Per-finding detail

### P0-a / P0-b / P0-c — non-deterministic map iteration (the PR's actual target)
Mechanism: Go randomizes map iteration order per iteration. On base,
`assembleMountInfos`, the `runtimeInfos` first-key pick in
`injectCheckMountReadyScript`, and `buildFilePrefetcherConfig`'s default
`pvc://` list all ranged over maps directly, so the mutated pod spec (postStart
command, injected ConfigMap location, prefetcher env) varied per webhook
invocation. The PR replaces those with `utils.OrderedKeys` (sorted).
Observed vs expected: base = multiple distinct outputs (see above); head =
exactly one, sorted. Fix verified.

### F1 — single-invocation determinism test
`mount_point_script_test.go`'s new Context
("should deterministically select the first runtime info's namespace based on key order")
calls `injectCheckMountReadyScript` once. One invocation cannot detect
non-determinism: on base the test passes whenever Go happens to iterate `a-pvc`
first (~1/3 of runs), so as a regression guard it is flaky-by-design. The
assembleMountInfos test in the same PR correctly uses a 50-iteration loop.
The harness's `TestVInjectCheckMountReadyScriptNamespaceDeterministic` (40
iterations, fresh fake client each) is the suggested shape; it passes on head.

## Round 2 (head `08c111a01`, reviewed 2026-11-19)

The PR was force-pushed as a single rewritten commit. The delta
`ac9e3e127..08c111a01` responds to the round-1 findings:

| Round-1 finding | Delta response | Re-verify verdict |
|---|---|---|
| F1 (single-invocation namespace test) | test now loops 50 iterations with a fresh fakeClient per iteration and asserts ns-a=1 / ns-m=0 / ns-z=0 | **FIXED** — and the in-PR test itself is now red on base (`results/round2/base-pr-tests-red-on-base.log`, both determinism specs FAIL on base code) |
| F2 (no-op sorted loops in nodeaffinitywithcache) | all three hunks reverted; file is byte-identical to base (`git diff dcb76167..08c111a01 -- pkg/webhook/plugins/nodeaffinitywithcache/` is empty) | **FIXED** |
| F3 (no-op sort in mountpropagationinjector) | hunk kept, defensive comment added ("Sort keys defensively so debug log output is deterministic") | **FIXED** (matches the recommended drop-or-comment option) |
| F4 (order-blind spec name) | spec renamed to "should mutate pod with multiple runtimeInfos" | **FIXED** |
| F5 (loop-break-to-first-key idiom) | loop now skips nil entries, so the loop form is semantically meaningful | **OBSOLETE** (no change needed) |
| F6 (nil runtimeInfo deref, pre-existing) | nil guard added: `if ri := runtimeInfos[k]; ri != nil` — first *valid* key wins; all-nil degrades to a clean API error in namespace "" instead of a panic | **FIXED** — new contract test `TestVInjectCheckMountReadyScriptNilSkip` passes on round-2 head and panics on round-1 head (`results/round2/nilskip-panics-on-round1-head.log`) |

Full re-verify at round-2 head: all 5 manifest findings **FIXED** (P0-a/b/c, F1, F6) —
`results/round2/unit.parsed`. Full-package sweep on the round-2 head: 12 packages all `ok`
(`results/round2/head-full-package-tests.log`); `go vet` + `gofmt -l` clean
(`results/round2/head-vet-fmt.log`); 5 sequential repeat runs stable (4/4 `ok` each round).
Round-2 evidence lives in `results/round2/`.

## Proposed fixes (NOT applied to production here)
- **F1**: wrap the new namespace-selection test body in a repeated-invocation
  loop (fresh `fakeClient` per iteration) like the harness test does.
- Optional (pre-existing, out of this PR's scope): guard `runtimeInfos[k]`
  against nil in `injectCheckMountReadyScript` the way sibling plugins do
  (`mountpropagationinjector`, `nodeaffinitywithcache`).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/webhook-deterministic-mutation-claude`
(fork `cheyang/fluid`), production code untouched, so it grafts onto whatever
the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/webhook-deterministic-mutation-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/webhook-deterministic-mutation \
     pkg/application/inject/fuse/premise_determinism_test.go \
     pkg/webhook/plugins/fileprefetcher/premise_determinism_test.go
   ```
2. Prereqs: Go toolchain only (module cache; no cluster, no envtest).
3. Re-run: `bash docs/verification/webhook-deterministic-mutation/scripts/re-verify.sh`
   (auto-resolves the current PR head from the manifest `pr` URL) — or
   `go test -count=1 -run 'TestV' ./pkg/application/inject/fuse/ ./pkg/webhook/plugins/fileprefetcher/`.
4. Polarity: all tests are contract tests — they should be GREEN on any code
   that keeps the determinism fix. There are no canaries to invert.
5. Harness-bites: run once against pre-fix code (base `dcb76167`) to confirm
   the tests go red — already recorded in `results/base-differential.log`.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/webhook-deterministic-mutation-claude
(fork cheyang/fluid). Background: a review of fluid PR #6262 (sorted map iteration
for deterministic webhook mutation) produced premise claims P0-a/b/c and test-gap
finding F1; the harness reproduced all of them (base fails / head passes). The PR
may have advanced. Read docs/verification/webhook-deterministic-mutation/README.md
("Continuing after the fix") and follow it: graft the harness onto the current PR
head via scripts/re-verify.sh, re-run the unit layer, mind the polarity table (all
contract tests; no canaries), run the harness-bites check against the base if the
head changed, and report an observed-vs-expected table. Do not run any live-layer
or cluster actions.
```

## Round 3 (2026-11-27) — head `5d4117057` (force-push, history rewritten to one commit)

The PR was force-pushed: `08c111a01` → `5d4117057`. The rewrite **reverts the round-2
namespace-pick fix and deletes its guards**:

- `pkg/application/inject/fuse/mount_point_script.go:43-47` is back to
  `for _, v := range runtimeInfos { namespace = v.GetNamespace(); break }` —
  unordered iteration and unguarded nil deref, byte-identical to base `dcb76167`
  (the only remaining change in that file is the `assembleMountInfos` `OrderedKeys`
  hunk).
- The in-PR 50-iteration namespace-determinism spec (round-2's F1 fix, proven to bite
  on base by both reviewers) was **deleted** from `mount_point_script_test.go`.
- The "should mutate pod with multiple runtimeInfos" spec was deleted from
  `mount_propagation_injector_test.go`.

New in this round (verified fixed): `utils.OrderedKeys` for sidecar `VolumeMounts` and
`ExtraEnvs` in `buildFilePrefetcherSidecarContainer`, `utils.OrderedKeys` in
`datasetusageinjector`, PVC name in the mountpropagation nil-RuntimeInfo error.
New harness contract test `TestVFilePrefetcherSidecarOrderDeterministic` (F7) pins the
sidecar ordering; it is red on base (`results/round3/base-unit-bite.out`:
`VolumeMounts order non-deterministic or unsorted: got [vol-z vol-a vol-m …]`) and green
on head.

### Round-3 re-verify table (all contract polarity)

| ID | Test | base `dcb76167` | prev head `08c111a01` | head `5d4117057` | Verdict |
|----|------|------------------|------------------------|-------------------|---------|
| P0-a | TestVAssembleMountInfosDeterministic | fail | pass | **pass** | Fixed |
| P0-b | TestVInjectCheckMountReadyScriptNamespaceDeterministic | fail | **pass** | **fail** (`map[ns-a:4 ns-m:8 ns-z:28]`) | **REGRESSED — revision-regression** |
| P0-c | TestVDefaultFileListDeterministic | fail | pass | **pass** | Fixed |
| F1 | TestVInjectCheckMountReadyScriptNamespaceDeterministic | fail | **pass** | **fail** | **REGRESSED** (in-PR 50-iteration spec also deleted) |
| F6 | TestVInjectCheckMountReadyScriptNilSkip | fail (SIGSEGV) | **pass** | **fail (SIGSEGV at mount_point_script.go:45)** | **REGRESSED** (pre-existing defect; round-2 bonus fix dropped) |
| F7 | TestVFilePrefetcherSidecarOrderDeterministic | fail | n/a (new) | **pass** | Fixed |

Raw evidence: `results/round3/head-unit.out`, `results/round3/base-unit-bite.out`,
`results/round3/prev-head-08c111a-unit.out`, `results/round3/head-full-package-tests.log`
(the PR's own suite is green at head — the guarding tests were deleted, so nothing fails).
Head suite/vet/gofmt clean; CI all-22 checks green at `5d4117057`.
