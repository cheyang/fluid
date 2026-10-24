# Verification — PR #6217: sort nodeSelector keys in MergeNodeSelectorAndNodeAffinity

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6217
- **Reviewer:** Claude (Reviewer A, debate round 2026-10-24)
- **Head reviewed:** `4fd9701c5f21dffb23fc4e37de64d430c15f4c3e`
- **Base (merge-base):** `86e3adf35a7c26875e818edad3e01dfac15f0290`
- **Harness branch:** `verify/sort-nodeselector-determinism-claude`

## Premise verdict (P0): CONFIRMED

Issue #6216 claims that `MergeNodeSelectorAndNodeAffinity` emits `MatchExpressions`
in randomized map-iteration order, so `status.CacheAffinity` fluctuates across
reconcile cycles and the `reflect.DeepEqual` status gate in
`pkg/ddc/alluxio/status.go:120` fires a spurious status write on every cycle
for multi-label workers.

Reproduced on the merge-base **without** the patch (`results/base-premise-p0.out`):

- All 4 contract tests FAIL on base; each FAIL is the symptom, not a harness error.
- `TestPR6217StatusComparisonStableContract` simulates the controller's exact
  decision point (`persisted` vs freshly computed affinity under
  `reflect.DeepEqual`) and fails on cycle 0 — a spurious status diff exists
  whenever the order flips between two invocations.
- Diagnostic `TestPR6217ObserveOrderVariance`: **6 distinct key orderings
  observed across 1000 invocations on base** vs **exactly 1 on the fixed head**
  (`results/head-contract.out`).

