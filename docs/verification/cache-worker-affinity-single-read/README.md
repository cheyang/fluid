# cache-worker-affinity-single-read — verification harness

Reproducible evidence for the review of **PR #6200**
(https://github.com/fluid-cloudnative/fluid/pull/6200), Reviewer A (Claude).

Layers run against the **code under review** (PR head `6e3345ef3359bd3b7d6b829715f1db57b97fb043`),
plus the **merge-base** `d8b37f28d645ae5cf3f0d853a79fc72ad222f968` for the premise check:

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (component) | `ConstructComponentStatusAndAffinity` on both managers: deep-copy / no cross-call aliasing | `go test ./pkg/ddc/cache/component/ -run 'TestVerify' -v` |
| 2. Integration (engine, fake client) | whole `CheckAndUpdateRuntimeStatus` cycle: worker Get count, affinity merge semantics, error propagation | `go test ./pkg/ddc/cache/engine/ -run 'TestVerify' -v` |
| 3. Live (real cluster) | **not run** — a cluster IS reachable on this host (`kubectl get no` succeeds, 2+ nodes, aliyun v1.36.2), but the debate-pipeline configuration for this round explicitly restricts stage ② to unit + integration; skipped by configuration, not because the probe failed. |

There is no envtest wiring for `pkg/ddc/cache/**` (no `KUBEBUILDER_ASSETS` usage in the
package; `pkg/controllers/v1alpha1/cacheruntime` has no test files), so the fake-client
ginkgo/Go-test suites are the deepest layer this package itself defines. Full package sweep:
`go test ./pkg/ddc/cache/...` (see results; 12 pre-existing failures, identical on base and PR head).

> Test polarity: all harness tests here are **contract** tests (assert intended behavior).
> On unfixed (base) code the Get-count test FAILS with observed count 2 — that red result is
> the premise reproduction and the harness-bites proof in one.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is whether the
problem exists today.

| | |
|---|---|
| Claimed symptom | issue #5879: "`StatefulSetManager` method `GetNodeAffinity` calls kubeclient.GetStatefulSet for every status update cycle. Since the StatefulSet spec (nodeSelector + affinity) rarely changes, this adds unnecessary API server load." |
| Linked issue | #5879, OPEN, created 2026-05-14, label `features` — still valid, no rewrite of this path since (the linked PR #5836 discussion is the origin) |
| Reported component | CacheRuntime worker status path — `pkg/ddc/cache/component` StatefulSetManager + `pkg/ddc/cache/engine` status update |
| Patched component | exactly those files (`advanced_statefulset_manager.go`, `daemonset_manager.go`, `component_manager.go`, `engine/status.go`) |
| Component match | Yes |
| **Verdict** | **Confirmed** — on merge-base, one `CheckAndUpdateRuntimeStatus` cycle performs **2 Gets of the worker AdvancedStatefulSet** (ConstructComponentStatus + GetNodeAffinity); PR head performs **1** |
| Evidence | results/base-engine-verify.txt (`observed 2`, `observed 4` after two cycles) vs results/pr-head-engine-verify.txt (`1`, `2`) |

Caveat recorded (finding F1, non-blocking): the reconciler's client is the controller-runtime
manager client, whose structured reads are served from the shared informer cache, so the
"API server load" wording in the issue overstates the cost — the actual saving is one
informer-cache lookup plus one `DeepCopy` of the AdvancedStatefulSet per cycle. The duplicate
read itself is real and is gone.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| C1 | exactly 1 worker Get per status cycle (PR's core claim); base does 2 | 2 | **Confirmed** (PR) / reproduced on base | results/pr-head-engine-verify.txt, results/base-engine-verify.txt |
| C2 | affinity semantics preserved: nodeSelector AND affinity.NodeAffinity merged into status.CacheAffinity | 2 | **Confirmed** | results/pr-head-engine-verify.txt (TestVerifyWorkerAffinityMergedFromSelectorAndAffinity — a path the PR's own tests do not cover) |
| C3 | missing worker workload still fails the status cycle (error not swallowed by merged read) | 2 | **Confirmed** | results/pr-head-engine-verify.txt (TestVerifyWorkerMissingErrorPropagation) |
| C4 | derived affinity is a fresh deep copy per call — no cross-call aliasing (the hazard the PR body cites for rejecting engine-level caching) | 1 | **Confirmed** | results/pr-head-component-verify.txt; bite check results/bite-check-sabotage.txt |

## Per-finding detail

- **C1 (PR claim, premise)**: `verifyCountingClient` (harness-local copy of the PR's counting
  wrapper, kept under its own name so the file grafts onto base too) wraps the fake client and
  counts `Get` calls whose key names the worker component. PR head: 1 per cycle, 2 per two
  cycles. Base: 2 per cycle, 4 per two cycles → the duplicate read exists on base and is gone
  on the PR. This is also the harness-bites proof: the contract test is red on unfixed code
  for exactly the right reason.
- **C2 (behavior preservation)**: worker STS seeded with both `nodeSelector: {disktype: ssd}`
  and `affinity.NodeAffinity.Required...{zone: zone-a}`; after one status cycle
  `status.CacheAffinity` holds ONE merged term containing both expressions
  (MergeNodeSelectorAndNodeAffinity appends selector expressions to each existing term).
  Passes on base AND PR head → semantics preserved. Note the PR's own new tests only set
  `nodeSelector`; the Affinity-path coverage comes from this harness.
- **C3 (error propagation)**: no worker STS in the fake client → `CheckAndUpdateRuntimeStatus`
  returns an error and `ready=false`, on both base and PR head.
- **C4 (no aliasing)**: two calls to `ConstructComponentStatusAndAffinity` return independent
  objects; mutating the first result does not leak into the second call or the stored workload.
  A per-call aliasing sabotage does NOT trip this (controller-runtime Get deep-copies), which
  is why the bite check models the real hazard — a cached affinity across calls — and the test
  goes red there, then passes again after revert (results/bite-check-sabotage.txt).

## Live run notes

Not run this round (see layer table). If run later, the observable signal would be the
controller's API-request metrics / client-go cache `cache_read_total` vs `apiserver_request_total`
for `statefulsets` resource: one cache read per status cycle instead of two; and
`status.CacheAffinity` in the CacheRuntime CR tracking an out-of-band nodeSelector change on
the next sync.

## Proposed fixes (NOT applied to production here)

No code defect confirmed that needs a fix. Non-blocking suggestions for the author are in the
review findings (F1 framing note, F2 interface simplification).

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/cache-worker-affinity-single-read-claude` (production code
untouched — commit only adds test files + this docs tree), so it grafts onto whatever the fixed
code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-worker-affinity-single-read-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cache-worker-affinity-single-read \
     pkg/ddc/cache/component/verify_affinity_contract_test.go \
     pkg/ddc/cache/engine/verify_worker_affinity_harness_test.go
   ```
   Or just: `bash docs/verification/cache-worker-affinity-single-read/scripts/re-verify.sh`
   (no argument: resolves the current PR head from `manifest.pr`, grafts, runs, reports).
2. Prereqs: Go toolchain only (module vendor dir is committed; `go test` needs nothing else).
   No envtest assets required — no envtest layer exists for this package.
3. Re-run the two commands in the layer table.
4. Polarity: all tests are contract tests — they must be GREEN. There are no bug-canaries, so
   nothing needs inverting. For the premise check, run the engine harness file against the
   merge-base and expect exactly `TestVerifyWorkerGetCountPerStatusCycle` to fail (that is the
   P0 reproduction, not a regression).
5. Harness-bites: re-do the sabotage in results/bite-check-sabotage.txt if you suspect the
   deep-copy claim, or simply re-run the engine harness on merge-base for the count claim.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cache-worker-affinity-single-read-claude
(github.com/cheyang/fluid). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6200 produced claims C1-C4; a harness
verified them (premise P0 confirmed on merge-base). The PR may have been updated since.
Read docs/verification/cache-worker-affinity-single-read/README.md ("Continuing after the
fix") and follow it: bash scripts/re-verify.sh to graft the harness onto the current PR head
and re-run the unit + integration layers; all tests are contract tests and must pass; the
engine harness run against merge-base d8b37f28 must fail exactly TestVerifyWorkerGetCount-
PerStatusCycle with observed 2 (premise). Report an observed-vs-expected table. Do not run
cluster-wide destructive actions; live layer is out of scope.
```
