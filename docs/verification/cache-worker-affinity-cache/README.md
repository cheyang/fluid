# cache-worker-affinity-cache — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6200
(`perf(cache): avoid fetching statefulset for node affinity on every status update cycle`,
head `106b165013bb23896e957557df3b9701a6667441`, base `d8b37f28d645ae5cf3f0d853a79fc72ad222f968`).

Layers run against the **code under review** (PR head `106b1650`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `CacheEngine.setWorkerComponentStatus` / `CheckAndUpdateRuntimeStatus` with a counting fake client | `go test ./pkg/ddc/cache/engine/ -run 'TestVerifyWorkerAffinityFetchCount\|TestVerifyStaleCacheAffinityAfterWorkloadChange\|TestVerifyCacheAffinityPointerAliasing' -v` |
| 2. Integration | same code against a real API server (envtest, k8s 1.36.2), counting HTTP GETs at a wrapped transport, with a direct client vs an informer-cached client | `KUBEBUILDER_ASSETS=$HOME/.local/share/kubebuilder-envtest/k8s/1.36.2-linux-amd64 go test ./pkg/ddc/cache/engine/ -run 'TestVerifyAPIServerLoadPremise' -v -timeout 10m` |
| 3. Live | skipped per review setup (no KUBECONFIG layer) | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.
> F1 is a contract test (currently RED on the PR head). F2 is a canary (currently GREEN).
> F3 is a characterization contract (GREEN both before and after; it documents where the
> premise's claimed load does and does not exist).

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "`StatefulSetManager` method `GetNodeAffinity` calls kubeclient.GetStatefulSet for every status update cycle. Since the StatefulSet spec (nodeSelector + affinity) rarely changes, this adds unnecessary API server load." (issue #5879) |
| Linked issue | #5879, OPEN, filed 2026-05-14 by xliuqq, label `features` — still valid |
| Reported component | CacheRuntime cache engine worker status update (`pkg/ddc/cache/engine`) |
| Patched component | `pkg/ddc/cache/engine/{engine.go,status.go}` |
| Component match | Yes |
| **Verdict** | **Confirmed** (mechanism), with an **impact correction** (F3): the per-cycle fetch exists on base, but it is an informer-cache read in fluid's default deployment, not API-server load |
| Evidence | base: 4 worker ASTS Gets across 2 cycles (`results/unit-base.txt`), envtest direct client: 4 API GETs / 2 cycles, cached client: 0 (`results/integration-base.txt`) |

Mechanism verification (base branch, harness grafted, production untouched):

| client | base (`d8b37f28`) | PR head (`106b1650`) |
|---|---|---|
| fake client, worker ASTS `client.Get` count over 2 cycles | 4 (2 per cycle) | 3 (2 then 1) |
| envtest, direct (uncached) client: API-server GETs of worker ASTS / 2 cycles | 4 | 3 |
| envtest, informer-cached client (production default): API-server GETs / 2 cycles | **0** | **0** |

The cached-client row matters: `cmd/cache/app/cache.go:123` wires
`NewClient: controllers.NewFluidControllerClient`, which only disables the cache for
Secrets (`pkg/controllers/manager.go:45-49`), and controller-runtime always injects the
manager's informer cache as the read path (`vendor/sigs.k8s.io/controller-runtime/pkg/cluster/cluster.go:218-226`).
So in the default deployment the "duplicate Get" never leaves the process; the PR saves
one informer lookup + ASTS deepcopy per status cycle, not API-server traffic.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | redundant per-cycle worker ASTS fetch exists on base | 1+2 | Confirmed | base 4 Gets / 2 cycles vs PR 3; `results/unit-base.txt`, `results/integration-base.txt` |
| F1 | once `status.cacheAffinity` is set it never tracks the worker workload's affinity again | 1 | **Confirmed** (contract test RED on PR head, GREEN on base and under always-fetch bites check) | `results/unit-pr-head.txt`, `results/bites-check.txt` |
| F2 | fetch branch aliases `e.cacheAffinity` and `status.CacheAffinity` (other branches DeepCopy) | 1 | Confirmed (canary GREEN) | `results/unit-pr-head.txt` |
| F3 | premise's "API server load" does not occur with the production-default cached client | 2 | Confirmed | `results/integration-{base,pr-head}.txt` |

## Per-finding detail

### F1 — stale `status.cacheAffinity` after worker workload affinity changes (contract, RED on PR head)

`TestVerifyStaleCacheAffinityAfterWorkloadChange` seeds a CacheRuntime whose
`status.cacheAffinity` holds affinity A (`topology.kubernetes.io/zone=zone-a`) while the
worker AdvancedStatefulSet now selects `disktype=ssd` (affinity B — e.g. after the ASTS
was re-created from an updated CacheRuntime/CacheRuntimeClass spec, or edited directly;
fluid itself never updates affinity on an existing ASTS: `reconcileStatefulSet` is
create-only and `SyncComponentSpec` only syncs image/resources/replicas).

- base: one status cycle updates `status.cacheAffinity` to B — PASS.
- PR head: `status.cacheAffinity` stays A — FAIL. Because the engine is long-lived
  (`CacheRuntimeReconciler.GetOrCreateEngine` caches engines in a map) and the
  `status.CacheAffinity != nil` branch backfills `e.cacheAffinity` from persisted status,
  the affinity is never re-fetched again, even across controller restarts.
- Blast radius: `status.cacheAffinity` is consumed by the `nodeaffinitywithcache`
  webhook (`pkg/webhook/plugins/nodeaffinitywithcache/node_affinity_with_cache.go:159,194`)
  to inject required/preferred node terms into application pods, and copied into
  ThinRuntime status (`pkg/ddc/thin/referencedataset/sync.go:235`). Stale terms can pin
  app pods to nodes without cache workers, or make them unschedulable.

This matches the Copilot reviewer's "missing cache invalidation" comment; independently
reproduced here.

### F2 — pointer aliasing in the fetch branch (canary, GREEN on PR head)

`TestVerifyCacheAffinityPointerAliasing`: in the `else` branch,
`e.cacheAffinity = affinity; status.CacheAffinity = affinity` store the same pointer;
the other two branches DeepCopy. Harmless today (nothing mutates either reference
in-place); latent fragility. Canary flips red once the fetch branch DeepCopy-copies.

### F3 — premise impact correction (contract, GREEN on both)

`TestVerifyAPIServerLoadPremise` wraps the envtest `rest.Config` transport and counts
single-object API-server GETs of the worker ASTS across two status cycles:
direct client = 4 (base) / 3 (PR head); informer-cached client = 0 (both). The PR's
stated motivation ("API server load") is inaccurate for the default deployment; the
benefit is CPU-level (one fewer informer lookup + ASTS deepcopy per cycle).

## Harness-bites check

`results/bites-check.txt`: on the PR head, `status.go` was temporarily patched to
always fetch (pre-PR behavior); F1's contract test went GREEN; the patch was reverted
and `git status` confirmed the production diff is empty.

## Proposed fixes (NOT applied to production here)

- **F1**: either (a) accept the tradeoff and document it on the caching code +
  `status.cacheAffinity` API comment, or (b) avoid caching entirely: have
  `ConstructComponentStatus` (which already Gets the worker ASTS every cycle) also
  return the merged node affinity — same API savings, zero staleness, at the cost of a
  `ComponentManager` interface change, or (c) invalidate `e.cacheAffinity` when the
  worker workload's UID changes.
- **F2**: `status.CacheAffinity = affinity.DeepCopy()` in the fetch branch.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cache-worker-affinity-cache-codex` (remote
`reviewfork` = https://github.com/cheyang/fluid.git); production code untouched.

1. Graft onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-worker-affinity-cache-codex
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cache-worker-affinity-cache \
     pkg/ddc/cache/engine/verify_pr6200_codex_test.go \
     pkg/ddc/cache/engine/verify_pr6200_aliasing_prhead_test.go \
     pkg/ddc/cache/engine/verify_pr6200_envtest_test.go
   ```
   or run `bash docs/verification/cache-worker-affinity-cache/scripts/re-verify.sh`
   from a checkout of this branch (it auto-discovers the current PR head from
   `manifest.pr`, grafts, runs unit + integration, and prints Fixed/Still-broken per
   finding honoring polarity).
2. Prereqs: Go toolchain; layer 2 needs envtest binaries (`KUBEBUILDER_ASSETS`; e.g.
   `setup-envtest use 1.36.2`). If `/tmp` is small, set `GOTMPDIR`/`TMPDIR` to a
   disk-backed dir.
3. Polarity: F1 contract should go GREEN when fixed; F2 canary flips RED when the
   aliasing is removed (invert it then); F3 stays GREEN unless the controller's client
   caching changes.
4. Note: `verify_pr6200_aliasing_prhead_test.go` references the unexported
   `cacheAffinity` field; if a fix removes/renames it, that file is a Harness-update
   (delete or adjust it).
5. `.last-reviewed` = `106b165013bb23896e957557df3b9701a6667441`.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cache-worker-affinity-cache-codex
(remote https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6200 produced findings F1 (stale
status.cacheAffinity, contract test), F2 (pointer aliasing, canary), F3 (premise
impact characterization). Read docs/verification/cache-worker-affinity-cache/README.md
("Continuing after the fix") and follow it: graft the harness onto the fixed code (or
run scripts/re-verify.sh with no args), mind the polarity table, and report an
observed-vs-expected table. Do not touch production code.
```
