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

## Round 2 (incremental, 2026-11-18) — head `32aab5bc2fe5`, base `5e6473599f78`

Dependabot force-pushed a rebase (`35c388d` -> `32aab5b`); the zap patch hunks
are byte-identical (only git blob index lines differ). The base moved
`ff99142` -> `5e64735`, pulling in #6240 (sigs.k8s.io/yaml 1.6.0), #6244
(cache-node capacity labels), #6252 (docs).

Re-verification (`scripts/re-verify.sh <sha>`, now with shell-layer support):

| claim | new base `5e64735` | new head `32aab5b` | verdict |
| --- | --- | --- | --- |
| C1 build/verify/vet | pass | pass (`all modules verified`, vet clean on all `./cmd/...`) | pass |
| C2 level parsing | FAIL only on `"warning"` alias (expected delta) | pass | pass, differential preserved |
| C3 logger shape | pass | pass | pass |
| C4 nil-Object | FAIL (panic, interface conversion) | pass | fix confirmed present on new base->head |
| C5 lazyWith race | pass (short run) | pass under `-race` | pass |

Raw: `results/round2-head-32aab5b/`, `results/round2-base-5e64735/`.
`go mod tidy` no-op at head; vendored zap tree byte-identical to upstream
v1.28.0 (diff vs module cache, `results/round2-head-32aab5b/vendor-fidelity.txt`);
zap go.sum h1 unchanged from round 1 (`IZzaP1Fv...`); vendor tree identical to
previous head (`git ls-tree` diff empty).

### CI failure this round is base-caused, not PR-caused (F3)

`staticcheck` fails on the head with ST1019 in
`pkg/utils/dataset/lifecycle/node_test.go` (duplicate import of
`sigs.k8s.io/controller-runtime/pkg/client`, unaliased + `ctrlclient`).
Attribution, all evidence in `results/round2-head-32aab5b/ci-staticcheck-attribution.txt`:

- Introduced by base commit `6f55f846d` (#6244) — the file had 1 import line at
  `ff99142`/`35c388d`, 2 at `5e64735`/`32aab5b`.
- CI `staticcheck` fails on the base commit `5e64735` itself (master run
  44003825813) and on the PR head (job 133775273253); identical message.
- Reproduced locally at head with staticcheck v0.7.0-dev
  (`staticcheck-lifecycle.txt`; note: released 2025.1.1 cannot decode Go 1.27
  export data, use a master build).
- Fixed on master by `1321a217d` (#6257, removes the unaliased import);
  master `dcb76167b` staticcheck green (CI + local
  `staticcheck-lifecycle-master-dcb76167b.txt` empty).
- Unblock path: rebase the PR onto master >= `1321a217d` (`@dependabot rebase`).

### F1 spot re-check (round 2)

Targeted unit run on the previously failing areas
(`pkg/utils/helm`, `pkg/ddc/alluxio`, `pkg/utils/mount/...`, `pkg/ddc/cache/engine`):
timing-normalized failing set identical on `5e64735` and `32aab5b`
(`unit-targeted-base.txt` vs `unit-targeted-head.txt`) — still
environment-only (ddc-helm binary, fake-client/mount fixtures), CI unittest green.
