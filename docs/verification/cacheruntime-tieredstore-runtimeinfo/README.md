# cacheruntime-tieredstore-runtimeinfo — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6181
("fix(cache): build RuntimeInfo from the worker tiered store", fixes #6174).

Layers run against the **code under review** (PR head `68e98ac1137a45de2c65286dbcf0f9db8a819fb0`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `CacheEngine.getRuntimeInfo` -> `tieredstore.GetLevelStorageMap` (in-memory, fake client) | `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -count=1 -run TestCacheEngine -args -ginkgo.focus="V1 "` |
| 2. Integration | full chain CacheRuntime spec -> `getRuntimeInfo` -> `lifecycle.SyncScheduleInfoToCacheNodes` -> `labelCacheNode` -> node labels, against a fake client (real label-writing code, no envtest needed) | `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -count=1 -run TestCacheEngine -args -ginkgo.focus="V2 \|V3 \|V4 "` |
| 3. Live | skipped for this review (no KUBECONFIG layer) | see `verify-manifest.json` `liveNote` |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

The harness is a single additive test file,
`pkg/ddc/cache/engine/verify_pr6181_runtimeinfo_tieredstore_test.go` (specs V1-V4).
It compiles against both the merge-base and the PR head, so the same file serves as the
premise reproducer (on base) and the regression guard (on the fix).

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "a CacheRuntime that declares a real tiered store still advertises zero cache capacity on every node it lands on": `fluid.io/s-h-cache-t-default-mooncake-demo = 0B`, no `s-h-cache-m-`/`s-h-cache-d-` labels (issue #6174) |
| Linked issue | #6174, OPEN, created 2026-08-28, still valid — the base code at the merge-base matches the report line-for-line (`base.WithTieredStore(datav1alpha1.TieredStore{})` at `pkg/ddc/cache/engine/runtime.go:100`) |
| Reported component | `CacheEngine.getRuntimeInfo` (`pkg/ddc/cache/engine`) -> node capacity labels |
| Patched component | `pkg/ddc/cache/engine/runtime.go` + new `convertToLegacyTieredStore` in `pkg/ddc/cache/engine/transform_tiered_store.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | Harness grafted onto merge-base `f2785f84`: V1 fails (empty `GetLevelStorageMap`), V2 fails with node labels `{fluid.io/s-h-cache-t-default-mooncake-demo: "0B"}` and no disk label — the exact symptom quoted in the issue. `results/base-f2785f84-harness.txt` |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | CacheRuntime RuntimeInfo is built with an empty TieredStore, so capacity labels are never populated | 1+2 (on base) | **Confirmed** | base: V1/V2/V3 red with the issue's exact symptom; `results/base-f2785f84-harness.txt` |
| — (fix works) | With the PR, the worker tiered store reaches RuntimeInfo and the node labels | 1+2 (on head) | Verified | V1-V3 green on PR head; `results/prhead-68e98ac1-harness.txt` |
| F1 | Pre-existing CacheRuntimes keep stale `0B`/missing capacity labels after upgrade (labels are written once; `addScheduleInfoToNode` skips nodes already carrying the runtime label) | 2 | **Confirmed** (limitation disclosed by the author; canary V4 green on base AND head) | `results/prhead-68e98ac1-harness.txt` |
| F2 | The PR's added tests never exercise the call-site wiring (`runtime.go` `WithTieredStore(convertToLegacyTieredStore(...))`); reverting that one line would leave all PR-added tests green | 1+2 | Confirmed by construction — V1/V2/V3 fail when the harness runs on base, which differs from head only in that wiring | `results/base-f2785f84-harness.txt` vs `results/prhead-68e98ac1-harness.txt` |

No code defects were found in the conversion itself; see the findings document for details.

## Per-finding detail

### P0 — premise (runs against base)

- V1 builds a `CacheEngine` on a fake client holding a `CacheRuntime` with
  `spec.worker.tieredStore.levels[0] = {emptyDir: {quota: 1Gi}}` (the repro YAML from the
  issue), calls `getRuntimeInfo()`, and reads `tieredstore.GetLevelStorageMap`.
  - Base: map is empty `{}` -> FAIL. Head: `{Disk: 1Gi}` -> PASS.
- V2 adds the worker `AdvancedStatefulSet` + a scheduled worker pod + the node, then runs the
  real `lifecycle.SyncScheduleInfoToCacheNodes`.
  - Base: node labels are `fluid.io/s-h-cache-t-default-mooncake-demo: "0B"`, no
    `s-h-cache-d-`/`s-h-cache-m-` labels — byte-for-byte the symptom in issue #6174. FAIL.
  - Head: `s-h-cache-d-...: "1GiB"`, `s-h-cache-t-...: "1GiB"`. PASS.

### F1 — stale labels on pre-existing runtimes (canary)

- V4 seeds a node that already carries the runtime label and the stale
  `s-h-cache-t-...: "0B"` label, with a worker pod on it, then runs
  `SyncScheduleInfoToCacheNodes` on the PR head. The sync succeeds but the labels are
  untouched (`addScheduleInfoToNode` -> `hasRuntimeLabel` -> skip). PASS (asserts current
  behavior). If a follow-up implements label healing on upgrade, this test flips to red and
  must be inverted into a contract test.
- This is the limitation the PR body itself discloses ("A CacheRuntime that existed before
  this change therefore keeps its 0B label until it is recreated"); the canary proves the
  mechanism and gives the follow-up a tripwire.

### F2 — missing test for the wiring line

- The bug being fixed lived at the call site (`WithTieredStore(datav1alpha1.TieredStore{})`),
  not in conversion logic. The PR's added tests exercise `convertToLegacyTieredStore` in
  isolation and feed its output to `base.BuildRuntimeInfo`, so a revert of the one-line
  wiring in `getRuntimeInfo` would keep every PR-added test green. V1/V2/V3 demonstrate the
  missing coverage: they fail on base (where only the wiring differs) and pass on head. They
  can be uplifted as-is if the maintainers want that guard.

## Harness-bites check

The same unmodified test file was run on the merge-base (`f2785f84`, via a scratch worktree,
nothing committed there): V1/V2/V3 go red with the issue's symptom, V4 stays green. On the PR
head all four go green. So the contract tests fail for the right reason (the missing tiered
store), pass for the right reason (the fix), and the canary genuinely exercises the skip path
(it asserts a state only reachable through `addScheduleInfoToNode`'s early return).

Production code is untouched on this branch (`git diff origin/pr/6181 -- ':!docs/verification' ':!*_test.go'` is empty except for the harness test file).

## Proposed fixes (NOT applied to production here)

- **F1**: heal labels on upgrade, e.g. re-run `labelNodeWithCapacityInfo` when the capacity
  labels are absent/stale even if the runtime label is present, or bump the labels when the
  runtime's generation/spec changes. Out of scope for this PR; the author flagged it for a
  separate decision.
- **F2**: add a `getRuntimeInfo`-level test (V1/V2/V3 in the harness are drop-in candidates)
  so the call-site wiring cannot silently regress.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cacheruntime-tieredstore-runtimeinfo-codex`
(fork `https://github.com/cheyang/fluid.git`), production code untouched.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cacheruntime-tieredstore-runtimeinfo-codex
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cacheruntime-tieredstore-runtimeinfo \
       pkg/ddc/cache/engine/verify_pr6181_runtimeinfo_tieredstore_test.go
   ```
2. Prereqs: Go toolchain only (module cache). No envtest, no cluster. Set `FLUID_UNIT_TEST=true`.
3. Re-run the two layer commands above, or simply:
   ```bash
   bash docs/verification/cacheruntime-tieredstore-runtimeinfo/scripts/re-verify.sh
   ```
   (no ref needed — the manifest's `pr` URL auto-discovers the current PR head).
4. Polarity: V1/V2/V3 are contract tests (should be GREEN on the fix). V4 is a canary: GREEN
   means the stale-label limitation still stands; if it flips RED the limitation was fixed —
   invert V4 into a contract test.
5. Harness-bites: run the same commands against the merge-base (`f2785f84`) to confirm
   V1/V2/V3 still go red there.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cacheruntime-tieredstore-runtimeinfo-codex
(fork https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6181 confirmed premise P0 (CacheRuntime
RuntimeInfo built with empty TieredStore -> 0B capacity labels) and produced findings F1
(stale labels not healed on upgrade, canary) and F2 (call-site wiring untested). The harness
(4 ginkgo specs in pkg/ddc/cache/engine/verify_pr6181_runtimeinfo_tieredstore_test.go)
reproduced P0 on the merge-base and passes on the PR head. Read
docs/verification/cacheruntime-tieredstore-runtimeinfo/README.md ("Continuing after the
fix"), then run scripts/re-verify.sh from a checkout of the verify branch, mind the polarity
table (V4 is a canary — invert it if it flips), and report an observed-vs-expected table.
```
