# Verification — PR #6219 (CacheRuntime tieredStore drift warning)

Reviewer: codex (Reviewer B in the pr-debate pipeline). Round: initial.
Head under review: `58afa3a9651bef192bf5457bb552aed3656d95dc` (base `86e3adf35a7c26875e818edad3e01dfac15f0290`).
Harness branch: `verify/cacheruntime-tieredstore-drift-codex`. Production code untouched — the branch adds only test files and this directory.

## Premise (P0) — verdict: CONFIRMED

Issue #6192 (open, 2026-09-11) claims: *"Changing `spec.worker.tieredStore` on an existing CacheRuntime updates the runtime ConfigMap but not the worker Pod. The quota ends up recorded as two different values, and no event, condition or log line says so."*

Reproduced deterministically against the **base** commit (`results/base-premise-unit.txt`):

| observation point | after create | after editing quota 8Gi→16Gi + sync |
| --- | --- | --- |
| ConfigMap `runtime.json` worker quota | 8Gi | **16Gi** (follows spec) |
| worker ASTS volume `tiered-store-level-0-memory` sizeLimit | 8Gi | **8Gi** (frozen at creation) |
| events mentioning tieredStore | none | **none** (silent) |

Component match: report and patch both live in `pkg/ddc/cache/engine`. The mechanism also matches the code: `generateRuntimeConfigData` rebuilds the ConfigMap from spec on every sync while `SyncComponentSpec` only patches replicas/image/resources, never volumes.

On the PR head the same harness shows the split still exists (by design — the PR is detection, not patching) but is now surfaced: the canary `TestVerifyPR6219Premise_SilentOnBase` flips with
`Warning RuntimeTieredStoreConflict Worker tieredStore cannot be updated in-place after creation: level 0 (processMemory): spec quota 16Gi does not match worker volume tiered-store-level-0-memory sizeLimit 8Gi...` (`results/head-all-layers.txt`).

## Findings and evidence

| id | verdict | layer | what the harness shows |
| --- | --- | --- | --- |
| FIX (contract) | confirmed on head | unit + envtest | drift edit ⇒ Warning event with both quotas; steady state emits nothing (no false positive after real API round-trip) |
| F1 | confirmed (canary pins it) | unit | deleting the worker ASTS to "recreate the worker" never heals: `syncRuntimeSpec` fails with NotFound forever, nothing recreates the workload; only recreating the CacheRuntime (control in the same test) converges and stops the warnings |
| F2 (medium blind spot) | confirmed (canary pins it) | unit | editing only `emptyDir.medium` (""→Memory) flips ConfigMap `mediumType` HDD→MEM while the pod volume stays disk-backed and no event fires |
| F3 (client scope) | confirmed (canary pins it) | unit | editing `spec.client.tieredStore` updates the ConfigMap client levels with no warning (PR covers worker only) |

### F1 — remediation guidance wedges the runtime (major)

The event message ("Recreate the worker to apply changes.") and the new docs ("deleting and recreating the worker workload (or the CacheRuntime)") suggest deleting the worker workload. `TestVerifyPR6219RemediationDeleteWorkerWorkloadDoesNotHeal` proves that path bricks the runtime: `SetupWorkerComponent` only runs from `Setup` and is gated on `Status.Worker.Phase == None`, so a deleted worker ASTS is never recreated; `syncRuntimeSpec` fails on `GetPodSpec` NotFound on every reconcile (requeue 20s, `Failed to sync the ddc` events), and the drift warning itself stops firing. Only recreating the CacheRuntime works. The docs/message should say "delete and recreate the CacheRuntime" (or the controller should learn to recreate a missing workload).

### F2 — `emptyDir.medium`-only edits are invisible (minor)

`checkWorkerTieredStoreDrift` compares quotas (processMemory/emptyDir) and hostPath paths, but not the emptyDir medium. A medium-only edit produces the same class of silent split the PR exists to surface (ConfigMap says MEM, volume is disk). Canary: `TestVerifyPR6219MediumOnlyEditInvisibleToDriftCheck`.

### F3 — client tieredStore drift unwarned (minor / question)

Issue #6192 notes the client DaemonSet likely splits the same way; the PR implements the worker check only. `spec.{worker,client}.tieredStore` are both documented as immutable, but only worker drift raises the event. Canary: `TestVerifyPR6219ClientTieredStoreEditSilent`. Acceptable scope for this PR if tracked as follow-up.

## How to run

