# Verification: deterministic nodeSelector merge order (PR #6217 / issue #6216)

Reviewer: Codex (Reviewer B). Harness is additive only — production code untouched
(`git diff 4fd9701c..HEAD -- pkg/ api/ cmd/` outside the two new test files is empty).

## Premise (P0) — runs against BASE

Issue #6216 (quoted): "Because Go map iteration order is randomized by design, when
`nodeSelector` contains multiple labels (multi-label workers), `expressions` are appended
in arbitrary order across reconciliation cycles... `reflect.DeepEqual`... detect[s] a
spurious difference... resulting in an unnecessary status update write to the API server
and triggering a subsequent re-reconcile on every cycle."

- Reported component: `pkg/utils/kubeclient/pod.go` `MergeNodeSelectorAndNodeAffinity`
- Patched component: same — **component match: yes**
- Downstream comparators confirmed in code: `pkg/ddc/alluxio/status.go:121`,
  `pkg/ddc/cache/engine/status.go:150` (the #6200 CacheEngine path), and the jindo /
  juicefs / thin / jindofsx / jindocache status syncs all gate `Status().Update()` on
  `reflect.DeepEqual(runtime.Status, runtimeToUpdate.Status)`.

**Verdict: CONFIRMED (differential, unit + integration).**

## Findings / claims

| id | claim | layer | polarity | base (86e3adf3) | head (4fd9701c) |
|----|-------|-------|----------|-----------------|-----------------|
| P0 | Multi-label nodeSelector yields nondeterministic MatchExpressions order -> spurious DeepEqual diff -> status update churn | unit+integration | contract | FAIL (reproduced) | PASS (fixed) |
| C1 | Merged output is sorted and stable across 300 calls (nil podAffinity path) | unit | contract | FAIL | PASS |
| C2 | Recomputed vs persisted affinity DeepEqual-stable across 200 simulated reconcile cycles | unit | contract | FAIL (172/200 flapped) | PASS (0/200) |
| C3 | Merge into existing NodeSelectorTerms (append path) is deterministic | unit | contract | FAIL | PASS |
| C4 | End-to-end reconcile simulation vs real API server (envtest): exactly 1 status update over 30 cycles | integration | contract | FAIL (26/30 updated) | PASS (1/30) |

Observed-vs-expected raw output: `results/unit-base.txt`, `results/unit-head.txt`,
`results/integration-base.txt`, `results/integration-head.txt`.

Note on base flap rate: for a 6-key single-bucket map Go randomizes the iteration start
offset (~8 rotations), so ~7/8 of cycles mismatch, not 719/720. Both rates churn; the
unit run observed 172/200, the envtest run 26/30.

## How to run

Unit layer:
```bash
go test ./pkg/utils/kubeclient/ -run 'TestVerify' -count=1 -v
```

Integration layer (needs envtest binaries; e.g. `setup-envtest use 1.34.1 -p path`):
```bash
KUBEBUILDER_ASSETS=<path-to-k8s-assets> go test ./test/verification/nodeselectororder/ -count=1 -v
```

One-shot re-verification against the current PR head (or any ref):
```bash
bash docs/verification/nodeselector-deterministic-order/scripts/re-verify.sh [ref]
```
Exit 0 iff every contract finding passes on the target ref.

## Polarity

All tests are CONTRACT tests: FAIL on buggy code, PASS on fixed code. "All green" ==
fixed. There are no canaries to invert.

## Harness-bites check

The PR is itself the fix, so the differential run doubles as the bites check: same
harness fails on base sha `86e3adf3` and passes on head sha `4fd9701c`. No production
code was modified at any point.

## Continuing after the fix lands

1. Check out this branch (`verify/nodeselector-deterministic-order-codex`).
2. `bash docs/verification/nodeselector-deterministic-order/scripts/re-verify.sh`
   resolves the current PR head from `manifest.pr` and grafts the harness onto it.
3. Keep these tests as the regression suite for future rounds; advance `.last-reviewed`.

## Round 2 (incremental, head f5bb49a3, base moved 86e3adf3 -> e5251b39)

PR was force-pushed 4fd9701c -> f5bb49a3 (squash/rebase). Delta is test-only:
`pkg/utils/kubeclient/pod_test.go` +84 lines adding the previously-requested
append-path coverage (one multi-label + existing-NodeSelectorTerms DescribeTable
entry and one 100-invocation determinism `It` on the append path). Production
`pod.go` is byte-identical between the two heads. Master advanced by a single
CI-only commit (codeql-action bump #6218); `pkg/`, `api/`, `cmd/` identical
between the two bases.

Re-verification (2026-10-25, unit + integration layers):

| id | new base (e5251b39) | new head (f5bb49a3) | evidence |
|----|--------------------|---------------------|----------|
| P0 | FAIL — reproduced (unit 169/200 flapped; envtest 20/30 status updates) | PASS — fixed (0 flap; 1 update/30 cycles) | results/{unit,integration}-base-e5251b39.txt, results/{unit,integration}-head-f5bb49a3.txt |
| C1 | FAIL | PASS | same |
| C2 | FAIL (169/200) | PASS (0/200) | same |
| C3 | FAIL | PASS | same |
| C4 | FAIL (20/30 updated) | PASS (1/30) | same |

`scripts/re-verify.sh` against head f5bb49a3: C1–C4 all FIXED, exit 0
(results/reverify/{unit,integration}.{out,parsed}).

PR's own suite, harness-bites check: the head's `pod_test.go` (incl. the two new
append-path specs) grafted onto base e5251b39 fails 4/10 focused specs — both new
append-path specs red on unfixed code — and passes 10/10 on head f5bb49a3
(`go test ./pkg/utils/kubeclient/ -ginkgo.focus=MergeNodeSelectorAndNodeAffinity`,
results/prsuite-head-f5bb49a3.txt). The new tests genuinely pin the fix.

Round-1 finding F1 (PR's suite lacked a multi-label append-path case): FIXED at
f5bb49a3 — the author added exactly that coverage and it is red-on-base /
green-on-head.
