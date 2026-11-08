# pr6233-pvcnotbound-log — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6233
(`fix(thin): reduce log noise from not-yet-bound PVC in ShouldUpdateUFS`).

Layers run against the **code under review** (head `952e37ed16c99cea73fe044ea95d4b07630e5c66`,
base `a01c038bcc115e46f0fd74faf5e5ca5e9c6aa840`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `ThinEngine.ShouldUpdateUFS` → `updateFuseConfigOnChange` → `transformFuseConfig` → `extractVolumeInfo` with a fake client and a level-recording log sink | `go test ./pkg/ddc/thin/ -run 'TestVerifyPR6233_' -v` |
| 2. Integration | identical scenario against a **real API server** (envtest, k8s 1.34.1), Dataset CRD installed, PVC status set via the status subresource | `KUBEBUILDER_ASSETS=$(ls -d ~/.local/share/kubebuilder-envtest/k8s/* \| sort -V \| tail -1) go test ./pkg/ddc/thin/ -run 'TestVerifyPR6233_Envtest' -v` |
| 3. Live | skipped (debate setup: no live/KUBECONFIG layer; log routing needs no cluster) | see `liveNote` in verify-manifest.json |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "For a `pvc://` mount whose PVC exists but is not yet `Bound`, `extractVolumeInfo` returns: `persistent volume claim <name> not bounded yet` ... but `ShouldUpdateUFS` now emits it at Error level roughly once per `syncRetryDuration` (default 5s) until the PVC binds." (issue #6203) |
| Linked issue | #6203, OPEN, filed 2026-10-08 by maintainer @cheyang, still valid |
| Reported component | `ThinEngine.updateFuseConfigOnChange` / `ShouldUpdateUFS` (`pkg/ddc/thin`) |
| Patched component | `pkg/ddc/thin/transform_config.go`, `pkg/ddc/thin/ufs.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** — the symptom reproduces on base at both unit and integration layers |
| Evidence | base run: `pending PVC produced 1 Error log records ([Failed to update fuse config])` per `ShouldUpdateUFS` call — `results/base-harness.txt`, `results/base-envtest.txt` |

Mechanism check on base: `extractVolumeInfo` errors for pending PVC → `transformFuseConfig`
wraps (`errors.Wrapf`, pkg/errors v0.9.1 which implements `Unwrap`) → `updateFuseConfigOnChange`
propagates → `ShouldUpdateUFS` logs `Error` and returns nil; `pkg/ddc/base/syncs.go:100`
nil-guards `ufsToUpdate`, so only the log surface is affected. Matches the issue exactly.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0a | pending PVC → no Error-level log from ShouldUpdateUFS | 1 | Confirmed on base (fails), fixed on head (passes) | `results/base-harness.txt` vs `results/head-harness.txt` |
| P0b | pending PVC notice emitted at verbosity V(1)+, not V(0) | 1 | Fixed on head (passes, level recorded = 1) | `results/head-harness.txt` |
| P0  | premise against a real API server | 2 | Confirmed on base (fails), fixed on head (passes) | `results/base-envtest.txt` vs `results/head-envtest.txt` |
| GUARD | unexpected errors (PVC missing) still log at Error | 1 | Passes on **both** base and head — behavior preserved | both harness result files |
| F2 | PVC in `ClaimLost` (VolumeName set) is demoted to V(1) together with ClaimPending | 1 | **Holds on head** (canary) — PR-introduced scope widening beyond issue #6203's "ClaimPending or empty VolumeName" | canary PASS in `results/head-harness.txt`, FAIL on base (`CANARY FLIPPED ... Error logs`) in `results/base-harness.txt` |
| F3 | PR's own `testCaptureLogSink` discards the verbosity level, so its demotion test cannot catch a `V(1)`→`V(0)` regression | 1 | Confirmed by code reading; head behavior itself proven correct by P0b (level-aware sink records level 1) | `pkg/ddc/thin/ufs_ginkgo_test.go:408-420` vs `verify_pr6233_pvcnotbound_test.go` |

Whole-package sweep on head: `go test ./pkg/ddc/thin/...` → all ok
(`results/head-full-package.txt`), including the PR's own new Ginkgo specs and the new
`wantErrIs: ErrPVCNotBound` table case (which also proves `errors.Is` traverses the
pkg/errors `Wrapf` + `fmt.Errorf %w` chain with pkg/errors v0.9.1).

## Per-finding detail

### P0 (premise) — Confirmed
- Expected on base (per issue): Error log each sync. Observed: exactly 1 Error record
  `Failed to update fuse config` per call, fake client **and** real API server.
- On head: 0 Error records; one info record `PVC is not bounded yet, will update fuse
  config once bounded` at level 1.

### F2 (minor) — ClaimLost folded into the transient demotion
`extractVolumeInfo` returns the sentinel whenever `len(pvc.Spec.VolumeName) == 0 ||
pvc.Status.Phase != corev1.ClaimBound`. A PVC whose bound PV is lost transitions to
`ClaimLost` with `VolumeName` still set — a storage failure, not self-resolving
provisioning — yet it now takes the V(1) branch and becomes invisible at default log
verbosity. The canary `TestVerifyPR6233_Canary_LostPVCDemotedToVerboseLog` proves the
current behavior and flips red once the predicate is narrowed (e.g. only wrap
`ErrPVCNotBound` when `Phase == ClaimPending || VolumeName == ""`, let `ClaimLost` keep
Error). Impact is bounded: no functional change (update skipped either way), and a lost PV
breaks the workload with other, louder signals — hence minor, not major.

### F3 (minor) — PR's new demotion test is level-blind
`testCaptureLogSink.Info` records only the message; `Enabled` returns true for every level.
Replacing `t.Log.V(1).Info` with `t.Log.Info` (V(0)) would keep the PR's new spec green
while restoring per-sync log spam at default verbosity — defeating the PR's stated goal.
Fix: record the level in the sink and assert `level == 1` (this harness's
`verifyPR6233LogSink` does exactly that; `TestVerifyPR6233_Premise_PendingPVCLogsAtVerboseLevel`
proves head currently emits at level 1).

## Proposed fixes (NOT applied to production here)
- **F2**: narrow the sentinel wrap in `extractVolumeInfo` to the genuinely transient states,
  e.g. wrap `ErrPVCNotBound` only when `pvc.Status.Phase == corev1.ClaimPending ||
  len(pvc.Spec.VolumeName) == 0`, leaving `ClaimLost` on the plain error path.
- **F3**: make `testCaptureLogSink` record `level` alongside the message and assert
  `level >= 1` for the pending-PVC notice.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr6233-pvcnotbound-log-codex` (production code untouched;
diff vs PR head is additive test + docs files only).

```bash
git fetch https://github.com/cheyang/fluid.git verify/pr6233-pvcnotbound-log-codex
git checkout verify/pr6233-pvcnotbound-log-codex
bash docs/verification/pr6233-pvcnotbound-log/scripts/re-verify.sh   # auto-fetches current PR head
```

Polarity recap for the next round: P0a/P0b/P0/GUARD/F3 are contract tests — green = good.
F2 is a canary — red on a future head means "ClaimLost now logs at Error"; if that was the
intended fix, invert the canary (assert the Error record) so it keeps guarding.

Kickoff prompt for a fresh agent: "Continue verifying fluid-cloudnative/fluid PR #6233 from
branch verify/pr6233-pvcnotbound-log-codex; run
docs/verification/pr6233-pvcnotbound-log/scripts/re-verify.sh and advance .last-reviewed."
