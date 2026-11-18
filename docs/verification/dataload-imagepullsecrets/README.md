# dataload-imagepullsecrets — bug verification

Reproducible evidence for the findings raised while reviewing
[PR #6266](https://github.com/fluid-cloudnative/fluid/pull/6266) (Reviewer A / Claude).

Layers run against the **code under review** (`4fac0265a36800fa4a87cc22991eb0be8556742c`, PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `genDataLoadValue` imagePullSecrets priority (jindofsx, jindocache, legacy jindo), POSIX-path helpers (`NormalizeSubPath`, `UFSPathBuilder`, `IsValidMountRoot`) | `go test ./pkg/ddc/jindofsx ./pkg/ddc/jindocache ./pkg/ddc/jindo ./pkg/utils ./pkg/utils/validation -run 'TestVerify' -count=1` |
| 2. Integration | helm render of `charts/fluid-dataloader/{jindofsx,jindocache}` with genDataLoadValue-shaped values | `bash docs/verification/dataload-imagepullsecrets/scripts/render_dataload_chart.sh` |
| 3. Live | not run this round (round policy: unit + integration only) | see `liveNote` in verify-manifest.json |

> Test polarity: all harness tests here are **contract** tests (assert intended behavior) — they
> FAIL on buggy code and PASS when fixed. `TestVerifyLegacyJindoImagePullSecrets` is a contract
> test that currently fails on the PR head; that failure is the reproduction of finding F2.

## Problem premise (P0)

Answered before the findings, run against the **base** branch (`dcb76167`) without the patch.

| | |
|---|---|
| Claimed symptom | issue #6247: "a DataLoad on that runtime still runs with the controller wide secrets, so it fails to pull if the image lives in the same private registry" — the PR applies this to the Jindo engines |
| Linked issue | #6247, OPEN, created 2026-11-15 (3 days old), still valid |
| Reported component | JuiceFS (and Alluxio) `data_load.go` / `data_migrate.go` |
| Patched component | `pkg/ddc/jindofsx/load_data.go`, `pkg/ddc/jindocache/load_data.go` (+ a POSIX-path commit in `pkg/utils`) |
| Component match | **Partial by design**: the PR says "Part of #6247" (not "fixes"), body states "JuiceFS remains outside this PR". No auto-close risk. The requested *behavior* (runtime secrets replace env default) is what's implemented, for a different engine family |
| **Verdict** | **Confirmed** (for Jindo): on base, `genDataLoadValue` returns the env secret even when `runtime.Spec.ImagePullSecrets` is set, while the runtime chart (`transform.go` → `.Values.imagePullSecrets`) honors the runtime list — the exact inconsistency #6247 describes |
| Evidence | results/premise-differential-base.txt — harness + PR tests FAIL on base (`got [{env-secret}], want [{runtime-secret}]`), PASS on head |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| F1 | runtime `spec.imagePullSecrets` replace env default in jindofsx + jindocache DataLoad values; env fallback when empty | 1 | **Confirmed (fixed by PR)** | results/premise-differential-base.txt |
| F2 | legacy `jindo` engine impl (JINDO_ENGINE_TYPE=jindo) still ignores runtime secrets | 1 | **Confirmed (gap, unfixed)** | results/f2-bites-check.txt — `TestVerifyLegacyJindoImagePullSecrets` FAILS on head, passes with the one-line fix |
| F3 | POSIX-path switch is behavior-neutral on Linux | 1 | **Confirmed (no regression)** | results/f3-base-vs-head.txt — identical PASS on base and head |
| F4 | generated values render into the DataLoad job pod via helm chart | 2 | **Confirmed** | results/chart-render-*.yaml — job pod carries `runtime-secret`; block absent when unset |

## Per-finding detail

### F1 — imagePullSecrets priority (the PR's core change)
- Mechanism: `pkg/ddc/{jindofsx,jindocache}/load_data.go:113` now reads
  `runtime.Spec.ImagePullSecrets` first, falling back to `docker.GetImagePullSecretsFromEnv`.
  Matches the established engine convention (alluxio/efc `parse*Image`: runtime first, env
  fallback) and #6247's "replace, don't merge" semantics.
- Command: `go test ./pkg/ddc/jindofsx ./pkg/ddc/jindocache -run 'TestVerify' -count=1`
- Observed on head: PASS (replace + fallback + empty-empty). Observed on base: FAIL. Expected: PASS on head.
- Harness-bites: verified by reverting production files to base (results/premise-differential-base.txt).

### F2 — legacy jindo engine coverage gap
- Mechanism: `pkg/ddc/jindo/load_data.go:110` still builds DataLoad secrets from env only, while
  `pkg/ddc/jindo/transform.go:93` feeds `runtime.Spec.ImagePullSecrets` to the runtime chart, and
  the engine is still selectable (`pkg/utils/jindo.GetDefaultEngineImpl`, `JINDO_ENGINE_TYPE=jindo`).
- Command: `go test ./pkg/ddc/jindo -run TestVerifyLegacyJindoImagePullSecrets -count=1`
- Observed on head: FAIL (`got [{env-secret}], want [{runtime-secret}]`) — reproduction.
- Harness-bites: applying the same 3-line fix makes it pass; fix reverted afterwards
  (results/f2-bites-check.txt).
- Reviewer severity: **minor / non-blocking** (legacy engine, author may scope it out; the PR
  body's "covers the Jindo DataLoad paths" is accurate for jindofsx+jindocache only).

### F3 — POSIX path switch
- Mechanism: `filepath`→`path` in `NormalizeSubPath`, `UFSPathBuilder`, `IsValidMountRoot`. On
  Linux the two packages are byte-for-byte equivalent for these operations, so the switch changes
  nothing in production; it fixes semantics/tests on non-Linux hosts (paths are consumed by Linux
  containers).
- Observed: table of boundary inputs (leading/double slashes, `..` traversal, backslash-as-data,
  Windows drive letters) passes identically on base and head — no Linux regression, and the
  Windows-path semantics now pinned (drive-letter path → not absolute).

### F4 — chart plumbing (integration)
- `helm template charts/fluid-dataloader/{jindofsx,jindocache}` with the `dataloader.imagePullSecrets`
  shape that `TestVerifyDataLoadValuesYAMLEncoding` proves `genDataLoadValue` emits. Job pod spec
  carries the secret; no `imagePullSecrets` block when none supplied.

## Live run notes

Not run this round (unit + integration only, per round policy). For L3: create a JindoRuntime
whose `spec.imagePullSecrets` references a secret with private-registry creds, run a DataLoad,
`kubectl get pod <dataload-pod> -o jsonpath='{.spec.imagePullSecrets}'` must show the runtime
secret, and the pod must not enter ImagePullBackOff.

## Proposed fixes (NOT applied to production here)

- **F2**: mirror the same runtime-first/env-fallback block in `pkg/ddc/jindo/load_data.go` (3
  lines, verified by the harness). If the legacy engine is out of scope by decision, say so in
  the PR body ("jindofsx + jindocache only; legacy jindo engine excluded") and optionally file a
  follow-up issue.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/dataload-imagepullsecrets-claude` (production code untouched),
grafted onto whatever the fixed code is.

```bash
git fetch https://github.com/cheyang/fluid.git verify/dataload-imagepullsecrets-claude
git checkout <fixed-branch>
git checkout FETCH_HEAD -- docs/verification/dataload-imagepullsecrets \
  pkg/ddc/jindofsx/load_data_verify_test.go pkg/ddc/jindocache/load_data_verify_test.go \
  pkg/ddc/jindo/load_data_verify_test.go pkg/utils/posix_paths_verify_test.go \
  pkg/utils/validation/posix_verify_test.go
bash docs/verification/dataload-imagepullsecrets/scripts/re-verify.sh   # or: re-verify.sh <fixed-ref>
```

Polarity: every harness test is a contract test. After the author fixes F2,
`TestVerifyLegacyJindoImagePullSecrets` flips from FAIL to PASS and `re-verify.sh` exits 0
(it also fetches the current PR head automatically from `verify-manifest.json`'s `pr` URL).
No canary inversions are needed.

Copy-paste kickoff for a fresh agent: "You are resuming a review verification. Read
docs/verification/dataload-imagepullsecrets/README.md and verify-manifest.json, run
scripts/re-verify.sh against the current PR head of
https://github.com/fluid-cloudnative/fluid/pull/6266, and report per finding
Fixed / Still-broken / Partial / Harness-update."
