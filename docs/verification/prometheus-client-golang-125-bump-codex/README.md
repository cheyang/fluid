# Verification harness — PR #6230 (bump prometheus/client_golang 1.20.5 → 1.25.0)

- PR: https://github.com/fluid-cloudnative/fluid/pull/6230
- Reviewed head: `af817d9ad264de238c2ecabb43a220e77f9c1b95`
- Base (merge-base): `30d488bb7ecd601fb8f338387d9b1fec1c4a2a2a`
- Reviewer: Reviewer B (Codex), round 1 (initial). Production code untouched.

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

## Handoff / continuing after the fix

The F1 contract test is direction-agnostic: it passes whether the fix reverts to
a Go-1.25-compatible client_golang or raises every pinned toolchain to ≥ 1.26.
After a fix lands, run `re-verify.sh` from this branch — it fetches the current
PR head, grafts this harness, and reports Fixed / Still-broken. No live/cluster
layer exists or is needed (failure precedes any binary execution).
