# Verification — PR #6177 — merge CacheRuntime resources onto the CacheRuntimeClass baseline

Reviewer: Claude (Reviewer A, first round). Branch: `verify/cache-resources-merge-claude`, based on PR head `c1c303e8`.
Production code is untouched on this branch — the diff is this harness only.

## Premise (P0) — CONFIRMED

Issue #6173: a CacheRuntime that names only `limits.memory` loses the template's other
requirements. Reproduced on the **merge base** `e0fc4c18` with contract canaries
(`verify_premise_6177_test.go`), which assert the *correct* behavior and therefore fail
on unfixed code:

| probe | base `e0fc4c18` (bug present) | PR head `c1c303e8` (fix) |
| --- | --- | --- |
| `TestP0SyncDesiredKeepsUnstatedTemplateKeys` | FAIL — `limits.cpu dropped (got "0")`, `requests dropped: got map[]` | PASS |
| `TestP0CreationKeepsUnstatedTemplateKeys` | FAIL — same drop on the creation render | PASS |
| `TestP2ClaimsSurvive` | FAIL — claims dropped at both layers (sync resolved nil) | PASS |

Raw output: `results/unit-base.log`, `results/unit-head.log`.

The reported component (`pkg/ddc/cache` resource resolution feeding
`AdvancedStatefulSetManager.updateResources`) matches the patched component
(`pkg/ddc/cache/engine`), and the symptom matches the PR's description exactly.

## Findings verified

| id | claim | polarity | layer | verdict | evidence |
| --- | --- | --- | --- | --- | --- |
| B1 | Partially specified CacheRuntime resources keep the unstated template keys, at creation and at sync | contract | unit | **Confirmed (fixed)** | P0 canaries red on base, green on head |
| B2 | Creation render and sync resolution agree for every layer shape (no rolling divergence) | contract | unit | **Confirmed** | `TestP1CreationAndSyncAgree` (6-case matrix incl. claims, mixed shapes) passes at head; at base it fails on the claims case |
| B3 | Resource claims survive both paths (old code dropped them entirely) | contract | unit | **Confirmed** | `TestP2ClaimsSurvive` |
| B4 | End-to-end sync via fake client moves the named key and keeps the rest | contract | integration | **Confirmed** | PR's own ginkgo spec (2 specs focused: 2 passed) |
| B5 | Sync converges — requests stay nil and a second sync does not patch (no reconcile churn) | contract | integration | **Confirmed** | PR's own ginkgo spec; nil is preserved, `Semantic.DeepEqual` does not fire again |

No code-level defect was found in the PR; the review findings are design/test-gap items
(see the debate document in the run workspace, not on this branch).

## Harness layout

- `../../pkg/ddc/cache/engine/verify_premise_6177_test.go` — plain Go contract tests
  (P0 premise reproduction, P1 creation/sync equivalence, P2 claims survival).
  Runs with the package's normal suite: `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -run 'TestP0|TestP1|TestP2' -v`.
- `results/` — captured raw output:
  - `unit-base.log` / `unit-head.log` — canaries at merge base vs PR head.
  - `suite-summary.log` — full engine suite base vs head (274→287 specs, 262→275 passed;
    the 12 failures are byte-identical pre-existing failures in `ufs_test.go` /
    `sync_test.go`'s ReportSummary spec, present at base, unrelated to this PR), plus
    `pkg/ddc/cache/component` (ok).
- `scripts/re-verify.sh` — re-run against a future PR head: `bash scripts/re-verify.sh`
  (auto-fetches the current head via `manifest.pr`).

## Polarity

All tests are **contract** tests: green at head means fixed, red means broken. None are
bug-canaries, so nothing needs inverting after the fix.

## Continuing after the fix

From a checkout of this branch on any machine:

```bash
bash docs/verification/cache-resources-merge/scripts/re-verify.sh
```

It fetches the current PR head from the `pr` URL, grafts `harnessPaths` onto it, runs the
unit + integration layers, and prints per-finding Fixed / Still-broken / Partial. Exit 0
iff all findings are fixed. `.last-reviewed` holds `c1c303e8` — advance it after the next
review round.

## Live layer (L3) — skipped

Skipped by the debate configuration (unit + integration mandated for this round). The
environment probe did find a reachable cluster (1 node, v1.36.2-aliyun.1); it was left
untouched. See `liveNote` in `verify-manifest.json` for the exact manual procedure if the
live check is wanted later.
