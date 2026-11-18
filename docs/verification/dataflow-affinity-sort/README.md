# Verification — PR #6258: Sort map iteration in dataflow GenerateNodeAffinity

- PR: https://github.com/fluid-cloudnative/fluid/pull/6258 (fixes #6238)
- Head under review: `79ba4d90aae5544101855982325975b72c8e4205`
- Base: `5e6473599f78263a49e521854d80f918a1a10bb0`
- Reviewer: codex (Reviewer B). Production code untouched; the harness is one additive
  test file: `pkg/dataflow/zz_verify_affinity_order_test.go`.

## Premise (P0) — CONFIRMED on base

Issue #6238 claims `GenerateNodeAffinity` iterates the job annotations map directly, so
the generated `MatchExpressions` order is non-deterministic. The harness contract test was
grafted unchanged onto base `5e647359` (which has the unsorted `range annotations` loop)
and **fails at iteration 0**:

```
got:  [zone region hostname rack a-first]   (results/unit-base-harness.txt)
want: [a-first k8s.rack kubernetes.io/hostname topology.kubernetes.io/region topology.kubernetes.io/zone]
```

The prefix-collision control test passes on base, so the red is the claimed symptom, not a
harness artifact. Component match: report and patch both name `pkg/dataflow/helper.go`.

## Claims and results (observed vs expected)

| id | claim | layer | polarity | base | head | evidence |
|----|-------|-------|----------|------|------|----------|
| P0 | map iteration yields non-deterministic MatchExpressions on base | unit | contract (red = symptom) | FAIL (symptom present, as expected) | PASS | results/unit-base-harness.txt, results/unit-head-harness.txt |
| A1 | head emits sorted, deterministic MatchExpressions every call (40 iterations, adversarial key set) | unit | contract | n/a | PASS | results/unit-head-harness.txt |
| A2 | slash-form scope annotations (`.../inject`, `.../labels`) never leak into MatchExpressions | unit | contract | PASS | PASS | results/unit-head-harness.txt |
| A3 | PR's own tests (`TestGenerateNodeAffinitySortsCustomizedAffinityAnnotations`, strict `TestGenerateNodeLabels`) pass | unit | contract | n/a | PASS (`-count=10`) | results/unit-head-full-pkg-count10.txt, results/unit-head-full-pkg-verbose.txt |
| A4 | caller suites (dataload 45/45 specs, datamigrate) stay green | integration (fake client) | contract | n/a | PASS | results/integration-head-callers.txt |

## Harness-bites check

The fix was temporarily reverted on the head worktree (unsorted `range annotations` +
import removed): both `TestVerifyGenerateNodeAffinityDeterministicSortedOrder` and the PR's
own `TestGenerateNodeAffinitySortsCustomizedAffinityAnnotations` went red
(results/unit-head-harness-bites.txt). The revert was then restored; `git diff HEAD --
pkg/dataflow/helper.go` is empty. The harness exercises the real path and bites.

## Layer notes

- L1 unit: `go test ./pkg/dataflow/ -run 'TestVerifyGenerateNodeAffinity' -count=1 -v`
- L2 integration: `go test ./pkg/controllers/v1alpha1/dataload/... ./pkg/controllers/v1alpha1/datamigrate/... -count=1`
  (fake-client status-handler suites of both `GenerateNodeAffinity` callers)
- L3 live: skipped per round instructions. No live-only signal exists: the change reorders
  ANDed `NodeSelectorRequirement`s inside one `NodeSelectorTerm`, which the kube-scheduler
  treats order-insensitively; the only externally visible effect is a stable, sorted
  `OperationStatus.NodeAffinity`.

## Re-running after a fix / on another machine

```bash
git fetch https://github.com/cheyang/fluid.git verify/dataflow-affinity-sort-codex
git checkout verify/dataflow-affinity-sort-codex   # or a worktree
bash docs/verification/dataflow-affinity-sort/scripts/re-verify.sh   # auto-fetches current PR head
```

All findings are `contract` polarity: green = good. There are no canaries to invert.

## Kickoff prompt for a fresh agent

```
Continue verification of https://github.com/fluid-cloudnative/fluid/pull/6258.
Fetch branch verify/dataflow-affinity-sort-codex from https://github.com/cheyang/fluid.git,
read docs/verification/dataflow-affinity-sort/README.md and verify-manifest.json, then run
bash docs/verification/dataflow-affinity-sort/scripts/re-verify.sh and report per-finding
Fixed / Still-broken / Harness-update. Advance .last-reviewed to the head you verified.
```
