# pr-6244-capacity-label-refresh — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6244
(`fix(lifecycle): refresh outdated capacity labels on cache nodes`, fixes #6204).
Reviewer: codex. Round 1: initial (head e0cba66ef9447434750a73be698402b4b958b893,
base ff99142f9ec5c32a0c9166de023cb8936f0e5ae5). Round 2: incremental re-verify of
head 7de88000e21b1e299d6f205f86057a177c5bf8fc (same base) — see "Round 2" below.

Layers run against the code under review (e0cba66):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | public `SyncScheduleInfoToCacheNodes` against fake client: stale-label refresh, tier-removal delete, no-churn steady state, cross-runtime isolation, add/remove regression guards | `go test -count=1 ./pkg/utils/dataset/lifecycle/ -run TestLifecycle -args -ginkgo.focus="PR6244 verification"` |
| 2. Integration | same public path against a REAL apiserver (envtest, k8s 1.31.0) via a manager cached client: proves strategic-merge-patch label set + null-value label DELETE, and no re-patch of a steady node (resourceVersion unchanged) | `KUBEBUILDER_ASSETS=<envtest assets> go test -count=1 ./pkg/utils/dataset/lifecycle/ -run TestPR6244Envtest -v` |
| 3. Live | skipped per round policy (no KUBECONFIG layer) | — |

> Polarity: every test is a CONTRACT test (asserts intended behavior). On base they FAIL
> (= premise reproduction), on the PR head they PASS. No bug-canaries.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "an existing CacheRuntime still shows fluid.io/s-h-cache-t-*: 0B with no mem/disk labels. It only clears once the worker moves off the node or the runtime is recreated." (issue #6204, quoted) |
| Linked issue | #6204, OPEN, filed 2026-10-09, label `bug`, still valid; references #6181 which fixed the *writer* but not already-labelled nodes |
| Reported component | CacheRuntime node capacity labels → cache engine → `pkg/utils/dataset/lifecycle` |
| Patched component | `pkg/utils/dataset/lifecycle/node.go` (`SyncScheduleInfoToCacheNodes`), used by the cache engine at `pkg/ddc/cache/engine/sync.go:122` and all other engines |
| Component match | Yes |
| **Verdict** | **Confirmed** — reproduced on base at both layers |
| Evidence | base unit: P0a/P0b FAIL while 4 control specs PASS (results/unit-base.txt); base envtest: 60s timeout waiting for stale labels to be refreshed (results/integration-base.txt). Head: all green (results/unit-head.txt, results/integration-head.txt). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | On base, an already-labelled node with stale capacity labels (t=0B from a pre-#6181 CacheRuntime) is never refreshed; a removed tier's label is never deleted | 1+2 | **Confirmed** | base: P0a/P0b FAIL, envtest timeout; head: PASS (results/) |
| F1 | The PR's wiring actually refreshes stale labels through the public `SyncScheduleInfoToCacheNodes` path (desired ∩ actual set, after applyNodeChanges) — the PR's own tests only call the private helper | 1 | Fix verified | unit-head.txt: P0a/P0b PASS on head, FAIL on base |
| F2 | Label deletion for a removed tier works against a real apiserver (strategic-merge patch with null values), not only against the fake client; steady nodes are not re-patched | 2 | Fix verified | integration-head.txt: mem label deleted, total recomputed, steady rv unchanged; integration-base.txt: refresh never happens |

No defect in the PR itself was found; F1/F2 are the verified behaviors of the fix.

## Per-finding detail

### P0 / F1 — stale capacity labels refreshed (unit)

Scenario: node already carries `fluid.io/s-alluxio-fluid-spark=true` and
`fluid.io/s-h-alluxio-t-fluid-spark=0B` (pre-#6181 state), worker pod on the node,
runtime has mem=1GiB + ssd=2GiB.

- base (ff99142): after `SyncScheduleInfoToCacheNodes`, labels unchanged — m/d labels
  absent, t stays `0B` → spec FAILS (expected 1GiB/2GiB/3GiB). Tier-removal spec FAILS
  likewise (stale mem label kept). All 4 control specs PASS, so the failures are
  specific to the missing refresh, not the harness.
- head (e0cba66): labels become `m=1GiB d=2GiB t=3GiB`; mem label deleted when the
  tier is dropped. 6/6 PASS.

### F2 — real apiserver semantics (integration, envtest k8s 1.31.0)

Same scenario through a manager **cached** client (exactly what engines use:
`e.Client` = manager client; a direct client strips TypeMeta after typed Gets, which
breaks `GetPodsForStatefulSet`'s owner-ref kind check — an artifact of direct clients,
verified separately, NOT a production bug).

- head: first sync refreshes the stale labels; second (steady) sync does NOT re-patch
  (resourceVersion unchanged); after switching the runtime to a disk-only tier, the
  mem label is deleted via a real strategic-merge patch and total becomes `2GiB`.
  Final labels: `map[fluid.io/s-alluxio-fluid-spark:true fluid.io/s-h-alluxio-d-fluid-spark:2GiB fluid.io/s-h-alluxio-t-fluid-spark:2GiB]`.
- base: 60s timeout — stale labels never refreshed → premise holds against a real
  apiserver too.

## Proposed fixes (NOT applied to production here)

None required — the PR's fix is verified working. Harness-bites check = the base/head
runs above (red on base, green on head; production code untouched by this branch).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr-6244-capacity-label-refresh-codex` on the
reviewer's fork (`https://github.com/cheyang/fluid.git`), production code untouched.

```bash
git fetch https://github.com/cheyang/fluid.git verify/pr-6244-capacity-label-refresh-codex
git checkout verify/pr-6244-capacity-label-refresh-codex
bash docs/verification/pr-6244-capacity-label-refresh/scripts/re-verify.sh   # auto-fetches current PR head
# or against an explicit ref:
bash docs/verification/pr-6244-capacity-label-refresh/scripts/re-verify.sh <ref>
```

- Integration layer needs envtest assets: set `KUBEBUILDER_ASSETS` (e.g.
  `setup-envtest use 1.31.0 -p path`) or the script defaults to
  `/home/ecs-user/envtest-assets/k8s/1.31.0-linux-amd64` if present.
- Exit 0 iff every finding is Fixed. `.last-reviewed` records the reviewed head;
  advance it after each round per the script's printed hint.
- Copy-paste kickoff for a fresh agent: "Run
  `bash docs/verification/pr-6244-capacity-label-refresh/scripts/re-verify.sh` from a
  checkout of `verify/pr-6244-capacity-label-refresh-codex`; then review
  `git diff $(cat docs/verification/pr-6244-capacity-label-refresh/.last-reviewed)..HEAD`."

## Round 2 — incremental (head 7de88000e, delta e0cba66e..7de88000e)

The delta is a single commit (`fix(lifecycle): skip excluded nodes when refreshing
capacity labels`): +5 LOC in `syncCapacityInfoOnNode` mirroring the
`addScheduleInfoToNode` nodeExcludeSelector guard verbatim, plus two entry-point
specs in the PR's own `node_test.go` (repair + zero-patch steady state; excluded
node untouched). This directly targets consensus findings F1 (selector bypass) and
F2 (no entry-point test) from round 1.

New harness specs (both CONTRACT, two desired nodes with stale 0B labels, one
matching `type=virtual-kubelet`):

| spec | contract | base ff99142 | previous e0cba66 | head 7de88000 |
|------|----------|--------------|------------------|---------------|
| E1-excluded-node-is-not-refreshed | excluded node byte-identical (labels + resourceVersion) | pass (no refresh exists) | **FAIL** (excluded node patched m=1GiB/d=2GiB/t=3GiB) | **pass** |
| E2-exclude-guard-does-not-skip-normal-nodes | non-matching sibling still refreshed in the same sync | fail (no refresh exists) | pass | pass |

E1/E2 split deliberately: a single combined spec fails on base for the sibling
reason, which would muddy the F1 differential. Full differential evidence:
results/unit-base-r2.parsed, results/unit-head-r2.parsed (previous-head E1 failure
reproduced on the harness branch, which is based on e0cba66e).

Re-verify verdicts on 7de88000e (`bash docs/verification/pr-6244-capacity-label-refresh/scripts/re-verify.sh 7de88000e21b1e299d6f205f86057a177c5bf8fc`):

| ID | verdict | detail |
|----|---------|--------|
| F1 (exclude bypass) | **FIXED** | E1 passes on head; guard is verbatim parity with addScheduleInfoToNode (node.go:106-109 vs :222-225) |
| F2 (entry-point coverage) | **FIXED** | P0a/P0b/E2/C1 pass on head; PR's own two entry-point specs pass on head (results/author-tests-head-r2.txt) and the exclude spec FAILS when the guard is reverted (bites check, results/author-tests-bites-r2.txt) |
| F3 (tier-drop deletion mechanics) | verified (unchanged) | P0b passes; the silently-dropped-level trigger stays code-path-only, deferred to the #6204 follow-up per PR body §IV |
| AC-compat | pass | C1-C4 green on head; full package suite green on 7de88000e (results/full-package-head-r2.txt) |
| AC-envtest | pass | TestPR6244Envtest against envtest k8s 1.31.0 (results/integration-head-r2.out) |

Author also filed #6246 (OPEN) for the pre-existing removeScheduleInfoFromNode
selector gap (round-1 nit N1) and documented the exclude-selector interaction in
the PR body. Production code untouched by this branch (harness + docs only).
