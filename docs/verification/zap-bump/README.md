# Verification — zap-bump (PR #6241) — Reviewer A (Claude)

PR: https://github.com/fluid-cloudnative/fluid/pull/6241
Head: `35c388d5a797a2442e1b98996975a31008e8f295`
Base: `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`
Branch: `verify/zap-bump-claude` (this branch; production code untouched — additive harness only)

PR type: Dependabot bump `go.uber.org/zap` 1.27.0 → 1.28.0 (go.mod, go.sum,
vendor/modules.txt, vendor/go.uber.org/zap/** — 18 files, no fluid source changes).

## Premise (P0): Confirmed

This is a dependency-maintenance PR, so the premise to prove is *the upgrade is
genuine, complete, and non-regressive*, not that a user-facing bug existed. All
three sub-claims hold:

1. **Genuine**: every file under `vendor/go.uber.org/zap` is byte-identical to
   the upstream `go.uber.org/zap@v1.28.0` module (downloaded from
   proxy.golang.org and verified against the checksum DB), and the PR's `go.sum`
   h1 entry matches the official hash `h1:IZzaP1Fv73/T/pBMLk4VutPl36uNC+OSUh3JLG3FIjo=`.
   See `results/vendor-fidelity-head.txt`.
2. **Complete**: no non-test `.go` file is missing from any vendored zap package;
   `go mod tidy` on the PR head produces **zero** drift in go.mod/go.sum; `go build
   ./...` exits 0 (which also enforces vendor/modules.txt ↔ go.mod consistency).
3. **Non-regressive**: the vendored tree demonstrably carries ≥ 1.27.1 semantics —
   tests A3 and A4 **fail on the base** (zap 1.27.0) and **pass on the head**
   (see `results/unit-base.txt` vs `results/unit-head.txt`), and the regression
   guards A1/A2/A5 pass on both.

## Observed vs expected

| # | Test / check (file) | Polarity / layer | Base (1.27.0) | Head (1.28.0) | Verdict |
| --- | --- | --- | --- | --- | --- |
| A1 | `TestHarnessFluidLoggerPathWorks` (pkg/utils/zapcompat/zapcompat_harness_test.go) — mirrors the exact ctrlzap.New option chain all 11 `cmd/*` apps use, dev+prod branches | contract / unit | PASS | PASS | fluid's logger path unregressed |
| A2 | `TestHarnessLevelStringsStillAccepted` — every level spelling v1.27.0 accepted (debug/DEBUG/info/INFO/""/warn/WARN/error/ERROR/dpanic/DPANIC/panic/PANIC/fatal/FATAL) | contract / unit | PASS | PASS | acceptance set is a strict superset; no regression |
| A3 | `TestHarnessWarningLevelNewlyAccepted` — "warning"/"WARNING" → warn | contract / unit (discriminating) | **FAIL** | PASS | vendored tree moved to ≥ 1.27.1 |
| A4 | `TestHarnessNilObjectNoPanic` — `zap.Object(k, nil)` marshals as explicit null instead of panicking | contract / unit (discriminating) | **FAIL** (panic: interface conversion) | PASS | 1.27.1 nil-panic fix present |
| A5 | `TestHarnessVendoredZapVersionConsistent` — go.mod ↔ vendor/modules.txt agree | contract / unit | PASS | PASS | vendor bump internally consistent |
| S1 | `scripts/verify-vendor-fidelity.sh` — vendor tree vs upstream module + go.sum vs checksum DB | contract / integration | n/a (version would differ) | PASS | supply-chain integrity |

All tests are **contract** polarity: they assert intended behavior. A3/A4 double as
the harness-bites proof — they are red on the un-bumped base and green on the head,
so the harness demonstrably exercises the changed zap code rather than passing vacuously.

## Attribution of local test failures (not PR findings)

Running the full unit suite (`go list ./... | grep -v controller | grep -v e2etest |
xargs go test`, 115 packages) on the head shows 6 failing packages / 5 test functions:
`TestAlluxio` (pkg/ddc/alluxio), `TestCacheEngine` (pkg/ddc/cache/engine),
`TestDestroyWorker` (pkg/ddc/jindo and pkg/ddc/jindofsx), `TestCheckMountPointBroken`
(pkg/utils), `TestHelm` (pkg/utils/helm — needs the `helm` binary).

The **identical** set fails on the **merge-base** commit
(`results/unit-base-failing-pkgs.log` vs `results/unit-head.log`), so these are
pre-existing / local-environment failures, **not** caused by this PR. GitHub CI
reports success for this PR. They are recorded here so a later round does not
mistake them for regressions.

## Blast radius (why no live layer)

- fluid's own zap call sites (all 11 `cmd/*` apps): `zapOpt.AddCaller()`,
  `NewProductionEncoderConfig()`, `NewDevelopmentEncoderConfig()`,
  `zapcore.NewConsoleEncoder`, `zapcore.CapitalLevelEncoder`, `zapcore.ISO8601TimeEncoder`
  — none changed upstream.
- The only other vendored-graph consumer of zap is
  `sigs.k8s.io/controller-runtime/pkg/log/zap`, whose level flag uses its own
  lowercase `levelStrings` map (not `zapcore.Level.UnmarshalText`); fluid does not
  bind those flags.
- Nothing in fluid or its vendored graph calls `zap.Object`, `WithLazy`/`LazyWith`,
  `BufferedWriteSyncer`, `RegisterSink`, `DictObject`, or `CheckPreWriteHook`.
- `zapcore.Level.UnmarshalText`'s acceptance set strictly grew (uppercase spellings
  now handled by the ToLower fallback; "warning" added) — verified superset by A2/A3.

## How to run

```bash
# from a checkout of this branch
go test -count=1 -v ./pkg/utils/zapcompat/
bash docs/verification/zap-bump/scripts/verify-vendor-fidelity.sh
go build ./...
# or everything at once against the current PR head:
bash docs/verification/zap-bump/scripts/re-verify.sh
```

## Continuing after the fix / next round

- Re-run: `bash docs/verification/zap-bump/scripts/re-verify.sh` (no ref: fetches
  the current PR head via the manifest `pr` URL), or pass an explicit ref.
- Expected on the current head: A1–A5 + S1 all green, build green.
- If a future revision regresses the bump (e.g. partial vendor update), A5 or S1
  goes red directly. If zap is bumped again past 1.28.0, S1 re-checks fidelity
  against the new declared version automatically (it reads go.mod, not a pin).
- No canaries; no polarity inversion needed after any fix.

## Handoff prompt (fresh agent, another machine)

```
Checkout branch verify/zap-bump-claude from the reviewer fork
(https://github.com/cheyang/fluid.git). Read
docs/verification/zap-bump/verify-manifest.json and this README, then run
`bash docs/verification/zap-bump/scripts/re-verify.sh` — it grafts the harness
onto the current PR #6241 head and reports per-check pass/fail. Record outcomes
in results/ and advance docs/verification/zap-bump/.last-reviewed to the reviewed
head sha, then commit and push to the same branch.
```
