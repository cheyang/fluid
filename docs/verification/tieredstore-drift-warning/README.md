# tieredstore-drift-warning — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6219.

**Round 6** (head `724796907bd7f9124386e321e1b020898b21944a`, reviewed 2026-11-07, rebased onto
base `a01c038bcc115e46f0fd74faf5e5ca5e9c6aa840` = #6162 dataset-phase restore + #6227 Go 1.26.10
bump + #6222 codeql bumps + #6232 chart image bump, squashed to a single PR commit): the PR
content is **byte-identical** to round 5 for 9 of 10 files. The only PR-side change in the entire
round is **one comment line** in `test/gha-e2e/curvine/test.sh:522` — the exact round-5 cosmetic
residue recorded under TS-E2E-REVERT-VACUOUS-LEG ("the inline comment still reads
resolution-flavored — one comment line would finish the job the log line already did"): it now
reads "Trigger reconcile after revert and assert event count remains stable at 1 (no new conflict
events emitted)". No executable line changed. The head-vs-head diffs for `sync.go`/`sync_test.go`
are the #6162 base movement (CacheEngine.Sync dataset-status block, sync.go:84-117), not PR
content; the drift-check region is byte-identical to round 5 and #6162's hunk does not interact
with it. All five harness contracts plus the P0 premise test re-run green on the new head
(`results/unit-head-724796907.txt`, `results/reverify-round6.txt`); the premise was additionally
re-run against the **new base** `a01c038b` (PASS — the silent split still reproduces without the
PR). Full engine package 345 passed / 0 failed / 16 pre-existing env skips (+2 vs round 5 =
#6162's own new specs; note: without `-gcflags="all=-N -l"` gomonkey specs fail — that is the
known inlining artifact, not a regression). Required CI fully green on 724796907 (runs
42265387872/42265388300, 2026-11-06, head_sha verified via gh api) with all four drift phases
confirmed in the kind-e2e v1.22.17 job log (job 128055838695). The carried advisory
TS-REQUEUE-REEMISSION is unchanged (no controller/requeue file in the PR diff; -1 env still maps
to NoRequeue) — maintainer decision. #6192 and #6221 both re-checked OPEN 2026-11-07.

**Round 5** (head `a8f4e5584730987740bd0b81aaab18a33c2c74a4`, reviewed 2026-11-04, rebased onto
base `7f27b7e2a9cc6343e8905abd37a236e4c735dc24` = #6223 HPA scale subresource): the PR content is
**byte-identical** to round 4 for every file except `test/gha-e2e/curvine/test.sh`, whose entire
delta inside `verify_tieredstore_drift_warning` is the three round-4 SHOULDs: (1) both `count == 1`
legs now fail safe on an empty/failed kubectl query (`[[ -z "$count" || "$count" != "1" ]]`, closing
TS-E2E-COUNT-QUERY-MASK), (2) the revert leg log now reads "Revert stability verified: event count
remained 1" instead of overclaiming resolution (closing TS-E2E-REVERT-VACUOUS-LEG; the cooldown-reset
leg remains the real proof), (3) the dead `jsonpath='{len(.items)}'` fallback is now
`-o go-template='{{len .items}}'` (closing TS-E2E-LEN-JSONPATH — empirically, kubectl go-template
renders `len` while jsonpath errors "unrecognized identifier len"). Base movement #6223 touches the
same package (status/worker/master/component-manager/statefulset status) but not
`sync.go`/`cm.go`/`GetPodSpec` — no overlap with the drift-check path. All five harness contracts
plus the P0 premise test re-run green on the new head (`results/unit-head-a8f4e55.txt`,
`results/reverify-round5.txt`); full engine package 343 passed / 0 failed / 16 pre-existing env skips
(+2 vs round 4 = #6223's own new specs). Required CI fully green on a8f4e558 with all four drift
phases confirmed in the kind-e2e v1.22.17 job log (job 125777363705) carrying the new
"Revert stability verified" wording. Shell-level bite-checks for the three e2e hygiene fixes in
`results/bite-check-round5.txt`. #6221 (client-side detection) remains OPEN.

**Round 4** (head `133accac3a7cea53280ada93393b28433e71a6d7`, reviewed 2026-11-03, rebased onto
base `96a3765eaae0f23140aef9b30152538396ed7ec2`): the PR content is **byte-identical** to round 3
for every file except `.github/workflows/kind-e2e.yml` — the author dropped the PR's own
`ubuntu-24.04` runner-pin hunk, because the pin (with its rationale and the tracking issue
#6224) now lands in the **base** via maintainer PR #6225. That closes ledger TS-E2E-RUNNER-PIN
exactly as suggested (separate CI PR with one-sentence rationale). All five harness contracts
re-run green on the new head (`results/unit-head-133accac.txt`); the required CI gate is fully
green on 133accac (build x2, unittest, lint, staticcheck, Analyze(go)/CodeQL, SonarCloud,
codecov, backward-compat v1.22–v1.33, kind-e2e v1.22–v1.33 — runs 41392006282/41392006360/
41392006503/41392006576, 2026-11-02), with all four `verify_tieredstore_drift_warning`
assertion phases confirmed in the kind-e2e v1.22.17 job log (job 125105233485) carrying the
"Delete and recreate the CacheRuntime" remediation text. #6221 (client-side detection) and
#6224 (runner un-pin) remain OPEN.

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

### Round 4 — head 133accac (re-verify run `results/reverify/`, focused run `results/unit-head-133accac.txt`)

| ID | Claim | Layer | Polarity | Verdict | Evidence |
|----|-------|-------|----------|---------|----------|
| TS1 | Worker tieredStore quota drift emits a `RuntimeTieredStoreConflict` Warning through the **full `engine.Sync`** entry point, remediation text "Delete and recreate the CacheRuntime" | 1 | contract | **FIXED / working** (PASS on new head) | `results/unit-head-133accac.txt` |
| TS2 | `spec.client.tieredStore` drift stays silent — documented scope decision tracked in #6221 (still OPEN, re-checked 2026-11-03); the canary flips red when #6221 lands | 1 | canary | **STILL-PRESENT** (PASS = gap present, as documented) | `results/unit-head-133accac.txt` |
| TS3 | emptyDir `medium` flips (disk `""` ↔ `Memory`) detected in both directions | 1 | contract | **FIXED** (PASS on new head) | `results/unit-head-133accac.txt` |
| TS4 | Throttle across consecutive Syncs: 1 event per 3 syncs, re-emission after interval, cooldown reset on resolution | 1 | contract | **FIXED / working** (PASS on new head) | `results/unit-head-133accac.txt` |
| TS5 | A level carrying **both** `hostPath` and `emptyDir` is dispatched by the drift check the same way `extractTieredStoreLevels` reports it — hostPath first; the ConfigMap ground truth is asserted in the same test | 1 | contract | **FIXED** (PASS on new head) | `results/unit-head-133accac.txt` |

Round-4 delta: PR content unchanged (only the runner-pin hunk was dropped, its rationale moved
to base #6225); no new findings; no harness changes needed — the harness ran unmodified against
the new head, so no `Harness-update` verdicts. P0 premise evidence (base run) is unchanged from
round 1.

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

## Round 5 verdict table

| finding | round-4 status | round-5 status | evidence |
|---|---|---|---|
| TS1 drift-warning contract | FIXED | still FIXED | unit-head-a8f4e55.txt |
| TS2 client-silence canary | pass (expected) | still pass (expected; #6221 OPEN) | unit-head-a8f4e55.txt |
| TS3 medium-flip contract | FIXED | still FIXED | unit-head-a8f4e55.txt |
| TS4 throttle contract | FIXED | still FIXED | unit-head-a8f4e55.txt |
| TS5 dispatch-precedence contract | FIXED | still FIXED | unit-head-a8f4e55.txt |
| TS-E2E-COUNT-QUERY-MASK (nit) | open | FIXED this round | bite-check-round5.txt S1 + CI log |
| TS-E2E-REVERT-VACUOUS-LEG (nit) | open | FIXED this round | bite-check-round5.txt S3 + CI log |
| TS-E2E-LEN-JSONPATH (nit) | open | FIXED this round | bite-check-round5.txt S2 + CI log |
| TS-REQUEUE-REEMISSION (advisory) | open | still open (unchanged code; maintainer decision) | code read |
