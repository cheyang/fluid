# go12611-toolchain-bump — verification harness

Reproducible evidence for the review of [PR #6255](https://github.com/fluid-cloudnative/fluid/pull/6255)
(Go toolchain 1.26.10 → 1.26.11), reviewed at head `e2d5e04e7da4bc46bce314a44d991f3f62d2e0c0`,
base `f432bdc87ac35941763b74e8e87683c7a84de5d9`.

This PR is a patch-level toolchain bump with **no Fluid source changes** (only
go.mod `toolchain`, 12 Dockerfiles, 3 GitHub workflows, CircleCI comment, Travis).
So the harness asserts the *contracts the bump must satisfy* rather than bug
repros. All checks are **contract polarity**: they pass on the PR head and fail
on the base.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (shell) | registry digest, govulncheck, version-pin consistency, build+vet+unit suite | `bash scripts/re-verify.sh` (runs everything against the current PR head) |
| 2. Integration | n/a — no source change; CI's kind-e2e ×6 / backward-compat ×6 on the PR head are the e2e evidence (all SUCCESS at `e2d5e04`) | — |
| 3. Live | not needed for a toolchain bump | — |

Prereqs: `git`, `go` (any ≥ 1.26.11, or older + `GOTOOLCHAIN=auto` — the
scripts pin `GOTOOLCHAIN` to go.mod's `toolchain`), `govulncheck`
(`go install golang.org/x/vuln/cmd/govulncheck@latest`, or set `GOVULNCHECK`),
`curl`+`python3` for the registry queries.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is
whether the problem exists today.

| | |
|---|---|
| Claimed symptom | govulncheck v1.9.0 (DB 2026-11-11) reports **GO-2026-6859 / CVE-2026-97054** (HTTP/1 server crash due to `MaxBytesReader` header race in `net/http`) as reachable at symbol level with go1.26.10; go1.26.11 has the fix |
| Linked issue | none ("NONE"); context PR #6227 (the previous 1.25.12→1.26.10 bump, already merged) |
| Reported component | Go stdlib `net/http` compiled into Fluid binaries (e.g. `utils.NewPprofServer` → `http.Server.ListenAndServe`) |
| Patched component | go.mod toolchain, Dockerfiles, CI configs — i.e. exactly the layer that decides which stdlib ships in Fluid's binaries |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | base + `GOTOOLCHAIN=go1.26.10`: GO-2026-6859 reported reachable (`Found in: net/http@go1.26.10`, `Fixed in: net/http@go1.26.11`, traces via `http.CanonicalHeaderKey`, `http.Client.Do`, `pprof.init`); head + go1.26.11: stdlib finding gone, only the 4 pre-existing module-level `k8s.io/kubernetes` advisories remain — exactly the PR's before/after table. `results/p0-govulncheck-base.log`, `results/p0-govulncheck-head.log` |

Exposure caveat (matches the PR body's own note): no Fluid or vendored code calls
`http.MaxBytesReader`/`MaxBytesHandler` (checked by grep), so the crash trigger
is absent in practice; the value of the bump is clearing the scanner finding and
shipping release images on a patched toolchain.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| C4-digest | `sha256:dc40a13b…` is the multi-arch index digest of `golang:1.26.11-bookworm` (docker.io + mirror.gcr.io), covering linux/amd64, arm64/v8, arm/v7, 386, ppc64le; all 12 Dockerfiles pin it consistently | 1 | Confirmed (pass on head, fail pattern checked) | `results/digest-head.log` |
| C1-vuln | GO-2026-6859 / stdlib advisories cleared at symbol level with the pinned toolchain | 1 | Confirmed (pass on head; bites: fails on base, see `results/vulncheck-base-bites.log`) | `results/vuln-head.log` |
| C5-consistency | bump is complete: no stale `1.26.10` refs, go.mod/3 workflows/Travis updated, old digest fully replaced | 1 | Confirmed (pass on head; bites: fails on base) | `results/consistency-head.log` |
| C2-build | `go build ./...` + `go vet` with go1.26.11 | 1 | Confirmed | `results/build-unit.log` (driver run) |
| C2-unit | Makefile-equivalent unit suite with go1.26.11: **70 ok / 0 FAIL** | 1 | Confirmed | `results/unit-tests-head.log` |

## Refuted during verification (kept for the record)

- "Unit tests fail at head with go1.26.11 (`TestDestroyWorker` in
  `pkg/ddc/jindo` + `pkg/ddc/jindofsx`, `jindoruntimes.data.fluid.io "spark" not
  found`)" — **refuted: harness artifact, not a product regression.** The first
  full-suite invocation prefixed `FLUID_UNIT_TEST=true` on `go list` (first
  command of the pipeline) instead of on `xargs` (as the Makefile does), so the
  test binaries ran with the env unset, `testutil.IsUnitTest()` flipped to
  false, and `getRuntimeInfo()` took the live-cluster path → "not found".
  Correct invocation (env on xargs): 70 ok / 0 FAIL at head. Also passes in
  isolation at base with both 1.26.10 and 1.26.11, and the PR's `unittest` CI
  job (ubuntu-latest, `-race`) is green. Do not re-trip on this;
  `run-unit-tests.sh` encodes the correct invocation and documents the trap.

## Observed-vs-expected table

| Check | Expected | Observed |
|-------|----------|----------|
| P0 base go1.26.10 govulncheck | GO-2026-6859 reachable | reachable, traces via pprof/net-http call sites ✔ |
| P0 head go1.26.11 govulncheck | stdlib 0, 4 k8s.io/kubernetes module advisories | identical ✔ |
| digest | dc40a13b… = index digest, 5 linux platforms, 12/12 Dockerfiles | ✔ (docker.io + mirror.gcr.io) |
| consistency | no 1.26.10 leftovers; 3 workflows + travis + go.mod | ✔ |
| build/vet | clean | ✔ |
| unit suite | 70 ok / 0 FAIL | 70 ok / 0 FAIL ✔ |

## Findings carried into the review ledger

- **R1** (nit, pre-existing, not introduced by this PR):
  `docker/Dockerfile.webhook:1` header comment says "Build the dataset-controller
  manager binary" but the file builds the webhook binary. Copy-paste leftover;
  this PR only touched the image lines.
- **R2** (minor, pre-existing): CircleCI uses the floating `cimg/go:1.26` tag
  (not digest-pinned like the Dockerfiles); builds there rely on the go toolchain
  auto-download to reach 1.26.11. Documented in the config comment; accepted
  repo pattern, surfaced for awareness only.

No blocking or major findings. No production code changed by the PR, and the
harness adds only files under `docs/verification/`.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/go12611-toolchain-bump-claude`
(fork `cheyang/fluid`), production code untouched.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/go12611-toolchain-bump-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/go12611-toolchain-bump
   ```
2. Prereqs: git, go, govulncheck (or `GOVULNCHECK=` path), curl, python3.
3. Run `bash docs/verification/go12611-toolchain-bump/scripts/re-verify.sh`
   (no argument — it fetches the current PR head from the manifest's `pr` URL).
4. All checks are contract polarity: all green = the bump's contracts hold.
   A red C1-vuln means the toolchain regressed to a vulnerable Go; a red
   C4-digest means the pinned image digest no longer matches the tag (re-push /
   tag drift — update the pin deliberately if so); a red C2-unit means the
   toolchain change broke tests (check the FLUID_UNIT_TEST trap above first).
5. Harness-bites (already done this round, `results/vulncheck-base-bites.log`):
   against base, C1-vuln and C5-consistency go red — the checks detect the
   pre-patch state.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/go12611-toolchain-bump-claude
(https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6255 (Go 1.26.10 -> 1.26.11)
found no code defects; a contract harness on that branch verified the premise
(GO-2026-6859 cleared), the image digest, the version-pin consistency and the
unit suite. The PR may have advanced. Read
docs/verification/go12611-toolchain-bump/README.md ("Continuing after the fix")
and follow it: run scripts/re-verify.sh (no argument), report the per-check
table, review the last-reviewed..head delta, then advance .last-reviewed and
push. Note the FLUID_UNIT_TEST-on-xargs trap documented in run-unit-tests.sh.
```
