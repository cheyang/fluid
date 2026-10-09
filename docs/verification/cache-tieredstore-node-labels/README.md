# cache-tieredstore-node-labels — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6181
(Reviewer B / codex). Production code is untouched; the harness is one additive test file,
`pkg/ddc/cache/engine/verify_pr6181_codex_test.go`.

Layers run against the **code under review** (PR head `6ea38789`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `CacheEngine.getRuntimeInfo` → `base.BuildRuntimeInfo` → `tieredstore.GetLevelStorageMap` (pure conversion chain) | `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -run 'TestVerifyPR6181RuntimeInfoStorageMap' -count=1 -gcflags=all=-l` |
| 2. Integration (fake client) | the real label-writing path: `getRuntimeInfo` → `lifecycle.SyncScheduleInfoToCacheNodes` → `labelCacheNode` → `labelNodeWithCapacityInfo` against a fake API server holding the CacheRuntime, worker AdvancedStatefulSet, a scheduled worker pod and the node | `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -run 'TestVerifyPR6181NodeCapacityLabels' -count=1 -gcflags=all=-l` |
| 3. Live | skipped per debate setup (see `liveNote` in verify-manifest.json for the signal to check) | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. The bug-canary (F1) asserts current behavior and must be INVERTED when fixed.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "a CacheRuntime that declares a real tiered store still advertises zero cache capacity on every node it lands on" — `fluid.io/s-h-cache-t-default-mooncake-demo = 0B`, no `-m-`/`-d-` labels (issue #6174) |
| Linked issue | #6174, OPEN, filed 2026-08-28, still valid: base branch `runtime.go` still passed `datav1alpha1.TieredStore{}` |
| Reported component | `pkg/ddc/cache/engine/runtime.go` (`CacheEngine.getRuntimeInfo`) |
| Patched component | same file + new `convertToLegacyTieredStore` in `pkg/ddc/cache/engine/transform_tiered_store.go` |
| Component match | Yes — the patch wires the worker tiered store into exactly the RuntimeInfo the label path consumes (`sync.go` → `SyncScheduleInfoToCacheNodes`) |
| **Verdict** | **Confirmed** — reproduced on the merge-base `f2785f84` |
| Evidence | `results/base-run.txt`: storage map `map[]`; node labels `fluid.io/s-h-cache-t-default-demo="0B"`, `-m-`/`-d-` absent. `results/head-run.txt`: same harness green, `1GiB` total + disk label present |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Empty TieredStore in RuntimeInfo → 0B/missing capacity labels | 1+2 | **Confirmed** (base), fix verified (head) | base-run.txt vs head-run.txt |
| — | Fix correctness: issue spec (1Gi emptyDir) → total=1GiB, disk=1GiB, no mem label | 2 | Confirmed on PR head | head-run.txt, `TestVerifyPR6181NodeCapacityLabels/issue_repro` |
| — | Fix correctness: processMemory 4Gi + hostPath 1Gi/3Gi → mem=4GiB, disk=4GiB (per-path quotas summed, not averaged), total=8GiB | 1+2 | Confirmed on PR head | head-run.txt, `mixed_processMemory_+_hostPath` cases |
| F1 | Stale labels: editing `spec.worker.tieredStore` (1Gi→2Gi) after a node is labelled never updates the node labels; pre-existing CacheRuntimes keep `0B` after upgrade | 2 | **Confirmed** (canary; disclosed by the author in the PR's "Special notes") | head-run.txt, `TestVerifyPR6181StaleLabelsAfterSpecChange` logs `="1GiB"` after the 2Gi edit |

## Per-finding detail

### P0 — premise (contract tests, run against base)
Command: `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -run 'TestVerifyPR6181' -count=1 -gcflags=all=-l`
in a worktree of merge-base `f2785f84` with only `verify_pr6181_codex_test.go` added.
Observed: both contract tests fail with the exact symptom from issue #6174
(`storage map[]`, total label `0B`, no `-m-`/`-d-` labels). Expected on fixed code: pass.
This doubles as the harness-bites check: same file, red on base / green on PR head,
production code untouched in both runs.

### Fix validation (contract, run against PR head `6ea38789`)
Same command on the PR head. Observed: all pass; node labels match the issue's
expectation (`1GiB` total + `1GiB` disk for the issue's spec; `4GiB` mem + `4GiB` disk +
`8GiB` total for a mixed processMemory/hostPath spec — per-path hostPath quotas are
summed, not averaged across paths).

### F1 — stale capacity labels (bug-canary)
`TestVerifyPR6181StaleLabelsAfterSpecChange`: first sync labels node1 `1GiB`; after
editing the runtime quota to `2Gi` and re-syncing with a fresh engine, labels remain
`1GiB`. Root cause is pre-existing and engine-agnostic: `calculateNodeDifferences` only
visits newly added nodes and `addScheduleInfoToNode` skips nodes already carrying the
runtime label. The PR discloses this in "Special notes". The canary documents it and
flips red once re-labelling is implemented (invert assertions then).

## Proposed fixes (NOT applied to production here)
- **F1**: out of scope for this PR; track separately. Options: recompute capacity labels
  when the runtime generation/tiered store changes (e.g. hash label + reconcile on
  mismatch), or re-label assigned nodes during sync when the computed values differ.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cache-tieredstore-node-labels-codex` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the code under test:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cache-tieredstore-node-labels-codex
   git checkout <ref-to-test>
   git checkout FETCH_HEAD -- docs/verification/cache-tieredstore-node-labels pkg/ddc/cache/engine/verify_pr6181_codex_test.go
   ```
2. Run unit + integration layers:
   ```bash
   FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -run 'TestVerifyPR6181' -count=1 -gcflags=all=-l
   ```
   Or simply, from a checkout of this branch: `bash docs/verification/cache-tieredstore-node-labels/scripts/re-verify.sh`
   (no args: auto-fetches the current PR head; exit 0 iff every finding is Fixed).
3. Polarity table:
   - `TestVerifyPR6181RuntimeInfoStorageMap`, `TestVerifyPR6181NodeCapacityLabels` — contract:
     PASS = fix working, FAIL = premise back (regression).
   - `TestVerifyPR6181LabelFormatSanity` — harness pin: PASS expected always; failure means
     label naming changed and the other assertions need updating.
   - `TestVerifyPR6181StaleLabelsAfterSpecChange` — canary: PASS = limitation still present;
     FAIL at the "canary flipped" lines = F1 fixed, invert assertions to lock in the new behavior.
     (Fails at its precondition on pre-fix code; only meaningful once P0 is fixed.)

Copy-paste kickoff prompt for a fresh agent:
> Check out branch `verify/cache-tieredstore-node-labels-codex` from
> https://github.com/cheyang/fluid.git, read
> `docs/verification/cache-tieredstore-node-labels/README.md` and `verify-manifest.json`,
> then run `bash docs/verification/cache-tieredstore-node-labels/scripts/re-verify.sh`
> and report per finding Fixed / Still-broken / Partial / Harness-update.
