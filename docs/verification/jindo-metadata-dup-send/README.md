# jindo-metadata-dup-send — bug verification

Reproducible evidence for the review of PR https://github.com/fluid-cloudnative/fluid/pull/6298
(fixes issue #6297). Reviewer: A (Claude). Round: initial (2026-11-27).

Layers run against the **code under review** (`dc85e482a8a23ecac8724b436e05f56edf10d2e4`,
base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | goroutine/channel contract of `syncMetadataInternal()` in both engines | `FLUID_UNIT_TEST=true go test -race -count=1 -run 'TestVerifySyncMetadataSingleSendNoLeak|TestSyncMetadataInternal' -v ./pkg/ddc/jindocache/ ./pkg/ddc/jindofsx/` |
| 2. Integration (package-level) | full affected-package suites incl. `-race` | `FLUID_UNIT_TEST=true go test -race -count=1 ./pkg/utils/testutil/... ./pkg/ddc/jindocache/... ./pkg/ddc/jindofsx/...` |
| 3. Live | not run (no KUBECONFIG this round; claim is fully decidable at unit layer) | — |

> Test polarity: both harness tests are **contract** tests (assert intended behavior).
> They FAIL on buggy code (duplicate `resultChan <- result`) and PASS when fixed.
> The PR's own `testutil.VerifyMetadataSyncChannelClosedOnce` is also contract polarity.

## Problem premise (P0)

Run against the **base** branch without the patch.

| | |
|---|---|
| Claimed symptom | "the goroutine blocks forever on `resultChan <- result` … leaking a goroutine and preventing `defer base.SafeClose(resultChan)` from running" on every metadata sync completion (issue #6297) |
| Linked issue | #6297, OPEN, created 2026-11-26 (one day before this PR) — still valid |
| Reported component | `pkg/ddc/jindocache/metadata.go` + `pkg/ddc/jindofsx/metadata.go` `syncMetadataInternal()` |
| Patched component | exactly those two files, duplicate send removed |
| Component match | **Yes** — exact |
| **Verdict** | **Confirmed** (real problem) |
| Evidence | On base, `TestVerifySyncMetadataSingleSendNoLeak` FAILS in both engines (a *second* value is observable on `MetadataSyncDoneCh`, proving the duplicate send); a one-off probe that consumes exactly one result — like the real reconcile loop — shows goroutines 3→4, one leaked goroutine per sync, so the deferred `base.SafeClose` never runs. `results/base-premise-p0.log`, `results/base-goroutine-leak-probe.log` |

Note on the issue's secondary claim: the "panics with `send on closed channel`" half of the
mechanism is **not reachable** in jindocache/jindofsx today — no code path external to the
goroutine closes `MetadataSyncDoneCh` in those engines (only `alluxio/shutdown.go:52-53` does an
external `base.SafeClose`). In practice the duplicate send *blocks forever* (the leak), which is
the real, reproduced defect. The fix removes both failure modes, so this nuance does not change
the verdict.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | duplicate send / goroutine leak exists on base | 1 (against base) | **Confirmed** | `results/base-premise-p0.log`, `results/base-goroutine-leak-probe.log` |
| PR6298-F1 | after the fix: exactly one result is sent, the goroutine exits, and the channel is closed by the deferred `SafeClose` | 1 | **Confirmed (fixed on head)** | `results/head-focused-race.log` — both engines PASS incl. `-race`; PR's own `TestSyncMetadataInternal` also PASS |
| PR6298-F1 | no regression in affected packages | 2 | **Confirmed** | `results/head-full-suite.log` — all four packages `ok` under `-race` |

## Per-finding detail

### P0 / PR6298-F1 — duplicate send on unbuffered `MetadataSyncDoneCh`

Mechanism (base): the sync goroutine calls `base.SafeSend(resultChan, result)` (consumed by the
receiver in `syncMetadataInternal`, which then nils `e.MetadataSyncDoneCh`), then executes a
second **unguarded** `resultChan <- result`. No second receive ever occurs in the real reconcile
loop, so the goroutine blocks forever and the deferred `base.SafeClose(resultChan)` never runs —
one leaked goroutine (plus retained engine/channel memory) per metadata sync.

Harness (additive, this branch):
- `pkg/ddc/jindocache/metadata_dup_send_verify_test.go` — `TestVerifySyncMetadataSingleSendNoLeak`
- `pkg/ddc/jindofsx/metadata_dup_send_verify_test.go` — same, for `JindoFSxEngine`

Observed vs expected:
- base: FAIL — "unexpected second value sent on MetadataSyncDoneCh"; leak probe: before=3 after=4.
- head: PASS (0.00s, also under `-race`).
- harness-bites: re-applying only the deleted line `resultChan <- result` on head makes **both**
  my harness test and the PR's `TestSyncMetadataInternal` FAIL (`results/head-harness-bites-unfixed.log`),
  then the un-fix was reverted — production diff empty again.

## Proposed fixes (NOT applied to production here)

- The PR's fix (delete the duplicate send) is the correct minimal fix; it matches the
  `SafeSend`-only pattern of alluxio/juicefs/thin. No additional production change required.

## Live run notes

Not applicable this round (no KUBECONFIG; unit layer is decisive for this claim).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/jindo-metadata-dup-send-claude` (production code untouched),
pushed to the reviewer fork `https://github.com/cheyang/fluid.git`.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/jindo-metadata-dup-send-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/jindo-metadata-dup-send \
     pkg/ddc/jindocache/metadata_dup_send_verify_test.go \
     pkg/ddc/jindofsx/metadata_dup_send_verify_test.go
   ```
2. Prereqs: Go toolchain (go1.26+) only. No cluster needed.
3. Re-run: `bash docs/verification/jindo-metadata-dup-send/scripts/re-verify.sh`
   (no argument — it resolves the current PR head from `manifest.pr`).
4. Read results via the polarity table: both tests are **contract** tests — they must be GREEN.
   If either goes red, the duplicate send (or an equivalent regression) is back.
5. Harness-bites: run Layer 1 once against pre-fix code to confirm it still goes red
   (`results/head-harness-bites-unfixed.log` shows the expected red).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/jindo-metadata-dup-send-claude of
https://github.com/cheyang/fluid.git. Background: review of PR
https://github.com/fluid-cloudnative/fluid/pull/6298 (fixes issue #6297, duplicate
send on MetadataSyncDoneCh in jindocache/jindofsx syncMetadataInternal). A harness
confirmed the premise on base and the fix on head. Read
docs/verification/jindo-metadata-dup-send/README.md ("Continuing after the fix") and
follow it: graft the harness onto the current PR head, run
scripts/re-verify.sh, mind the polarity table (all contract tests must be GREEN),
and report an observed-vs-expected table. Do not run cluster-wide destructive actions.
```
