# Verification harness — PR #6230 (bump prometheus/client_golang 1.20.5 → 1.25.0)

Reviewer A (Claude), round 2 (incremental), 2026-11-07.
Head under review: `a70ab0a5074cd15b05bcfe222927a8972b82d32d`
Base: `91a58c1d9d473c6063be7817f398a048233de0f2` (moved from `30d488bb` — the PR was rebased onto PR #6227, the Go 1.25.12 → 1.26.10 + x/net v0.62.0 migration, i.e. round 1's Option A remediation)

Round 1 (2026-11-06) reviewed head `af817d9ad` on base `30d488bb`; its raw results are archived as `results/*-r1.txt`. Production code is untouched by this harness (diff = this directory only).

## ROUND 2 — all findings fixed by the rebase onto #6227

| id | claim | polarity | layer | new base | prev head (r1) | head (r2) | verdict |
| --- | --- | --- | --- | --- | --- | --- | --- |
| P0 | premise: bump is sound w/ suitable toolchain | contract | unit | n/a | PASS | PASS | **Confirmed** (re-confirmed on the final remediation head, incl. build under the exact CI-pinned `GOTOOLCHAIN=go1.26.10`) |
| F1 | CI pins Go ≥ go.mod minimum | contract | unit | PASS | FAIL | **PASS** | **Fixed** |
| F2 | all 11 digest-pinned builder images satisfy the module minimum | contract | unit | PASS | FAIL | **PASS** (11/11 golang:1.26.10) | **Fixed** |
| S1 | dep bump itself builds/vets/links | contract | unit | PASS | PASS | **PASS** | **Pass** |

- F1 evidence: `results/ci-toolchain-contract.txt` — `GO_VERSION: 1.26.10`, go.mod minimum `1.26.0`, and `go build ./pkg/... ./cmd/...` PASS under `GOTOOLCHAIN=go1.26.10` (the exact CI toolchain; this also retires the round-1 caveat that verification ran only under go1.27). New-base control: `go build` PASS at `91a58c1` (`results/base-build-r2.txt`).
- F2 evidence: `results/builder-image-go.txt` — 11/11 `FROM golang:1.26.10-bookworm@sha256:23ce713…` satisfies `go 1.26.0`; `.circleci` → `cimg/go:1.26` with an updated comment narrating `toolchain go1.26.10`; `.travis.yml` → 1.26.10. (Round 1's F3 — the toolchain directive disappearing and the stale CircleCI comment — is resolved by the same rebase: `toolchain go1.26.10` is back in go.mod.)
- CI on head a70ab0a: **all 16 previously-failing checks pass** (build ×2, unittest, lint, staticcheck, kind-e2e ×5, backward-compat ×5, ci/circleci: build); only `tide` is pending for missing approved/lgtm labels (`results/ci-status-r2.txt`).
- Unit suite at head: 66 ok / 4 FAIL (`results/unit-suite-head-r2.txt`); the same 4 packages (pkg/ddc/alluxio, pkg/ddc/cache/engine, pkg/utils, pkg/utils/helm) fail identically at the new base `91a58c1` (`results/unit-suite-base-r2-failures.txt`) and CI's unittest job is green on this head — environmental, unchanged from round 1 (E1).

## Premise (P0): Confirmed

The PR is a routine dependabot bump of `github.com/prometheus/client_golang`
(plus the transitive refresh: testify 1.12.1, prometheus/common 0.72.0,
procfs 0.22.0, protobuf 1.36.12, golang.org/x/* bumps, klauspost/compress
dropped from the module graph). The only fluid code that imports client_golang
is `pkg/metrics` (GaugeVec/CounterVec registered into controller-runtime's
registry). The bump is *sound*: with a Go ≥ 1.26 toolchain the vendored tree
builds, vets clean, and the full unit suite is no worse than base (4 failing
packages fail identically at base). Fluid uses none of the API surface broken
in v1.25.0 (`api/prometheus/v1`). No security driver, no linked issue; the
concrete benefit to fluid is modest (promhttp nil-URL panic fix #2065 and the
Gather() collector-panic recovery #1961 both apply to controller-runtime's
/metrics path).

## Observed vs expected

| id | claim | polarity | layer | base | head | verdict |
| --- | --- | --- | --- | --- | --- | --- |
| P0 | premise: bump is sound w/ suitable toolchain | contract | unit | n/a | PASS | **Confirmed** |
| F1 | CI pins Go 1.25.12 (GOTOOLCHAIN=local) but go.mod requires go 1.26.0 → all 16 required checks fail | contract | unit | PASS | **FAIL** | **Confirmed (blocker)** |
| F2 | all 11 digest-pinned builder images (golang:1.25.12-bookworm) are below the module minimum → non-hermetic image builds | contract | unit | PASS | **FAIL** | **Confirmed (major)** |
| S1 | isolation control: dep bump itself builds/vets/links with go ≥ 1.26 | contract | unit | n/a | PASS | **Pass** |

## How to run

```bash
bash scripts/re-verify.sh            # fetches current PR head, runs all layers
bash scripts/re-verify.sh <ref>      # graft harness onto a specific ref
```

Layers (all repo-local; no cluster, no docker, no KUBECONFIG):

- `scripts/check_ci_toolchain_contract.sh` — F1. Reads `GO_VERSION` from
  `.github/workflows/*`, the `go` directive from go.mod, and builds with
  `GOTOOLCHAIN=go<CI pin>` exactly as CI does. First run downloads the pinned
  toolchain (network).
- `scripts/check_builder_image_go.sh` — F2. Compares go.mod's minimum against
  every `FROM golang:<ver>` in `docker/Dockerfile*`.
- `scripts/check_dep_bump_soundness.sh` — S1. `go build ./...` + `go vet` +
  no-`client_golang/api`-import check; needs a local Go ≥ the module minimum.
- Integration layer (full unit suite): documented in
  `results/unit-suite-head.txt` / `results/unit-suite-base-attribution.txt`.

## Harness bites (verified this round)

Same commands, two states:

- F1: base `30d488bb` → PASS (`results/ci-toolchain-contract-base.txt`,
  builds with the pinned 1.25.12); head `af817d9ad` → FAIL
  (`results/ci-toolchain-contract.txt`, exact CI error line). Red for the right
  reason: the failure text matches the GitHub Actions logs byte-for-byte
  (`results/ci-failure-evidence.txt`).
- F2: base → 11/11 ok; head → 11/11 FAIL.

The "apply the fix and watch it flip" bite-check is realized by the base
worktree (a state where the Go pin is consistent); no production edit is
needed to prove polarity.

## Root cause + why every CI job is red

`client_golang` v1.25.0's go.mod requires `go 1.26.0`, so dependabot rewrote
fluid's `go` directive from 1.25.0 → 1.26.0 (and dropped
`toolchain go1.25.12`). All three GitHub workflows pin
`GO_VERSION: 1.25.12` and `actions/setup-go` v7 runs with `GOTOOLCHAIN=local`,
so every job that invokes `go` (build ×2, unittest, lint, staticcheck, and
`make generate` in the 5 kind-e2e + 5 backward-compat jobs) fails *before
compiling anything* with:

```
go: go.mod requires go >= 1.26.0 (running go 1.25.12; GOTOOLCHAIN=local)
```

16 of 18 checks are red for this one reason (`results/ci-failure-evidence.txt`).

## Proposed fixes — RESOLVED in round 2

**Option A was taken**: PR #6227 (Go 1.25.12 → 1.26.10 + golang.org/x/net v0.62.0) landed in
master and this PR was rebased onto it (base `30d488bb` → `91a58c1`). Option B (cap at
`client_golang` v1.24.1) is no longer needed. Round-2 residual note: go.sum retains a full
hash entry for `klauspost/compress v1.20.1` (client_golang v1.25.0 still lists it as a
requirement) while the vendored copy and the go.mod indirect entry are gone — correct
`go mod tidy`/`go mod vendor` behavior since no package fluid builds imports it; vendored
`go build ./...` proves consistency.

## Continuing after the fix

- Re-run: `bash scripts/re-verify.sh` (no args) — it fetches the current PR
  head, grafts this harness, and prints F1/F2/F2/S1 as Fixed / Still-broken /
  Partial / Harness-update. Exit 0 iff F1 and F2 are resolved.
- If the author takes Option B, `go.mod`'s minimum drops to ≤ 1.25.x and both
  F1 and F2 flip green with no harness change.
- If the author takes Option A, `GO_VERSION`/Dockerfiles move to 1.26.x and
  both flip green; if the workflows change shape (e.g. GO_VERSION renamed),
  treat a red/green-less F1 as `Harness-update` and adjust the pin-extraction
  regex.
- All tests are contract-polarity (green = correct), so after a fix everything
  must simply be green; there are no bug-canaries to invert.
- Unit-suite attribution: if `pkg/ddc/alluxio`, `pkg/ddc/cache/engine`,
  `pkg/utils`, `pkg/utils/helm` still fail, compare against
  `results/unit-suite-base-attribution.txt` — those failures exist on base and
  are not this PR's.

## Kickoff prompt for a fresh agent

> You are resuming a review verification. Checkout branch
> `verify/prometheus-client-golang-bump-claude` of the reviewer fork, read
> `docs/verification/prometheus-client-golang-bump/README.md` and
> `verify-manifest.json`, then run `bash docs/verification/prometheus-client-golang-bump/scripts/re-verify.sh`
> (needs git, go ≥ 1.26, network for the pinned-toolchain download). Report
> per-finding verdicts and the observed-vs-expected table; do not modify
> production code.
