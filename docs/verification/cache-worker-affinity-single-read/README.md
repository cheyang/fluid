# cache-worker-affinity-single-read — bug verification

Reproducible evidence for the review of [PR #6200](https://github.com/fluid-cloudnative/fluid/pull/6200)
(`perf(cache): derive worker node affinity during status construction to avoid duplicate read`,
refs #5879).

Layers run against the **code under review** (current head `0182b4f1`; previous reviewed head
`7b6392c2`; base merge-base `d8b37f28` for the premise check). Harness is **additive only** —
production code untouched (`git diff 0182b4f1 -- pkg/ docs/` shows only new test files + this
directory).

## Round 2026-10-12 — delta `7b6392c2..0182b4f1` (verified)

The author pushed one commit, `0182b4f1` "refactor(cache): delegate
`ConstructComponentStatus` to `ConstructComponentStatusAndAffinity`" — exactly the F1
de-duplication fix proposed in the prior round — and retargeted the PR body from
`fixes #5879` to `refs #5879` (resolving the prior F2 merge-metadata ask; no auto-close).

New claim **P5** (contract, unit layer) guards the delegation: `ConstructComponentStatus`
on **both** managers still performs **exactly one** workload `Get` (counting client),
returns a status **identical** to the combined method's status half, and still propagates
missing-workload errors.

- `re-verify.sh` against `0182b4f1`: **all 8 claims pass** (P1–P5, C1–C3),
  `results/reverify/`.
- Harness-bites for P5: mutating the ASTS delegate to call the combined method twice
  (simulated duplicate-read regression, in a throwaway worktree, reverted) makes P5 FAIL
  at the `gets == 1` assertion — the spec demonstrably bites.
- Full-suite attribution on `0182b4f1`: `go test ./pkg/ddc/cache/...` — component PASS;
  engine fails the **same 12 pre-existing specs** (11× `ufs_test.go`, 1× `sync_test.go`)
  with a byte-identical failing-spec set on base `d8b37f28`, previous head `7b6392c2`,
  and head `0182b4f1` — pre-existing, not PR-caused.
- Blast-radius of the delegation on non-worker callers (`status.go` master/client,
  `master.go`/`worker.go`/`client.go`): one extra `MergeNodeSelectorAndNodeAffinity`
  computation per call — deep-copying, non-mutating, nil-safe, no extra API reads.
  Log line changes from "start to ConstructComponentStatus" to
  "start to ConstructComponentStatusAndAffinity"; no test asserts the old string.


| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (component) | `AdvancedStatefulSetManager`/`DaemonSetManager` `ConstructComponentStatusAndAffinity`: independence, aliasing, merge parity | `go test -count=1 ./pkg/ddc/cache/component/ -run TestCacheComponent -ginkgo.focus 'Verification'` |
| 2. Integration (engine, in-process fake API server) | full `CacheEngine.CheckAndUpdateRuntimeStatus` cycle: worker Get count per cycle, `status.CacheAffinity` content, zero staleness, error propagation | `go test -count=1 ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus 'Verification'` |
| 3. Live | skipped for this review round (no cluster); the observable signal would be API-server audit-log GET counts for the worker StatefulSet per reconcile | — |

> Test polarity: **contract** tests throughout (assert intended behavior). No canaries:
> every assertion encodes the behavior we want (1 Get/cycle), and the P1 spec doubles as
> the premise reproduction when run on base (it goes red there with 2 Gets/cycle).

## Problem premise (P0)

Run against the **base** branch (`d8b37f28`) without the patch, because the question is
whether the problem exists today.

| | |
|---|---|
| Claimed symptom | #5879: "get node affinity for worker do not call kubeclient.GetStatefulSet in every status update cycle … `StatefulSetManager` `GetNodeAffinity` calls kubeclient.GetStatefulSet for every status update cycle … adds unnecessary API server load" |
| Linked issue | #5879, OPEN, created 2026-05-14, labeled `features`; still valid (no fix on base) |
| Reported component | `pkg/ddc/cache` worker status path (`StatefulSetManager.GetNodeAffinity`) |
| Patched component | `pkg/ddc/cache/component/*_manager.go`, `pkg/ddc/cache/engine/status.go` — same path |
| Component match | Yes |
| **Verdict** | **Confirmed** — on base, each `CheckAndUpdateRuntimeStatus` cycle performs **2** worker AdvancedStatefulSet Gets (`ConstructComponentStatus` + `GetNodeAffinity`); on head it performs **1** |
| Evidence | `results/base-engine-verify.txt`: `P1 … cycle 1: expected exactly 1 worker Get, got 2` (FAILED on base); `results/head-engine-verify.txt`: 5/5 PASS on head |

Note on scope: the issue title asks for *no* GetStatefulSet call per status cycle; the PR
reduces 2→1 (the remaining read is the one status construction itself requires). Affinity
content, staleness, and error semantics are all preserved (P2/P3/P4 pass on base **and**
head — no behavioral change beyond the read consolidation).

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | duplicate worker read exists on base (premise) | 2 | Confirmed | base P1 red with "got 2"; head green with 1 |
| P1 | exactly ONE worker ASTS Get per `CheckAndUpdateRuntimeStatus` cycle | 2 | Confirmed (fixed) | head-engine-verify.txt: counts 1 then 2 after two cycles |
| P2 | `status.CacheAffinity` == `MergeNodeSelectorAndNodeAffinity(nodeSelector, affinity)` of the worker pod template (parity with removed `GetNodeAffinity`) | 2 | Confirmed (unchanged) | passes on base and head |
| P3 | out-of-band worker nodeSelector change reflected on the very next cycle (zero staleness, no caching) | 2 | Confirmed (unchanged) | passes on base and head |
| P4 | missing worker workload → error (no silent nil affinity) | 2 | Confirmed (unchanged) | passes on base and head |
| C1 | successive `ConstructComponentStatusAndAffinity` calls return deep-independent affinity objects | 1 | Confirmed | head-component-verify.txt |
| C2 | returned affinity does not alias the stored workload | 1 | Confirmed | head-component-verify.txt |
| C3 | derived affinity equals `MergeNodeSelectorAndNodeAffinity` for ASTS **and** DaemonSet managers | 1 | Confirmed | head-component-verify.txt |
| P5 | (round 2026-10-12) `ConstructComponentStatus` delegation: exactly one workload Get, identical status content, error propagation — for both managers | 1 | Confirmed | results/reverify/unit.out |

Pre-existing failures: `go test ./pkg/ddc/cache/...` fails 12 specs
(11× `pkg/ddc/cache/engine/ufs_test.go`, 1× `pkg/ddc/cache/engine/sync_test.go`)
**identically on base `d8b37f28`, previous head `7b6392c2`, and head `0182b4f1`**
(failing-spec sets diff empty). They are not caused by this PR and are not counted
against it.

## Per-finding detail

- **P1 (the PR's core claim)**: `getCallCountingClient`-style wrapper
  (`verifyAffinityCountingClient`) counts `Get` calls whose target object is an
  `AdvancedStatefulSet` named `<runtime>-worker`. Observed on head: 1 after cycle 1,
  2 after cycle 2. Observed on base: 2 after cycle 1 — the contract test fails on base,
  which is both the premise reproduction and the harness-bites check (red pre-fix, green
  post-fix, without ever editing production code).
- **P2/P3/P4** pin the unchanged behaviors around the consolidated read so a future
  regression in the merge/staleness/error path flips them red.
- **C1/C2/C3** guard the manager-level contract: `MergeNodeSelectorAndNodeAffinity`
  deep-copies the workload's affinity, so per-call results are independent (no
  cross-cycle state), and mutating a returned affinity cannot corrupt the workload or
  later reads.

## Harness-bites check

Run layer 2 against base (`d8b37f28`): `P1` fails with `cycle 1: expected exactly 1
worker Get, got 2` — the test demonstrably exercises the changed path. Reverted (base
tree discarded); production diff on the harness branch is empty.

## Live run notes

Not run this round (no cluster). If wanted: deploy a CacheRuntime, watch the apiserver
audit log for `GET /apis/apps/appsplus.fluid.io/.../advancedstatefulsets/<runtime>-worker`
per reconcile tick of the cache runtime controller — expect one per tick after this PR,
two before.

## Proposed fixes (NOT applied to production here)

The review found no correctness defects. Prior non-blocking suggestions and their status
after round 2026-10-12:

- ~~`ConstructComponentStatus` (both managers) duplicates the Get + error-handling block
  of `ConstructComponentStatusAndAffinity`~~ — **applied by the author in `0182b4f1`**
  (exact 3-line delegate proposed); verified by P5.
- ~~`fixes #5879` auto-close concern~~ — **resolved**: PR body now says `refs #5879`,
  so merging no longer auto-closes the issue.
- `DaemonSetManager.ConstructComponentStatusAndAffinity` has no production caller (worker
  components always resolve to the AdvancedStatefulSet manager via `NewComponentHelper`);
  it exists for interface conformance only. Still open (nit).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cache-worker-affinity-single-read-claude` (production
code untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-worker-affinity-single-read-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cache-worker-affinity-single-read \
     pkg/ddc/cache/engine/status_affinity_verify_test.go \
     pkg/ddc/cache/component/status_affinity_verify_test.go
   ```
2. Prereqs: Go toolchain only (layers 1–2 use fake clients; no envtest, no cluster).
3. Re-run:
   ```bash
   go test -count=1 ./pkg/ddc/cache/component/ -run TestCacheComponent -ginkgo.focus 'Verification'
   go test -count=1 ./pkg/ddc/cache/engine/   -run TestCacheEngine   -ginkgo.focus 'Verification'
   ```
   Or `bash docs/verification/cache-worker-affinity-single-read/scripts/re-verify.sh`
   (no argument = fetches the current PR head from the manifest's `pr` URL).
4. Read results via the polarity table: all specs are **contract** — all should be GREEN.
5. Harness-bites: run layer 2 once against pre-fix code (merge-base `d8b37f28`) to confirm
   `P1` still goes red (`got 2`).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cache-worker-affinity-single-read-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6200 produced the harness claims
P1..P4, C1..C3 in docs/verification/cache-worker-affinity-single-read/README.md; the
premise P0 (duplicate worker read) was Confirmed on merge-base d8b37f28. The PR may have
been updated since. Read that README ("Continuing after the fix") and follow it: graft
the harness onto the current PR head, re-run both layers (all contract polarity —
everything should be green), run the harness-bites check against d8b37f28 (P1 must be
red with "got 2"), and report an observed-vs-expected table. Clean up scoped test
resources; do not run cluster-wide destructive actions.
```
