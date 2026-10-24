# tieredstore-drift-warning — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6219 (head `58afa3a9651bef192bf5457bb552aed3656d95dc`, base `86e3adf35a7c26875e818edad3e01dfac15f0290`).

Production code is untouched on this branch — the diff is the harness only
(`pkg/ddc/cache/engine/premise_tieredstore_split_test.go`,
`pkg/ddc/cache/engine/verify_tieredstore_drift_test.go`, this directory).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (fake client) | the real creation path (`transform` + `SetupWorkerComponent`/`SetupClientComponent`), the real sync path (`syncRuntimeValueConfigMap` + `syncRuntimeSpec`), and the full `engine.Sync` entry point, against a controller-runtime fake client | `go test ./pkg/ddc/cache/engine -run 'TestPremiseTieredStoreSplitWorker|TestVerifyWorkerDriftWarnsThroughFullSync|TestVerifyClientTieredStoreDriftSilent|TestVerifyEmptyDirMediumFlipSilent' -count=1 -v` |
| 2. Integration | not needed for these claims: every finding is decided by controller-internal state (ConfigMap vs workload template vs recorder output) that the fake-client layer already exercises with the production code paths | — |
| 3. Live | skipped per round policy (unit + integration only) | see `liveNote` in the manifest |

> Polarity: contract tests assert intended behavior (FAIL on broken code).
> Bug-canaries assert the current gap (PASS now, FLIP TO RED when fixed).

## Problem premise (P0)

Answered against the **base** branch, without the patch.

