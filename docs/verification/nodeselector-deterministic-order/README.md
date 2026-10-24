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
