# cache-affinity-claude — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6200
("perf(cache): derive worker node affinity during status construction to avoid duplicate read").

Layers run against the **code under review**:

- HEAD = PR head `1d66cffe96cb327274f029d922e338af0b7e6f13`
- BASE = merge-base `d8b37f28d645ae5cf3f0d853a79fc72ad222f968` (origin/master)

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (ginkgo, fake client) | worker read count per status cycle, affinity derivation, error paths | `go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus='(Premise verification\|Worker node affinity derivation)' -v` |
| 2. Integration-ish (ginkgo, fake client at component level) | `ConstructComponentStatusAndAffinity` on both managers | `go test ./pkg/ddc/cache/component/` |
| 3. Live | **skipped this round** — per the debate-pipeline scope (unit + integration only); a live cluster was reachable but out of scope. Optional L3 signal documented in the manifest `liveNote`. | — |

> Test polarity: the premise canary `premise_worker_get_count_test.go` is a **contract**
> test — it FAILS on BASE (observed 2 worker Gets per cycle = the reproduction) and PASSES
> on HEAD (1 Get per cycle). All other tests referenced are the PR's own new specs,
> re-run here as independent confirmation.

## Problem premise (P0)

Answered before the findings, and run against the **base** branch without the patch, because
the question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom | "each `CacheRuntime` status update cycle read the worker `AdvancedStatefulSet` twice: 1. in `manager.ConstructComponentStatus(...)` … 2. immediately after in `manager.GetNodeAffinity(...)`" (PR body); issue #5879: "get node affinity for worker do not call kubeclient.GetStatefulSet in every status update cycle" |
| Linked issue | #5879, **open**, feature request, created 2026-05-14 — still valid |
| Reported component | cache-runtime status update cycle (`StatefulSetManager.GetNodeAffinity`) |
| Patched component | `pkg/ddc/cache/component/*` + `pkg/ddc/cache/engine/status.go` — exactly the reported path |
| Component match | Yes |
| **Verdict** | **Confirmed** (real problem) |
| Evidence | `results/base-canary.txt`: contract canary on BASE fails with observed `<int>: 2` worker Gets per status cycle (expected 1). `results/head-canary.txt`: same test on HEAD passes with exactly 1 Get per cycle. |

> Note: the PR *halves* the per-cycle reads (2 → 1) rather than eliminating the Get
> entirely (issue #5879 suggested caching or reading from the runtime value). Since
> `ConstructComponentStatus` needs the workload read anyway to compute replica status,
> 1 read/cycle is the natural floor without introducing cache staleness. See finding Q1
> in the debate document.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | BASE reads the worker workload 2× per status cycle | 1 | **Confirmed** | `results/base-canary.txt` (fails: got 2) vs `results/head-canary.txt` (passes: 1) |
| H1 | HEAD reads it exactly 1× per cycle, affinity derived from the same read, out-of-band nodeSelector changes reflected next cycle, error propagates when worker missing | 1 | **Confirmed** | PR's 3 new specs re-run green: `results/head-pr-new-specs.txt` (3/3 passed) |
| H2 | Both managers return status+affinity from one read | 2 | **Confirmed** | `results/head-component-pkg.txt` (`ok … pkg/ddc/cache/component`) |
| F1 | `ComponentManager.GetNodeAffinity` is dead code after this PR (no production callers) | grep | **Confirmed** | `results/getnodeaffinity-callers.txt`: HEAD has only declarations; BASE had the caller `engine/status.go:75` |

Pre-existing failures, **not** introduced by the PR: `pkg/ddc/cache/engine` has 12 failing
specs in `ufs_test.go` / `sync_test.go` on both BASE and HEAD (identical spec sets);
`results/full-pkg-base-vs-head.txt` (BASE 262P/13F = 12 pre-existing + premise canary;
HEAD 266P/12F = same 12 pre-existing, canary + 3 PR specs green).

## Per-finding detail

- **P0 (premise)** — `pkg/ddc/cache/engine/premise_worker_get_count_test.go` wraps the fake
  client in `premiseGetCountingClient`, counts `Get` calls keyed on the worker workload name,
  and runs `CheckAndUpdateRuntimeStatus` twice. On BASE the first cycle already observes 2
  (ConstructComponentStatus + GetNodeAffinity). On HEAD it observes 1. The file only uses
  helpers that exist on both refs, so the same source grafts onto either.
- **H1** — re-ran the PR's own three new specs (`-ginkgo.focus='Worker node affinity
  derivation'`): single-Get-per-cycle, zero-staleness on out-of-band nodeSelector update
  (ssd → nvme picked up on the next cycle), and error propagation when the worker component
  is absent. 3/3 green.
- **H2** — component-level specs for `ConstructComponentStatusAndAffinity` on
  `AdvancedStatefulSetManager` and `DaemonSetManager`: package `ok`.
- **F1** — repo-wide grep: after the PR, `GetNodeAffinity` has zero production callers
  (only the interface declaration and the two implementations). Suggested cleanup: remove
  it from the `ComponentManager` interface (this is exactly the method issue #5879
  complained about; keeping it as seemingly-live API invites reintroducing the duplicate
  read). Reviewer did not modify production code.
- Harness-bites check: the canary is red on BASE and green on HEAD (the PR *is* the fix),
  and the production diff on this branch is empty apart from the added test file
  (`git diff 1d66cffe --stat` shows only `premise_worker_get_count_test.go` + this docs dir).

## Live run notes

Skipped this round: the debate pipeline scoped stage ② to unit + integration layers. A live
cluster was reachable from the review machine, but running against it was out of scope for
this phase. If an L3 run is wanted later: deploy a CacheRuntime, watch apiserver request
metrics/audit for `GET /apis/apps.../statefulsets/.../<runtime>-worker` across two status
cycles, expect exactly 1 per cycle after the patch (2 before).

## Proposed fixes (NOT applied to production here)

- **F1 (minor)**: drop `GetNodeAffinity` from the `ComponentManager` interface and the two
  managers, or mark it deprecated — its only caller was removed by this PR.
- **F2 (nit)**: `ConstructComponentStatus` (master/client paths) now pays a discarded
  `MergeNodeSelectorAndNodeAffinity` per call via the wrapper; harmless, only worth a
  comment or splitting the wrapper if it ever shows up in profiles.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cache-affinity-claude` (production code untouched), so it
grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-affinity-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cache-affinity-claude pkg/ddc/cache/engine/premise_worker_get_count_test.go
   ```
   Or just: `bash docs/verification/cache-affinity-claude/scripts/re-verify.sh` from a
   checkout of the branch — it auto-resolves the current PR head from the manifest's `pr`
   URL, grafts, runs unit + integration layers, and prints per-finding verdicts.
2. Prereqs: Go toolchain only (no envtest assets needed — both layers use the fake client).
3. Re-run the layer commands above.
4. Read results via the polarity table: all referenced tests are **contract** tests — they
   should be GREEN on fixed code. (The canary's red-on-BASE run is the reproduction record,
   not a state you should see post-fix.)
5. Harness-bites: run Layer 1 once against the BASE commit to confirm it goes red (2 Gets).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cache-affinity-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6200 verified its premise
(duplicate worker-workload read per status cycle) with a contract canary.
Read docs/verification/cache-affinity-claude/README.md ("Continuing after the
fix") and follow it: graft the harness onto the current PR head, re-run the
unit + integration layers with scripts/re-verify.sh, mind the polarity table
(all contract), run the harness-bites check against base d8b37f28, and report
an observed-vs-expected table. Do not modify production code; push only to
verify/* branches on the reviewer fork.
```
