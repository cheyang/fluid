# Verification — PR #6249 — skip excluded nodes when removing schedule info

Reviewer: Codex (Reviewer B). PR: https://github.com/fluid-cloudnative/fluid/pull/6249
Fixes: https://github.com/fluid-cloudnative/fluid/issues/6246
Base: `5e6473599f78263a49e521854d80f918a1a10bb0` (round 3 rebase; rounds 1-2: `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`) — Head reviewed: `07ff473a41006de5c688580e40ada8edadd0d544` (round 5; round 4: `b1d9294cc8f2398e7d09187783f849d1dfc057fa`; round 3: `964c8ef2230a4eb18ec5c440884f40097eac2079`, round 2: `eb313c44a08c63f0eb482e8a6c09330927e8246a`, round 1: `145a2c1334643913f827e0755e01d92b336ea7cd`)

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

## Round 3 — incremental review of eb313c44..964c8ef22 (2026-11-18)

The PR was rebased (`ff99142f` -> `5e6473599`, which pulls in #6244's capacity-refresh
path with its own exclude-selector check) and gained one commit `964c8ef22`: the three
inline selector checks (add / capacity refresh / removal) are funnelled into a shared
`isNodeExcluded(nodeLabels, nodeName, caller)` helper with byte-identical log strings,
plus comments on `Helper.TearDownWorkers` (pkg/ctrl/worker.go) and
`runtime.syncScheduleInfoNodeExcludeSelector` (charts/fluid/fluid/values.yaml)
documenting that labels left on a node before it was excluded stay until the runtime is
deleted — the operator-facing note round 2 asked for. The four PR-owned specs are
byte-identical to round 2 (pure rebase). Because the base moved, the premise and the
bites check were re-run against the NEW base.

| check | result | evidence |
|-------|--------|----------|
| re-verify.sh (C1/C2 grafted onto 964c8ef22) | F-fix FIXED (pass=2 fail=0); persisted report shows the 33-spec grafted suite | results/reverify/unit.out, results/reverify/ginkgo-report.json |
| full pkg/utils/dataset/lifecycle suite on head | 31/31 pass (new base 27 + 4 PR-owned) | results/round3-head-full-package.txt |
| premise on NEW base: C1/C2 grafted onto 5e6473599 | C1 FAILS (bug still reproduces), C2 PASSES | results/round3-base-harness.txt |
| bites on NEW base: base node.go + head node_test.go | 2 selector-matching specs FAIL, 2 control specs PASS | results/round3-base-pr-own-specs.txt |
| refactor equivalence | all `nodeExcludeSelector.Matches` call sites route through `isNodeExcluded`; guard order (Get -> exclude -> hasRuntimeLabel) unchanged; `go build ./pkg/ctrl/... ./pkg/utils/dataset/...` OK | this table, head tree inspection |

F-QR-STALE-LABELS-01: the semantic question stays open for the maintainer (it asks for
product confirmation, not docs), but its SHOULD item — the operator-facing sentence —
now exists at charts/fluid/fluid/values.yaml:88, and the worker.go comment pins down why
TearDownWorkers must NOT check the selector. CI on 964c8ef22 is all green (incl. the
kind-e2e and backward-compat matrices); PR open, mergeable.

