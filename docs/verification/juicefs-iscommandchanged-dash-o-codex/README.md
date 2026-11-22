# juicefs-iscommandchanged-dash-o — bug verification (reviewer: codex)

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6283
("fix(juicefs): parse mount options after the standalone -o argument", fixes issue #6282).

Code under review: head `cf975a08cacccb43ad32bd44bca721df93bfec78`, base `253f6bb84f26a906fba301550b5c81ebdd73af35`.
Production code is untouched on this branch; the harness is one additive Go test file.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `JuiceFSEngine.isCommandChanged` parsing + real `genFuseMount`/`genWorkerMount` generators (CE + EE, fuse + worker) | `go test -gcflags=all=-l ./pkg/ddc/juicefs/ -run 'TestVerifyPR6283' -v -count=1` |

> Polarity: every test is a CONTRACT test (asserts intended behavior). Red on base = the bug;
> green on head = fixed. No canaries.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom (issue #6282, quoted) | "a changed option such as `cache-size` is never seen: the mount script ConfigMap is not updated and the worker StatefulSet template does not get its restart annotation, so the new options are not picked up" |
| Claimed mechanism | "splitting the command string on the bare substring `-o` ... but `-o` also appears inside the mount path, which contains the dataset name ... and inside option names such as `cache-partial-only`" |
| Linked issue | #6282, OPEN, filed 2026-11-20 (2 days before review), still valid |
| Reported component | `pkg/ddc/juicefs` `JuiceFSEngine.isCommandChanged` (callers `syncWorkerSpec`, `updateFuseCmdConfigmapOnChanged`) |
| Patched component | `pkg/ddc/juicefs/sync_runtime.go` `isCommandChanged` |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | Contract tests red on base, green on head: results/unit-base.log vs results/unit-head.log |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | dash-o in dataset name or option name hides option changes | 1 | Confirmed on base; fixed on head | `changed=false` for cache-size 1024→2048 on base, `true` on head; results/unit-base.log, results/unit-head.log |
| AC1 | dataset name containing `-o`: option change detected (fuse+worker, CE+EE) | 1 | Pass on head; fails on base (premise) | `TestVerifyPR6283_DatasetDashO_ChangeDetected`, `TestVerifyPR6283_GeneratorRoundTrip` |
| AC2 | option name containing `-o` (`cache-partial-only`): change detected | 1 | Pass on head; fails on base | `TestVerifyPR6283_OptionNameDashO_ChangeDetected` |
| AC3 | no over-reporting: same options different order; metrics-only diff ignored | 1 | Pass on base and head | `TestVerifyPR6283_DatasetDashO_NoChangeDifferentOrder`, `TestVerifyPR6283_MetricsOptionIgnored`, 20× map-order round trip |
| AC4 | option removal detected with dash-o dataset name | 1 | Pass on head; fails on base | `TestVerifyPR6283_DatasetDashO_OptionRemoved` |
| AC5 | no package regression | 1 | Full `pkg/ddc/juicefs` suite green on head (12.2s), baseline green on base | results/package-head.log, results/package-base-noop.log |

Extra signal: for the enterprise edition the generated option list itself embeds the dataset
name (`cache-group=default-demo-output`), so on base the parse broke twice over; the
generator round-trip covers that path too.

## Harness-bites check

The premise run IS the bites check: the same contract tests are red on base (253f6bb) and
green on head (cf975a0) with zero changes other than the PR itself. The guard tests (AC3)
stay green on both, proving the tests exercise the path rather than failing for unrelated
reasons.

## Proposed fixes (NOT applied here)

None required — the PR's `strings.Cut(command, " -o ")` is correct for every command the
generators can emit (everything before the separator — binary path, source, mount path — is
space-free: k8s namespace/name cannot contain spaces, and options always follow the
separator). Optional follow-up: make the PR's hardcoded-string tests generator round-trips
like `TestVerifyPR6283_GeneratorRoundTrip` so command-format drift cannot silently rebreak
comparison.

## Continuing after the fix (possibly on another machine)

Harness branch: `verify/juicefs-iscommandchanged-dash-o-codex` on the reviewer's fork
(`https://github.com/cheyang/fluid.git`). Production code untouched.

1. Graft: `git fetch https://github.com/cheyang/fluid.git verify/juicefs-iscommandchanged-dash-o-codex && git checkout <ref-to-test> && git checkout FETCH_HEAD -- docs/verification/juicefs-iscommandchanged-dash-o-codex pkg/ddc/juicefs/verify_pr6283_iscommandchanged_codex_test.go`
2. Prereqs: Go toolchain only (`go 1.26+` per go.mod; built here with go1.27).
3. Re-run: `bash docs/verification/juicefs-iscommandchanged-dash-o-codex/scripts/re-verify.sh` (auto-discovers the PR head from `manifest.pr`) or the raw `go test` command above.
4. All tests are contract polarity: green = correct. If a future fix renames the function or
   changes the command format, adjust the additive test file (Harness-update), never the
   production code from this branch.

### Kickoff prompt for a fresh agent
```text
Continue verification for fluid-cloudnative/fluid PR #6283 on branch
verify/juicefs-iscommandchanged-dash-o-codex (remote https://github.com/cheyang/fluid.git).
Read docs/verification/juicefs-iscommandchanged-dash-o-codex/README.md, then run
bash docs/verification/juicefs-iscommandchanged-dash-o-codex/scripts/re-verify.sh from a
checkout of that branch. All tests are contract polarity (green = fixed). Report an
observed-vs-expected table per AC id in verify-manifest.json.
```
