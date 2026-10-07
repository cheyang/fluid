# thin-ufs-errprop — bug verification

Reproducible evidence for the findings raised while reviewing
[fluid-cloudnative/fluid#6187](https://github.com/fluid-cloudnative/fluid/pull/6187)
("fix(thin): propagate the transformFuseConfig error in updateFuseConfigOnChange").

Reviewer: **Reviewer A (Claude)**, first round, 2026-10-08.
Branch: `verify/thin-ufs-errprop-claude`, based on the PR head `7ac6f29c`.
Production code is **untouched** on this branch — the only additions are this harness
(one test file + docs).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `ThinEngine.ShouldUpdateUFS` / `updateFuseConfigOnChange` with a fake client and a recording `logr` sink | `go test ./pkg/ddc/thin/ -run 'TestVerifyClaude_ShouldUpdateUFSLogsTransformFailure\|TestThinEngine_updateFuseConfigOnChange' -count=1 -gcflags="all=-N -l"` |
| 2. Integration | full `pkg/ddc/thin` package suite (123 Ginkgo specs + unit tables) and the `thinruntime` controller suite (fake-client reconcile flows); `go build ./...`, `go vet` | `go test ./pkg/ddc/thin/... -count=1 -gcflags="all=-N -l"`; `go test ./pkg/controllers/v1alpha1/thinruntime/... -count=1` |
| 3. Live | — | **Skipped by operator instruction** (debate pipeline runs unit+integration only). A probe showed a live cluster was reachable (2 nodes, v1.36.2); the skip is a policy decision, not a failed probe. |

> Test polarity: **all tests here are CONTRACT tests** — they assert the intended
> behavior, so they FAIL on the buggy code (that failure is the reproduction) and
> PASS when fixed. No bug-canaries.

## Problem premise (P0)

Run against the **base** branch `54a41cd4` (master, 2026-10-08) without the patch.

| | |
|---|---|
| Claimed symptom | "`updateFuseConfigOnChange` discards the error returned by `transformFuseConfig` … the only caller, `ShouldUpdateUFS`, reacts to a non-nil error … Because `nil` is returned, that `Log.Error` never fires and the transform failure is completely silent." (PR body) |
| Linked issue | none (no `fixes #N`; `#3432` is mentioned as the origin of the line — verified by `git blame 968a471c4`, "Feature: support dynamic mount for ossfs (#3432)", 2024-05-09) |
| Reported component | `pkg/ddc/thin/ufs.go` — `ThinEngine.updateFuseConfigOnChange` |
| Patched component | `pkg/ddc/thin/ufs.go:110` — exactly that function |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | Both contract tests grafted onto base master FAIL with the claimed symptom: `updateFuseConfigOnChange` returns `error <nil>` for a `pvc://missing-pvc` dataset, and `ShouldUpdateUFS` emits zero error-log entries — see `results/l2-base-reproduction.txt` |

The failure path is genuinely reachable on base: `transformFuseConfig` →
`extractVolumeInfo` → `kubeclient.GetPersistentVolumeClaim` errors for a missing PVC
(`pkg/ddc/thin/transform_config.go:61-64`, `pkg/utils/kubeclient/volume_claim.go:26`),
and also for a PVC that exists but is not yet `Bound` — a normal transient state.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | On master, a `transformFuseConfig` failure is swallowed (`return update, nil`): no error returned, no log emitted | 1 | **Confirmed** | `results/l2-base-reproduction.txt` — both contract tests red on base with exactly the claimed symptoms |
| F1 | The one-token fix (`return update, err`) makes `updateFuseConfigOnChange` return the error and `ShouldUpdateUFS` log it, with no other behavior change (`ufsToUpdate` still nil, `update` still false) | 1 | **Confirmed** | `results/l1-unit-head.txt` — PR's new test case and the harness log-capture test both green on the PR head |
| F2 | No regression in the wider engine/controller surface | 2 | **Confirmed** | `results/l2-thin-suite-head.txt`, `results/l2-thinruntime-controller-head.txt`, `results/l2-build-vet-gofmt-head.txt` — full thin suite (123/123 Ginkgo specs), thinruntime controller suite, `go build ./...`, `go vet` all clean |

## Per-finding detail

### P0 — premise (runs against base)

- Mechanism: base `pkg/ddc/thin/ufs.go:110` returns `update, nil` inside
  `if err != nil`, discarding the `transformFuseConfig` error. The two sibling
  error paths in the same function (`GetConfigmapByName` line ~101,
  `UpdateConfigMap` line ~118) do return the error, so this is an oversight
  introduced with the rest of the hunk in #3432 (blame: `968a471c4`).
- Command: graft `pkg/ddc/thin/ufs_test.go` (PR version) +
  `pkg/ddc/thin/ufs_verify_claude_test.go` onto `54a41cd4`, run the L1 command.
- Observed: both tests FAIL — `testcase transform fuse config failed failed due
  to error <nil>` and `expected an error log 'Failed to update fuse config'
  mentioning missing-pvc, got error entries: []`. Expected on a healthy base:
  PASS. The symptom is exactly the claimed one, so the premise is real.

### F1 — the fix works and only changes observability

- `TestThinEngine_updateFuseConfigOnChange` (PR's own table case "transform fuse
  config failed") asserts `wantUpdate:false, wantErr:true` — green on head.
- `TestVerifyClaude_ShouldUpdateUFSLogsTransformFailure` (harness, contract):
  fake client + recording `logr.LogSink`; asserts `ShouldUpdateUFS()` returns
  nil and that an error entry `Failed to update fuse config` naming
  `missing-pvc` is emitted — green on head, red on base.
- Net-behavior analysis (code-read, cross-checked by the tests): on the error
  path `update` is `false` both before and after, `updateFusePod()` is skipped
  either way, `ShouldUpdateUFS` returns nil either way, and
  `pkg/ddc/base/syncs.go:100` guards `if ufsToUpdate != nil` — so the only
  observable difference is the error log. The PR body's "scope, stated
  honestly" section is accurate.

### F2 — no collateral damage

- Full thin package suite on head: `ok` (all 123 Ginkgo specs pass; on Linux
  the five failures the author saw locally do not appear — they were Windows
  path artifacts as the author reported).
- thinruntime controller suite on head: `ok`.
- `go build ./...` exit 0; `go vet` on both packages exit 0.
- `gofmt -l pkg/ddc/thin/` flags only `transform_fuse.go`, which is pre-existing
  and untouched by this PR (empty diff vs merge-base).

## Harness-bites check

`results/l1-harness-bites-revert-fix.txt`: with **only** the one-token fix
reverted on the PR head (production `ufs.go` edited, then restored — tree clean
afterwards), both contract tests flip red with the base symptoms. The harness
exercises the right path; green is not vacuous.

## Live run notes

Not run. The debate pipeline operator instructed stage ② to run unit +
integration only. Probe result recorded for honesty: a live cluster **was**
reachable (`kubectl get no` → 2 Ready nodes, v1.36.2-aliyun.1); the live layer
was skipped by policy, not because the probe failed. Nothing was created or
mutated on that cluster.

## Proposed fixes (NOT applied to production here)

None — the PR's own fix is correct and sufficient. Optional polish (non-blocking):

- The new error now surfaces for the *transient* "PVC not bounded yet" state at
  Error level on every sync tick until the PVC binds; that is defensible
  observability, but if it turns out noisy, a `Log.V(1)` / warning for the
  not-yet-bound case is the knob. Not requested.
- The PR's test asserts error presence only (`(err != nil) != tt.wantErr`); it
  cannot distinguish *which* error. The harness's log-capture test closes that
  gap for the reviewer's purposes.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/thin-ufs-errprop-claude` in the reviewer's
fork (`https://github.com/cheyang/fluid.git`), based on PR head `7ac6f29c`.

```bash
git fetch https://github.com/cheyang/fluid.git verify/thin-ufs-errprop-claude
git checkout <fixed-ref>
git checkout FETCH_HEAD -- pkg/ddc/thin/ufs_verify_claude_test.go docs/verification/thin-ufs-errprop
bash docs/verification/thin-ufs-errprop/scripts/re-verify.sh   # no arg = fetch current PR head from manifest.pr
```

Prereqs: Go ≥ 1.25 toolchain; `jq` for `re-verify.sh`. The gomonkey-based tests
in the package need `-gcflags="all=-N -l"` (the L1/L2 commands include it).
`re-verify.sh` prints per finding **Fixed / Still-broken / Partial**; all tests
are contract polarity, so green = fixed.

### Kickoff prompt for a fresh agent

```text
You are re-verifying fluid PR #6187 (thin-engine error propagation). Check out
branch verify/thin-ufs-errprop-claude from https://github.com/cheyang/fluid.git,
read docs/verification/thin-ufs-errprop/README.md, then run
bash docs/verification/thin-ufs-errprop/scripts/re-verify.sh (it fetches the
current PR head via manifest.pr). All tests are CONTRACT polarity: green means
fixed; red means the error is being swallowed again. The delta since the last
round is in .last-reviewed — review last-reviewed..head incrementally and add
any new findings to the harness before re-running.
```
