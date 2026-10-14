# k8s-132-bump — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/5392
("chore(deps): bump k8s.io/kubernetes from 1.29.15 to 1.32.10", head `8534d282`, base `3e99063`).

Layers run against the **code under review** (8534d282):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | repo compiles; CSI interface satisfaction; port-allocator allocation contract | `go test ./pkg/ddc/base/portallocator/... ./pkg/csi/plugins/... -run 'TestVerify' -v` |
| 1b. Build signature split | which of F1/F2 is present | `bash docs/verification/k8s-132-bump/scripts/check-build.sh` |
| 2. Integration | not needed — findings are compile-time; the compile check *is* the integration of the whole module graph | — |
| 3. Live | skipped (debate policy) | — |

> Test polarity: all tests here are **contract** tests — they FAIL (or the package fails to
> build) on the broken code and PASS on fixed code. On the un-fixed head the unit layer cannot
> even compile, so `re-verify.sh` reports HARNESS-UPDATE; for F1/F2 read that state as
> STILL-BROKEN (the finding *is* the compile failure). `scripts/check-build.sh` gives the
> precise per-finding signal at any tree state.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | Dependabot chore, no user symptom. Implicit claim: "k8s.io/kubernetes 1.29.15 → 1.32.10 with the transitives dependabot picked is a safe, buildable upgrade while every k8s.io/* staging module stays `replace`-pinned at v0.29.15" |
| Linked issue | none (no `fixes #N`) |
| Reported component | go.mod / vendor dependency set |
| Patched component | go.mod / go.sum / vendor only (no non-vendor source changes) |
| Component match | Yes |
| **Verdict** | **Refuted** — the bumped module set does not compile (F1), and even after fixing F1 the CSI plugin does not compile (F2). Positive evidence below; base builds green. |
| Evidence | `results/head-8534d282-check-build.out` vs `results/base-3e99063-build.out`; matches the PR's failing CI (build, unittest, lint, staticcheck, e2e all red) |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| F1 | structured-merge-diff v4.4.2 (bumped) renamed `merge.Updater.IgnoredFields`→`IgnoreFilter`; apimachinery stays pinned at v0.29.15 and still uses `IgnoredFields` → whole repo fails to compile | 1 | **Confirmed** | `results/head-8534d282-check-build.out` (2 errors in `vendor/k8s.io/apimachinery/pkg/util/managedfields/internal/structuredmerge.go:56,73`) |
| F2 | csi-spec v1.9.0 (bumped) added `ControllerModifyVolume` to `ControllerServer`; fluid's `controllerServer` embeds `csicommon.DefaultControllerServer` (v1.8-era) and does not implement it → `pkg/csi/plugins` fails to compile. Masked by F1 on the raw head | 1 | **Confirmed** (via probe) | `results/probe-fixed-unit.out` header + `results/probe-notes.md`; with only F1 reverted, build fails at `pkg/csi/plugins/driver.go:107` |
| F3 | vendored `portallocator.New` in 1.32 always applies the `ServiceNodePortStaticSubrange` offset (gate removed upstream). Fluid's `newBitMapAllocator` factory discards the offset, so dynamic allocation still covers the full range — behavior unchanged | 1 | **Refuted as a bug / kept as regression guard** | `TestVerifyAllocateBatchCoversFullRange` passes on base and on probe-fixed head (`results/base-3e99063-unit.out`, `results/probe-fixed-unit.out`) |

## Per-finding detail

### F1 — module skew: repo does not compile
- Command: `go build ./...` at head `8534d282`.
- Observed: `vendor/k8s.io/apimachinery/pkg/util/managedfields/internal/structuredmerge.go:56:4: unknown field IgnoredFields in struct literal of type merge.Updater` (and line 73).
- Expected: clean build (base `3e99063` builds clean — control).
- Mechanism: the PR bumps `sigs.k8s.io/structured-merge-diff/v4` v4.4.1→v4.4.2 (pulled in by the kube-openapi/kubernetes bump). v4.4.2 replaced `Updater.IgnoredFields map[APIVersion]*fieldpath.Set` with `Updater.IgnoreFilter map[APIVersion]fieldpath.Filter` (see vendored `merge/update.go`). `k8s.io/apimachinery` is `replace`-pinned at v0.29.15 and still sets `IgnoredFields`. Because *every* fluid package imports apimachinery, nothing builds → all CI red.
- Harness: `TestVerifyRepoCompilesPortallocator` (existence = compiled) + `check-build.sh` signature grep.

