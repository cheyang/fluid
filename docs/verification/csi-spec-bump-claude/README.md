# csi-spec-bump — bug verification (reviewer: claude)

Reproducible evidence for the findings raised while reviewing
[PR #6229](https://github.com/fluid-cloudnative/fluid/pull/6229)
(bump `github.com/container-storage-interface/spec` 1.8.0 → 1.13.0).

Layers run against the **code under review** (head `7b5d0397f1d67d24cf61ee0faf680314a7c1d00f`, base `30d488bb7ecd601fb8f338387d9b1fec1c4a2a2a`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | module compiles + CSI server interface satisfaction (`TestCSISpecCompatHarness`, compile-time `var _ csi.*Server = …` assertions) | `bash docs/verification/csi-spec-bump-claude/scripts/unit-layer.sh` |
| 2. Integration | not needed — the defect is a compile-level contract; no API server involved | — |
| 3. Live | not applicable — the binary cannot be built at head | — |

> Test polarity: `TestCSISpecCompatHarness` is a **contract** test — it FAILS on the
> current PR head (the package does not compile; that *is* the reproduction) and
> PASSES once the csi-common compatibility is restored. Verified to flip green with
> the simulated fix (results/unit-layer-fixsim.json).

## Problem premise (P0)

Answered before the findings, run against the **base** branch without the patch.

| | |
|---|---|
| Claimed symptom | "Bumps github.com/container-storage-interface/spec from 1.8.0 to 1.13.0" — dependency currency; no user-visible symptom, no closing issue |
| Linked issue | none (dependabot PR; `#N` mentions are the upstream spec's release notes, not fluid issues) |
| Reported component | CSI spec Go bindings (vendored) |
| Patched component | go.mod / go.sum / vendored generated csi code — same |
| Component match | Yes |
| **Verdict** | **Confirmed** — base is at v1.8.0, builds and passes its CSI plugin tests (results/build-base.out, results/test-base.out). A currency bump is legitimate maintenance; the damage it does is finding F1 below. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| F1 | Bumping the CSI spec ≥ 1.10.0 breaks the build: the vendored `kubernetes-csi/drivers v1.0.2` csi-common `Default*Server` types no longer satisfy the regenerated `csi.*Server` interfaces | 1 | **Confirmed** | base: `go build ./...` exit 0 / head: 3 compile errors (results/build-base.out vs results/build-head.out); harness red at head (results/unit-layer-head.json), green with fix (results/unit-layer-fixsim.json) |

## Round 2 — 2026-11-07 (head force-updated: dependabot rebase)

The PR head moved `7b5d0397f1d6` → `93c51d5b9afe` and the merge-base advanced
`30d488bb7ecd` → `91a58c1d9d47` (master took #6227: Go 1.25.12 → 1.26.10 /
x/net v0.62.0, and #6222: codeql-action bumps). Delta analysis:

- `git diff 7b5d0397..93c51d5` non-vendor changes are **exactly** the files
  master's advance touched (workflow ymls, Dockerfiles, .travis.yml,
  .circleci) — i.e. pure rebase churn; the PR's own footprint at the new base
  is still only `go.mod`/`go.sum`/vendor (6 files, same set as round 1).
- The vendored `container-storage-interface/spec` files are **byte-identical**
  between the two heads; `kubernetes-csi/drivers` stays pinned at v1.0.2.
  Nothing in the rebase addresses F1 — it cannot, short of fluid-side work.

Re-verification on the new head (base also re-verified, since it moved):

| Check | Result | Evidence |
|-------|--------|----------|
| `re-verify.sh` (auto-resolves head = `93c51d5b9afe`) | **F1 STILL-BROKEN** (contract test fail=1) | results/reverify/unit.out (copy: results/r2-reverify-unit.out) |
| new base `91a58c1d`: `go build ./...`, stock `./pkg/csi/plugins/...` tests, `go mod verify` | all pass; spec still v1.8.0 | results/r2-base-91a58c1-evidence.out |
| head `93c51d5b`: `go mod verify` | all modules verified | results/r2-head-93c51d5-evidence.out |
| CI on new head (toolchain now go1.26.10) | build/unittest/lint/staticcheck/kind-e2e ×5/backward-compat ×5/circleci all **fail** with the identical `csi-common/utils.go:76,84,92 mustEmbedUnimplementedIdentityServer` errors; CodeQL/Sonar/DCO pass | results/r2-ci-checks.txt |

Toolchain independence of F1 is now observed on go1.25.12 (round 1),
go1.26.10 (CI at the new head), and go1.27.0 (local, both rounds).
`.last-reviewed` advanced to `93c51d5b9afe` for the next round.

## Per-finding detail

### F1 — module does not compile at the PR head (blocker)

Mechanism:

- spec ≥ v1.10.0 is generated with `protoc-gen-go-grpc v1.3.0`, whose server
  interfaces require `mustEmbedUnimplemented*Server()` and new RPC methods
  (`ControllerModifyVolume`, `GetSnapshot`, `ControllerListVolumeHealth`,
  `ControllerGetVolumeHealth`, `NodeGetVolumeHealth`, `NodeGetStorageHealth`).
  (v1.9.0 is the last old-style release — verified from the module proxy.)
- fluid vendors `github.com/kubernetes-csi/drivers v1.0.2` (Jan 2019, the
  **latest released version**; the repo's master no longer ships
  `pkg/csi-common` at that path), whose `DefaultIdentityServer` /
  `DefaultControllerServer` / `DefaultNodeServer` predate that pattern.
- fluid's own `controllerServer` / `nodeServer` (pkg/csi/plugins) embed those
  defaults, so they fail too.

Observed (head, `go build ./...`):

```
vendor/github.com/kubernetes-csi/drivers/pkg/csi-common/utils.go:76:20: cannot use ids
  (variable of type *DefaultIdentityServer) as csi.IdentityServer value in argument
  to s.Start: *DefaultIdentityServer does not implement csi.IdentityServer
  (missing method mustEmbedUnimplementedIdentityServer)
```
(×3, utils.go:76/84/92; full log in results/build-head.out; test run in results/test-head.out)

Expected: module compiles and `./pkg/csi/plugins` tests pass, as on base
(results/build-base.out, results/test-base.out).

Remediation scope measured by simulation (NOT applied to production — see
results/fixsim-csi-common-patch.diff): embedding
`csi.Unimplemented{Identity,Controller,Node}Server` into the three csi-common
`Default*Server` structs makes `go build ./...` **pass** and all `./pkg/csi/...`
tests pass (results/fixsim-build.out, results/fixsim-csi-tests.out). Since
vendored code can't be patched sustainably, the real fix needs one of:
- a `replace` directive pointing at a fork of kubernetes-csi/drivers that
  regenerates csi-common against spec ≥ 1.10, or
- inlining/dropping the small csi-common helpers in fluid itself, or
- not taking this bump (note: **no** spec version ≥ 1.9.0 compiles without
  changes — v1.9.0 fails differently, `controllerServer` missing
  `ControllerModifyVolume`; see results/spec-v1.9.0-downgrade.out).

CI attribution: PR check rollup shows `build` (ubuntu-latest, ubuntu-24.04-arm),
`unittest`, `lint`, `staticcheck`, all `kind-e2e-test`/`backward-compat-test`
matrix entries FAILURE, while CodeQL/Sonar/DCO pass
(results/ci-statusrollup.json) — consistent with a compile-level break
introduced by the PR.

Not PR findings (pre-existing, environmental; fail identically on base —
results/base-env-preexisting.out): `pkg/utils/helm` (needs a `ddc-helm` stub
binary in PATH) and `pkg/utils` `TestCheckMountPointBroken`.

## Proposed fixes (NOT applied to production here)

- **F1**: see the three options above; smallest sustainable one is the
  `replace`-fork or dropping csi-common. The 3-line embedding patch in
  results/fixsim-csi-common-patch.diff is the *shape* of the fix and proof the
  rest of fluid is source-compatible with spec 1.13.0.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/csi-spec-bump-claude` (production code
untouched — only this docs tree plus one additive test file), so it grafts onto
whatever the fixed code is.

1. Re-run everything against the current PR head (auto-resolves head from `pr`):
   ```bash
   git checkout verify/csi-spec-bump-claude
   bash docs/verification/csi-spec-bump-claude/scripts/re-verify.sh
   ```
   Expected after the author's fix: `F1 … FIXED` and exit 0.
2. Or graft onto a specific ref: `bash …/scripts/re-verify.sh <fixed-ref>`.
3. `TestCSISpecCompatHarness` is a contract test — green means fixed; if it
   stays red, the compiler errors are embedded in the JSON output events
   (results/reverify/unit.out after the run).

Kickoff prompt for a fresh agent:

> You are re-verifying PR fluid-cloudnative/fluid#6229. Checkout branch
> `verify/csi-spec-bump-claude`, run
> `bash docs/verification/csi-spec-bump-claude/scripts/re-verify.sh` (no args —
> it fetches the current PR head). Finding F1 is a contract test
> (`TestCSISpecCompatHarness`, unit layer): FIXED iff it passes. Report the
> per-finding table, advance `.last-reviewed` to the new head, commit and push
> to the reviewer fork only.

## Harness-bites check (step 4 of the skill)

- unfixed head → `TestCSISpecCompatHarness` **fail** (results/unit-layer-head.json,
  compiler errors embedded as output events)
- head + 3-line csi-common embedding patch (throwaway copy, production diff empty)
  → `TestCSISpecCompatHarness` **pass** (results/unit-layer-fixsim.json)
- full `./pkg/csi/plugins` package also passes with the harness file present
  (results/fixsim-csi-tests.out)