```bash
# unit layer (all harness tests; note the premise canary is EXPECTED to fail on the fixed head)
go test -count=1 -v ./pkg/ddc/cache/engine -run 'TestVerifyPR6219'

# integration layer (envtest; needs KUBEBUILDER_ASSETS or ~/.local/share/kubebuilder-envtest, self-skips otherwise)
go test -count=1 -v ./pkg/ddc/cache/engine -run 'TestVerifyPR6219Envtest'

# premise against BASE (copy the premise file into a base checkout first)
git worktree add /tmp/fluid-base 86e3adf35a7c26875e818edad3e01dfac15f0290
cp pkg/ddc/cache/engine/verify_pr6219_premise_test.go /tmp/fluid-base/pkg/ddc/cache/engine/
cd /tmp/fluid-base && go test -count=1 -v ./pkg/ddc/cache/engine -run 'TestVerifyPR6219Premise'

# one-line re-verification against a future fixed head
bash docs/verification/cacheruntime-tieredstore-drift/scripts/re-verify.sh
```

The PR's own ginkgo suite (and the whole `pkg/ddc/cache/engine` package) requires `go test -gcflags="all=-N -l"` because of gomonkey; without it 35 specs fail on base and head alike — pre-existing, unrelated to this PR.

## Continuing after the fix

