# jindo-metadata-dup-send — bug verification (Reviewer B / codex)

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6298
(fixes https://github.com/fluid-cloudnative/fluid/issues/6297).

Layers run against the **code under review** (head `dc85e482a8a23ecac8724b436e05f56edf10d2e4`,
base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `syncMetadataInternal` sender-goroutine contract (exactly one send, then channel closed, goroutine exits) for both engines, plus the PR's own `TestSyncMetadataInternal` | `FLUID_UNIT_TEST=true go test ./pkg/ddc/jindocache/ ./pkg/ddc/jindofsx/ -run 'TestVerifyMetadataSync\|TestSyncMetadataInternal' -count=1 -v` |
| 2. Integration | full package suites of the touched packages incl. `pkg/ddc/base` (SafeSend/SafeClose ginkgo specs) | `FLUID_UNIT_TEST=true go test ./pkg/ddc/jindocache/... ./pkg/ddc/jindofsx/... ./pkg/ddc/base/... -count=1` |
| 3. Live | skipped per review policy (no KUBECONFIG layer) | — |

> Test polarity: all harness tests are CONTRACT tests — they FAIL on the pre-fix (base)
> code and PASS on the fixed code. There are no bug-canaries in this harness.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "the goroutine blocks forever on `resultChan <- result` (line 178), leaking a goroutine on every sync and preventing `defer base.SafeClose(resultChan)` from running" and "if `resultChan` is already closed … line 178 immediately panics with `send on closed channel`" (issue #6297, quoted) |
| Linked issue | #6297, OPEN, filed 2026-11-26 (one day before the PR), still valid |
| Reported component | `pkg/ddc/jindocache/metadata.go` + `pkg/ddc/jindofsx/metadata.go`, `syncMetadataInternal()` |
| Patched component | exactly those two files/functions |
| Component match | **Yes** |
| **Verdict** | **Confirmed** (leak + duplicate send reproduced on base; see nuance on the panic sub-claim below) |
| Evidence | `results/base-jindocache-repro.txt`, `results/base-jindofsx-repro.txt` |

Nuance: the issue's second symptom (panic on send-to-closed-channel) is **not reachable** in the
current codebase: the only closer of `MetadataSyncDoneCh` for jindocache/jindofsx is the sender
goroutine's own `defer base.SafeClose(resultChan)`, which runs only after all sends. Unlike
alluxio (`pkg/ddc/alluxio/shutdown.go:52`) and vineyard, these engines never close the channel
from outside. The real, reproduced bug is the goroutine leak (one parked sender goroutine per
completed metadata sync). This nuance does not affect the validity of the fix.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Base code sends twice per completed sync and leaks the sender goroutine | 1 (run against base) | **Confirmed** | base run: "duplicate send: received an unexpected second value"; "goroutine leaked: before=3, after=4" — both engines (`results/base-*-repro.txt`) |
| B1 | The PR fix makes one completed sync produce exactly one send, then closes the channel and exits the goroutine | 1 | Confirmed fixed on head | all contract tests PASS on head, incl. `-race` (`results/head-repro-and-pr-test.txt`, `results/head-race.txt`) |
| — | PR's own `TestSyncMetadataInternal` bites on base (harness-bites check) | 1 | Confirmed | grafted onto base it FAILs with "unexpected second value sent to metadata sync channel" (`results/base-pr-test-bites.txt`) |
| — | No regression in touched packages | 2 | Pass | full suites of jindocache, jindofsx (+operations), base all `ok` (`results/head-full-packages.txt`, `results/head-base-pkg.txt`) |

## Per-finding detail

### P0 / B1 — duplicate send + goroutine leak

Mechanism (base code): the goroutine spawned in `syncMetadataInternal` executes
`base.SafeSend(resultChan, result)` and then a bare `resultChan <- result`. The channel is
unbuffered and the receiver branch reads **at most one** value before setting
`e.MetadataSyncDoneCh = nil`; the next sync creates a **new** channel. So the second send never
has a receiver: the goroutine parks forever and its `defer base.SafeClose(resultChan)` never runs.

- Harness: `pkg/ddc/jindocache/metadata_dup_send_verify_test.go` and
  `pkg/ddc/jindofsx/metadata_dup_send_verify_test.go` (additive, in-package; production code
  untouched).
  - `TestVerifyMetadataSyncDuplicateSend`: starts one sync, consumes the single legitimate
    result, then observes the channel. Expected (fixed): channel closed, no second value.
    Observed on base: a second value (`ok == true`) — the duplicate send.
  - `TestVerifyMetadataSyncGoroutineLeak`: same drive, then never touches the channel again
    (production reality) and polls `runtime.NumGoroutine()`. Expected: returns to baseline.
    Observed on base: `before=3, after=4`, stable for 3s.
- Harness-bites: the PR's own updated `TestSyncMetadataInternal` (with
  `testutil.VerifyMetadataSyncChannelClosedOnce`) was grafted onto the base tree and FAILs there
  with "unexpected second value sent to metadata sync channel" — the PR's regression test does
  detect the bug.

## Proposed fixes (NOT applied to production here)

- B1 is exactly what the PR does: delete the bare `resultChan <- result` line. No further fix
  requested.
- Optional follow-up (pre-existing, out of scope for this PR): jindocache/jindofsx never close
  `MetadataSyncDoneCh` on engine shutdown (alluxio/vineyard do), so a sync whose result is never
  consumed (dataset removed mid-sync, master disabled) still leaves the sender goroutine parked
  in `base.SafeSend` until process exit.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/jindo-metadata-dup-send-codex` (remote: the reviewer's fork),
production code untouched.

1. `git fetch <fork> verify/jindo-metadata-dup-send-codex && git checkout verify/jindo-metadata-dup-send-codex`
2. Re-run everything against the current PR head:
   `bash docs/verification/jindo-metadata-dup-send-codex/scripts/re-verify.sh`
   (no ref needed — the head is auto-discovered from `manifest.pr`; exit 0 iff all findings fixed)
3. Prereqs: Go toolchain only (repo is vendored). `-race` variant:
   `FLUID_UNIT_TEST=true go test -race ./pkg/ddc/jindocache/ ./pkg/ddc/jindofsx/ -run 'TestVerifyMetadataSync|TestSyncMetadataInternal' -count=1`
4. Polarity: all tests are contract tests — GREEN means the fix is still in place; a RED
   `TestVerifyMetadataSync*` means the duplicate send regressed.
5. Harness-bites: to re-prove the harness still bites, check out base
   `b8ff60e309c341b58a9e26b085b76e6067e6bb60`, copy the two `metadata_dup_send_verify_test.go`
   files in, and run them — both must FAIL.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/jindo-metadata-dup-send-codex of the reviewer's
fork. Background: review of https://github.com/fluid-cloudnative/fluid/pull/6298 (fix for the
duplicate send on MetadataSyncDoneCh in jindocache/jindofsx) confirmed the bug on base and the
fix on head; the harness is contract-only. Read
docs/verification/jindo-metadata-dup-send-codex/README.md and run
docs/verification/jindo-metadata-dup-send-codex/scripts/re-verify.sh to re-check the current PR
head, then review the delta recorded by .last-reviewed and advance that marker.
```