### F2 — CSI spec v1.9.0 interface break (masked by F1)
- Probe: on a scratch copy of the head, revert *only* structured-merge-diff to v4.4.1 (go.mod + vendor + modules.txt) → next build error is `pkg/csi/plugins/driver.go:107:3: cannot use d.newControllerServer() (value of type *controllerServer) as csi.ControllerServer value in argument to s.Start: *controllerServer does not implement csi.ControllerServer (missing method ControllerModifyVolume)`.
- Mechanism: csi-spec v1.9.0's generated `ControllerServer` interface gained `ControllerModifyVolume`; v1.8.0's did not have it. `controllerServer` relies on the embedded `*csicommon.DefaultControllerServer` for the methods it doesn't override, and that type predates the new method.
- Harness: `pkg/csi/plugins/csi_interface_verify_test.go` — compile-time assertion `var _ csi.ControllerServer = (*controllerServer)(nil)`.
- Probe after *both* fixes (structured-merge-diff reverted + `ControllerModifyVolume` stub added): `go build ./...` and `go vet ./...` clean, all harness tests PASS — i.e. the compile breakage is exactly {F1, F2}, nothing further hides behind them.

### F3 — portallocator behavior change is neutralized (regression guard kept)
- Upstream 1.32 removed the `ServiceNodePortStaticSubrange` gate: `portallocator.New` now always passes `calculateRangeOffset(pr)` (min 16 ports for ranges >16) to the allocator factory; in 1.29 the offset was 0 with the gate off.
- Fluid's `newBitMapAllocator` factory ignores the offset (`allocator.NewAllocationMap(max, rangeSpec)`), so fluid behavior is identical on 1.29 and 1.32. Verified: full-range dynamic allocation (incl. the would-be static subrange 20000-20015 of a 256-port range) succeeds on base and on probe-fixed head.
- The contract test remains to catch any future "fix" that starts honoring the offset (which would reserve the subrange and change which ports fluid hands out).

## Proposed fixes (NOT applied to production here)
- **F1+F2 (root cause)**: this bump cannot land alone. Either (a) close it and do a coordinated Kubernetes 1.32 upgrade — move every `k8s.io/*` replace pin to v0.32.x together with `k8s.io/kubernetes v1.32.10` and adapt the CSI plugin — or (b) pin structured-merge-diff back to v4.4.1 and csi-spec to v1.8.0, which defeats the bump. (a) is the real path; while prototyping, the probe shows {structured-merge-diff v4.4.1 + a `ControllerModifyVolume` stub} is sufficient to compile, but running 1.32 vendored kubernetes code against v0.29 staging libraries is an upstream-untested combination and should not ship.
- **F2 minimal**: implement `ControllerModifyVolume` on `controllerServer` returning `codes.Unimplemented` (probe stub), or bump `github.com/kubernetes-csi/drivers` to a version whose `DefaultControllerServer` covers the v1.9 interface.
- **Process**: add `k8s.io/kubernetes` (and the k8s staging modules) to `.github/dependabot.yml` `ignore` or a `groups:` entry so dependabot stops generating single-module bumps that cannot satisfy the replace pins.

## Continuing after the fix
1. Fetch this branch, then `bash docs/verification/k8s-132-bump/scripts/re-verify.sh` (no args — resolves the current PR head from `manifest.pr`). Green = all findings fixed. On the still-broken head expect HARNESS-UPDATE for F1/F2 (means: does not compile) — cross-check with `scripts/check-build.sh` for the exact signatures.
2. If the author fixes by upgrading everything to v0.32.x, the harness grafts cleanly (it only touches `go.mod`-independent test files); if `newBitMapAllocator`'s signature changes, adjust the F3 test (Harness-update) but keep the full-range-coverage assertion.
3. Kickoff prompt for a fresh machine: "Check out verify/k8s-132-bump-codex from the cheyang/fluid fork and run docs/verification/k8s-132-bump/scripts/re-verify.sh; report per-finding Fixed/Still-broken for F1..F3."

Polarity table: all tests are contract tests — green = fixed. There are no canaries to invert.

Results inventory (`results/`):
- `head-8534d282-check-build.out` — `check-build.sh` on the PR head: F1 PRESENT, build fails.
- `head-8534d282-unit.out` — `go test -run TestVerify` on the PR head: both packages `[build failed]`.
- `probe-notes.md` — what the probe tree changed and its full build/vet result.
- `probe-fixed-unit.out` — head + probe fixes: all 3 harness tests PASS (harness-bites proof).
- `base-3e99063-build.out` / `base-3e99063-unit.out` — base control: build clean, all 3 tests PASS.