Harness note (infrastructure, not a PR issue): this branch's re-verify.sh persisted
results into the repo BEFORE its exit trap ran `git checkout -f` back to the branch,
so tracked results/reverify/* files were silently reverted to their committed content
each run (the verdicts were always computed from the fresh temp-dir report and were
correct; only the persisted artifacts were stale). Commit "fix re-verify.sh persist
vs cleanup ordering" backs the fresh results up and restores them after the checkout;
the round-3 reverify/ artifacts are the first genuinely fresh ones.

## Round 4 — incremental review of 964c8ef22..b1d9294cc (2026-11-19)

One commit, test-only: `b1d9294cc test(lifecycle): cover the exclude selector on the add path`
adds `Describe("Test addScheduleInfoToNode()")` to node_test.go (node_test.go:785) with two
specs — `should only label the node that doesn't match the exclude selector` (:806) and
`should label every node without an exclude selector` (:821) — adopting the round-3 nit
F-NIT-ADD-PATH-COVERAGE-01 in the PR itself. No production-code change in the delta.

| check | result | evidence |
|-------|--------|----------|
| re-verify.sh (C1/C2 grafted onto b1d9294cc) | F-fix FIXED (pass=2 fail=0) | results/reverify/unit.out, results/reverify/ginkgo-report.json |
| full pkg/utils/dataset/lifecycle suite on head | 33/33 pass (round-3's 31 + 2 new add-path specs) | results/round4-head-full-package.txt |
| polarity on base: base 5e6473599 + head node_test.go | 2 removal-guard specs FAIL (bug still reproduces), BOTH new add-path specs PASS (correct: the add guard predates this PR, so these pin pre-existing behavior and must never be red on base) | results/round4-base-pr-own-specs.txt |
| bites: head node.go minus the isNodeExcluded call in addScheduleInfoToNode | `should only label the node that doesn't match the exclude selector` FAILS; nil-selector spec PASSes; tree reverted clean | results/round4-add-guard-bites.txt |

F-NIT-ADD-PATH-COVERAGE-01 is fixed by this delta: dropping the add-path guard now fails a
spec in the PR's own test file (verified by the bites run, matching the author's comment
2026-11-18T03:11:17Z). The pre-existing e2e-coverage gap
(codex-pr6249-no-e2e-exclude-selector) is unchanged — still nothing under test/ or
integration/ references the selector env. CI on b1d9294cc: 22/22 SUCCESS (incl. kind-e2e
and backward-compat x5), PR open, mergeable.

## Round 5 — incremental review of b1d9294cc..07ff473a4 (2026-11-22)

One commit, test-only: `07ff473a4 test(lifecycle): drive both nodes in the nil exclude
selector spec` rewrites the body of `should label every node without an exclude selector`
(node_test.go:821) to loop over both fixture nodes (`excluded-node`, `real-node`), driving
addScheduleInfoToNode on each and asserting each gains the runtime label (+6/-4, no
production change). This adopts the round-4 nit F-NIT-ADD-SPEC-TITLE-01 by driving both
nodes — the title now matches the spec's scope. `git diff b1d9294cc..07ff473a4 --
pkg/utils/dataset/lifecycle/node.go pkg/ctrl/worker.go charts/fluid/fluid/values.yaml`
is empty, so the round-3/4 production-code trace carries verbatim.

| check | result | evidence |
|-------|--------|----------|
| re-verify.sh (C1/C2 grafted onto 07ff473a4) | F-fix FIXED (pass=2 fail=0 miss=0) | results/reverify/unit.out, results/reverify/ginkgo-report.json |
| full pkg/utils/dataset/lifecycle suite on pure head | 33/33 pass (spec count unchanged; the delta rewrites one spec body) | results/round5-head-full-package.txt |
| polarity + premise on base: base 5e6473599 + head node_test.go | 2 removal-guard specs FAIL with the exact #6246 symptom (excluded node reduced to {type: virtual-kubelet}); the round-5 nil-selector spec PASSes (correct: nil-selector labeling predates this PR via #5285); 4 Passed \| 2 Failed in the focused run | results/round5-base-pr-own-specs.txt |
| bites: head node.go minus the isNodeExcluded call in addScheduleInfoToNode | `should only label the node that doesn't match the exclude selector` FAILS; the round-5 nil-selector spec PASSes; tree reverted clean, `go build ./pkg/ctrl/... ./pkg/utils/dataset/...` OK | results/round5-add-guard-bites.txt |

F-NIT-ADD-SPEC-TITLE-01 is fixed by this delta (verified by execution: the spec body now
drives both nodes; green on head; correct green polarity on base). The author's comment
2026-11-19T06:35:52Z ("thx, the nil selector spec drives both nodes now in 07ff473")
matches the diff. Residual cosmetic note from the nit (the fixture node is still named
`excluded-node` in a nil-selector spec) is unchanged and stays trivial. The pre-existing
e2e-coverage gap (codex-pr6249-no-e2e-exclude-selector) is unchanged — the delta touches
no test/ or integration/ path. CI on 07ff473a4: 22/22 pass (gh pr checks), PR open,
mergeable; no maintainer activity after 2026-11-18T23:41:43Z redefines the confirmed
stale-label semantic.
