# Verification: PR #6200 — derive worker node affinity during status construction

- **PR**: https://github.com/fluid-cloudnative/fluid/pull/6200 (`perf(cache): derive worker node affinity during status construction to avoid duplicate read`), head `6e3345ef3359bd3b7d6b829715f1db57b97fb043`
- **Linked issue**: #5879 (OPEN) — "get node affinity for worker do not call kubeclient.GetStatefulSet in every status update cycle"
- **Base (merge-base)**: `d8b37f28d645ae5cf3f0d853a79fc72ad222f968`
- **Reviewer**: Reviewer B (Codex), round 2026-10-10. Production code untouched; this branch adds test files + docs only.
- **Note**: this is a *fresh* harness for the current head. A previous revision of this PR (head `1d66cffe`, engine-level caching design) was verified on branch `verify/cache-worker-affinity-single-read-codex`; the author has since rewritten the PR (no engine cache, `GetNodeAffinity` fully removed), so this round re-verifies from scratch on a new branch.

## P0 — premise verdict: **CONFIRMED**

Claim (issue #5879): the worker node-affinity path performs a dedicated workload Get on every status update cycle.

Reproduced on the **base** branch with a Get-counting client wrapped around
`CacheEngine.CheckAndUpdateRuntimeStatus` (the per-reconcile status sync entry point):

| layer | base (d8b37f28) | PR head (6e3345ef) |
|---|---|---|
| unit (fake client) | **2.0 worker ASTS Gets / cycle** (`results/base-unit.txt`) | **1.0 / cycle** (`results/head-unit.txt`) |
| integration (envtest, real API server) | **2 Gets / cycle** (`results/base-envtest.txt`) | **1 / cycle** (`results/head-envtest.txt`) |

Mechanism on base: `setWorkerComponentStatus` calls `ConstructComponentStatus` (Get #1)
then `GetNodeAffinity` (Get #2). The PR derives affinity from the single status read.
Component match: report names the CacheRuntime worker path; the PR touches exactly
`pkg/ddc/cache/{component,engine}`. The `fixes #5879` claim is sound in substance: after
the PR there is no dedicated affinity fetch at all (affinity piggybacks on the mandatory
status read).

Caveat on impact framing: the controller-runtime client is informer-backed for reads in
production, so the eliminated cost is a cached Get + deepcopy per cycle, not necessarily a
live API-server round trip. The cleanup is still strictly positive (fewer calls, and the
status/affinity pair now comes from one consistent read).

## Findings → tests → results

| id | claim | test | polarity | layer | base | head | verdict |
|---|---|---|---|---|---|---|---|
| P0 | duplicate worker Get per status cycle | `TestZZVerifyPR6200SingleWorkerGetPerCycle` | contract (asserts fixed behavior) | unit | FAIL (2.0/cycle) | PASS (1.0/cycle) | confirmed on base; fixed on head |
| P0 | same, real API server | `TestZZVerifyPR6200EnvtestSingleRead` | contract | integration | FAIL (2/cycle) | PASS (1/cycle) | confirmed on base; fixed on head |
| V1 | observable behavior unchanged: `status.CacheAffinity` = merge(worker nodeSelector, worker pod affinity), worker replica status mirrors workload | `TestZZVerifyPR6200AffinityParity` | contract | unit | PASS | PASS | no behavior delta |
| V2 | missing worker workload → error propagates, ready=false | `TestZZVerifyPR6200WorkerMissingError` | contract | unit | PASS | PASS | error semantics unchanged |
| V3 | out-of-band nodeSelector change reflected next cycle (zero staleness) | `TestZZVerifyPR6200OutOfBandNodeSelectorReflected` (+ envtest equivalent) | contract | unit + integration | PASS | PASS | freshness preserved |
| V1-harness | parity test bites | mutation: `MergeNodeSelectorAndNodeAffinity(nil, nil)` in ASTS manager | — | unit | — | FAIL under mutation, PASS after revert | harness proven (`results/mutation-check.txt`) |

## How to run

Unit layer (fake client, no prerequisites):

```bash
go test ./pkg/ddc/cache/engine/ \
  -run 'TestZZVerifyPR6200SingleWorkerGetPerCycle|TestZZVerifyPR6200AffinityParity|TestZZVerifyPR6200WorkerMissingError|TestZZVerifyPR6200OutOfBandNodeSelectorReflected' \
  -count=1 -v
```

Integration layer (needs envtest binaries; auto-detected from `$KUBEBUILDER_ASSETS` or
`~/.local/share/kubebuilder-envtest/k8s/<ver>-linux-amd64`):

```bash
go test ./pkg/ddc/cache/engine/ -run 'TestZZVerifyPR6200EnvtestSingleRead' -count=1 -v
```

Live layer (L3): skipped per round scope (unit + integration only). If ever needed, the
observable signal is a CacheRuntime reconcile loop issuing exactly one
AdvancedStatefulSet Get per cycle (e.g. via API-server audit log or client metrics).

## Harness-bites check

The P0 contract test is red exactly where the duplicate read exists (base) and green on
the PR head — that base/head split is the primary bites proof. For the behavior-parity
tests, a scratch mutation of the ASTS manager (`MergeNodeSelectorAndNodeAffinity(nil, nil)`)
turned `TestZZVerifyPR6200AffinityParity` and `TestZZVerifyPR6200OutOfBandNodeSelectorReflected`
red (`results/mutation-check.txt`); the mutation was reverted and `git status` confirms
production code is untouched.

## Pre-existing, unrelated failures (scope note)

`go test ./pkg/ddc/cache/engine/` shows 12 ginkgo spec failures in `ufs_test.go` /
`sync_test.go` (mount/exec-mock tests). Identical count reproduces on the pristine base
branch (`results/preexisting-failures-base.txt` vs `results/preexisting-failures-head.txt`),
so they are local-environment noise, not caused by this PR. The PR's own lanes
(`unittest`, `kind-e2e-test`) are green upstream.

## Continuing after a fix / on another machine

There is no pending fix this round: the current head passes the whole harness. If the PR
is updated, run `bash docs/verification/cache-worker-affinity-single-read-r2/scripts/re-verify.sh`
from a checkout of this branch — it grafts the harness onto the current PR head
(auto-fetched from `manifest.pr`) and re-runs both layers, printing per-finding
Fixed / Still-broken / Harness-update. All tests are `contract` polarity: green = good.
`.last-reviewed` records the head reviewed this round; advance it after each round.
