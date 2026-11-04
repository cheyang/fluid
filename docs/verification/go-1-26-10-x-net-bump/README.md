# Verification — PR #6227: bump Go 1.25.12 → 1.26.10 and golang.org/x/net v0.58.0 → v0.62.0

Reviewer: Codex (stage ② harness). Branch: `verify/go-1-26-10-x-net-bump-codex`.
Reviewed head: `6e97fc15205a29df5de03dcd6358d6f2441ceb39`, base: `7f27b7e2a9cc6343e8905abd37a236e4c735dc24`.
Production code untouched — this branch adds only `docs/verification/**`.

## Premise (P0): CONFIRMED

The PR claims go1.25.12 + x/net v0.58.0 carry reachable vulnerabilities that only
Go 1.26.9/1.26.10 + x/net ≥ v0.60.0 fix. Verified independently with
govulncheck v1.9.0 (vuln DB 2026-11-05), symbol-level (`-mode=source`):

| ref | toolchain | reachable findings |
| --- | --- | --- |
| base `7f27b7e2` | go1.25.12 + x/net v0.58.0 | **22**: 18 affecting stdlib packages, 5 `golang.org/x/net@v0.58.0` (all fixed in v0.60.0), 4 `k8s.io/kubernetes@v1.29.15` |
| head `6e97fc15` | go1.26.10 + x/net v0.62.0 | **4**: only the out-of-scope `k8s.io/kubernetes` ones (GO-2025-3521/3547/3915/4240) |

Both PR-highlighted advisories confirmed real and correctly versioned via
vuln.go.dev: GO-2026-6617 (net/http + x/net HPACK encoder race, stdlib fixed
1.26.9, x/net fixed v0.60.0 — reachable here through the CSI gRPC server and
kubelet client, see traces in results/) and GO-2026-6809 (crypto/x509 distrust,
fixed only in 1.26.10; reachable via pkg/utils/webhook/generator's
`x509.Certificate.Verify`). x/net v0.62.0's own go.mod declares `go 1.26.0`
(proxy.golang.org), so the `go` directive bump is forced, as the PR states.

## Observed vs expected

| check | polarity | expected | observed | evidence |
| --- | --- | --- | --- | --- |
| P0 premise: base has reachable stdlib/x-net vulns | canary (runs on base) | reproduces | reproduced (22 reachable, incl. GO-2026-6617, GO-2026-6809) | results/p0-govulncheck-base-summary.txt |
| C1 Dockerfile pins = live index digest of golang:1.26.10-bookworm | contract | match | all 12 Dockerfiles match `sha256:23ce71…87eb` | results/c1-image-digest.txt |
| C2 build + vet + `go mod verify` under go1.26.10 | contract | clean | clean (vendored) | results/c2-build-vet.txt |
| C3 CI-equivalent unit suite (`-race -gcflags="all=-N -l"`) | contract | pass | 70 packages ok, 0 FAIL, 0 races (linux/amd64) | results/c3-unit-tests.txt |
| C4 `make update-crd` / `make gen-openapi` diff-free | contract | no diff | no diff (controller-gen v0.19.0 + openapi-gen work under go1.26.10) | results/c4-codegen-diff.txt |
| C5 govulncheck head: 0 reachable stdlib/x-net | contract | clean | clean; 4 k8s.io/kubernetes remain (documented out of scope) | results/c5-govulncheck-head-summary.txt |
| C6 urlstrictcolons delta + url.Parse blast radius | contract | reject multi-colon host; call sites confined | both hold | results/c6-urlstrictcolons.txt |

## Harness-bites evidence (the checks can fail)

- C1 on the **base** tree exits 1: base pins `golang:1.25.12-bookworm@sha256:a9c0…6aa4`
  while that tag's live index digest is now `sha256:6359…f82` (tag republished
  after a Debian base update). The script detects pin drift. Caveat for future
  rounds: docker-library rebuilds patch tags, so C1 can start failing through
  *tag drift*, not a PR defect; if it fails, check whether the pin matched at
  PR time via the PR's CI docker builds before blaming the revision.
- C3 without `-gcflags="all=-N -l"` fails on **both** base and head
  (gomonkey patches silently don't apply; helm tests hit
  `exec: "ddc-helm": executable file not found`). The Makefile's
  `LOCAL_FLAGS ?= -gcflags="all=-N -l"` is mandatory — the failure mode is
  identical on both refs, so it is harness configuration, not PR attribution.
- C5/P0 polarity: base scan is positive, head scan is negative — the scan
  discriminates.
- C6(a) flips with the `go` directive: same program accepts
  `http://localhost:80:80/` under `go 1.25.0` and rejects it under `go 1.26.0`.

## How to re-run

From a checkout of this branch (unit layer = fast checks; integration layer =
long unit suite + codegen):

```bash
bash scripts/re-verify.sh            # resolves current PR head via manifest.pr
# or individually, from the repo root of the ref under test:
bash docs/verification/go-1-26-10-x-net-bump/scripts/check-image-digest.sh
bash docs/verification/go-1-26-10-x-net-bump/scripts/check-build-vet.sh
bash docs/verification/go-1-26-10-x-net-bump/scripts/check-govulncheck.sh head
bash docs/verification/go-1-26-10-x-net-bump/scripts/check-urlstrictcolons.sh
bash docs/verification/go-1-26-10-x-net-bump/scripts/check-unit-tests.sh     # ~15 min
bash docs/verification/go-1-26-10-x-net-bump/scripts/check-codegen-diff.sh
# premise (runs against BASE): check-govulncheck.sh base   # canary: exit 0 while vulns reproduce
```

Prereqs: network (proxy.golang.org, vuln.go.dev, Docker Hub registry API),
`govulncheck` in PATH (`go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`).
Toolchains auto-resolve via `GOTOOLCHAIN` from the checked-out go.mod.

All findings are `contract` polarity: green = behavior holds. There are no
bug-canaries to invert this round (no defects found). P0's base-side canary
stays valid only against the base ref.

## Continuing after a fix / next round

This round found no defects, so the harness is a regression suite: any future
revision of this PR (version/digest changes) must keep C1–C6 green. Resume with:
`bash scripts/re-verify.sh` on this branch; advance `.last-reviewed` after review.
Copy-paste kickoff for a fresh machine: "check out
verify/go-1-26-10-x-net-bump-codex from https://github.com/cheyang/fluid.git,
read docs/verification/go-1-26-10-x-net-bump/README.md, run scripts/re-verify.sh".

## Environment used

linux/amd64, host go1.27.0 with GOTOOLCHAIN auto-download (go1.25.12, go1.26.10),
govulncheck v1.9.0, vuln DB 2026-11-05, no KUBECONFIG (live layer skipped).