- `FIX-*` contract tests must PASS on the fixed head.
- `P0-silent` must FAIL on the fixed head (it is the canary for the fix; keep it as the historical record or invert it).
- F1/F2/F3 canaries PASS while the gaps exist; when a gap is closed the corresponding canary FLIPS to fail — invert the assertion then.
- Raw outputs: `results/base-premise-unit.txt` (base), `results/head-all-layers.txt` (head, unit+envtest), `results/head-pr-own-tests.txt` (PR's own 12 drift specs), `results/head-full-package.txt` (whole package with required gcflags).

## Resume prompt (fresh machine)

```
Read docs/verification/cacheruntime-tieredstore-drift/{README.md,verify-manifest.json} on branch
verify/cacheruntime-tieredstore-drift-codex of https://github.com/cheyang/fluid.git, then run
bash docs/verification/cacheruntime-tieredstore-drift/scripts/re-verify.sh
```

---

## Round 2 — head `b56db670bfd45383cf8cdf237125fd82b90a1e13` (2026-10-25)

The author addressed all round-1 findings in a force-pushed single commit
(`58afa3a9..b56db670`). `scripts/re-verify.sh` on the new head:
**RESULT: all findings fixed** (`results/reverify-summary.txt`).

| id | round-1 state | round-2 verdict | evidence |
| --- | --- | --- | --- |
| P0 premise | confirmed on base | fix canary flipped on head | `results/head-b56db670-harness.txt` |
| FIX contracts | green | still green (unit + envtest) | `results/head-b56db670-harness.txt` |
| F1 remediation wedge | open (major) | **fixed** — event message (sync.go:302) and all en+zh doc sites now say "delete and recreate the CacheRuntime"; zero "recreate the worker" references remain | new contract `TestVerifyPR6219RemediationGuidanceRecreateCacheRuntime` + `TestVerifyPR6219DocsGuidanceAndScope` |
| F2 medium blind spot | open (minor) | **fixed** — `checkEmptyDirDrift` compares `Medium`; canary flipped red with the new medium drift message | `TestVerifyPR6219MediumDriftNowDetected` (contract); PR's own 2 medium specs green |
| F3 client doc scope | open (minor) | **fixed** — docs scoped to `spec.worker.tieredStore`; client detection deferred to issue #6221 (OPEN, author-filed) | `TestVerifyPR6219DocsGuidanceAndScope`; client-silence canary stays green as the pre-existing-behavior record |
| TS-E2E-GAP | open (major) | **fixed** — `verify_tieredstore_drift_warning` in test/gha-e2e/curvine/test.sh runs in the required kind-e2e checks; all 5 k8s versions green on b56db670 | `gh pr checks 6219` 2026-10-25; .github/scripts/gha-e2e.sh:117 |
| TS-VAR-SCOPE | open (nit) | **fixed** — per-engine `tieredStoreWarningInterval` field with 0-fallback getter; engine_test.go covers it | engine.go:58,71-76,95 |

Harness changes this round (additive): F1/F2/F3 re-polarized from canaries to
contracts because the fixes landed as text/comparison/docs changes the round-1
canaries could not observe. The round-1 canary tests stay in the tree:
`TestVerifyPR6219MediumOnlyEditInvisibleToDriftCheck` flipped red (= blind spot
closed, superseded by the F2 contract); `...RemediationDeleteWorkerWorkloadDoesNotHeal`
and `...ClientTieredStoreEditSilent` stay green, pinning pre-existing behavior the
PR deliberately did not change (now unreferenced by guidance / tracked in #6221).
`results/head-b56db670-pr-specs.txt`: the PR's 15 focused drift specs all pass
(`-gcflags="all=-N -l"` required, pre-existing gomonkey constraint).

New nit this round (non-blocking): `verify_tieredstore_drift_warning`'s revert
patch restores `quota: 1Gi` but drops the level's original `high`/`low` fields
(JSON merge replaces the array), and the "resets upon reversal" step is not
asserted (no check that the warning stops) — harmless today because the drift
check ignores high/low and the dataset is deleted right after.

## Round 3 — head `6809f62f98f70182595a644979bfe430d22cb0ff` (2026-10-30)

Force-pushed rebase onto the moved base (`e5251b3` → `fe074af`, which is #6215/#6214/#6217/#6186 — no CacheRuntime drift-path overlap beyond `sync.go` context lines) plus one production-code behavior change and a much stronger e2e. `git range-diff e5251b3..b56db670 fe074af..6809f62f` isolates the author's delta to:

1. `sync.go`: drift-check dispatch flipped to HostPath-before-EmptyDir, matching `extractTieredStoreLevels` (cm.go). Fixes round-2 question TS-DISPATCH-PRECEDENCE.
2. `sync_test.go`: new spec pinning that precedence ("should give precedence to hostPath over emptyDir …").
3. `test/gha-e2e/curvine/test.sh`: `verify_tieredstore_drift_warning` extended — UID+namespace+Warning-type scoped event queries (fixes TS-E2E-NS-FLAG), annotation-triggered reconcile cycles asserting the aggregated event `count` stays 1 under persistent drift (fixes TS-E2E-THROTTLE-ASSERT), revert of the full original level incl. `high`/`low`, and a cooldown-reset leg that re-introduces drift and asserts immediate re-emission within 60 s (fixes the TS-E2E-REVERT-ASSERT core).
4. `.github/workflows/kind-e2e.yml`: runner pinned `ubuntu-latest` → `ubuntu-24.04` (CI hygiene; all 5 kind-e2e jobs green on head).

`scripts/re-verify.sh` on the new head: **RESULT: all findings fixed** — the six round-2 entries plus the two new round-3 contracts below (fresh raw output in `results/reverify/`, now actually persisted: the script previously copied results before the trap's `git checkout -f` restore, which wiped them; fixed this round).

| id | round-2 state | round-3 verdict | evidence |
| --- | --- | --- | --- |
| TS-DISPATCH-PRECEDENCE | open question (Copilot-only, harness-unverified) | **fixed + harness-verified** | contract `TestVerifyPR6219DispatchMatchesConfigMapExtraction`: a both-fields level (creation rejects multi-medium levels, CRD has no one-of — reachable only via edit) warns as hostPath AND `extractTieredStoreLevels` reports the hostPath mount path for the same level. Bite-check: restoring the round-2 order makes the warning vanish (`expected a RuntimeTieredStoreConflict warning event, got events: []`) — the contract fails on the old code. |
| R3-HOSTPATH-STEADY (new guard) | n/a | green | `TestVerifyPR6219HostPathSteadyStateNoWarning`: hostPath level with matching pod volume emits no warning under the hostPath-first dispatch (no-false-positive guard). |
| TS-E2E-THROTTLE-ASSERT | open (minor) | **fixed** | e2e now annotates the CacheRuntime twice under persistent drift and asserts `items[0].count == 1`. Mechanism verified by code read: the runtime watch predicate enqueues on any resourceVersion change (pkg/ctrl/watch/runtime.go onUpdateFunc), `engine.Sync` runs `syncRuntimeSpec` on every reconcile, and client-go's default event correlator (record.NewBroadcaster via controller-runtime) aggregates repeats into one object with count++ (events_cache.go: maxEvents=10 / 600 s window) — so a broken per-reconcile throttle would grow `count` and fail the assertion. All 5 kind-e2e versions green on head (gh pr checks 6219, 2026-10-30). |
| TS-E2E-REVERT-ASSERT | open (minor) | **fixed** | revert now restores the full original level (`high:"0.8",low:"0.5"` included), and the new cooldown-reset leg (re-patch → expect count≥2 or a second item within 60 s) genuinely proves resolution was observed — the throttle alone would block re-emission for 10 min. Residual nit: the intermediate "count stays 1 after revert" assertion is vacuous w.r.t. resolution (the throttle guarantees it); the reset leg is the real teeth. |
| TS-E2E-NS-FLAG | open (nit) | **fixed** | every events query in the new function carries `-n default` + `involvedObject.uid=<uid>,type=Warning,reason=…`. |
| TS-REQUEUE-REEMISSION | open (Copilot advisory) | still open, non-blocking | PR does not touch requeue behavior (pre-existing infra); consequence unverified; not claimed by the author. |

PR's own suites on head: 16/16 focused drift specs and the full ginkgo package (341 passed, 0 failed) green (`results/head-6809f62f-pr-specs.txt`; `-gcflags="all=-N -l"` required, pre-existing gomonkey constraint). The only red tests in the package are the two intentionally flipped round-1 canaries (see manifest polarityNotes).
