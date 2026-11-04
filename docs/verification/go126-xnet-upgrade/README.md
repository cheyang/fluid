# go126-xnet-upgrade — dependency/toolchain bump verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6227 (Go 1.25.12 → 1.26.10,
golang.org/x/net v0.58.0 → v0.62.0, gomonkey v2.13.0 → v2.14.4, Dockerfiles, CI).

Layers run against the **code under review** (head `6e97fc15205a29df5de03dcd6358d6f2441ceb39`, base `7f27b7e2a9cc6343e8905abd37a236e4c735dc24`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit / differential | plain `go test` vs canonical make-test flags; govulncheck base vs head; module & image-digest consistency | `scripts/10-f1-plain-gotest.sh <head-wt> <base-wt>`, `scripts/20-premise-vulncheck.sh <base-wt> <head-wt>`, `scripts/30-acceptance.sh <head-wt>` |
| 2. Integration (envtest) | not needed — no controller code changed; CI's kind-e2e + backward-compat (6 k8s versions) are the integration evidence, all green on the head sha | — |
| 3. Live (real cluster) | skipped per round policy (no KUBECONFIG); CI kind-e2e covers image build + runtime on the new toolchain | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when fixed.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "Upgrades the Go toolchain from 1.25.12 to 1.26.10 and golang.org/x/net from v0.58.0 to v0.62.0 to fix reachable vulnerabilities." Notably GO-2026-6617 (HTTP/2 HPACK encoder race, "reachable from the webhook and CSI gRPC servers") and GO-2026-6809 (crypto/x509, "Fixed only in 1.26.10"). |
| Linked issue | none ("fixes: NONE"); supersedes stale Dependabot PR #5275 |
| Reported component | stdlib + golang.org/x/net as used by the webhook, CSI gRPC server, and the Go build/Docker/CI toolchain |
| Patched component | go.mod (`go 1.26.0` + `toolchain go1.26.10`, x/net v0.62.0 + MVS bumps), vendored deps, 12 Dockerfiles, CI configs |
| Component match | Yes — vulns live in the toolchain/dep versions this PR bumps |
| **Verdict** | **Confirmed** |
| Evidence | govulncheck v1.9.0 on base: 22 reachable vulns incl. GO-2026-6617 (x/net v0.58.0) and GO-2026-6809 (crypto/x509@go1.25.12); on head: only the 4 k8s.io/kubernetes vulns remain (out of scope, unchanged). `results/vulncheck-base.log`, `results/vulncheck-head.log` |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | reachable vulns exist on base and are gone on head | 1 | **Confirmed** | base 22 reachable (13 stdlib + 5 x/net + 4 k8s) → head 0 stdlib/x/net; `results/vulncheck-*.log` |
| A1 | `go build ./...` with toolchain go1.26.10 | 1 | **Passed** | exit 0, `results/acceptance-misc.log` |
| A2 | go.mod/go.sum/vendor consistency | 1 | **Passed** | `go mod verify` ok; `go mod tidy` + `go mod vendor` no diff |
| A3 | Docker digest pin is a valid multi-arch index matching `golang:1.26.10-bookworm` | 1 | **Passed** | registry query; `results/docker-digest-check.txt` |
| A4 | full unit suite, canonical make-test flags (`-gcflags="all=-N -l"`) | 1 | **Passed** | 70 pkgs ok, 0 FAIL; `results/unit-tests-head-makestyle.log` |
| F1 | plain `go test ./pkg/utils/helm/` panics on head, passes on base | 1 | **Confirmed (minor)** | `scripts/10-f1-plain-gotest.sh`: head FAIL / base PASS / head + `-N -l` PASS; `results/f1-*.log` |

## Per-finding detail

### F1 [minor, introduced by this PR] `pkg/utils/helm/utils.go:46`
Go 1.26.10's compiler inlines `exec.LookPath` into `InstallRelease` under default
build flags (the inlined frame is visible in the panic trace: `os/exec.LookPath(...)`
attributed to `utils.go:46`). The gomonkey `ApplyFunc(exec.LookPath, …)` patches in
`utils_test.go` then no longer intercept the call; the **real** `LookPath` runs and
calls the still-patched `os.Stat` (returns `nil, nil`), so `findExecutable`
nil-derefs at `os/exec/lp_unix.go:27` (`m := d.Mode()`). 7/29 ginkgo specs panic.
Observed: `FAIL` with 7 `Summarizing 7 Failures`. Expected: `ok`.
The project's canonical path (`make test` / CI, which always appends
`-gcflags="all=-N -l"` from `Makefile:15`) is unaffected — that is why CI's
`unittest` job is green on this exact sha. Three other packages
(`pkg/ddc/alluxio`, `pkg/ddc/cache/engine`, `pkg/utils`) show the same
inlining fragility with default flags **already on the base branch** (pre-existing,
not introduced here); helm is the one package where base passes and head fails.

### gomonkey bump (commit 2) — observation, not a finding
On linux/amd64 with canonical flags, base code (gomonkey v2.13.0) + go1.26.10
passes `pkg/utils/helm`, `pkg/utils/cmdguard`, `pkg/csi/plugins`, so the
v2.13.0 → v2.14.4 bump is not observably required on this platform/OS. The PR's
justification is darwin/arm64 (not testable here) and gomonkey#191. Test-only
dependency, CI green — acceptable either way.

## Proposed fixes (NOT applied to production here)
- **F1** (optional, cosmetic): none required for merge. If desired, make the helm
  tests robust to inlining by patching at a level that survives it (e.g. patch
  `exec.Command`/`lookPath` indirectly via a package-level `lookPath` var), or
  document that unit tests must run via `make test` (the `-N -l` requirement
  already applies to 3 other packages on master).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/go126-xnet-upgrade-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/go126-xnet-upgrade-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/go126-xnet-upgrade
   ```
2. Prereqs: Go ≥ 1.27 (or set `GOTOOLCHAIN=go1.26.10`; the pinned toolchain is
   auto-downloaded), network for module downloads, govulncheck
   (`go install golang.org/x/vuln/cmd/govulncheck@latest`), curl + python3 for
   the Docker Hub digest check.
3. Re-run: `bash docs/verification/go126-xnet-upgrade/scripts/re-verify.sh`
   (or the individual `scripts/10-…`, `20-…`, `30-…` with worktree paths).
4. Read results: F1 is fixed only when `[head@go1.26.10] PASS` **without**
   `-N -l`; A1–A4 should stay PASS.
5. Harness-bites: F1's differential is red on the PR head today
   (`results/f1-head.log`), and the `-N -l` control proves the harness
   actually exercises the mechanism.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/go126-xnet-upgrade-claude
(remote https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6227 (Go 1.26.10 + x/net
v0.62.0 bump) produced finding F1 (plain `go test ./pkg/utils/helm/` panics
under go1.26.10 due to exec.LookPath inlining vs a gomonkey patch) and
acceptance items A1–A4, all recorded in
docs/verification/go126-xnet-upgrade/README.md. Graft the harness onto the
current PR head, run scripts/re-verify.sh, report per-item
Fixed / Still-broken / Harness-update with the observed-vs-expected table,
and advance .last-reviewed. Do not modify production code.
```
