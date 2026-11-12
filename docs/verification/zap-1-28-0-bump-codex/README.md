# Verification: PR #6241 — chore(deps): bump go.uber.org/zap 1.27.0 → 1.28.0

Reviewer: Codex (Reviewer B). Mode: initial. Head: `35c388d5a797a2442e1b98996975a31008e8f295`. Base: `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`.
Harness is additive-only; production code untouched (`git diff` vs head shows only this directory + `pkg/utils/zapcompatverify/`).

## Premise (P0) — CONFIRMED (runs against BASE)

Dependabot bump of `go.uber.org/zap` 1.27.0 → 1.28.0. The release carries real
bug fixes (nil-`Object` panic #1501, `WithLazy` race #1511) and one additive
feature (`CheckPreWriteHook` #1534). Grafting the harness onto the base commit
reproduces the pre-fix behavior (`zap.Object(nil)` panics; `"warning"` level
alias unrecognized) — see `results/base-unit.txt`. The same tests pass on the
PR head — see `results/head-unit-race.json`. Fluid only consumes zap through
`cmd/*/app` logging setup and `sigs.k8s.io/controller-runtime/pkg/log/zap`
(plus `zapcore` encoders), so the blast radius is the process logging path.

## Claims / polarity (all `contract`: pass = behavior correct)

| id | claim | layer | test |
| --- | --- | --- | --- |
| C1 | repo builds with vendored zap 1.28, module hashes verify | integration (build) | `go build -mod=vendor ./...`, `go mod verify`, `go vet` on zap-consuming cmds |
| C2 | level text parsing stays backward compatible (`DEBUG/INFO/WARN/...` uppercase, empty=info) and new `"warning"` alias works | unit | `TestLevelUnmarshalCompatibility` |
| C3 | fluid's production logger shape (NewProductionEncoderConfig + CapitalLevelEncoder + ISO8601TimeEncoder + console encoder + AddCaller) still works | unit | `TestFluidProductionLoggerShape` |
| C4 | `zap.Object(key, nil)` no longer panics (1.28 fix present) | unit | `TestObjectNilMarshalerDoesNotPanic` |
| C5 | `NewLazyWith` safe under concurrent use (`-race`) | unit | `TestLazyWithConcurrentUse` |

## Observed vs expected

| claim | base (1.27.0) | head (1.28.0) | verdict |
| --- | --- | --- | --- |
| C1 | n/a (control) | build OK, `all modules verified`, vet clean | pass |
| C2 | FAIL only on `"warning"` alias (new in 1.28) | pass | pass, delta as expected |
| C3 | pass (control) | pass | pass |
| C4 | FAIL — panics: `interface conversion: interface is nil, not zapcore.ObjectMarshaler` | pass | fix confirmed present |
| C5 | no race triggered in this run (not proof of absence) | pass under `-race` | pass |

Note: the 1.28 `level.go` diff *removes* the explicit uppercase cases from the
internal switch, but `UnmarshalText` still lowercases before retrying, so
uppercase parsing is preserved — C2 pins this. `BufferedWriteSyncer.Stop`
(deadlock fix #1428) and `CheckedEntry.Before` are additive/fix-only and not
exercised by fluid code.

## How to re-run

```bash
go test -race -count=1 ./pkg/utils/zapcompatverify/
go build -mod=vendor ./... && go mod verify && go vet -mod=vendor ./cmd/fluidapp/... ./cmd/dataset/... ./cmd/csi/...
# or, after a rebase/new head:
bash docs/verification/zap-1-28-0-bump-codex/scripts/re-verify.sh
```

## Continuing after updates

Kickoff prompt for a fresh machine: "check out branch
`verify/zap-1-28-0-bump-codex` from https://github.com/cheyang/fluid.git, then
run `bash docs/verification/zap-1-28-0-bump-codex/scripts/re-verify.sh` (it
auto-fetches the current PR #6241 head). All findings are contract polarity:
green = good. If a future zap bump changes level parsing or logger setup,
update the tests additively and note it as Harness-update."
