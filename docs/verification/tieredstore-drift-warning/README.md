# tieredstore-drift-warning — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6219.

**Round 3** (head `6809f62f98f70182595a644979bfe430d22cb0ff`, reviewed 2026-10-30, rebased onto
base `fe074af38434fc4b618686de6459a9173570b48b`): all round-2 contracts survive the rebase (the
base move brought #6186's `reportIncompleteVersion`/`desiredComponentVersion` into the same
`syncRuntimeSpec` region), TS2 stays the expected client-silence record, and the new **TS5**
contract pins the round-3 precedence fix (ledger TS-DISPATCH-PRECEDENCE) with a bite-check in
`results/bite-check-round3.txt`. The e2e-side round-2 SHOULD items (throttle assertion, full-level
revert, no-new-events assertion, cooldown-reset assertion, `-n`/UID/Warning-type event scoping)
are verified from the CI kind-e2e logs on this head, not from this harness — see the round-3
ledger.

**Round 2** (head `b56db670bfd45383cf8cdf237125fd82b90a1e13`, reviewed 2026-10-25): the
revision fixed the round-1 findings. TS3 flipped to red exactly as predicted and is now the
inverted contract test `TestVerifyEmptyDirMediumFlipDetected`; TS4 is a new contract for the
throttle-across-Syncs property; TS1 additionally pins the reworded remediation text; TS2
stays a canary pinned to follow-up issue #6221. Round-2 bite-checks in
`results/bite-check-round2.txt`. Round 1 (head `58afa3a9651bef192bf5457bb552aed3656d95dc`,
base `86e3adf35a7c26875e818edad3e01dfac15f0290`) is preserved below.

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

### Round 3 — head 6809f62 (re-verify run `results/reverify/`, focused run `results/unit-head-6809f62.txt`)

| ID | Claim | Layer | Polarity | Verdict | Evidence |
|----|-------|-------|----------|---------|----------|
| TS1 | Worker tieredStore quota drift emits a `RuntimeTieredStoreConflict` Warning through the **full `engine.Sync`** entry point, remediation text "Delete and recreate the CacheRuntime" | 1 | contract | **FIXED / working** (PASS on rebased head) | `results/unit-head-6809f62.txt` |
| TS2 | `spec.client.tieredStore` drift stays silent — documented scope decision tracked in #6221 (still OPEN); the canary flips red when #6221 lands | 1 | canary | **STILL-PRESENT** (PASS = gap present, as documented) | `results/unit-head-6809f62.txt` |
| TS3 | emptyDir `medium` flips (disk `""` ↔ `Memory`) detected in both directions | 1 | contract | **FIXED** (PASS on rebased head) | `results/unit-head-6809f62.txt` |
| TS4 | Throttle across consecutive Syncs: 1 event per 3 syncs, re-emission after interval, cooldown reset on resolution | 1 | contract | **FIXED / working** (PASS on rebased head) | `results/unit-head-6809f62.txt` |
| TS5 | **New this round:** a level carrying **both** `hostPath` and `emptyDir` (no CRD one-of validation) is dispatched by the drift check the same way `extractTieredStoreLevels` reports it — hostPath first; the ConfigMap ground truth is asserted in the same test | 1 | contract | **FIXED** (PASS on head; FAILS with the pre-fix EmptyDir-first order) | `results/unit-head-6809f62.txt`, `results/bite-check-round3.txt` |

