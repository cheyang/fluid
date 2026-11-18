# jindo-quota-sum — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6261
(round 1 head `22d2d07c7c…`, round 2 head `305e466f49…`, base `1321a217d78ab99c0cb76d2ea334d593a3765509`).

## Round 2 (head 305e466f4) — the F1 fix landed

The PR added one commit, `305e466f4 fix(jindo): read the m suffix as Mi when
summing tieredstore quotas`: `SumQuotas` now maps a trailing `m` back to `Mi`
before `resource.ParseQuantity`, plus a helper-level regression test
(`pkg/utils/jindo/quota_test.go`). The m→Mi restoration is centralized in the
shared helper, so it also fixes the pre-existing single-value `quota: 512Mi`
path (round-1 F5), which flows through the same `SumQuotas` call.

`scripts/re-verify.sh` (grafted onto 305e466f4, all contract polarity):

| ID | Claim | Verdict | Evidence |
|----|-------|---------|----------|
| B1–B5 | round-1 contracts (sum/error paths/single/spaces) | FIXED (all pass) | `results/reverify/` |
| B6 | `SumQuotas` semantics table (incl. inverted milli row + new Mi rows) | FIXED | `results/reverify/unit.out` |
| F1 | Mi-denominated quotaList sums as Mi in both engines (round-1 blocker) | **FIXED** — `TestVerifyQuotaListMiUnitsSum`, `TestVerifyJindoCacheQuotaListMiUnitsSum`, `TestVerifyMiUnitsSumExceedsLimitReturnsError` pass; request `4Gi`, persisted `4Gi` | `results/reverify/integration.out` |
| F5 | single-value `512Mi` → 512Mi request (pre-existing at base) | **FIXED** — `TestVerifySingleQuotaMiUnits` passes | `results/reverify/integration.out` |

Harness-bites (round 2): `results/bites-check-round2.txt` — the same F1/F5
tests fail on pre-fix production code (22d2d07, no m→Mi mapping) with the exact
round-1 signature (`request "4096m"`, `{{4096 -3} … 4096m DecimalSI}`
persisted), and the unit rows return `300m/4096m/512m`. Production diff vs
22d2d07 on this branch: empty.

