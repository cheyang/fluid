# Verification — zap-bump (PR #6241) — Reviewer A (Claude)

PR: https://github.com/fluid-cloudnative/fluid/pull/6241
Head: `32aab5bc2fe517a1b58feee064ab22443ba86535` (round 2; round 1 was `35c388d5a797a2442e1b98996975a31008e8f295`)
Base: `5e6473599f78263a49e521854d80f918a1a10bb0` (round 2; round 1 was `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`)
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

## Round 2 (2026-11-18) — incremental re-review of the rebased head

The PR was rebased (force-push): head `35c388d5` → `32aab5bc`, merge-base
`ff99142f` → `5e647359` ( Dependabot picked up master's `sigs.k8s.io/yaml`
1.4.0→1.6.0 bump, #6244/#6177/#6252, and re-applied the zap commit).

**The PR's own content is unchanged by the rebase**: `git diff 35c388d..32aab5b
-- vendor/go.uber.org/zap` is empty, go.mod's zap line is identical, and the
PR diff (merge-base…head) is still exactly the 18 zap-bump files. No fluid
source is touched. (evidence: round-2 re-verify log)

Re-verified on the new head, all green (`results/round2-re-verify-head.txt`):
A1–A5 zapcompat PASS, vendor fidelity PASS (byte-identical to upstream v1.28.0,
checksum DB match), `go build ./...` PASS, `go mod tidy -diff` zero drift,
`go vet ./pkg/... ./cmd/... ./api/...` clean (`results/round2-tidy-vet-head.txt`).
Differential re-confirmed on the **new** base: A3/A4 red on `5e64735` (zap
1.27.0) → green on head (`results/round2-unit-newbase-zapcompat.txt`).
Blast radius re-checked at the new head: zero non-vendor references to
`zap.Object/WithLazy/NewLazyWith/RegisterSink/DictObject/BufferedWriteSyncer/CheckPreWriteHook`;
`go.uber.org/multierr` stays v1.11.0.

### F3 (new this round): CI staticcheck is red — baseline, not this PR

The round's only new signal: the GitHub `staticcheck` check on the head is
**FAILURE** (all 19 other checks green, incl. build/unittest/lint/kind-e2e):

```
pkg/utils/dataset/lifecycle/node_test.go:36:2: package "sigs.k8s.io/controller-runtime/pkg/client" is being imported more than once (ST1019)
	pkg/utils/dataset/lifecycle/node_test.go:37:2: other import of "sigs.k8s.io/controller-runtime/pkg/client"
```

Differential attribution (`results/round2-staticcheck-differential.txt`,
`scripts/check-staticcheck-baseline.sh`; staticcheck 2025.1.1/0.6.1 on go1.26.10
— the CI's exact versions):

| ref | ST1019 present |
| --- | --- |
| head `32aab5bc` | **yes** (exit 1) |
| new base `5e647359` | **yes** (exit 1, byte-identical error) |
| master tip `dcb76167b` | no (exit 0) |

Provenance: the duplicate import entered master via `6f55f846d` (#6244) and
reached this PR through the rebase; the PR's file list never touches
`node_test.go`. GitHub's own runs corroborate: `staticcheck` on commit
`5e647359` = FAILURE, on master tip = SUCCESS (fixed there by `1321a217d`
/#6257, which deleted the duplicate import). **Verdict: pre-existing /
baseline, not introduced by this PR.** Remedy (already available upstream):
rebase this PR onto current master (≥ `1321a217d`) — verified clean on the
master tip; the zap diff contributes no staticcheck findings of its own.
Branch protection requires no specific status checks, so the red X does not
mechanically block merge, but the project's check suite should be green.

### F1 re-check (unchanged, still environment-only)

The same 5 packages fail locally on the new head as on the old head/base
(`results/round2-f1-failing-pkgs-head.txt`: TestAlluxio, TestCacheEngine,
TestDestroyWorker ×2, TestCheckMountPointBroken — the `ddc-helm`-binary and
fake-client/mount fixture issues). GitHub's `unittest` check is SUCCESS on
this head. Still not a PR finding; never gating.

## How to run

```bash
# from a checkout of this branch
go test -count=1 -v ./pkg/utils/zapcompat/
bash docs/verification/zap-bump/scripts/verify-vendor-fidelity.sh
go build ./...
# or everything at once against the current PR head:
bash docs/verification/zap-bump/scripts/re-verify.sh
# F3 staticcheck differential (head base [fix-ref]); capture stdout/stderr separately:
bash docs/verification/zap-bump/scripts/check-staticcheck-baseline.sh <head> <base> [master-tip]
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