| | |
|---|---|
| Claimed symptom | "Changing `spec.worker.tieredStore` on an existing CacheRuntime updates the runtime ConfigMap but not the worker Pod. The quota ends up recorded as two different values, and no event, condition or log line says so." (issue #6192, OPEN since 2026-09-11) |
| Reported component | CacheRuntime controller sync: `pkg/ddc/cache/engine` (`cm.go` ConfigMap rebuild, `sync.go` worker spec sync) |
| Patched component | same package (`sync.go`, `engine.go`, `constants.go`) + docs |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | `results/unit-base.txt` — on base `86e3adf3`, after editing the quota 8Gi→16Gi the ConfigMap `runtime.json` worker level carries `16Gi` while the worker ASTS volume `tiered-store-level-0-memory` keeps `sizeLimit 8Gi`, and the recorder attached to the engine saw **zero** events. The mechanism matches the issue's analysis: `generateRuntimeConfigData` recomputes from spec on every sync, `SyncComponentSpec` patches only replicas/image/resources, and `TransformRuntimeTieredStore` runs only at Setup. On the PR head the same test run (`results/unit-head.txt`) shows the split still present — by design, the PR warns about it — plus the new `RuntimeTieredStoreConflict` Warning. |

## Summary of results

| ID | Claim | Layer | Polarity | Verdict | Evidence |
|----|-------|-------|----------|---------|----------|
| TS1 | Worker tieredStore quota drift emits a `RuntimeTieredStoreConflict` Warning through the **full `engine.Sync`** entry point (not just `syncRuntimeSpec`) | 1 | contract | **Confirmed working** (test PASS on head) | `results/unit-head.txt` |
| TS2 | `spec.client.tieredStore` drift is **silent**: the ConfigMap client levels follow the edited spec, the client DaemonSet volume keeps its creation-time size, and no `RuntimeTieredStoreConflict` event fires — although the PR docs present the field path as `spec.{worker,client}.tieredStore` and the tieredStore userguide says "Modifying `tieredStore` on an existing CacheRuntime will trigger a `RuntimeTieredStoreConflict` Warning event" | 1 | canary | **Confirmed gap** (test PASS = gap present) | `results/unit-head.txt`, `results/bite-check.txt` §2 |
| TS3 | Flipping an emptyDir level's `medium` (disk `""` ↔ `Memory`) is a real split (ConfigMap `mediumType` flips, creation-path memory accounting flips, the worker volume stays disk-backed) that the drift check does not report: it compares only volume name + quota | 1 | canary | **Confirmed gap** (test PASS = gap present) | `results/unit-head.txt`, `results/bite-check.txt` §3 |

## Per-finding detail

### TS1 — contract: full-Sync warning works

`TestVerifyWorkerDriftWarnsThroughFullSync` builds the runtime/class/dataset, creates the
workloads via `transform` + `SetupMasterComponent`/`SetupWorkerComponent`/`SetupClientComponent`
(so the worker volume carries the creation-time 8Gi), patches the spec quota to 16Gi the way
`kubectl patch` would, runs `engine.Sync`, and expects a `RuntimeTieredStoreConflict` Warning.
Observed: PASS on head; FAIL when the drift check is temporarily disabled (`results/bite-check.txt` §1),
so the test bites.

### TS2 — canary: client tieredStore drift is silent

`TestVerifyClientTieredStoreDriftSilent` keeps the worker consistent (so the worker check cannot
be the event source), gives the client a `processMemory` 4Gi level at creation, edits the spec to
8Gi, runs `engine.Sync`, and observes: ConfigMap client quota `8Gi`, client DaemonSet volume
sizeLimit still `4Gi`, and **no** `RuntimeTieredStoreConflict` event. The issue itself flagged
this case ("`spec.client.tieredStore` should behave the same way, going by the code … I have only
reproduced the worker case"), so the gap is a known scope decision — but the docs added by this PR
(`cacheruntime_spec_update.md` §4.1 field path `spec.{worker,client}.tieredStore`, and
`cache_runtime_tieredstore.md` item 8) read as if the warning covers client edits too.
Bite check: adding client-side detection temporarily makes the canary FAIL (`results/bite-check.txt` §2).

### TS3 — canary: emptyDir medium flip is invisible

`TestVerifyEmptyDirMediumFlipSilent` creates a worker with a disk-backed emptyDir level (10Gi),
edits only `medium: Memory`, runs `engine.Sync`, and observes: the worker volume stays
disk-backed (`medium: ""`) with the same quota, and no event fires. The drift check's emptyDir
branch compares only `sizeLimit`, never `Medium` — yet the medium decides whether the creation
path charges the quota to container memory (`handleEmptyDir`) and what `mediumType` the ConfigMap
reports. Bite check: comparing `Medium` temporarily makes the canary FAIL (`results/bite-check.txt` §3).

## Harness-bites check (Step 4)

All three checks recorded in `results/bite-check.txt`; production code restored with
`git checkout -- pkg/ddc/cache/engine/sync.go` after each run (the branch diff stays harness-only):

1. contract test FAILS with the drift check disabled → it really tests the PR's fix;
2. client canary FLIPS TO RED when client-side detection is added;
3. medium canary FLIPS TO RED when `Medium` is compared.

## Proposed fixes (NOT applied here)

- **TS2**: either extend drift detection to the client DaemonSet pod spec (the issue's stated
  preference: "`spec.client.tieredStore` should behave the same way"), or scope the docs to the
  worker only — field path `spec.worker.tieredStore`, and reword `cache_runtime_tieredstore.md`
  item 8 to say worker.
- **TS3**: in the emptyDir branch, also compare `vol.EmptyDir.Medium != level.EmptyDir.Medium`
  and report a drift message (the PR's `expectedVolumes`/orphan logic already handles the rest).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/tieredstore-drift-warning-claude` (production code untouched),
so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/tieredstore-drift-warning-claude
   git checkout <fixed-ref>
   git checkout FETCH_HEAD -- docs/verification/tieredstore-drift-warning \
     pkg/ddc/cache/engine/premise_tieredstore_split_test.go \
     pkg/ddc/cache/engine/verify_tieredstore_drift_test.go
   ```
2. Run everything:
   ```bash
   bash docs/verification/tieredstore-drift-warning/scripts/re-verify.sh
   ```
   (no ref: it resolves the current PR head from the manifest's `pr` URL). Expected outcomes:
   - TS1 stays `FIXED` (contract, keeps passing);
   - TS2/TS3 are canaries: `FIXED(flipped)` when the gaps are closed — then invert their
     assertions (see the polarity notes in the test file headers) or delete them if the fix is
     a docs scope change;
   - `TestPremiseTieredStoreSplitWorker` is the premise demo, not a finding: it asserts the
     ConfigMap/volume split itself, which stays until someone changes the update semantics.
3. Unit layer prerequisites: go ≥ 1.24 (repo `go.mod`), module cache network access on first run.

Kickoff prompt for a fresh agent: "Checkout the fixed ref of fluid PR #6219, graft
`docs/verification/tieredstore-drift-warning` + the two test files from branch
`verify/tieredstore-drift-warning-claude` of https://github.com/cheyang/fluid.git, run
`bash docs/verification/tieredstore-drift-warning/scripts/re-verify.sh`, and report the
per-finding verdicts honoring canary polarity."