Component match: the issue names `pkg/utils/kubeclient/pod.go` and
`status.CacheAffinity`/`WorkerNodeAffinity`; the PR patches exactly that
function, and the callers (`pkg/ddc/{alluxio,jindo,jindofsx,jindocache,juicefs,thin,vineyard}/status.go`,
`pkg/ddc/cache/component/{daemonset,advanced_statefulset}_manager.go`) all
consume it in the status path. Valid, open issue (#6216, created 2026-10-21),
symptom and mechanism match.

## Hypotheses / claims

| id | claim | polarity | layer |
| --- | --- | --- | --- |
| P0 | premise: order is nondeterministic on base → spurious DeepEqual status diffs | contract (asserts correct behavior; red on base) | unit |
| F1 (fix) | sorted keys → stable order and DeepEqual-stable status across invocations | contract | unit |
| F1-coverage-append-path | determinism also holds on the append path (worker spec already carries NodeSelectorTerms) — **not covered by the PR's own tests** | contract | unit |

## Observed vs expected

| claim | expected | observed | verdict |
| --- | --- | --- | --- |
| P0 on base 86e3adf3 | symptom reproduces | 4 contract tests FAIL; 6 distinct orderings/1000 calls; spurious DeepEqual diff at cycle 0 (`results/base-premise-p0.out`) | **Confirmed** |
| F1 on head 4fd9701c | all contract tests pass | 4/4 PASS; 1 distinct ordering/1000 calls (`results/head-contract.out`) | **Fixed** |
| F1 append path on head | DeepEqual-stable | PASS (`results/head-contract.out`, TestPR6217AppendPathDeterministicContract) | behavior correct; **coverage gap in PR's own suite** (see findings doc) |
| PR's own ginkgo specs on head | pass | 8/8 specs pass, incl. the two new ones (`results/integration-head.json`) | PASS |
| Full kubeclient package on head | pass | `ok ... 0.210s` (`results/head-full-package.out`) | PASS |
| Harness bites | tests red with fix reverted, green with fix restored | red when the sort is temporarily reverted, green after restore; production diff empty after restore (`results/harness-bites-fix-reverted.out`, `results/harness-bites-restored.out`) | **Harness proven to bite** |
| Caller blast radius | no new failures | `pkg/ddc/alluxio` (2 specs) and `pkg/ddc/cache/engine` (1 spec) fail **identically on base and head** — pre-existing, unrelated (metadata sync / engine suite); thin, cache/component pass (`results/callers-blast-radius.out`) | no PR-caused regressions |

## How to run

Unit layer (contract tests + diagnostic):

```bash
go test ./pkg/utils/kubeclient/ -run 'TestPR6217' -v -count=1
```

Integration layer (the PR's own ginkgo specs, JSON report):

```bash
go test ./pkg/utils/kubeclient/ -run TestKubeclient \
  -ginkgo.focus 'MergeNodeSelectorAndNodeAffinity' \
  -ginkgo.json-report=/tmp/ginkgo-report.json -count=1
```

One-shot re-verification of the whole set against the current PR head:

```bash
bash docs/verification/sort-nodeselector-determinism/scripts/re-verify.sh
```

(no argument: fetches the current PR head via `manifest.pr`; prints per-finding
Fixed / Still-broken / Partial / Harness-update.)

## Polarity

All tests are **contract** tests: they assert the intended behavior and are red
on buggy code. There are no bug-canaries, so "all green" == "fixed" here with
no inversions needed. `TestPR6217ObserveOrderVariance` is a diagnostic that
never fails; it logs the count of distinct orderings (base: 6, fixed: 1) so the
asymmetry stays visible in raw output.

## Continuing after the fix

The fix is already in the PR head. To re-check on a future head (or after
review feedback changes the code):

```bash
git clone https://github.com/cheyang/fluid.git && cd fluid   # or any clone with this branch
git checkout verify/sort-nodeselector-determinism-claude
bash docs/verification/sort-nodeselector-determinism/scripts/re-verify.sh
```

The script grafts `harnessPaths` onto the fetched PR head, runs the unit +
integration layers, and reports per finding. If the function is renamed or the
harness no longer compiles, expect `HARNESS-UPDATE` — adjust the test file, re-run.

Kickoff prompt for a fresh agent:

> You are resuming a verification harness for fluid PR #6217 (issue #6216,
> nondeterministic MatchExpressions order from map iteration). Check out branch
> verify/sort-nodeselector-determinism-claude, read
> docs/verification/sort-nodeselector-determinism/README.md, run
> scripts/re-verify.sh with no argument, and report per-finding verdicts
> (Fixed / Still-broken / Partial / Harness-update). All tests are contract
> polarity — green means fixed. Then review the last-reviewed..head delta
> printed by the script and advance .last-reviewed if you reviewed it.

## Notes / scope

- The pre-existing caller failures (`pkg/ddc/alluxio` metadata sync specs,
  `pkg/ddc/cache/engine` TestCacheEngine) fail identically on the merge-base —
  they are not attributable to this PR.
- A related, **pre-existing** nondeterminism of the same class remains in the
  webhook consumer (`pkg/webhook/plugins/nodeaffinitywithcache`): it iterates
  `runtimeInfos` / `preferredRuntimes` maps when building injected node
  selector terms, so multi-dataset pods can get terms in varying order across
  admissions. Out of scope for this PR (introducedByPR: false); see the
  findings document.

## Round 2 (2026-10-25, head f5bb49a3) — incremental re-verification

- **Delta `4fd9701c..f5bb49a` is test-only:** +84 lines in `pkg/utils/kubeclient/pod_test.go`;
  `pod.go` (production) is byte-identical to the previous head. The author amended the
  same commit to add the append-path coverage requested in round 1 (F1-coverage-append-path):
  a `Entry("multi-label node selector appended to existing node affinity terms sorted
  deterministically", …)` in the DescribeTable and a 100-invocation
  `It("…when appending to existing terms across repeated calls", …)`.
- **Base movement `86e3adf3 → e5251b39` is master-only** (dependabot bump of
  codeql-action in `.github/workflows/codeql-analysis.yml` and
  `.github/workflows/sonarcloud.yaml`); the PR branch was **not** rebased —
  merge-base with master is still `86e3adf3`, so no contract re-baselining was needed.
- **Re-verify on head f5bb49a3:** unit layer 5/5 contract tests PASS
  (`results/reverify/unit.out`); integration layer 10/10 focused specs PASS — the
  PR's own suite now runs **10** focused specs (was 8), including both new ones
  (`results/reverify/ginkgo-report.json`, `results/reverify/integration.parsed`).
  Verdict: F1-coverage-append-path **FIXED**.
- **Differential — the new tests bite:** head's `pod_test.go` grafted onto the
  merge-base `86e3adf3` production code → both new specs FAIL on base and PASS on
  head (`results/r2-base-bite.out`, raw `results/r2-base-ginkgo-focus.json`):
  `multi-label node selector appended to existing node affinity terms sorted
  deterministically` and `…appending to existing terms across repeated calls` both
  fail on base, alongside the round-1 specs.
- **Full package + vet on head:** `go test ./pkg/utils/kubeclient/ -count=1` → ok;
  `go vet` clean (worktree `/tmp/pr6217-recheck`).
- **CI at head f5bb49a:** every concrete check SUCCESS (unittest, staticcheck, lint,
  Analyze, CodeQL, SonarCloud, codecov, builds, kind-e2e-test and
  backward-compat-test across k8s v1.22.17–v1.33.2); PR OPEN, not draft, MERGEABLE.
- **Harness fix this round:** `scripts/re-verify.sh` persisted raw output into the
  tree *before* the exit-trap `git checkout -f` restored the reviewer branch, which
  reverted tracked result files to their stale committed versions. Persist now runs
  inside cleanup after the restore (commit on this branch). This was an
  evidence-persistence bug only — verdicts were always computed from the fresh
  temp-dir output.
- Round-1 follow-ups closed by the author: PR body no longer mentions the
  nonexistent `status.WorkerNodeAffinity` (says `status.CacheAffinity`); the
  pre-existing unsorted map-range sites are now tracked in issue #6220.
- `.last-reviewed` advanced to `f5bb49a31169236b0817bcd9d989bc9567ff33a3`.