Full sweep: `results/head-sweep-round2.txt` — `FLUID_UNIT_TEST=true go test`
on `./pkg/utils/jindo/ ./pkg/ddc/jindofsx/ ./pkg/ddc/jindocache/` all **ok**
on 305e466f4 + this harness (includes the PR's own new `TestSumQuotas`).

Polarity note: the round-1 unit row "milli parses as milli" asserted the
then-current behavior and flipped when the fix landed; it is inverted to the
new contract ("jindo m suffix is Mi") per the flip policy, and joined by the
explicit F1/F5 rows. Residual observations (unchanged from round 1): negative
elements still sum (F4), the master-memory heuristic still no-ops for lists
(F6), the g→Gi restoration is still duplicated per engine (F7), the worker
limit-check error still says "master limits memory" (F8).


Reviewer: Claude (Reviewer A). Harness branch: `verify/jindo-quotalist-sum-claude`
(fork `cheyang/fluid`). Production code is untouched on this branch — the diff
is tests + docs only.

Layers run against the **code under review** (`22d2d07c7`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `jindoutils.SumQuotas` table | `go test ./pkg/utils/jindo/ -run TestSumQuotasContract -v` |
| 2. Integration (fake client + scheme) | `JindoFSxEngine` / `JindoCacheEngine` `transformWorkerResources` incl. the runtime-update retry path | `go test ./pkg/ddc/jindofsx/ ./pkg/ddc/jindocache/ -run 'TestPremise\|TestVerify\|TestTransformWorkerResourcesWithQuotaList' -v` |
| 3. Live | skipped for this round (no KUBECONFIG per round instructions) | — |

> Test polarity: **all tests here are contract tests** (assert intended
> behavior). On pre-fix (base) code the multi-value cases PANIC inside
> `resource.MustParse` and the utils layer does not compile (`SumQuotas`
> undefined) — see `results/bites-check.txt`.

## Problem premise (P0)

Run against the **base** branch without the patch.

| | |
|---|---|
| Claimed symptom | "JindoRuntime controller panics when setting quotaList in tieredstore level" — `panic: cannot parse '10Gi,5Gi': quantities must match the regular expression ...` (issue #2077) |
| Linked issue | #2077, OPEN, created 2022-08-19, label `bug`, still valid (the `MustParse` site is still present at base `transform.go:679`) |
| Reported component | `pkg/ddc/jindofsx` (`transformWorkerResources` → `resource.MustParse`) |
| Patched component | `pkg/ddc/jindofsx/transform.go`, `pkg/ddc/jindocache/transform_resources.go`, shared helper `pkg/utils/jindo.SumQuotas` |
| Component match | Yes. Both jindo engines that contain the panic site are patched (`jindofsx` when `JINDO_ENGINE_TYPE=jindofsx`, `jindocache` as the default impl). The legacy `pkg/ddc/jindo` engine has no `MustParse` quota path, so nothing reachable is left unfixed. |
| **Verdict** | **Confirmed** |
| Evidence | `results/base-premise.txt` — `TestPremiseQuotaListMustNotPanic` on base panics with `cannot parse '10Gi,5Gi'` at `transform.go:679`, byte-for-byte the issue's stack trace. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | multi-value MEM quotaList panics the controller on base | base | Confirmed (panic) | `results/base-premise.txt` |
| B1 | multi-value quotaList: no panic, worker memory request = SUM of quotas (10Gi+5Gi → `15Gi`), and the runtime object is persisted with the summed request | 2 | Confirmed | `results/head-harness.txt` — `TestVerifyQuotaListSummedRequest`, `TestVerifyJindoCacheQuotaListSummedRequest` pass; same calls panic on base |
| B2 | invalid quota in the list returns an error instead of panicking | 2 | Confirmed | `TestVerifyInvalidQuotaReturnsError`, `TestVerifyJindoCacheInvalidQuotaReturnsError`, PR's `TestTransformWorkerResourcesWithQuotaList` |
| B3 | summed quota above the worker memory limit returns an error | 2 | Confirmed | `TestVerifySumExceedsLimitReturnsError`, `TestVerifyJindoCacheSumExceedsLimitReturnsError` |
| B4 | single-value quota behavior unchanged (5g → 5Gi) | 2 | Confirmed | `TestVerifySingleQuotaStillWorks` |
| B5 | CRD-documented whitespace format `"100Gi, 50Gi"` survives the round trip | 2 | Confirmed | `TestVerifyQuotaListWithSpaces` |
| B6 | `SumQuotas` semantics table (binary/decimal/spaces/milli/invalid/empty) | 1 | Confirmed | `TestSumQuotasContract` |
| N1 | pre-existing: jindo unit round-trip turns `Mi` into `m`, parsed by k8s as *milli* — a `512Mi` MEM quota yields a ~0 memory request | 1 | Confirmed as pre-existing (present at base for single values too; PR neither introduces nor fixes) | `TestSumQuotasContract` row "milli parses as milli" |
| N2 | pre-existing: `pkg/ddc/jindofsx` `TestDestroyWorker` and all of `pkg/ddc/jindo`'s failing tests fail identically on base | 2 | Confirmed pre-existing (not PR-caused) | `results/base-premise.txt`, `results/head-packages.txt` |

## Per-finding detail

- **B1 (the fix works)** — `transformWorkerResources` on both engines now
  computes `SumQuotas("10Gi,5Gi") = 15Gi`, sets
  `value.Worker.Resources.Requests.Memory = "15Gi"`, and persists
  `Spec.Worker.Resources.Requests[memory] = 15Gi` on the JindoRuntime via the
  retry path (fake client). Observed: request `15Gi`, persisted `15Gi`.
  Expected: same. On base the same call panics (harness-bites check).
- **B2/B3** — error paths replace the panic: `invalid quota "abc" in ...` and
  `the memory tierdStore's size 15Gi is greater than ... 12Gi`.
- **B4** — single-value path byte-identical to base behavior (`5g` → `5Gi`),
  guarding against regressions for the 99% case.
- **B5** — the API docs for `quotaList` literally show `"100Gi, 50Gi"` with a
  space; `transform()` hands down `"100g, 50g"`, and `SumQuotas` `TrimSpace`s
  each element, so the documented format works. (Without TrimSpace this would
  error — worth keeping.)
- **N1 (pre-existing, non-blocking)** — jindo's unit round-trip
  (`TransformQuantityToJindoUnit` / `transform()`) rewrites `Mi`→`m` and
  `transformWorkerResources` only restores `g`→`Gi`, never `m`→`Mi`. So a MEM
  quota written as `512Mi` is parsed by k8s as 512 *milli*bytes. This is true
  at base for single-value quotas (MustParse("512m")) and remains true after
  the PR for multi-value ones. Not introduced by this PR; a one-line
  `strings.ReplaceAll(userQuotas, "m", "Mi")`-style restoration (or parsing
  before the unit conversion) would fix it — recommend a follow-up issue.
- **N2 (pre-existing, non-blocking)** — `TestDestroyWorker` (jindofsx) and the
  `pkg/ddc/jindo` suite fail identically on base `1321a217`, so they are not
  attributable to this PR. The PR body discloses this honestly.

## Harness-bites check

`results/bites-check.txt`: with the three production files reverted to base
inside the head worktree, layer-2 tests panic with `cannot parse '10Gi,5Gi'`
and layer 1 fails to build (`undefined: SumQuotas`). The tests exercise the
changed path, not something adjacent. Production files were then restored;
`git diff` against the PR head is empty except for the added harness files.

## Live run notes

Skipped this round (round policy: unit + integration only, no KUBECONFIG). L3
signal if run later: create a JindoRuntime with MEM tier + `quotaList: 10Gi,5Gi`
against a real cluster; expect a successful setup with worker memory request
`15Gi`, and no controller panic in the logs. For an invalid quota, expect a
reconcile error event instead of a crash-looping controller.

## Proposed fixes (NOT applied to production here)

- None required for the PR's own scope — the PR is correct for its claim.
- Follow-up (pre-existing N1): restore `m`→`Mi` alongside `g`→`Gi` in both
  engines' `transformWorkerResources`, or parse quotas from
  `TieredStore.Levels[0].QuotaList` directly before the jindo unit conversion.
- Follow-up (minor): jindofsx worker-limit error message says "master limits
  memory" — copy-paste from the master check (pre-existing).

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/jindo-quotalist-sum-claude` (production
code untouched), so it grafts onto whatever the fixed code is.

1. Prereqs: Go toolchain (1.27 used here), `jq` for `scripts/re-verify.sh`.
   Layers 1+2 need no cluster — the fake client scheme is enough.
2. From a checkout of this branch:
   ```bash
   bash docs/verification/jindo-quota-sum/scripts/re-verify.sh
   ```
   With no argument it fetches the current PR head from
   `manifest.pr` (`https://github.com/fluid-cloudnative/fluid/pull/6261`),
   grafts `harnessPaths` onto it, runs both layers, and prints per-finding
   FIXED / STILL-BROKEN / PARTIAL / HARNESS-UPDATE. All tests are contract
   polarity: FIXED = green.
3. Advance `.last-reviewed` after each round (the script prints the command).
4. Layer 3 needs a real cluster (see Live run notes).

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/jindo-quotalist-sum-claude
(fork cheyang/fluid, PR under review fluid-cloudnative/fluid#6261).
Read docs/verification/jindo-quota-sum/README.md ("Continuing after the fix")
and follow it: run scripts/re-verify.sh, mind that every test is contract
polarity (green = fixed), report the observed-vs-expected table, and advance
.last-reviewed. Live layer only if a KUBECONFIG is provided. Do not merge,
approve, or comment on the PR.
```
