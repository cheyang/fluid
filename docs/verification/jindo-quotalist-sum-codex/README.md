# jindo-quotalist-sum-codex — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6261 (head 22d2d07c7c275a3baff2def3d24526ae8b74b380,
base 1321a217d78ab99c0cb76d2ea334d593a3765509). Reviewer: Codex (reviewer B).

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

## Summary of results (head 22d2d07c7c)

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | quotaList "10Gi,5Gi" panics the controller on base | 1 | **Confirmed** | `panic: cannot parse '10Gi,5Gi'` in `transformWorkerResources` on base, both engines (`results/premise-base-*.txt`) |
| F1 | fix sums Gi quota lists and persists the sum | 1 | Confirmed fixed on head | `TestVerifyQuotaListSumNoPanic`, `TestVerifyJindoCacheQuotaListSumNoPanic` PASS on head; worker request = "15Gi", CR spec updated (`results/unit-head.txt`) |
| F2 | Mi-denominated quota lists are summed as milli-BYTES | 1 | **Confirmed bug on head** | `TestVerifyQuotaListMiUnitsSum`, `TestVerifyJindoCacheQuotaListMiUnitsSum` FAIL on head: got `"4096m"` (≈4 bytes), want `"4Gi"` (`results/unit-head.txt`) |
| F3 | invalid quota element returns error instead of panic | 1 | Confirmed fixed on head | `TestVerifyInvalidQuotaListReturnsError` PASS (`results/unit-head.txt`) |

## Per-finding detail

### F2 — Mi quota lists sum to milli-bytes (the one open defect)

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

- **F2**: sum the quotas from the original `quotaList` values (before the jindo-unit
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