Round-3 delta: TS5 added (ledger TS-DISPATCH-PRECEDENCE, Copilot C4368713436 — fixed by the
head's HostPath-before-EmptyDir reordering); full `./pkg/ddc/cache/engine/...` package also run
green on the rebased head (composition/image/transform specs from the base's #6186 coexist with
the PR's 51 sync specs). The live e2e leg was verified from CI evidence (kind-e2e v1.22.17 job
122342754629 log shows all four new assertion lines) rather than a local cluster, per round policy.

### Round 2 — head b56db670 (re-verify run `results/reverify/`)

| ID | Claim | Layer | Polarity | Verdict | Evidence |
|----|-------|-------|----------|---------|----------|
| TS1 | Worker tieredStore quota drift emits a `RuntimeTieredStoreConflict` Warning through the **full `engine.Sync`** entry point, and the remediation text says "Delete and recreate the CacheRuntime" (the remedy that converges) | 1 | contract | **FIXED / working** (PASS on head) | `results/reverify/unit.out` |
| TS2 | `spec.client.tieredStore` drift stays silent — now an honestly-documented scope decision tracked in #6221; the canary flips red when #6221 lands | 1 | canary | **STILL-PRESENT** (PASS = gap present, as documented) | `results/reverify/unit.out` |
| TS3 | Flipping an emptyDir level's `medium` (disk `""` ↔ `Memory`) is detected in **both** directions (inverted from the round-1 canary, which flipped when the `Medium` comparison landed in `checkEmptyDirDrift`) | 1 | contract | **FIXED** (PASS on head) | `results/reverify/unit.out`, `results/bite-check-round2.txt` §1 |
| TS4 | Throttle across consecutive Syncs on the same engine: exactly 1 event per 3 syncs while drift persists, re-emission after the interval, immediate re-warn after resolution (cooldown reset) | 1 | contract | **FIXED / working** (PASS on head) | `results/reverify/unit.out`, `results/bite-check-round2.txt` §2 |

Round-2 deltas vs the round-1 table: TS3 polarity inverted (canary → contract) per policy;
TS2's gap is no longer a doc mismatch (docs are worker-scoped on head) and is pinned to
follow-up issue #6221.

### Round 1 — head 58afa3a9 (preserved)

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

### TS5 — contract (round 3): drift dispatch matches ConfigMap extraction precedence

`TestVerifyDispatchPrecedenceMatchesConfigMap` creates a worker with a Memory `emptyDir` level,
edits the level to carry **both** `hostPath` (`/mnt/cache`) and `emptyDir` (left exactly as
created — so an emptyDir-first dispatch would find no drift at all), runs `engine.Sync`, then
asserts two things: the ConfigMap's `runtime.json` reports the level as hostPath (`mediumType:
"HDD"`, hostPath-shaped `mountPaths`) — the ground truth the drift check must match — and the
`RuntimeTieredStoreConflict` warning validates the hostPath volume (`tiered-store-level-0-index-0`)
rather than dispatching to the emptyDir branch. Bite check: swapping the dispatch back to
EmptyDir-first makes it FAIL with "both-fields level edit was not reported"
(`results/bite-check-round3.txt`) — that silence is exactly the pre-fix behavior Copilot
C4368713436 predicted for a malformed-but-accepted level.

## Harness-bites check (Step 4)

Round 3 (`results/bite-check-round3.txt`, on head 6809f62): reverting the dispatch order to
EmptyDir-first fails TS5. Production code restored with
`git checkout -- pkg/ddc/cache/engine/sync.go` after the run.

Round 2 (`results/bite-check-round2.txt`, on head b56db670): removing the `Medium`
comparison fails TS3 in both subtests; disabling the throttle gate fails TS4. Production
code restored with `git checkout -- pkg/ddc/cache/engine/sync.go` after each run.

Round 1 (`results/bite-check.txt`, on head 58afa3a9): all three checks recorded;
production code restored the same way:

1. contract test FAILS with the drift check disabled → it really tests the PR's fix;
2. client canary FLIPS TO RED when client-side detection is added;
3. medium canary FLIPS TO RED when `Medium` is compared.

## Proposed fixes (NOT applied here)

- **TS2** (round 1): fixed by the author via doc scoping + follow-up issue #6221; the canary
  stays as #6221's regression test and must be inverted when #6221 lands.
- **TS3** (round 1): fixed by the author in `checkEmptyDirDrift` (Medium compared); the
  canary is now the inverted contract test.

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
   - TS1/TS3/TS4 are contracts on head b56db670: they stay green; a red result is a
     regression (event text, medium detection, or throttle);
   - TS2 is a canary: it passes while client drift stays undetected and flips to red when
     #6221 lands — then invert it into a contract test;
   - `TestPremiseTieredStoreSplitWorker` is the premise demo, not a finding: it asserts the
     ConfigMap/volume split itself, which stays until someone changes the update semantics.
3. Unit layer prerequisites: go ≥ 1.24 (repo `go.mod`), module cache network access on first run.

Kickoff prompt for a fresh agent: "Checkout the fixed ref of fluid PR #6219, graft
`docs/verification/tieredstore-drift-warning` + the two test files from branch
`verify/tieredstore-drift-warning-claude` of https://github.com/cheyang/fluid.git, run
`bash docs/verification/tieredstore-drift-warning/scripts/re-verify.sh`, and report the
per-finding verdicts honoring canary polarity."
