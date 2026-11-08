# Verification — PR #6233 — thin: reduce log noise from not-yet-bound PVC in ShouldUpdateUFS

Reviewer: **Claude (Reviewer A)** · Round: initial · PR head `952e37ed1` · base `a01c038bc`

## Premise (P0) — CONFIRMED

Issue #6203 claims: `ShouldUpdateUFS` emits `persistent volume claim <name> not
bounded yet` at **Error** level roughly once per sync period until a `pvc://`
PVC binds, after #6187 made `updateFuseConfigOnChange` propagate the error.

Reproduced on the **base** branch (no patch) at the unit layer:
`TestHarnessP0_PendingPVCNoErrorLog` fails on base with exactly the claimed log:

```
P0: expected NO Error-level log for a not-yet-bound PVC, got 1:
[Failed to update fuse config]
(errors: [failed to extract volume info from PersistentVolumeClaim "my-pvc": persistent volume claim my-pvc not bounded yet])
```

Cadence mechanism checked in code: `base.Sync()` → `UpdateUFS()` →
`ShouldUpdateUFS()`, gated by `permitSync` with `defaultSyncRetryDuration =
5s` (`pkg/ddc/base/template_engine.go:35`). Component match: the issue names
exactly the files the PR touches. → `results/base-harness.txt`

## Harness

Additive only (production code untouched). Two test files in `pkg/ddc/thin/`:

| file | compiles on base? | purpose |
| --- | --- | --- |
| `verify_lognoise_harness_test.go` | yes | P0 differential: log-level contracts via a logr capture sink |
| `verify_sentinel_harness_test.go` | no (needs `ErrPVCNotBound`) | sentinel survives the full wrap chain |

All polarity **contract** (assert intended behavior; red = bug).

## Observed vs expected

| ID | claim | base (a01c038) | head (952e37e) |
| --- | --- | --- | --- |
| P0 | pending pvc:// mount → no Error-level log from ShouldUpdateUFS | **FAIL** (Error log emitted — the bug) | **PASS** |
| V1 | demoted `PVC is not bounded yet...` message logged at V(≥1) Info | FAIL (nothing) | PASS |
| V2 | unexpected error (nonexistent PVC) still logs at Error | PASS | PASS |
| V3 | `errors.Is(err, ErrPVCNotBound)` holds at `updateFuseConfigOnChange` (through `transformFuseConfig`'s pkg/errors `Wrapf`) | n/a | PASS |
| V4 | `extractVolumeInfo` returns the sentinel wrapped, message keeps the PVC name | n/a | PASS |

Harness-bites check: the P0/V1 contracts are red on base and green on head —
they exercise exactly the behavior the PR changes. V2 passes on both, proving
the demotion does not over-suppress genuine errors.

Layer coverage: unit only (L1). The claims are in-process log-level behavior;
no envtest/cluster needed. Live layer (L3) skipped per this round's policy.

## Findings verdicts

No blockers, no majors. Non-blocking notes:

- **F1 [nit]** duplicated message text: the sentinel reads "persistent volume
  claim not bounded yet" and the wrapper repeats it, so the rendered error is
  `persistent volume claim my-pvc not bounded yet: persistent volume claim not
  bounded yet`. Cosmetic; also surfaces on the setup path
  (`transform_fuse.go:125` → reconcile error).
- **F2 [question]** a `pvc://` mount whose PVC does not exist *at all* still
  logs at Error with the same ~5s cadence. In-scope per issue #6203 ("PVC
  exists but is not yet Bound"), but the same alert-noise failure mode
  applies; worth a maintainer call on whether `apierrors.IsNotFound` should be
  demoted too.
- **F3 [nit]** `pkg/ddc/thin/transform_fuse.go:250` fails `gofmt -l` —
  pre-existing on base, file untouched by this PR.

## Continuing after the fix / next round

From a checkout of this branch (`verify/thin-pvc-notbound-claude`):

```bash
bash docs/verification/thin-pvc-notbound-claude/scripts/re-verify.sh
```

No ref argument needed — the manifest's `pr` URL resolves the current PR head.
It grafts the harness onto that head, runs the unit layer, and prints per
finding **Fixed / Still-broken / Partial / Harness-update**. All findings are
contract tests, so all-green = fixed. If a future commit renames
`ErrPVCNotBound` or reshapes the wrap chain, expect `Harness-update` on
V3/V4 — adjust the tests, re-run.

The P0 premise check (base branch run) is manual by design:
`git worktree add /tmp/base a01c038 && cp pkg/ddc/thin/verify_lognoise_harness_test.go /tmp/base/pkg/ddc/thin/ && cd /tmp/base && go test ./pkg/ddc/thin/ -run TestHarnessP0 -v`
— must be RED (that red is the reproduction).

## Kickoff prompt (fresh agent)

> You are re-verifying PR fluid-cloudnative/fluid#6233. Check out branch
> `verify/thin-pvc-notbound-claude` (from the reviewer fork
> https://github.com/cheyang/fluid.git), read
> `docs/verification/thin-pvc-notbound-claude/README.md` and
> `verify-manifest.json`, run `scripts/re-verify.sh`, report the per-finding
> verdict table honoring contract polarity, then review the
> `.last-reviewed..head` delta and update this README and `.last-reviewed`.
