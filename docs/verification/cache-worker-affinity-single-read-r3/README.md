# cache-worker-affinity-single-read-r3 — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6200 (head `7b6392c2`, base `d8b37f28`).
Round 3 of this debate topic, rebuilt from scratch on the force-pushed head; production code
untouched (harness = 4 additive `zz_verify_pr6200_*_test.go` files + this docs tree).

Layers run against the **code under review** (`7b6392c2`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | Get-count per status cycle, status/affinity parity, affinity-order stability, status-write churn (fake client) | `go test ./pkg/ddc/cache/engine/ -run 'TestVerifyPR6200' -v -count=1` |
| 2. Integration | real API server (envtest, k8s 1.31.0 assets): CRD round-trip of `status.cacheAffinity`, single-read on a real client, zero-staleness | `KUBEBUILDER_ASSETS=$(ls -d ~/.local/share/kubebuilder-envtest/k8s/* \| sort -V \| tail -1) go test ./pkg/ddc/cache/engine/ -run TestVerifyPR6200EnvtestCacheAffinityRoundTrip -v -count=1` |
| 3. Live | skipped (debate setup: no KUBECONFIG layer) | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy/base code and PASS on
> the PR head. Bug-canary tests (assert current behavior) PASS now and FLIP to red when the
> underlying behavior is fixed — then they must be inverted.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "each `CacheRuntime` status update cycle read the worker `AdvancedStatefulSet` twice" (PR body); issue #5879: "`StatefulSetManager` method `GetNodeAffinity` calls kubeclient.GetStatefulSet for every status update cycle ... this adds unnecessary API server load" |
| Linked issue | #5879, OPEN, filed 2026-05-14, still valid |
| Reported component | `pkg/ddc/cache` worker status path (`GetNodeAffinity`) |
| Patched component | `pkg/ddc/cache/component/*_manager.go` + `pkg/ddc/cache/engine/status.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** (real duplication on base) |
| Evidence | `TestVerifyPR6200SingleWorkerReadPerCycle` FAILS on merge-base with `workerGets == 2` after cycle 1; PASSES on head with `== 1` — see `results/unit-base-p0.txt` vs `results/unit-and-envtest-head.txt` |

Caveat recorded for the debate (finding F2): the CacheRuntime controller's client is
informer-cache backed (`controllers.NewFluidControllerClient` → `NewCacheClientBypassSecrets`)
unless `HELM_DRIVER=secret`, so in the default deployment the eliminated read hit the local
cache, not the API server. The "API server load" framing is exact only for the
`HELM_DRIVER=secret` deployment mode; the change is strictly beneficial either way.

## Round 4 update — head `0182b4f1` (delta `7b6392c2..0182b4f1`)

Delta reviewed this round: a single commit, "refactor(cache): delegate
ConstructComponentStatus to ConstructComponentStatusAndAffinity", touching only
`advanced_statefulset_manager.go` and `daemonset_manager.go` (-22/+4). It implements the
round-3 review recommendation: both `ConstructComponentStatus` methods are now 3-line
delegates of the combined single-read method. Blast radius checked: all 5 production call
sites of `ConstructComponentStatus` (engine status.go master/client paths, worker.go
SetupWorkerInternal, master.go, client.go) still cause exactly one workload Get; the
additionally computed-and-discarded affinity comes from
`kubeclient.MergeNodeSelectorAndNodeAffinity`, a pure in-memory, nil-safe function, so the
delegate cannot introduce a new failure mode. The only observable delta is the log message
string ("start to ConstructComponentStatus" -> "...AndAffinity"); no test or log-scraping
code depends on it.

New contract test `TestVerifyPR6200DelegateConstructComponentStatus`
(`zz_verify_pr6200_delegate_test.go`): for both managers, happy path and not-found path,
`ConstructComponentStatus` must return exactly the same status and error as the combined
method. Harness-bites: a temporary delegate break (DesiredReplicas forced to -7) flips it
red; fix reverted, production diff empty afterwards.

Re-verify on `0182b4f1` (unit + integration, see `results/reverify/`):

| ID | Polarity | Verdict |
|----|----------|---------|
| P0-fix-contract | contract | FIXED (1 worker Get/cycle) |
| parity | contract | FIXED |
| F3 | canary | STILL-PRESENT (pre-existing; delta did not touch pod.go) |
| P0-fix-contract-L2 | contract | FIXED (envtest round-trip green) |
| delegate-r4 | contract | FIXED |

PR's own suites on `0182b4f1`: `pkg/ddc/cache/component` fully green; `pkg/ddc/cache/engine`
shows the same 12 pre-existing `ufs_test.go` failures (gomonkey/`PrepareUFS` mount fixtures,
no reference to the changed code; r3 md5-matched them base-vs-head) — 265 passed / 12 failed
/ 16 skipped (`results/pr-suites-head-r4.txt`).

Also closed this round (PR metadata, not code): the PR body now says `refs #5879` instead of
`fixes #5879` (round-3 finding F1 — issue no longer auto-closes), and explicitly names the
informer-cache vs `HELM_DRIVER=secret` deployment modes (round-3 finding F2).

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Base reads worker ASTS twice per status cycle | 1 | Confirmed | base: `workerGets=2` per cycle (`results/unit-base-p0.txt`) |
| P0-fix | Head reads worker ASTS exactly once per cycle, CacheAffinity still populated | 1, 2 | Confirmed | head: `workerGets==1`, envtest round-trip green (`results/unit-and-envtest-head.txt`) |
| parity | `ConstructComponentStatusAndAffinity` returns the same status and the same merged affinity as the removed two-call path | 1 | Confirmed | `TestVerifyPR6200ParityCombinedMatchesLegacyOutputs` PASS (order-insensitive affinity compare) |
| F1 | `fixes #5879` auto-closes an issue whose stated ask ("do not call ... in every status update cycle") is only partially met (2→1 reads; still one read per cycle) | — | reasoning-only, not code-testable | see findings-codex.md |
| F3 | pre-existing: `MergeNodeSelectorAndNodeAffinity` emits `MatchExpressions` in map-iteration order → `reflect.DeepEqual` gate sees phantom diffs → spurious real status writes when the worker has ≥2 nodeSelector labels | 1 | Confirmed (pre-existing; base behaves identically) | `182/200` order variants; `4/5` (base: `4/5`, later head run `5/5`) cycles rewrote status with no spec change (`results/unit-and-envtest-head.txt`, `results/unit-base-p0.txt`) |

## Per-finding detail

### P0 / P0-fix — single read per status cycle
`zz_verify_pr6200_contract_test.go` wraps a fake client with a counter for Gets of the worker
AdvancedStatefulSet and runs two full `CheckAndUpdateRuntimeStatus` cycles.
- Base (`d8b37f28`): **RED**, `workerGets == 2` after cycle 1 — the premise reproduced.
- Head (`7b6392c2`): **GREEN**, `workerGets == 1` per cycle and `status.CacheAffinity` populated.
The same file compiles on both sides, so the base run doubles as the harness-bites check for
this contract.

### parity — combined method equals legacy two-call outputs
`zz_verify_pr6200_parity_test.go` seeds a worker ASTS with a 2-label nodeSelector plus
required+preferred node affinity, then compares the combined method's outputs against
`ConstructComponentStatus` and a freshly computed `MergeNodeSelectorAndNodeAffinity`
(order-insensitive). PASS on head.

### F3 — pre-existing affinity-order instability → status-write churn
`zz_verify_pr6200_canary_order_test.go` (canaries):
- `...CanaryAffinityExpressionOrderUnstable`: 200 identical `MergeNodeSelectorAndNodeAffinity`
  calls over a 5-label selector → 175-182 produce a different `MatchExpressions` order.
- `...CanaryStatusWriteChurn`: after cycle 1 converges the status, cycles 2..6 keep issuing
  real `Status().Update` writes with no spec change (observed 4/5 and 5/5 across runs; with 5
  labels a no-churn run has probability ≈ (1/120)^5 ≈ 4e-11).
- Identical results on the merge-base → **not introduced by this PR**. It matters here only
  because the PR's stated goal is cutting per-cycle API work, while this pre-existing defect
  issues real API-server *writes* in the same loop (and, in event-driven mode, each write
  re-triggers a reconcile since `runtimeEventHandler.onUpdateFunc` passes on any
  resourceVersion change).
- Harness-bites: temporarily sorting the selector keys in
  `MergeNodeSelectorAndNodeAffinity` flipped both canaries to red while contract/parity stayed
  green; the fix was then reverted (`results/harness-bites-sortfix.txt`, production diff empty
  afterwards).

### L2 — envtest round-trip
`zz_verify_pr6200_envtest_test.go` (skipped unless `KUBEBUILDER_ASSETS` is set): installs the
shipped CRDs (`config/crd/bases` + `charts/fluid/fluid/crds`) into envtest (k8s 1.31.0), runs
real cycles against a direct client. Proves: exactly 1 worker Get per cycle against a real API
server; `status.cacheAffinity` survives CRD schema round-trip (both the pod-affinity term and
the nodeSelector label); an out-of-band `nodeSelector` change (`ssd`→`nvme`) is reflected on
the very next cycle with no stale value.

### PR's own suites
`go test ./pkg/ddc/cache/component/...` fully green on head. `pkg/ddc/cache/engine` shows 12
failing `UpdateOnUFSChange` specs that fail **identically on the merge-base** (md5 of the
failing-spec lists matches; they shell out to `/mount.sh` with pod fixtures — environmental,
unrelated to this PR). See `results/pr-suites-head.txt`.

## Proposed fixes (NOT applied to production here)
- **F1**: change `fixes #5879` to `refs #5879` (or keep the issue open with a follow-up for
  the cached-affinity design the issue actually requested).
- **F3** (pre-existing, optional here): sort nodeSelector keys before building `expressions`
  in `MergeNodeSelectorAndNodeAffinity` (see `results/harness-bites-sortfix.txt` for the exact
  patch shape used in the bites check); then invert the two canaries into contract tests.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cache-worker-affinity-single-read-r3-codex`
(fork remote `reviewfork` = https://github.com/cheyang/fluid.git), production code untouched.

1. Graft onto fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-worker-affinity-single-read-r3-codex
   git checkout <fixed-ref>
   git checkout FETCH_HEAD -- docs/verification/cache-worker-affinity-single-read-r3 \
     pkg/ddc/cache/engine/zz_verify_pr6200_contract_test.go \
     pkg/ddc/cache/engine/zz_verify_pr6200_parity_test.go \
     pkg/ddc/cache/engine/zz_verify_pr6200_canary_order_test.go \
     pkg/ddc/cache/engine/zz_verify_pr6200_envtest_test.go
   ```
   or simply run `bash docs/verification/cache-worker-affinity-single-read-r3/scripts/re-verify.sh`
   from a checkout of this branch (no args: it fetches the current PR head via `manifest.pr`).
2. Prereqs: Layer 1 = Go toolchain only. Layer 2 = envtest binaries (any
   `~/.local/share/kubebuilder-envtest/k8s/*` install; the command auto-detects the newest).
3. Polarity: contract tests (`SingleWorkerReadPerCycle`, `Parity...`, envtest round-trip)
   should be GREEN; the F3 canaries stay GREEN only while the instability exists and must be
   inverted once expressions are sorted (a flip = fixed).
4. Harness-bites: `zz_verify_pr6200_contract_test.go` is self-contained — copy it onto the
   merge-base and watch it go red (`workerGets == 2`).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cache-worker-affinity-single-read-r3-codex
(remote https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6200 confirmed the premise (worker
AdvancedStatefulSet read twice per status cycle on base) and one pre-existing defect
(affinity expression order instability → spurious status writes). Read
docs/verification/cache-worker-affinity-single-read-r3/README.md ("Continuing after the
fix"), graft the harness onto the current PR head or run scripts/re-verify.sh with no args,
re-run layers 1-2, honor the polarity table (F3 canaries flip when fixed), and report
observed-vs-expected per finding. Do not touch production code; do not push anywhere except
the reviewer's fork.
```
