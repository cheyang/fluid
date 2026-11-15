# exclude-remove-skip — bug verification

Reproducible evidence for the review of [PR #6249](https://github.com/fluid-cloudnative/fluid/pull/6249)
(`fix(lifecycle): skip excluded nodes when removing schedule info`, fixes #6246).

Layers run against the **code under review** (`145a2c1334643913f827e0755e01d92b336ea7cd`, base
`ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (ginkgo + controller-runtime fake client, entry-point level) | `SyncScheduleInfoToCacheNodes` full chain: desired nodes from worker pods → actual nodes by runtime label → node differences → add/remove → label patches | `go test ./pkg/utils/dataset/lifecycle/... -run TestLifecycle -count=1` |
| 2. Integration (envtest) | not used — the package has no envtest convention and the claim is label bookkeeping fully exercised by layer 1; a real API server adds nothing here | — |
| 3. Live | not run this round (see live notes) | — |

> Test polarity: both harness specs are **contract** tests (assert intended behavior).
> `keeps the runtime label...` FAILS on the base branch (that red is the reproduction of
> issue #6246) and PASSES on the PR head. `still removes the runtime label when the selector
> does not match` PASSES on both — it guards the new check from over-matching.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is whether the problem
exists today.

| | |
|---|---|
| Claimed symptom | "`removeScheduleInfoFromNode` skips nodes that match `FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR`, but `removeScheduleInfoFromNode` doesn't check the selector, so an excluded node that has the runtime label still gets its Fluid labels removed when it leaves the cache node set." (issue #6246) |
| Linked issue | #6246, OPEN, filed 2026-11-15 (one day before review), still valid — no commit on master touches `removeScheduleInfoFromNode`'s selector handling |
| Reported component | `pkg/utils/dataset/lifecycle` node schedule-info sync (reached by every engine's `SyncScheduleInfoToCacheNodes`) |
| Patched component | same file/function (`removeScheduleInfoFromNode`) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | contract spec grafted onto base `ff99142` fails: the excluded node ends with labels `{type: virtual-kubelet}` only, i.e. the runtime label was removed — exactly the reported symptom. Same spec passes on head `145a2c13`. `results/base-run.txt`, `results/base-ginkgo-report.json`, `results/head-ginkgo-report.json` |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| issue-6246-removal-skip | A node matching the exclude selector keeps its runtime label when its worker pod leaves it (entry-point behavior through `SyncScheduleInfoToCacheNodes`) | 1 | Confirmed as a base-branch bug; fixed on head | base: spec `failed`; head: spec `passed` |
| issue-6246-no-false-skip | A configured selector that does NOT match the node does not block removal (guard against over-matching) | 1 | Holds on base and head | both runs: spec `passed` |
| control | The non-excluded node in the same sync run still gets its runtime label removed | 1 | Holds on base and head | asserted inside the same spec as issue-6246-removal-skip |

## Per-finding detail

### issue-6246-removal-skip (P0 / the PR's fix)

- Mechanism: on base, `applyNodeChanges` → `removeScheduleInfoFromNode` has no
  `nodeExcludeSelector` guard, so `unlabelCacheNode` patches the node and deletes the
  runtime/common/capacity/exclusive labels and decrements `dataset-num` even when the operator
  excluded the node via `FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR`. The add path
  (`addScheduleInfoToNode`, from #5285) has the guard; the removal path didn't.
- Scenario in the spec: two nodes get labelled while no selector is configured; the operator
  then sets `type=virtual-kubelet` as the exclude selector and the runtime scales off both
  nodes; the next sync must clean the normal node and leave the excluded node alone.
- Command: `go test ./pkg/utils/dataset/lifecycle/... -run TestLifecycle -count=1`
- Observed on base: `vk-node` labels reduced to `{"type": "virtual-kubelet"}` (labels removed — bug).
- Observed on head: `vk-node` keeps `fluid.io/s-alluxio-fluid-hbase=true`; `node-0` is cleaned.
- Artifacts: `results/base-run.txt` / `results/head-run.txt` and both ginkgo JSON reports.

### issue-6246-no-false-skip

- Same entry point, selector set to `type=something-else` (non-nil, non-matching): the removal
  path must still run. Passes on base and head, so the new guard's `Matches` false branch is
  covered and does not swallow ordinary removals.

## Live run notes

L3 was not run this round: the claim is pure label bookkeeping on Node objects, deterministic
and fully covered by layer 1 against the fake client; a live cluster would re-exercise the same
code path with more latency. If a live check is ever wanted: set
`FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR` on the controller, label a node before excluding it,
scale the runtime down, and confirm the node's `fluid.io/s-*` labels survive while a normal
cache node's are removed.

## Proposed fixes (NOT applied to production here)

- The PR head itself is the fix; no additional fix proposed. One coordination note: the sibling
  open PR #6244 (capacity label refresh, not yet in master) carries the same guard for its new
  `syncCapacityInfoOnNode` path, so after both merge the exclusion is consistent across
  add/remove/capacity. Nothing to do in #6249 itself.

## Harness-bites check

Done implicitly by construction: the contract spec is red on base (bug present) and green on
head (bug fixed) — head = base + exactly this PR's patch, so the "apply the proposed fix and
watch the contract go green" step is the head run itself. Production code in the harness branch
is untouched (`git diff 145a2c13 -- pkg/` is empty).

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/exclude-remove-skip-claude` (production code untouched), so
it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/exclude-remove-skip-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/exclude-remove-skip pkg/utils/dataset/lifecycle/node_pr6249_verify_test.go
   ```
2. Prereqs: Layer 1 = Go toolchain only (module cache warm after first `go mod download`).
3. Re-run: `bash docs/verification/exclude-remove-skip/scripts/re-verify.sh` (no argument: it
   fetches the current PR head from the manifest's `pr` URL, grafts the harness, runs layer 1,
   and prints Fixed / Still-broken per finding; exit 0 iff all contracts pass).
4. Read results via the polarity table: both specs are contracts — they should be GREEN on any
   revision that keeps the fix; a red `keeps the runtime label...` spec means the skip regressed.
5. Harness-bites: re-run once against a pre-fix ref (e.g. `ff99142`) to confirm the contract
   still goes red there.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/exclude-remove-skip-claude
(remote https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6249 (fixes #6246) confirmed the base-branch
bug "removeScheduleInfoFromNode ignores FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR" with a
contract spec; the PR head fixed it. Read docs/verification/exclude-remove-skip/README.md
("Continuing after the fix"), run scripts/re-verify.sh (no args) against the current PR head,
confirm both contract specs pass, and report the observed-vs-expected table. Clean up scoped
test resources; do not run cluster-wide destructive actions; do not merge, approve or label.
```
