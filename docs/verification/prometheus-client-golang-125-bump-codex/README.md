# Verification harness — PR #6230 (bump prometheus/client_golang 1.20.5 → 1.25.0)

- PR: https://github.com/fluid-cloudnative/fluid/pull/6230
- Reviewed head: `af817d9ad264de238c2ecabb43a220e77f9c1b95`
- Base (merge-base): `30d488bb7ecd601fb8f338387d9b1fec1c4a2a2a`
- Reviewer: Reviewer B (Codex). Round 1 (initial, head af817d9); Round 2 (incremental, head a70ab0a). Production code untouched.

## Premise (P0) — REFUTED as submitted

Dependabot PR; no linked issue; claimed change is a routine dependency bump of
`github.com/prometheus/client_golang` to v1.25.0. The implicit premise — "fluid
can consume v1.25.0 as a drop-in maintenance update" — is **refuted**: v1.25.0
raises its minimum Go version to 1.26 ("[CHANGE] Minimum required Go version is
now 1.26", client_golang v1.25.0 release notes), while fluid pins Go 1.25.12 in
every build entrypoint (3 GitHub workflows, `.circleci/config.yml`, 11
`docker/Dockerfile.*`). CI and image builds fail before compiling a single file.
Evidence: `results/l1-head.txt`, `results/ci-failures.txt`.

The bump itself is otherwise sound: with a toolchain that meets the requirement
(host go1.27), the full tree builds, vets, and metrics-adjacent + testify-heavy
tests pass (`results/l2-head-build-and-tests.txt`, `results/l2-testify-smoke.txt`).
The two clean fix directions:

1. **Pin back** to `client_golang v1.24.x` (v1.24 supports Go 1.25; includes the
   v1.24.1 promhttp nil-URL panic fix) until fluid adopts Go 1.26; add a
   dependabot `ignore` rule so the bot stops re-proposing v1.25.x; or
2. **Move the whole project to Go 1.26** in one deliberate PR: `go.mod`, all 3
   workflow `GO_VERSION` pins, `.circleci/config.yml` (`cimg/go`), all 11
   Dockerfiles (`golang:` base images), and re-check golangci-lint /
   staticcheck-action (`2025.1.1`) compatibility with Go 1.26.

## Findings

| ID | Severity | Claim | Layer | Evidence |
|----|----------|-------|-------|----------|
| F1 | blocker | PR head cannot be built by any of the project's pinned Go 1.25.12 toolchains (`go.mod requires go >= 1.26.0; GOTOOLCHAIN=local`) — all CI + image builds fail | L1 script + contract test | `results/l1-head.txt` vs `results/l1-base.txt`; CI run 41981310741 |
| F2 | (verified non-issue) | Vendored bump compiles/vets/tests clean under Go ≥ 1.26 — no API-break from client_golang v1.24/v1.25 changes reaches fluid code (fluid uses only `prometheus.NewGaugeVec/NewCounterVec` with ASCII names; never `api/prometheus/v1`) | L2 script | `results/l2-head-build-and-tests.txt` |

Environment note (pre-existing, NOT PR-caused): `pkg/utils` `TestCheckMountPointBroken`
fails in this sandbox on **both** base and head under **both** go1.25.12 and
go1.27 (gomonkey patching artifact of this environment). Excluded from findings.

## How to re-run

```bash
# L1: reproduce the toolchain gate (needs network once, to fetch go1.25.12 toolchain)
bash docs/verification/prometheus-client-golang-125-bump-codex/scripts/l1-toolchain-gate.sh <ref> fail   # PR head
bash docs/verification/prometheus-client-golang-125-bump-codex/scripts/l1-toolchain-gate.sh <base> pass  # control

# L2: soundness of the vendored bump under a >=1.26 host toolchain
bash docs/verification/prometheus-client-golang-125-bump-codex/scripts/l2-build-under-go126plus.sh <ref>

# Contract test alone (host toolchain, any ref):
go test ./docs/verification/prometheus-client-golang-125-bump-codex/ -run TestToolchainConsistency -count=1

# Full loop against a future fixed head (auto-resolves PR head; grafts harness):
bash docs/verification/prometheus-client-golang-125-bump-codex/scripts/re-verify.sh
```

## Observed vs expected

| Check | Expected on PR head | Observed |
|---|---|---|
| go1.25.12 + GOTOOLCHAIN=local `go list ./pkg/metrics/` (head) | fail with go-version gate | `go.mod requires go >= 1.26.0 (running go 1.25.12; GOTOOLCHAIN=local)` ✔ reproduced |
| same on base 30d488b | pass | pass ✔ (full `go build ./...` on base also passes under go1.25.12) |
| `TestToolchainConsistencyWithPinnedToolchains` on head | FAIL (red = reproduction) | FAIL, 15 pin sites flagged ✔ |
| same on base | PASS | PASS ✔ |
| same on simulated fix (all pins → 1.26) | PASS | PASS ✔ |
| `go build ./...` on head with go1.27 | pass if bump is sound | pass ✔ |
| `go vet` + metrics/common tests on head with go1.27 | pass | pass ✔ |

## Round 2 (incremental) — head a70ab0a, new base 91a58c1d — ALL FIXED

The remediation recommended in round 1 was executed: the Go 1.26 migration
PR #6227 (Go 1.26.10 + x/net v0.62.0) merged into master, and dependabot
rebased this PR onto it (new merge-base 91a58c1d includes #6227). The PR is
now a single clean commit: go.mod/go.sum/vendor only (client_golang 1.20.5 ->
1.25.0; companions testify 1.12.1, prometheus/common 0.72.0, procfs 0.22.0,
client_model 0.6.3, protobuf 1.36.12, oauth2 0.37.0, go.yaml.in/yaml/v3 3.0.5;
klauspost/compress and pmezard/go-difflib pruned out).

Outcomes (re-verify.sh + manual layers):

| Finding | Round 1 | Round 2 | Evidence |
|---|---|---|---|
| F1 toolchain gate (blocker) | FAIL on head | **FIXED** — all pins (3 workflows, cimg/go, 11 Dockerfiles, travis) now 1.26.10; contract test PASS; 21/21 CI checks green incl. ci/circleci: build | results/reverify/, results/ci-rollup-r2.txt, results/contract-tests-head-r2.txt |
| F2 builder images | 11/11 FAIL | **FIXED** — all docker/Dockerfile.* pin golang:1.26.10-bookworm; covered by the same contract test | results/contract-tests-head-r2.txt |
| F3 toolchain directive + stale circleci comment | absent/stale | **FIXED** — go.mod carries `toolchain go1.26.10`; .circleci comment rewritten; new guard TestGoModToolchainDirectivePresent (bites on prev head, passes on new) | results/harness-bites-prevhead-r2.txt vs results/contract-tests-head-r2.txt |
| F4 bump soundness (refuted risk) | pass under go1.27 | **RE-PASSED under the exact pinned go1.26.10**: build ./... OK, vet OK, go mod verify clean, `go mod tidy` and `go mod vendor` are no-ops (tidy/vendor-clean) | results/l2-head-r2-go12610.txt |
| Unit-suite regression | none (differential) | none: env-failing 4 packages fail identically at base 91a58c1d and head a70ab0a; authoritative: CI `unittest` SUCCESS on head | results/unit-attribution-r2.txt |
| M1 pkg/metrics has no tests | open (pre-existing) | still open — unchanged, tracking-issue candidate | `go test ./pkg/metrics/...` = [no test files] |
| N1 dependabot ignore rule | conditional | **moot** — project moved to Go 1.26; the bump is buildable, no ignore rule needed | .github/dependabot.yml unchanged, now harmless |
| Q1 remediation path | open | **resolved** — option A executed (#6227 merged, PR rebased) | git history ee16a8532 |

New round-2 observation (nit, non-blocking): within the 1.20.5 -> 1.25.0 range
(precisely: already in v1.24.x), promhttp's zstd negotiation became opt-in via
the side-effect package prometheus/promhttp/zstd; fluid does not import it, so
/metrics now negotiates gzip/identity instead of zstd. Identity is always
offered, no scraper can break; bandwidth/CPU nuance only. See
results/zstd-optin-note.txt.

## Handoff / continuing after the fix

The F1 contract test is direction-agnostic: it passes whether the fix reverts to
a Go-1.25-compatible client_golang or raises every pinned toolchain to ≥ 1.26.
After a fix lands, run `re-verify.sh` from this branch — it fetches the current
PR head, grafts this harness, and reports Fixed / Still-broken. No live/cluster
layer exists or is needed (failure precedes any binary execution).
