# gomonkey/v2 2.13.0 → 2.14.4 bump (PR #6228) — verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6228
(Dependabot bump of the test-only monkey-patching library gomonkey).

Layers run against the PR head `a6aa6afdd6c1c4062b9f12458aa82d4ceb033ed7` (Go 1.25.12, linux/amd64,
the same toolchain CI pins via `GO_VERSION`/`toolchain`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | vendored gomonkey behavior used by fluid (`harness/` pkg: ApplyFunc/ApplyMethod/ApplyFuncReturn/ApplyPrivateMethod/Origin/Reset, trampoline pool) | `go test ./docs/verification/gomonkey-2-14-4-bump/harness/ -gcflags="all=-N -l" -count=1 -v` |
| 2. Integration | fluid's own CI unit sweep (`make test` package set) against the bumped vendor tree | `CGO_ENABLED=0 go list ./... \| grep -v controller \| grep -v e2etest \| FLUID_UNIT_TEST=true xargs go test -race -gcflags="all=-N -l" -count=1` |
| 3. Live | not applicable (test-only dependency; no runtime component) | — |

> Polarity: contract tests assert intended behavior (PASS on good code). The two canaries pin
> **new 2.14.4 behavior** — they PASS on the PR head and FAIL on the 2.13.0 base. A future bump
> that removes the 256-slot cap or changes the entry encoding flips them red; re-check then.

## Problem premise (P0)

| | |
|---|---|
| Claim | Dependabot chore: pick up gomonkey 2.14.x fixes (darwin SIGBUS/icache, arm64 R10 clobber, amd64 unwind crash `invalid runtime symbol table` under CPU profiling — upstream issues #184/#187/#188/#189/#190/#191, fixed by PRs #188/#189/#192). |
| Linked issue | none in fluid (no `fixes #N`); upstream issues are referenced by the release notes. |
| Reported component | test-only dependency `github.com/agiledragon/gomonkey/v2` (88 `*_test.go` consumers in fluid; zero non-test imports). |
| Patched component | `go.mod`, `go.sum`, `vendor/modules.txt`, vendored gomonkey tree — same component. Match: yes. |
| **Verdict** | **Confirmed** (real, applicable update): the vendored tree is byte-identical to the sumdb-verified upstream `v2.14.4` module (proxy `Origin`: tag v2.14.4 = commit `331dbeb`, matching the release notes); the base's vendored 2.13.0 carries the 12-byte `MOVABS RDX; JMP [RDX]` entry patch that upstream issue #191 blames for the amd64 unwind crash (mechanism canary, see results). The 2.13.0-era crash itself was not independently re-reproduced here (upstream's own repro needed darwin/amd64 under Rosetta) — not required for a chore bump whose risk is regression, not efficacy. |
| Evidence | results/unit-base-go1.25.12.txt vs results/unit-head-go1.25.12.txt (mechanism canary flips); supply-chain diff (see below). |

Supply-chain integrity (run at review time):
- `go mod download -json github.com/agiledragon/gomonkey/v2@v2.14.4` → `Sum: h1:H+ptmSPvL39/+VWY2Fhms5jPNkrMJEzWxZFvAPocf8g=`, `GoModSum: h1:ap1AmDzcVOAz1YpeJ3TCzIgstoaWLA6jbbgxfB4w2iY=` — both byte-identical to the PR's `go.sum` lines.
- `diff -rq` of the proxy module vs `vendor/github.com/agiledragon/gomonkey/v2`: zero content differences; the only upstream-only entries are the ones `go mod vendor` always drops (`*_test.go`, `go.mod`, `go.sum`, `.github/`, unimported `dsl/` and `test/` packages).
- `go mod tidy` on the PR head is a no-op (go.mod/go.sum unchanged).
- Cross-compiles clean: `GOOS/GOARCH = darwin/arm64, darwin/amd64, linux/arm64, windows/amd64`.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|-------|
| P0 | bump is authentic and applicable | supply-chain + 1 | Confirmed | see above |
| B1 | 2.14.4 caps concurrent amd64 function patches at 256 per process; #257 panics `no reachable amd64 patch trampoline available` (2.13.0 had no cap) | 1 | Confirmed (new intended upstream behavior; no fluid test near the cap) | results/unit-head-go1.25.12.txt vs unit-base-go1.25.12.txt (canary flips) |
| B2 | entry patch is now a 5-byte `JMP rel32` into a NOSPLIT/NOFRAME trampoline (the #191/#192 unwind fix), was 12-byte `MOVABS;JMP` | 1 | Confirmed | canary flips; head entry bytes `e9 fb 09 ff ff`, base `48 ba b0 73 76 …` |
| B3 | API surface fluid uses round-trips correctly on 2.14.4 (incl. Origin re-apply, Reset releasing pool slots over 1024 cycles, re-patch reuse, short-func neighbor intact) | 1 | Confirmed | contract tests PASS on head (and base) |
| B4 | full CI-equivalent unit sweep passes on the head with `-race` | 2 | Confirmed | results/integration-head-go1.25.12-race.txt (71 pkgs ok, EXIT=0); also CI `unittest` SUCCESS |
| B5 | CI-skipped controller packages with gomonkey tests also pass locally (envtest k8s 1.31.0) | 2 | Confirmed, except pre-existing `dataprocess` breakage | results/integration-head-controller-pkgs.txt; dataprocess fails **identically on base** (ginkgo v1-style `BeforeSuite(..., 60)` at suite_test.go:51, broken since #5193 — NOT caused by this PR) |
| B6 | informational: harness also passes on Go 1.27.0 (post-CI toolchain) | 1 | Confirmed | results/unit-head-go1.27.0-informal.txt |

## Per-finding detail

- **B1 (the only behavior regression risk).** gomonkey 2.14.4 on amd64 routes every `ApplyCore`
  patch through a fixed pool of 256 trampoline slots (`core_trampoline_amd64.go`); slot #257
  panics. Repro: `TestConcurrentPatchCap` applies 300 patches to 300 distinct functions and
  observes the panic on head, success on base. Fluid impact today: none — no test uses
  `t.Parallel()` with gomonkey, per-test patch counts are small (max observed ~48 Apply calls in
  one *file*, spread across tests with `defer Reset()`), and the full suite passes. The cap is
  per-process with slots released by `Reset()` (`TestTrampolineSlotsReleasedOnReset`: 1024
  apply/reset cycles, no panic). Future test authors bulk-patching >256 functions in one live
  scope would hit a confusing panic — informational only.
- **B2.** `TestEntryFootprintIsFiveByteBranch` reads the patched entry via unsafe: `0xE9` on
  head vs `0x48` on base. This is the mechanism that removes the pcsp-mismatch crash window.
- **B5 (pre-existing, not a PR finding).** `pkg/controllers/v1alpha1/dataprocess` fails before
  any test runs (`node was passed an unknown decorator: '60'`), identically on base and head;
  invisible to CI because `make test` filters `controller` paths. Worth a separate cleanup PR.

## Proposed fixes

None required for merge. Optional: fix `dataprocess` `BeforeSuite(..., 60)` → `NodeTimeout(...)`
in a standalone PR (pre-existing).

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/gomonkey-2-14-4-bump-codex` (production code untouched;
`git diff` vs PR head shows only `docs/verification/**`).

```bash
git fetch https://github.com/cheyang/fluid.git verify/gomonkey-2-14-4-bump-codex
git checkout verify/gomonkey-2-14-4-bump-codex
bash docs/verification/gomonkey-2-14-4-bump/scripts/re-verify.sh   # auto-resolves current PR head
```

Prereqs: Go (harness pins `GOTOOLCHAIN=go1.25.12` implicitly via go.mod toolchain line; any
recent go auto-fetches it). Layer 2 needs nothing beyond the repo; the controller-package
addendum needs `KUBEBUILDER_ASSETS=<envtest k8s 1.31.0 dir>`. Polarity: the two canaries
(`TestConcurrentPatchCap`, `TestEntryFootprintIsFiveByteBranch`) are GREEN while 2.14.4 behavior
persists and FLIP red if a future bump changes the cap/encoding — treat a flip as "re-review",
not as failure.

### Kickoff prompt for a fresh agent
```text
Continue verification for fluid PR #6228 (gomonkey/v2 2.13.0→2.14.4 bump). Harness branch:
verify/gomonkey-2-14-4-bump-codex on https://github.com/cheyang/fluid.git. Read
docs/verification/gomonkey-2-14-4-bump/README.md, then run
docs/verification/gomonkey-2-14-4-bump/scripts/re-verify.sh (no args; it fetches the current PR
head). Contract tests must stay green; the two canaries pin current 2.14.4 behavior — if either
flips, re-review the new upstream release notes before trusting the suite.
```
