# jindo-quotalist-sum-codex — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6261 (round-4 head 72d7d98cae66aec4c8d84f137fe3932984c84c4c,
round-3 head 928ea4813c8b0d359e3a87f78cadaef2f862e875,
round-2 head 305e466f49d7aee3431aab3ee9deb531fb23087e, round-1 head 22d2d07c7c275a3baff2def3d24526ae8b74b380,
base 1321a217d78ab99c0cb76d2ea334d593a3765509).
Reviewer: Codex (reviewer B).

Layers run against the code under review:

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `jindoutils.SumQuotas`, `transformWorkerResources` (jindofsx + jindocache) against a fake client | `go test ./pkg/ddc/jindofsx/ ./pkg/ddc/jindocache/ ./pkg/utils/jindo/ -run 'TestVerify' -v` |
| 2. Integration | full package suites of the touched packages (fake-client based ginkgo + go tests, incl. the PR's own tests) | `go test ./pkg/ddc/jindofsx/ ./pkg/ddc/jindocache/ ./pkg/utils/jindo/` |
| 3. Live | skipped per debate policy | see `liveNote` in verify-manifest.json |

> Test polarity: all tests are contract tests (assert intended behavior) — they FAIL on
> buggy code and PASS when fixed. P0 runs against BASE: there the F1 tests panic, which is
> exactly the premise evidence.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "JindoRuntime controller panics when setting quotaList in tieredstore level ... panic: cannot parse '10Gi,5Gi'" (issue #2077) |
| Linked issue | #2077, OPEN since 2022-08-19, still valid |
| Reported component | `pkg/ddc/jindofsx.(*JindoFSxEngine).transformWorkerResources` |
| Patched component | `pkg/ddc/jindofsx/transform.go`, `pkg/ddc/jindocache/transform_resources.go`, `pkg/utils/jindo/jindo.go` |
| Component match | Yes — jindofsx (the reported stack) and jindocache (identical MustParse line) both patched; legacy `pkg/ddc/jindo` never parses quotas (string passthrough), nothing to fix there |
| **Verdict** | **Confirmed** |
| Evidence | On base, the F1 tests panic with the issue's exact message: `results/premise-base-jindofsx.txt`, `results/premise-base-jindocache.txt` |

## Summary of results

### Round 4 (head 72d7d98c, delta 928ea481..72d7d98)

Round-4 delta is a single docs commit (`72d7d98ca docs(jindo): recommend 512Mi for
quotaList API input`) — the F13 fix: the `QuotaList` field doc now recommends binary
units (`512Mi`) for API input and describes a lowercase `m` as Jindo's internal
notation for Mi, regenerated consistently into `api/v1alpha1/openapi_generated.go`,
`api/v1alpha1/swagger.json`, and both `api_doc.md` copies. No Go code changed.

Re-verified with `re-verify.sh 72d7d98cae66aec4c8d84f137fe3932984c84c4c`
(`results/round4/reverify-round4.txt`, raw: `results/round4/unit.out`,
`results/round4/integration.out`):

| ID | Claim | Layer | base 1321a217d | r3 928ea481 | head 72d7d98c | Evidence |
|----|-------|-------|----------------|-------------|---------------|----------|
| A1 | fix sums Gi quota lists and persists the sum | 1 | panic | pass | **pass** | `results/round4/reverify-round4.txt` |
| A2 | invalid quota element returns error instead of panic | 1 | panic | pass | **pass** | same |
| F1 | Mi-denominated quota lists must sum in Mi | 1 | panic | pass | **pass** | same |
| F5 | single-value Mi quota must yield an Mi request | 1 | FAIL | pass | **pass** | same |
| F3/F4 | negative quota elements are silently summed | 1 | same | same | STILL-PRESENT (canary passes; finding open by design) | `TestVerifySumQuotasNegativeCanary` |
| F13 | QuotaList doc must recommend 512Mi and call m Jindo-internal | doc-check | n/a (line absent) | FAIL (old sentence in all 5 copies) | **FIXED (pass)** | `results/round4/doc-check-head.txt` vs `doc-check-prev-head-928ea481.txt` (bites) |

Delta-specific checks:

- New pin `scripts/verify-quotalist-doc.sh` (F13): PASS at 72d7d98 on all five doc
  copies; FAILS at 928ea481 on all five (stale "is read as Mi (not milli)" sentence
  detected) — differential demonstrated.
- `FLUID_UNIT_TEST=true go test -count=1 ./pkg/utils/jindo/ ./pkg/ddc/jindofsx/
  ./pkg/ddc/jindocache/` all `ok` at 72d7d98 (`results/round4/head-sweep-round4.txt`),
  including the PR's own `TestTransformWorkerResourcesQuotaListPositive`.
- CI unchanged: all Actions workflows `action_required` on 72d7d98 (fork approval
  still pending); rollup = DCO + SonarCloud only (F10 still open; details in
  `results/round4/reverify-round4.txt`).

### Round 3 (head 928ea481, delta 305e466f..928ea481)

Round-3 delta is test/doc/wording only (no production-logic change): the PR's own
jindofsx engine-level positive-path test `TestTransformWorkerResourcesQuotaListPositive`
(value + CR persistence, Mi case), a `QuotaList` field-doc line recording that jindo
reads a lowercase `m` suffix as Mi, regenerated openapi/swagger/api-doc copies, and the
worker-limit error-message wording fix (F8). Re-verified with `re-verify.sh 928ea481`:

| ID | Claim | Layer | base 1321a217d | r1 22d2d07c | r2 305e466f | head 928ea481 | Evidence |
|----|-------|-------|----------------|-------------|-------------|---------------|----------|
| A1 | fix sums Gi quota lists and persists the sum | 1 | panic | pass | pass | **pass** | `results/reverify/unit.out` |
| A2 | invalid quota element returns error instead of panic | 1 | panic | pass | pass | **pass** | `results/reverify/unit.out` |
| F1 | Mi-denominated quota lists must sum in Mi | 1 | panic | FAIL | FIXED | **FIXED (pass)** | `results/reverify/unit.out` |
| F5 | single-value Mi quota must yield an Mi request | 1 | FAIL | FAIL | FIXED | **FIXED (pass)** | `results/reverify/unit.out` |
| F3/F4 | negative quota elements are silently summed | 1 | same | same | same | STILL-PRESENT (canary passes; finding open by design) | `TestVerifySumQuotasNegativeCanary` |

Delta-specific checks (`results/round3/delta-check.txt`):

- The PR's new `TestTransformWorkerResourcesQuotaListPositive` PASSES at 928ea481
  (helm value + persisted runtime request both `1Gi`), and BITES: grafted onto base
  1321a217 it panics (`cannot parse '512m,512m'` — the issue mechanism), grafted onto
  round-1 head 22d2d07 it fails (`helm value memory request = "1024m", want 1Gi`) —
  i.e. this test would have caught the round-1 Mi regression.
- `FLUID_UNIT_TEST=true go test ./pkg/utils/jindo/ ./pkg/ddc/jindofsx/ ./pkg/ddc/jindocache/`
  all `ok` at 928ea481 (includes the PR's own new test).
- CI unchanged: all Actions workflows `action_required` on 928ea481 (fork approval
  still pending); rollup = DCO + SonarCloud only (F10 still open).

Harness note: re-verify.sh persists `results/reverify/*` before its EXIT trap runs
`git checkout -f`, which restores any *tracked* result files to their committed state;
the tracked `*.parsed` files have been regenerated by hand from this run's raw
`unit.out`/`integration.out` (jq parse identical to the script's) so the branch state
matches the observed round-3 truth (all `TestVerify*` pass).


### Round 2 (head 305e466f, delta 22d2d07c..305e466f)

Round-2 delta: the author moved the `m`->`Mi` restoration into `SumQuotas` itself
(plus a helper-level unit test file `pkg/utils/jindo/quota_test.go`). Re-verified:

| ID | Claim | Layer | base 1321a217d | prev 22d2d07c | head 305e466f | Evidence |
|----|-------|-------|----------------|---------------|---------------|----------|
| P0 | quotaList "10Gi,5Gi" panics the controller on base | 1 | **panic (confirmed)** | pass | pass | `results/premise-base-*.txt` (round 1) |
| A1 | fix sums Gi quota lists and persists the sum | 1 | panic | pass | **pass** | `results/reverify/` (F1/A1 rows) |
| A2 | invalid quota element returns error instead of panic | 1 | panic | pass | **pass** | `results/reverify/` (A2 row) |
| F1 | Mi-denominated quota lists must sum in Mi | 1 | panic | FAIL ("4096m") | **FIXED** ("4Gi") | `TestVerifyQuotaListMiUnitsSum`, `TestVerifyJindoCacheQuotaListMiUnitsSum` pass at head |
| F5 | single-value Mi quota must yield an Mi request | 1 | FAIL ("512m") | FAIL ("512m") | **FIXED** ("512Mi") | `TestVerifySingleValueMiQuotaSum`, `TestVerifyJindoCacheSingleValueMiQuotaSum` (added round 2; base/prev pins reproduced this round) |
| F3 | negative quota elements are silently summed | 1 | same | same | STILL-PRESENT (canary passes) | `TestVerifySumQuotasNegativeCanary`: `SumQuotas("10Gi,-5Gi")` = `"5Gi"` |

Harness-update note: the round-1 root-cause demonstrator row
`SumQuotas("2048m,2048m") == "4096m"` in `TestVerifySumQuotasContracts` encoded the
*buggy* behavior and flipped red when the fix landed; the row was updated to the fixed
contract ("4Gi"). A new row documents the deliberate semantic choice that a raw
milli element ("500m", CRD-legal) is now read as 500Mi, matching how jindo reads the
same string in the helm values.

Integration layer at head: `FLUID_UNIT_TEST=true go test ./pkg/ddc/jindofsx/
./pkg/ddc/jindocache/ ./pkg/utils/jindo/` -> all three packages `ok` (incl. the PR's
own `TestSumQuotas`). Bare `go test` still fails `TestDestroyWorker` exactly as on
base (pre-existing environment artifact, CI runs with `FLUID_UNIT_TEST=true`).

### Round 1 (head 22d2d07c) — superseded by the table above

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| A1 | fix sums Gi quota lists and persists the sum | 1 | Confirmed fixed | `results/unit-head.txt` |
| F1 | Mi-denominated quota lists summed as milli-BYTES | 1 | Confirmed bug (fixed in round 2) | `results/unit-head.txt` |
| A2 | invalid quota element returns error | 1 | Confirmed fixed | `results/unit-head.txt` |

## Per-finding detail

### F1 — Mi quota lists sum to milli-bytes (FIXED at head 305e466f; round-1 analysis below kept for the record)

`transform()` normalizes each quotaList element from k8s to jindo units: `Gi`→`g`,
`Mi`→`m` (pkg/ddc/jindofsx/transform.go:99-107, same in jindocache). Before parsing,
`transformWorkerResources` restores only `g`→`Gi`; `m` is left as-is and
`resource.ParseQuantity("2048m")` means 2048 **milli**-bytes (≈2 bytes), not 2048Mi.

Consequence at head: quotaList `2048Mi,2048Mi` → worker memory request `4096m` (≈4 bytes),
and that value is also written back into `spec.worker.resources.requests.memory` of the
JindoRuntime CR. The worker is under-scheduled by ~10^9× and the CR carries a bogus spec.

Attribution: the unit confusion pre-exists on base for single-value Mi quotas (base
`transformWorkerResources(rt, value, "2048m")` sets request `"2048m"` — no panic, silent).
For multi-value Mi lists base panics; the PR turns the crash into a silent wrong value.
The cleanest fix is to sum the original quota strings (or also restore `m`→`Mi`) before
parsing — proposed-fix sketch verified: adding `strings.ReplaceAll(userQuotas, "m", "Mi")`
after the existing `g`→`Gi` line flips both F2 contract tests to green (harness-bites
check; reverted afterwards, production code untouched).

## Harness-bites check

Applying the proposed one-line `m`→`Mi` restoration to both engines makes the failing F2
contract tests PASS (run on 2026-11-18, then reverted; `git diff` of production files empty).
This proves the F2 tests actually exercise the claimed defect and are not red for unrelated
reasons.

## Proposed fixes (NOT applied to production here)

- **F1**: sum the quotas from the original `quotaList` values (before the jindo-unit
  `Gi→g`/`Mi→m` normalization), or extend the restore step to `m`→`Mi`. Also consider
  rejecting negative elements (the CRD pattern on `QuotaList` explicitly allows a sign).

## Continuing after the fix (possibly on another machine)

```bash
git fetch https://github.com/cheyang/fluid.git verify/jindo-quotalist-sum-codex
git checkout <fixed-ref>
git checkout FETCH_HEAD -- docs/verification/jindo-quotalist-sum-codex \
  pkg/ddc/jindofsx/transform_quotalist_verify_test.go \
  pkg/ddc/jindocache/transform_quotalist_verify_test.go \
  pkg/utils/jindo/jindo_sumquotas_verify_test.go
go test ./pkg/ddc/jindofsx/ ./pkg/ddc/jindocache/ ./pkg/utils/jindo/ -run 'TestVerify' -v
# or, from a checkout of the verify branch:
bash docs/verification/jindo-quotalist-sum-codex/scripts/re-verify.sh
```

Expected when fully fixed: every `TestVerify*` passes. `pkg/utils/jindo` tests only compile
on heads that contain `SumQuotas` (i.e. this PR or its successor).

Kickoff prompt for a fresh agent: "Check out verify/jindo-quotalist-sum-codex from
https://github.com/cheyang/fluid.git, read docs/verification/jindo-quotalist-sum-codex/README.md,
and run scripts/re-verify.sh against the current head of fluid-cloudnative/fluid PR #6261."
