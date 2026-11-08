# Verification — nodeselector-sort (PR #6234) — Reviewer A (Claude)

PR: https://github.com/fluid-cloudnative/fluid/pull/6234
Head: `c9dfc9f43c4924f558e6a2328194ac1e89c1e11e`
Base: `a01c038bcc115e46f0fd74faf5e5ca5e9c6aa840`
Branch: `verify/nodeselector-sort-claude` (this branch; production code untouched — additive harness only)

## Premise (P0): Confirmed

Claim from issue #6220 / the PR body: the four sites (requirenodewithfuse webhook,
nodeaffinitywithcache webhook, dataset volume creation, EFC volume creation) iterate Go
maps unsorted when building `NodeSelectorRequirement`s / scheduling terms, so output
order is non-deterministic for multi-key selectors or pods bound to multiple runtimes.

Evidence: the 5 harness tests below were run against the **base** commit and **fail**
with 4–6 distinct orderings across repeated calls; the same tests **pass** on the PR
head. See `results/base-run.txt`, `results/base-volume-run.txt` (red) and
`results/head-run.txt`, `results/head-volume-run.txt` (green).

## Observed vs expected

| # | Test (file) | Polarity / layer | Base | Head | Verdict |
| --- | --- | --- | --- | --- | --- |
| 1 | `TestHarnessFuseSelectorExpressionsSortedDeterministic` (pkg/webhook/plugins/requirenodewithfuse/determinism_harness_test.go) | contract / unit | FAIL (6 orderings / 500 calls) | PASS | Confirmed |
| 2 | `TestHarnessTieredLocalityPreferredOrderSortedDeterministic` (pkg/webhook/plugins/nodeaffinitywithcache/determinism_harness_test.go) | contract / unit | FAIL (4 orders / 300 calls) | PASS | Confirmed |
| 3 | `TestHarnessTieredLocalityRequiredOrderSortedDeterministic` (same file) | contract / unit | FAIL (4 orders / 300 calls) | PASS | Confirmed |
| 4 | `TestHarnessPVNodeAffinityExpressionsSortedDeterministic` (pkg/utils/dataset/volume/determinism_harness_test.go) | contract / unit | FAIL (6 orderings / 80 PVs) | PASS | Confirmed |
| 5 | `TestHarnessEFCPVNodeAffinityExpressionsSortedDeterministic` (pkg/ddc/efc/create_volume_determinism_harness_test.go) | contract / unit | FAIL (6 orderings / 60 PVs) | PASS | Confirmed |

All tests are **contract** polarity: they assert the intended (sorted, deterministic)
behavior, so they are red on the unfixed base and green on the fixed head. No canaries;
no polarity inversion is needed after a fix.

Non-reproduced harness tests: none — every claim is either proven (P0 + 5 sites) or
recorded as code-path findings not covered by a test (F1, F2 below), because a test
asserting *randomness* would be flaky by construction.

## Findings beyond the premise

- **F1 (minor, pre-existing)** `pkg/webhook/plugins/requirenodewithfuse/require_node_with_fuse.go` `Mutate()` still iterates the `runtimeInfos` map unsorted, so the *order of the per-runtime NodeSelectorTerms* stays random; the PR sorts only the MatchExpressions *within* each term. Scheduling semantics unchanged (terms are OR'ed). Not covered by a test on purpose (see above).
- **F2 (minor, pre-existing)** `pkg/dataflow/helper.go` (~line 50) iterates the pod annotation map unsorted when appending NodeSelectorRequirements — the same nondeterminism class, outside the scope of issue #6220's enumerated sites.
- **F3 (nit, PR-introduced)** `pkg/utils/affinity_test.go` `deterministic_order_over_repeated_calls` derives its expectation from the function under test, so it only detects cross-call randomness, not unsortedness; the `multiple_entries_sorted` table case is what actually pins the order.

## How to run

```bash
# from a checkout of this branch
go test -run 'TestHarness' ./pkg/webhook/plugins/requirenodewithfuse/ ./pkg/webhook/plugins/nodeaffinitywithcache/ ./pkg/ddc/efc/
go test -run 'TestHarnessPVNodeAffinityExpressionsSortedDeterministic' ./pkg/utils/dataset/volume/   # ~80s (PV creation path has a polling wait)
# or everything at once:
bash docs/verification/nodeselector-sort/scripts/re-verify.sh
```

## Continuing after the fix / next round

- Re-run: `bash docs/verification/nodeselector-sort/scripts/re-verify.sh` (no ref: fetches
  the current PR head via `git fetch origin pull/6234/head`), or pass an explicit ref.
- Expected outcomes on the current head: all 5 harness tests PASS. If a future revision
  regresses determinism, the corresponding contract test goes red directly — no canary
  to invert.
- Prerequisites: Go 1.21+ toolchain (repo go.mod), no cluster needed (L1 only).
- The manifest (verify-manifest.json) maps findings → tests → polarity → layer, and
  records the acceptance criteria and CI gate status.
- Kickoff prompt for a fresh agent: "Check out branch verify/nodeselector-sort-claude of
  cheyang/fluid, read docs/verification/nodeselector-sort/README.md and the manifest,
  run scripts/re-verify.sh against the current PR #6234 head, and report per finding
  Fixed / Still-broken / Partial / Harness-update."

## Harness-bites check

The differential run *is* the bites check for this PR: every contract test fails on the
unfixed base (multiple orderings observed) and passes on the PR head, so the tests
exercise exactly the changed code path. Production code is untouched on this branch
(diff is test files + docs only).
