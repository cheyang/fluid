# Verification — PR #6249 — skip excluded nodes when removing schedule info

Reviewer: Codex (Reviewer B). PR: https://github.com/fluid-cloudnative/fluid/pull/6249
Fixes: https://github.com/fluid-cloudnative/fluid/issues/6246
Base: `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5` — Head reviewed: `eb313c44a08c63f0eb482e8a6c09330927e8246a` (round 2; round 1: `145a2c1334643913f827e0755e01d92b336ea7cd`)

## Premise verdict (P0): CONFIRMED

Issue #6246 claims `removeScheduleInfoFromNode` ignores the
`FLUID_SCHEDULE_INFO_EXCLUDE_NODE_SELECTOR` exclusion that `addScheduleInfoToNode`
honors, so an excluded node holding the runtime label still gets its Fluid labels
stripped when its worker leaves. The contract spec **C1 fails on the base branch**
(the excluded node's `fluid.io/s-alluxio-fluid-hbase=true` label is removed) and
**passes on the PR head**. Component named by the issue == component patched
(`pkg/utils/dataset/lifecycle/node.go`).

## Hypotheses

| id | claim | polarity | layer |
|----|-------|------|-------|
| P0 | base `removeScheduleInfoFromNode` strips labels on excluded nodes | contract (red on base) | unit |
| F-fix | head: excluded node keeps ALL labels; non-excluded node still loses fluid labels | contract (green on head) | unit |

Harness: `pkg/utils/dataset/lifecycle/node_exclude_remove_verify_test.go` (specs C1, C2,
focused via `PR6249ExcludeSelectorRemoveHarness`). Additive only; production code untouched.

## How to run

Prereqs: Go 1.26+ toolchain (repo pins toolchain go1.26.10), network for module cache.

```bash
# On the verify branch (harness + PR head): both specs PASS
FLUID_UNIT_TEST=true go test ./pkg/utils/dataset/lifecycle/ -count=1 -run TestLifecycle \
  -ginkgo.focus=PR6249ExcludeSelectorRemoveHarness

# Against base (premise reproduction): C1 must FAIL, C2 must PASS
git worktree add /tmp/fluid-base-6249 ff99142f9ec5c32a0c9166de023cb8936f0e5ae5
cp pkg/utils/dataset/lifecycle/node_exclude_remove_verify_test.go \
   /tmp/fluid-base-6249/pkg/utils/dataset/lifecycle/
cd /tmp/fluid-base-6249 && FLUID_UNIT_TEST=true go test ./pkg/utils/dataset/lifecycle/ \
  -count=1 -run TestLifecycle -ginkgo.focus=PR6249ExcludeSelectorRemoveHarness

# Re-verify a future fix round (from a checkout of this branch; pass --manifest
# explicitly because this shared clone hosts other topics' manifests):
bash docs/verification/pr-6249-exclude-selector-remove/scripts/re-verify.sh \
  --manifest docs/verification/pr-6249-exclude-selector-remove/verify-manifest.json
```

## Observed vs expected

| run | expected | observed | evidence |
|-----|----------|----------|----------|
| C1 on base ff99142f | FAIL (bug reproduces) | FAIL — excluded node kept only `type=virtual-kubelet`; runtime label removed | results/base-harness-focused.txt |
| C2 on base ff99142f | PASS (guard, unchanged behavior) | PASS | results/base-harness-focused.txt |
| C1+C2 on head 145a2c13 | PASS | PASS (2/2) | results/head-harness-focused.txt |
| PR's own new specs on head | PASS | PASS (focus `Test removeScheduleInfoFromNode`) | results/head-pr-own-tests.txt |
| Full lifecycle package on head | PASS | PASS (26 specs, 0 failed) | results/head-full-package.txt |

Harness-bites check: identical harness flips C1 red→green exactly at base→head, and C2
passes on both, so the harness exercises the removal path and is not vacuously green.

## Notes for the maintainer (not code defects)

- Acknowledged trade-off (PR body §Ⅴ): while a node matches the selector, its pre-existing
  Fluid labels are never cleaned, so the `NodeAffinityWithCache` webhook keeps treating it
  as cache-holding. Consistent with the original feature intent of #5285 ("avoid syncing
  schedule info to nodes that match the selector" — sync is bidirectional). Worth one
  sentence in docs; not a merge blocker in my judgment.
- No live/envtest layer needed: the change is a pure client-side selector match around a
  label patch; the fake-client unit layer covers the decision boundary exactly.

## Handoff / continuing after a fix

The harness is a contract suite for this behavior: after any future change to
`removeScheduleInfoFromNode`, re-run it — both specs must stay green (no canary to
invert). `re-verify.sh` (above) grafts the harness onto the current PR head
auto-discovered from `manifest.pr` and prints FIXED / STILL-BROKEN per finding.
`.last-reviewed` records the reviewed head for the next incremental round.

## Round 2 — incremental review of 145a2c13..eb313c44 (2026-11-17)

Head advanced to `eb313c44a08c63f0eb482e8a6c09330927e8246a` (one test-only commit:
`nodeExcludeSelector = nil` pinned in the removal Describe's BeforeEach, a new
non-matching-selector spec, and a new entry-point spec driving
`SyncScheduleInfoToCacheNodes` with an excluded node plus a non-excluded control).

| check | result | evidence |
|-------|--------|----------|
| re-verify.sh (C1/C2 grafted onto eb313c44) | F-fix FIXED (pass=2 fail=0) | results/reverify/unit.out |
| full pkg/utils/dataset/lifecycle suite on head | 26/26 pass (prev head: 24) | results/round2-head-full-package.txt |
| harness-bites on the PR's own new specs: base node.go + head node_test.go | the entry-point spec and the matching-selector spec FAIL on base, non-matching specs PASS (expected polarity) | results/round2-base-pr-own-specs.txt |

Round-1 findings resolved by this delta: F-MT-ENTRY-01 (entry-point coverage now
in the PR) and F-NIT-SELECTOR-STATE-01 (BeforeEach pin + non-matching selector
case) are both verified fixed. F-QR-STALE-LABELS-01 (maintainer semantic
question) and the pre-existing e2e-coverage gap are unchanged by a test-only delta.
