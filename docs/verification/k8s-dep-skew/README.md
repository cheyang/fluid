# k8s-dep-skew — bug verification

Reproducible evidence for the findings raised while reviewing
[fluid-cloudnative/fluid#5392](https://github.com/fluid-cloudnative/fluid/pull/5392)
("chore(deps): bump k8s.io/kubernetes from 1.29.15 to 1.32.10", dependabot).

Harness branch: `verify/k8s-dep-skew-claude`, based on PR head `8534d282`.
**Production code is untouched** — the branch adds only this directory.

Layers run against the **code under review** (`8534d282`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (build gate) | whole-tree compile (`go build ./...`) | `bash scripts/check-build.sh` |
| 1b. Probe (build gate) | `csi.ControllerServer` interface satisfaction, unmasked via a throwaway smd shim | `bash scripts/probe-csi-interface.sh` |
| 2. Regression sweep | unit tests of every package that consumes the bumped vendored code | `bash scripts/run-affected-tests.sh` |
| 3. Live | not run this round (unit+integration policy); CI evidence captured instead | — |

> Test polarity: both gates are **contract** tests — they FAIL on the PR head (that is
> the reproduction) and must PASS once the PR is fixed.

## Problem premise (P0)

Dependabot bump; no linked issue, no symptom claimed. The checkable premise for a
chore(deps) PR is "the bumped tree is viable".

| | |
|---|---|
| Claimed symptom | none (routine dependency bump: "Bumps k8s.io/kubernetes from 1.29.15 to 1.32.10") |
| Linked issue | none |
| Reported component | go.mod `k8s.io/kubernetes` |
| Patched component | go.mod/go.sum/vendor for k8s.io/kubernetes + 7 transitive modules |
| Component match | yes |
| **Verdict** | **Refuted** for the bump's viability: `go build ./...` fails on the head while the base builds clean |
| Evidence | `results/01-base-build.txt` (exit 0), `results/02-head-build.txt` (compile error) |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| F1 | `go build ./...` exits 0 | 1 | **Confirmed (broken)** — smd v4.4.2 removed `merge.Updater.IgnoredFields`; replace-pinned apimachinery v0.29.15 still sets it | `results/02-head-build.txt` |
| F2 | `controllerServer` satisfies `csi.ControllerServer` | 1b | **Confirmed (broken)** — csi spec v1.9.0 adds `ControllerModifyVolume`; masked by F1, surfaced by the shim probe | `results/03-probe-build.txt` |
| F3 | k8s.io/* module versions stay coherent (repo convention: coordinated bump) | — | Confirmed as a design regression (major, not a compile break) | `results/06-coordinated-bump-convention.txt` |
| — | affected unit tests pass once the tree compiles | 2 | Pass with both breakers shimmed (no additional behavioral break found) | `results/04-affected-unit-tests.txt` |

## Per-finding detail

### F1 — vendored apimachinery fails to compile against smd v4.4.2

Mechanism: the PR requires `sigs.k8s.io/structured-merge-diff/v4 v4.4.2` (transitive
requirement of k8s.io/kubernetes v1.32.10) but `replace` pins keep
`k8s.io/apimachinery` at **v0.29.15**. smd v4.4.2 removed the deprecated
`merge.Updater.IgnoredFields` field (replaced by `IgnoreFilter`), while the vendored
apimachinery v0.29.15 `pkg/util/managedfields/internal/structuredmerge.go` still
assigns it (lines 56 and 73):

```
vendor/k8s.io/apimachinery/pkg/util/managedfields/internal/structuredmerge.go:56:4:
  unknown field IgnoredFields in struct literal of type merge.Updater
```

Control: base `3e99063a` (smd v4.4.1, which still has the field) → `go build ./...` exit 0.

Fixes (either, pick deliberately):
- minimal: `replace sigs.k8s.io/structured-merge-diff/v4 => sigs.k8s.io/structured-merge-diff/v4 v4.4.1`
- correct: coordinated bump — move every `k8s.io/*` replace + direct require to v0.32.x
  (client-go, apimachinery, api, component-base, …) and controller-runtime to a
  v0.32-compatible line, as commit fb13453f ("update kubernetes to v1.29.15", #5253)
  did for 1.29.

### F2 — csi spec v1.9.0 breaks pkg/csi/plugins

Mechanism: `github.com/container-storage-interface/spec` v1.8.0 → v1.9.0 adds
`ControllerModifyVolume` to the `csi.ControllerServer` interface
(`vendor/.../csi/csi.pb.go:6520`). `pkg/csi/plugins` implements the interface with a
concrete `controllerServer` that lacks the method, so
`pkg/csi/plugins/driver.go:107` (`s.Start(..., d.newControllerServer(), ...)`) no
longer type-checks. The error is invisible on the head because F1 fails first;
`scripts/probe-csi-interface.sh` copies the tree to a throwaway dir, restores the
removed smd field (dead code in v4.4.2 — cannot mask further errors), and rebuilds:

```
pkg/csi/plugins/driver.go:107:3: cannot use d.newControllerServer() (value of type
  *controllerServer) as csi.ControllerServer value in argument to s.Start:
  *controllerServer does not implement csi.ControllerServer (missing method
  ControllerModifyVolume)
```

Fix: implement `ControllerModifyVolume` returning `codes.Unimplemented` (or embed
`csi.UnimplementedControllerServer`) and do **not** advertise `MODIFY_VOLUME` in
`ControllerGetCapabilities`.

### F3 — partial bump violates the repo's coordinated k8s bump convention

`k8s.io/kubernetes` v1.32.10 is compiled against `k8s.io/{api,apimachinery,client-go,
component-base}` v0.29.15 and `sigs.k8s.io/controller-runtime` v0.17.5 via the
replace pins — a 3-minor skew upstream neither builds nor tests. The repo's own
history (fb13453f / #5253) bumps all k8s.io/* replaces in lockstep. Evidence of the
skew biting: F1 is exactly a skew compile break, and after both breakers are shimmed
the smd field-management code path silently changes semantics (`IgnoredFields` set by
apimachinery 0.29 is dead in smd 4.4.2). Unit tests of every package that consumes
the vendored 1.32 code pass (see `results/04-affected-unit-tests.txt`), so no
additional *proven* runtime break — hence major, not blocker.

### Not a finding (checked)

- `portallocator.New` offset behavior: 1.32 always applies `calculateRangeOffset`
  (ServiceNodePortStaticSubrange graduated); fluid's allocator factory ignores the
  offset argument, so no behavior change for fluid.
- `podutil.IsPodReady` unchanged 1.29 → 1.32 (only `FindPort` sidecar support and a
  new helper were added); `pkg/api/v1/pod` stays a leaf package with no feature-gate
  registrations. All k8s feature-gate packages (`apiserver/pkg/features`,
  `apiextensions-apiserver/pkg/features`, `kubernetes/pkg/features`) were **dropped**
  from the vendor closure, so no duplicate-gate-registration init panic is possible.
- `go mod tidy -diff` is clean — go.mod is internally consistent; the failure is the
  version combination itself, not a stale vendor tree.

## CI evidence

All compile-dependent GitHub checks fail on head `8534d282` — build (ubuntu-latest,
ubuntu-24.04-arm), unittest, lint, staticcheck, kind-e2e (5 versions), backward-compat
(5 versions); DCO/CodeQL/SonarCloud pass. Base builds locally, so the failures are
attributable to this PR. Raw rollup: `results/05-ci-status.json`.

## Proposed fixes (NOT applied to production here)

- **F1**: coordinated bump of all k8s.io/* replaces + client-go/apimachinery.api.
  component-base to v0.32.10, controller-runtime to a compatible release
  (or, minimal stopgap, `replace` smd back to v4.4.1).
- **F2**: implement `ControllerModifyVolume` as Unimplemented (no capability
  advertised), with a unit test asserting the returned status code.
- **F3**: fold into the F1 coordinated bump; do not land k8s.io/kubernetes 1.32
  against 0.29 pins.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/k8s-dep-skew-claude` (production code untouched),
so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/k8s-dep-skew-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/k8s-dep-skew
   ```
2. Prereqs: go toolchain (go 1.25+), jq. No cluster needed — all gates are
   build/unit level.
3. One-shot re-run (auto-resolves the current PR head):
   ```bash
   bash docs/verification/k8s-dep-skew/scripts/re-verify.sh
   ```
   Per-finding output: F1 Fixed/Still-broken via `go build ./...`; F2 via the shim
   probe (exit 0 = interface satisfied; 3 = harness needs updating because the
   vendored smd source moved); then the affected-package unit sweep.
4. Both gates are contract tests: green on fixed code, red on the broken head.
   Harness-bites was checked in the other direction this round — the gates are red
   on `8534d282` (that red *is* the reproduction), and the probe's smd shim was
   verified to surface F2 while leaving everything else compiling.
5. After the round, advance `.last-reviewed` to the reviewed head (re-verify.sh
   prints the command).

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/k8s-dep-skew-claude
(https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/5392 (dependabot bump of
k8s.io/kubernetes 1.29.15 -> 1.32.10) found two compile blockers F1 (smd v4.4.2 vs
replace-pinned apimachinery v0.29.15) and F2 (csi spec v1.9.0 ControllerServer
interface). Read docs/verification/k8s-dep-skew/README.md ("Continuing after the
fix") and follow it: graft the harness onto the current PR head, run
scripts/re-verify.sh, and report the per-finding Fixed/Still-broken table with raw
evidence. Do not modify production code; do not publish anything to GitHub.
```
