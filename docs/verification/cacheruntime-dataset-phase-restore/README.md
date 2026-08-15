# cacheruntime-dataset-phase-restore — bug verification

Reproducible evidence for the findings raised while reviewing
[PR #6162](https://github.com/fluid-cloudnative/fluid/pull/6162) —
*"fix(cache): restore Dataset to Bound after CacheRuntime recovers from an outage"*.

All layers run against the **code under review**: PR head `49b2a779` (base `a38da8a4`).
**Production code on this branch is untouched** — the only additions are this directory and
one test file, so the diff against the PR head is pure harness.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `CacheEngine.Sync` phase-restore branch + the `permitSync()` rate limiter, with the pod-exec seam counted | `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -gcflags='all=-N -l' -run TestCacheEngine -count=1 -args -ginkgo.label-filter='verify.pr6162'` |
| 2. Integration | **deliberately skipped** — see [Why no envtest layer](#why-no-envtest-layer) | — |
| 3. Live | real manager binary vs. a real cluster, real worker-pod outage, exact exec count | `scripts/live/live-setup.sh` → `scripts/live/live-scenario.sh <binary> <label>` → `scripts/live/live-teardown.sh` |

> **Test polarity:** every claim here is encoded as a **contract test** (asserts intended
> behavior): RED on buggy code, GREEN once fixed. There are **no bug-canaries**, so for this
> harness "all green" really does mean "all findings addressed" — nothing needs inverting.

`-gcflags='all=-N -l'` is not optional: the harness patches `NewCacheFileUtil` with gomonkey,
which needs inlining disabled. It matches the repo's own `LOCAL_FLAGS` in the `Makefile`.

## What the PR gets right (verified, not assumed)

Before the findings, the parts that hold up — each independently reproduced:

- **The root-cause analysis in the PR body is correct.** `utils.IsSetupDone` (`pkg/utils/dataset.go:82-90`)
  returns true whenever a `DatasetReady` condition merely *exists*, ignoring its `Status`. The
  Failed path (`dataset.go:91-94`) writes `DatasetReady/False`, so `IsSetupDone` stays true,
  so `runtime_controller.go:271` never re-enters `Setup` → `BindToDataset` (`setup.go:107`) is
  genuinely the only `Bound` writer on that path and genuinely never runs again.
- **The cache engine really has no other recovery path.** Unlike every other engine, there is
  no `health_check.go` under `pkg/ddc/cache/**` and no `CheckRuntimeHealthy` — `CacheEngine`
  satisfies `base.Engine` directly (`engine.go:40`), so `base/syncs.go:64` is never on this
  code path. `sync.go:89` is the only `Failed` writer and, pre-PR, nothing wrote `Bound` back.
- **The author's own regression test is genuine.** Reverting only `sync.go` and keeping their
  test fails exactly as they described: `Failed` vs `Bound`. See `results/L1-author-test-reverted.txt`.

## Summary of results

| ID | Claim (all contract polarity) | Layer | PR head `49b2a779` | Fix reverted | Verdict |
|----|-------------------------------|-------|--------------------|--------------|---------|
| F1-a | No cache-state pod-exec RPC while the `permitSync()` limiter is closed | 1 | **RED** — 1 RPC | GREEN — 0 | **Confirmed** — regression introduced by this PR |
| F1-c | ≤1 cache-state RPC per 5s limiter window across a not-ready→ready flap | 1 | **RED** — **3 RPCs / 3 flaps** | GREEN — 0 | **Confirmed** (deterministic) |
| F1-live | Does that amplification appear on a real cluster? | 3 | **6 execs / ~10 windows** | 5 execs | **Not reproduced — impact is ≈ +1 exec** |
| F1-b | Phase still restored to `Bound` while the limiter is closed | 1 | GREEN | RED — stays `Failed` | PR's win, at a boundary its own test misses |
| F2 | `DatasetReady` condition restored to `True`, not just the phase | 1 | GREEN | RED | PR correct — credit |
| — | Dataset recovers after a real outage (the PR's actual claim) | 3 | **`Bound` 5/5** | **`Failed` 0/5** | **PR fixes a real bug, proven live** |

**Net severity: minor.** F1 is a genuine violation of a documented invariant and worth fixing,
but the live layer bounds its cost at roughly one extra pod exec per recovery — not the storm
L1 shows the code *permits*. Recommended verdict: **comment, not a blocker**.

The **clean inversion across both columns** is what makes this evidence rather than assertion:
the F1 tests go green when the PR's change is removed, which proves they exercise the new
branch and that F1 is *introduced by this PR*; F1-b and F2 go red when it is removed, which
proves they are real regression tests for the original bug rather than vacuous.

Raw output: `results/L1-pr-head.txt`, `results/L1-fix-reverted.txt`.

## Per-finding detail

### F1 — the restore path bypasses the `permitSync()` rate limiter and issues an unthrottled pod exec

**Mechanism.** The new branch calls `e.UpdateDatasetStatus(BoundDatasetPhase, ...)`. For
`BoundDatasetPhase` that helper is *not* a cheap phase write — `dataset.go:49-55` calls
`e.GetCacheStates(...)`, which resolves to `CacheFileUtil.Execute` →
`kubeclient.ExecCommandInContainerWithTimeout` (`fileutils.go:57`): a synchronous exec into
the master pod whose timeout floor is `common.MinExecutionTimeoutSeconds = 20` seconds.

The PR places that call **outside** the `permitSyncEngineStatus` guard. Only the `else if`
retains it:

```go
} else {                                    // runtime is ready
    dataset, getErr := utils.GetDataset(...)
    if dataset.Status.Phase == FailedDatasetPhase {
        err = e.UpdateDatasetStatus(BoundDatasetPhase, ...)   // <-- unguarded; execs into the pod
    } else if permitSyncEngineStatus {
        err = e.syncDatasetCacheStates(...)                   // <-- guarded, as before
    }
}
```

That guard exists for exactly this reason, per the comment the PR leaves in place at
`sync.go:38`: *"permitSyncEngineStatus avoids frequent rpcs with engines with rate limited
retries"*, with `defaultSyncRetryDuration = 5 * time.Second` (`engine.go:36`).

**Why it repeats rather than firing once.** Three amplifiers compound:
1. `e.Client` is the informer-backed cached client — `cmd/cache/app/cache.go:162` passes
   `mgr.GetClient()`, and `NewFluidControllerClient` (`pkg/controllers/manager.go:35-49`)
   bypasses the cache only for `corev1.Secret`. So after the write lands, subsequent
   reconciles can still *read* `Failed` and restore again.
2. Reconciles fire on **every** AdvancedStatefulSet / DaemonSet status change — the update
   predicates (`pkg/ctrl/watch/advancestatefulset.go:68-74`, `daemonset.go:64-70`) end in
   `return true` and never distinguish spec from status. A recovering pod produces a burst.
3. A genuinely flapping runtime re-enters `Failed` on each not-ready reconcile, so each
   ready-reconcile pays another exec — measured at **3 execs across 3 flaps** in one 5s window.

**The comparison that settles intent.** Alluxio implements this same recovery and is *also*
called outside the limiter (`base/syncs.go:63` → `alluxio/health_check.go:81` →
`UpdateDatasetStatus(Bound)`) — and that is fine there, because Alluxio's
`UpdateDatasetStatus` (`alluxio/dataset.go:76+`) is a pure phase/condition/mounts write with
**no cache-state RPC**, and it wraps the whole transition in `if phase != dataset.Status.Phase`
so it is idempotent by construction. Cache-state collection lives in a *separate*
`UpdateCacheOfDataset()` step. The cache engine's helper has neither property.

So the finding is not "you must rate-limit the recovery" — it is "the cache engine's
`UpdateDatasetStatus(Bound)` is not the cheap helper Alluxio's is, and this call site assumes
it is."

**Observed vs expected.**

| | Expected | Observed on PR head |
|---|---|---|
| execs while limiter closed (F1-a) | 0 | 1 |
| execs across 3 flaps in one 5s window (F1-c) | ≤1 | 3 |

### F1-b / F2 — what a fix must not break

`F1-b` and `F2` are green on the PR and exist to pin the *shape* of an acceptable fix:

- **F1-b** fails if F1 is "fixed" by simply moving the restore inside
  `permitSyncEngineStatus` — that would make recovery wait on the limiter instead of being
  prompt, trading one defect for a milder one.
- **F2** fails if the restore is narrowed to a bare `Status.Phase` write that forgets the
  `DatasetReady` condition — which matters because `IsSetupDone` and every other consumer read
  the condition, not the phase.

Together they say: restore the phase **and** the condition, promptly, **without** the RPC.

## Live run notes

Environment: ACK cluster, `cn-hongkong`, Kubernetes `v1.36.1`, 3 nodes.

The live layer runs the real manager binary out-of-cluster (`--development=true
--enable-leader-election=false`), which honors `KUBECONFIG`, so no image build or in-cluster
deploy is needed. `live-setup.sh` scales `fluid-system/cacheruntime-controller` to 0 first —
otherwise two managers reconcile the same object and both issue execs — after a **pre-flight
check that refuses to run if any CacheRuntime/Dataset not owned by this harness exists**.
`live-teardown.sh` restores the recorded replica count. `live-scenario.sh` takes an
exclusive `flock` so two runs cannot corrupt the count.

**The exec counter is exact, not log-scraped.** The synthetic `CacheRuntimeClass`'s
`reportSummary` command appends one timestamped line per invocation to `/tmp/exec-count.log`
inside the master container, so counting lines counts kubelet execs, and the timestamps give
inter-arrival gaps directly.

Scenario: bring the Dataset to `Bound` → zero the counter → `kubectl delete pod
verify6162-worker-0` (worker `replicas: 1`, so `ReadyReplicas` drops to 0 and
`CheckAndUpdateRuntimeStatus` reports not-ready) → observe `Failed` → let the
AdvancedStatefulSet recreate the pod → observe recovery → settle 60s → read the counter.

Two live runs were done, in both builds (A/B), plus a single-outage warm-up:

**Run 1 — single outage** (`live-scenario.sh`, PR build): recovered `Failed` → `Bound` **33s**
after the outage began. The restore branch fired **once**, and its exec landed **16s** after
the previous cache-state exec — i.e. the 5s limiter window was **already open**, so here the
bypass cost **nothing**. Recorded as a negative result rather than quietly dropped.

**Run 2 — 5 aggressive flaps, A/B** (`live-flap.sh <binary> <label> 5 3`). The outage switch
is the worker's readiness probe, so each outage begins and ends on command. All execs
completed for real (8 starts / 8 finished; cache states landed on the Dataset), so these are
genuine kubelet round-trips, not attempts that failed fast.

| | PR build `49b2a779` | master (fix reverted) |
|---|---|---|
| Dataset phase after each flap | **`Bound` — 5 / 5** | **`Failed` — 0 / 5** |
| final phase | `Bound` | **`Failed`** |
| restore-branch log hits | 5 | 0 |
| exec attempts over 47s (≈10 limiter windows) | **6** | **5** |

Artifacts: `results/L3-flap-pr.txt`, `results/L3-flap-master.txt`,
`results/L3-scenario-pr.txt`.

**What this establishes, and what it does not.**

*Establishes the PR's value.* On master the Dataset is stuck `Failed` after every single
recovery, five for five, permanently — the reported bug reproduced end-to-end on a real
cluster. On the PR build it returns to `Bound` every time. This is the strongest evidence in
the whole harness and it is *for* the PR.

*Bounds F1's severity — downward.* The PR added **one** exec attempt (6 vs 5) across five
flaps in 47 seconds, against a throttled-build ceiling of roughly ten. The
three-execs-per-window amplification that L1 proves deterministically **did not materialize**
live, and the reason is structural: a real not-ready→ready cycle cannot complete faster than
the readiness-probe cadence (~10s here), which is longer than the 5s
`defaultSyncRetryDuration`. Each recovery therefore tends to get its own limiter window, so
the bypass usually costs nothing at all. Inter-arrival gaps confirm it — of seven gaps, only
one same-second pair is attributable to the new branch (`results/L3-flap-pr.txt`).

So F1 is a **real invariant violation with small measured impact**, not a production hazard.
L1 shows what the code permits; L3 shows what a cluster actually does. Both belong in the
report, and the second is why this is a comment rather than a blocker.

*Residual tail risk, not measured.* `GetCacheStates` has a 20s timeout floor
(`MinExecutionTimeoutSeconds`) and runs synchronously on a reconcile worker
(`--runtime-workers` defaults to 3). A master pod that accepts connections but cannot answer
`reportSummary` — plausible immediately after a recovery — would stall a worker for up to 20s
per unthrottled call. This harness did not reproduce that (the synthetic `reportSummary`
returns instantly), so it is flagged as a reason to fix F1, not as a measured defect.


### Fixture notes (two incidental pre-existing bugs found while building it)

Neither is caused by PR #6162; both are recorded because they cost time and are worth
separate issues:

1. **A `CacheRuntimeClass` without a `client` topology panics the controller.**
   `cm.go:172-177` gates on `!runtime.Spec.Client.Disabled` but then dereferences
   `runtimeClass.Topology.Client.Options` with no nil check → `SIGSEGV` in
   `generateRuntimeConfigData`, crashing the reconcile worker. Captured in
   `results/L3-incidental-nil-client-topology.txt`. (Adjacent to PR #6157's configmap
   nil-pointer work.)
2. **The client container must be privileged.** The engine injects a volumeMount with
   `mountPropagation: Bidirectional`, which the API server rejects on a non-privileged
   container — so a minimal runtime class fails `Setup` with a non-obvious DaemonSet
   validation error rather than anything mentioning privileges.

### Why no envtest layer

An integration layer would have added exactly one thing over L1: the *real* informer-cache
lag. L3 measures that directly and more honestly — the count of `"runtime is ready again,
restoring dataset phase from Failed to Bound"` lines in `manager.log` for a **single** outage
*is* the stale-read re-entry count. Building envtest for it would have meant puppeteering
`AdvancedStatefulSet` status inside a controller-less API server: a flaky duplicate of
evidence L1 already produces deterministically. Skipped on purpose, not overlooked.

## Proposed fixes (NOT applied to production here)

**Preferred — make the restore phase-only, and make the helper idempotent.** Both in
`pkg/ddc/cache/engine/dataset.go`, which fixes F1 at the source and lets every caller benefit:

```go
func (e *CacheEngine) UpdateDatasetStatus(phase datav1alpha1.DatasetPhase, ...) (err error) {
	var cacheStates common.CacheStateList

	// Adopt Alluxio's guard: skip the whole transition when the phase already matches, so a
	// stale cached read cannot re-issue the exec below.
	current, err := utils.GetDataset(e.Client, e.name, e.namespace)
	if err != nil {
		return err
	}
	if current.Status.Phase == phase {
		return nil
	}

	// only collect cache states when the limiter permits: this is a pod exec with a 20s
	// timeout floor, and sync.go's permitSyncEngineStatus exists to bound exactly that.
	if phase == datav1alpha1.BoundDatasetPhase && e.permitSync() {
		cacheStates, err = e.GetCacheStates(runtime, runtimeClass)
		...
	}
	...
}
```

With that, `sync.go` needs no extra `GetDataset` at all and the new branch collapses to:

```go
} else {
    // Restore a Dataset left Failed by a previous outage; cheap and idempotent, so it does
    // not need the permitSyncEngineStatus guard.
    if err = e.UpdateDatasetStatus(datav1alpha1.BoundDatasetPhase, runtime, runtimeClass); err != nil {
        return err
    }
    if permitSyncEngineStatus {
        if err = e.syncDatasetCacheStates(ctx, runtime, runtimeClass); err != nil {
            return err
        }
    }
}
```

This also removes a subtler wart in the current diff: on the reconcile that restores the
phase, `syncDatasetCacheStates` is **skipped entirely** because the restore takes the `if`
and the sync is in the `else if`.

**Minimal alternative** if reworking the helper is out of scope: keep the PR's structure but
add a `skipCacheStates bool` (or a phase-only `restoreDatasetPhase` helper) so the restore
path does not exec. Do **not** simply wrap the restore in `permitSyncEngineStatus` — F1-b
exists to catch that.

**Test suggestions for the PR itself** (independent of the fix):
- Assert the `DatasetReady` condition, not only `Status.Phase` (what F2 does).
- Cover the `permitSync() == false` branch. The shared `sync_test.go` fixture leaves
  `syncRetryDuration` at `0`, which makes `permitSync()` always true and hides this entire
  class of behavior; the harness sets it to `defaultSyncRetryDuration` explicitly.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/cacheruntime-dataset-phase-restore` in the reviewer's fork
(`origin` = `github.com/cheyang/fluid`) with production code untouched, so it grafts onto
whatever the fixed code turns out to be.

One-liner — no sha needed, it resolves the current PR head from `verify-manifest.json`'s `pr`
field and the delta start from `.last-reviewed`:

```bash
bash docs/verification/cacheruntime-dataset-phase-restore/scripts/re-verify.sh
```

Manual equivalent:

```bash
git fetch origin verify/cacheruntime-dataset-phase-restore
git fetch https://github.com/fluid-cloudnative/fluid.git pull/6162/head && git checkout FETCH_HEAD
git checkout origin/verify/cacheruntime-dataset-phase-restore -- \
  docs/verification/cacheruntime-dataset-phase-restore \
  pkg/ddc/cache/engine/sync_phase_restore_verify_test.go
FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -gcflags='all=-N -l' \
  -run TestCacheEngine -count=1 -args -ginkgo.label-filter='verify.pr6162'
```

Prerequisites: **L1** needs only the Go toolchain. **L3** needs a real cluster with the Fluid
CRDs installed plus `KUBECONFIG`, and permission to scale `fluid-system/cacheruntime-controller`.

Reading the results: all four tests are contract polarity, so **all green = fixed**, nothing
to invert. If F1-a/F1-c are green but F1-b went red, the fix gated the restore behind the
limiter — push back. Finish with a harness-bites check: re-run L1 against the pre-fix code
(`git stash` the production fix, or check out `49b2a779`) and confirm F1-a/F1-c go red again.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/cacheruntime-dataset-phase-restore (remote
origin = github.com/cheyang/fluid). Background: reviewing
https://github.com/fluid-cloudnative/fluid/pull/6162 produced finding F1 — the new
Failed->Bound restore in pkg/ddc/cache/engine/sync.go calls UpdateDatasetStatus(Bound),
which pod-execs via GetCacheStates, outside the permitSyncEngineStatus rate limiter. A
harness reproduced it (3 execs per 3 flaps in one 5s window vs <=1 expected). The PR may
now be updated.

Read docs/verification/cacheruntime-dataset-phase-restore/README.md, section "Continuing
after the fix", and follow it: run scripts/re-verify.sh (it resolves the current PR head
itself — do not ask for a sha), report per finding Fixed / Still-broken / Partial /
Harness-update, then incrementally review the .last-reviewed..head delta for regressions and
add any new findings to the harness. All four tests are contract polarity: all green = fixed,
nothing to invert. Watch specifically for a fix that gates the restore behind
permitSyncEngineStatus — F1-b must stay green. Run the harness-bites check before trusting a
green result. Advance .last-reviewed, commit, push to origin. Do not publish anything to
GitHub without explicit confirmation. If you run the live layer, use its setup/teardown
scripts so fluid-system/cacheruntime-controller is restored.
```
