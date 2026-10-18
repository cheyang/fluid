# Verification — PR #6177 — merge CacheRuntime resources onto the CacheRuntimeClass baseline

Reviewer: Claude (Reviewer A). Branch: `verify/cache-resources-merge-claude`, based on PR head `c1c303e8` (round 1) and re-verified against `e259d738` (round 2).
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

## Round 2 — delta `c1c303e8..e259d738`

The PR added 7 commits after round 1. The production change is `ecd41afa`
("clear a resource neither the CacheRuntime nor its template declares"):
`desiredComponentResources` no longer returns nil, and the worker's
`GetPodSpec`/tiered-store-quota read-back became unconditional. Everything else is
tests, docs, and the curvine e2e case the first round asked for.

All round-1 findings B1–B5 re-verified **FIXED** on `e259d738`
(`scripts/re-verify.sh`, see `results/reverify/`). Full engine suite: 292 passed /
12 failed at head vs 275 / 12 at the previous head — the failing set is
byte-identical (pre-existing `ufs_test.go` failures), so no revision regression.

New harness probes (`verify_delta_6177_test.go`):

| id | claim | polarity | verdict | evidence |
| --- | --- | --- | --- | --- |
| D1 | Neither side declaring resolves to a non-nil empty desired (the nil "leave alone" is gone; undeclared keys are cleared) | contract | **Confirmed** — fails on `c1c303e` (nil return), passes on `e259d738`; the intended semantic change, now pinned | `results/round2/unit-harness-{previous-head,head}.log` |
| D2 | The merge can synthesize requests > limits from individually valid inputs (template requests.cpu=1/limits.cpu=2, CacheRuntime limits.cpu=500m), and `syncRuntimeSpec` applies that combination to the workload with no error and no event | **canary** | **Still present** (open finding F6 in the round-2 ledger) | `TestD2MergeSynthesizesRequestsAboveLimits` passes at head: workload ends with requests.cpu=1 above limits.cpu=500m, sync returns nil. On the merge base the same input drops the requests instead (the #6173 bug), so the invalid combination is introduced by this PR's merge. The Kubernetes API server rejects pods whose requests exceed their limits, so on a real cluster this input now sticks the rollout with no event on the CacheRuntime. |
| D3 | An unconstrained container with a memory-medium tiered-store volume stays unconstrained and unpatched by sync (the now-unconditional quota read-back must not invent a memory request) | contract | **Confirmed** | `TestD3QuotaWithoutBaselineStaysUnconstrained` — resources stay nil, resourceVersion unchanged |

`.last-reviewed` advanced to `e259d738`.

## Harness layout

- `../../pkg/ddc/cache/engine/verify_premise_6177_test.go` — plain Go contract tests
  (P0 premise reproduction, P1 creation/sync equivalence, P2 claims survival).
- `../../pkg/ddc/cache/engine/verify_delta_6177_test.go` — round-2 tests (D1 nil-removal
  semantics, D2 requests-above-limits canary, D3 quota read-back boundary).
  Runs with the package's normal suite: `FLUID_UNIT_TEST=true go test ./pkg/ddc/cache/engine/ -run 'TestP0|TestP1|TestP2|TestD1|TestD2|TestD3' -v`.
- `results/` — captured raw output:
  - `unit-base.log` / `unit-head.log` — canaries at merge base vs PR head.
  - `suite-summary.log` — full engine suite base vs head (274→287 specs, 262→275 passed;
    the 12 failures are byte-identical pre-existing failures in `ufs_test.go` /
    `sync_test.go`'s ReportSummary spec, present at base, unrelated to this PR), plus
    `pkg/ddc/cache/component` (ok).
- `scripts/re-verify.sh` — re-run against a future PR head: `bash scripts/re-verify.sh`
  (auto-fetches the current head via `manifest.pr`).

## Polarity

P0/P1/P2, B1–B5, D1 and D3 are **contract** tests: green at head means fixed, red means
broken. **D2 is a bug-canary**: it passes while the unvalidated merge is the current
behavior; if the controller starts validating (or refusing) requests-above-limits
combinations, it flips to fail and must then be inverted or promoted to a contract test.

## Continuing after the fix

From a checkout of this branch on any machine:

```bash
bash docs/verification/cache-resources-merge/scripts/re-verify.sh
```

It fetches the current PR head from the `pr` URL, grafts `harnessPaths` onto it, runs the
unit + integration layers, and prints per-finding Fixed / Still-broken / Partial. Exit 0
iff all findings are fixed (D2 keeps it non-zero until the requests-above-limits finding
is resolved). `.last-reviewed` holds `e259d738` — advance it after the next review round.

## Live layer (L3) — skipped

Skipped by the debate configuration (unit + integration mandated for this round). The
environment probe did find a reachable cluster (1 node, v1.36.2-aliyun.1); it was left
untouched. See `liveNote` in `verify-manifest.json` for the exact manual procedure if the
live check is wanted later.

## Round 3 — delta `e259d738..497bbeaf`

The PR fixed the round-2 major (ledger F6) in commit `f02054bd` ("reject merged
resources whose request exceeds its limit") and re-signed the history (all commits
now carry `Signed-off-by`; the DCO gate is green at head). The mechanism:

- `validateResourceRequirements` (transform_common.go) reports every key whose
  request exceeds its limit, sorted for deterministic messages.
- Creation path: `transformMaster`/`transformWorker`/`transformClient` call
  `validateComponentResources` right after `transformComponentPodTemplate`, so a
  conflicting pair fails setup loudly with the component and keys named.
- Sync path: `validDesiredResources` (sync.go) wraps `desiredComponentResources`
  for master and worker; on conflict it returns nil — `SyncComponentSpec` skips the
  resource patch (image and replicas still sync) — and emits a
  `RuntimeResourcesConflict` Warning event on the CacheRuntime.

The validation sits *before* the tiered-store quota charge by design:
`withTieredStoreMemoryQuota` adds the quota to the request and the limit
symmetrically, so it cannot break requests ≤ limits.

Harness updates this round (all in `verify_delta_6177_test.go` /
`verify_delta_r3_6177_test.go`; production code untouched):

| id | claim | polarity | verdict | evidence |
| --- | --- | --- | --- | --- |
| D2 | A conflicting merge is refused: sync returns nil, the workload keeps its current resources, and a `RuntimeResourcesConflict` Warning event names the pair | contract (inverted canary) | **Fixed** — the round-2 canary flipped when `f02054bd` landed, so it was inverted into this contract; red on `e259d738`, green on `497bbeaf` | `results/round3/summary.txt` |
| D4 | The conflict does not abort the component: worker replicas still sync while the resource patch is skipped and the event names the memory pair | contract | **Confirmed** — red on `e259d738`, green on `497bbeaf` | `TestD4SyncConflictSkipsResourcesButSyncsReplicas` |
| D5 | The creation path fails loudly on a conflicting pair (`invalid worker resources: memory request 2Gi exceeds limit 1Gi`) | contract | **Confirmed** — red on `e259d738`, green on `497bbeaf` | `TestD5CreationRejectsConflictingPair` |
| D6 | The master call site in `syncRuntimeSpec` shares the same skip + event + replica behavior | contract | **Confirmed** | `TestD6MasterSyncConflictSkipsResources` |

All B1–B5/D1/D3 contracts from earlier rounds stay green at head (the fix regressed
nothing). Full engine suite with the harness grafted: 298 passed / 12 failed, the
failing specs byte-identical to the pre-PR base `e0fc4c18` (pre-existing
`ufs_test.go` failures) — no revision regression.

Changed base: master moved `e0fc4c18 -> 3e99063a`. A local test merge of
`497bbeaf` + `3e99063a` compiles, passes the harness and the full suite (311/12,
same failure set); the overlapping files (`sync.go`, curvine `test.sh`, the
spec-update docs) merge without semantic conflict.

`.last-reviewed` advanced to `497bbeaf`.

## Round 4 — delta `497bbeaf..3adc547d`

The delta is a single commit, `3adc547d` ("require equal requests and limits for
non-overcommittable resources") — the round-3 F9 remedy. `validateResourceRequirements`
now rejects, for `hugepages-*` and extended (domain-qualified) resources, a request that
differs from its limit or has no limit at all, via a local `isOvercommitAllowed` mirror.
The mirror was checked verbatim against
`k8s.io/kubernetes@v1.29.15 pkg/apis/core/v1/helper/helpers.go`
(`IsNativeResource && !IsHugePageResourceName`, where `IsNativeResource` = no `/` or
containing `kubernetes.io/`) and against `ValidateResourceRequirements`
(`pkg/apis/core/validation/validation.go:6568-6578`) — including the upstream quirk that
a `kubernetes.io/`-prefixed qualified name still counts as native (the PR pins that
behavior in its own spec, matching upstream). The hugepages-need-cpu-or-memory
cross-key rule is provably merge-safe and correctly left out: the merge preserves the
union of the per-map keys (`mergeResourceList` never removes), so whenever either input
carries hugepages it also carries cpu/memory, and that key survives the merge.

New harness probes (`verify_delta_r4_6177_test.go`, all compile on 497bbeaf too, so the
differential runs use one file):

| id | claim | polarity | verdict | evidence |
| --- | --- | --- | --- | --- |
| D7 | The validator mirrors the API server's non-overcommittable rules (request == limit; a request needs a limit; equal quantities spelled differently accepted; limit-only accepted; native and kubernetes.io-prefixed requests below limits still accepted) | contract | **Fixed** — red on `497bbeaf` (4 subtests), green on `3adc547d` | `results/round4/unit-d7d8e1e2-{prev,head}*.log` |
| D8 | The sync path refuses a merged non-overcommittable conflict end to end: workload resources untouched, replicas still move, `RuntimeResourcesConflict` event names `nvidia.com/gpu request 1 must equal limit 2`, second sync patches nothing | contract | **Fixed** — red on `497bbeaf`, green on `3adc547d` | same logs |
| E1 | A CacheRuntimeClass edit adding a pod-level `resourceClaims` entry + matching container claim must not leave an existing workload's pod template referencing an undeclared claim (replacement Pods would be rejected by `validateResourceClaimNames`, "must be one of the names in pod.spec.resourceClaims", and the rollout stalls with no CacheRuntime event) | contract | **STILL BROKEN at head — new finding F11, pre-existing, non-blocking.** Red on `3adc547d` and `497bbeaf` (container claim `shm` synced, pod-level list stays `[]`). Base probe (`scripts/base_probe_f11_test.go.txt`): the paired template shape (claim + requests/limits) reproduces on the merge base `e0fc4c18` (base `desiredComponentResources` copied the whole template struct), so the mechanism is pre-existing; the claim-only template shape does *not* wedge at base (base returned nil for claim-only), so the PR broadens that subcase. Copilot discussion_r4276559859 flags the same mechanism. | `results/round4/unit-base-probe-f11-e0fc4c18.log` |
| E2 | An explicit `limits: {}` on the CacheRuntime still clears the workload's stale limit (refutes Copilot discussion_r4212708077, which claimed the limit is "retained forever" because `desiredComponentResources` "returns nil" — it never returns nil) | contract | **Refutation confirmed** — green on base, `497bbeaf` and `3adc547d` | same logs |

All B1–B5, D1–D6 contracts stay green at head (`re-verify.sh` round 4, all FIXED).
Full engine suite with the harness grafted: 312 passed / 12 failed at `3adc547d` vs
298 / 12 at `497bbeaf` — the failing spec set is **byte-identical** between the two
heads (pre-existing `ufs_test.go` / `sync_test.go` ReportSummary failures, present at
base), so no revision regression. The go-test level adds exactly one failure: E1, the
F11 repro, by design.

Copilot's review of this head (5525027377) marks the non-overcommittable thread
resolved and adds one new finding — the claims mechanism E1 pins above; its other
carried thread (r4212708077, empty maps) is refuted by E2.

Compatibility: `git merge-tree --write-tree 3adc547d e4a10bf4` (current master) merges
cleanly.

`.last-reviewed` advanced to `3adc547d`.

## Round 5 — delta `3adc547d..ce4f0c4f`

Six commits closing three carried ledger items plus a docs cleanup:

- `4688a36c` drops the upgrade notes from `docs/{en,zh}/.../cacheruntime_spec_update.md`.
  Validated: the CacheRuntime is genuinely unreleased — the commits introducing
  `pkg/ddc/cache` (#5738/#5762) are contained in **no tag** (`git tag --contains` is
  empty; `git ls-tree -r v1.0.8 | grep -c cacheruntime` = 0), so no released user can
  hold a CacheRuntime workload to upgrade. The one-time repair roll still exists in
  code, but it can only ever fire for pre-release/dev builds. The PR description still
  carries the old upgrade paragraph — stale text, not a defect.
- `71d94397` + `4a8668f9` (F10): the creation path routes validation through
  `checkComponentResources`, which emits the same `RuntimeResourcesConflict` Warning
  event the sync path emits; specs added for worker, master and client.
- `94121e27` (F7): `syncRuntimeSpec` guards a topology-less class before the first
  dereference; spec added.
- `44e6c90a` + `ce4f0c4f` (F8): the resources equivalence table renders tiered-store
  workloads (emptyDir-Memory and process-memory tiers) and pins the quota each row
  charges, so a row can no longer pass with the quota silently dropped.

New harness probes (`verify_delta_r5_6177_test.go`):

| id | claim | polarity | verdict | evidence |
| --- | --- | --- | --- | --- |
| D9 | `syncRuntimeSpec` with a topology-less class is a no-op (no panic), workloads untouched (F7) | contract | **Fixed** — panics with a nil-pointer dereference on `3adc547d`, passes on `ce4f0c4f` | `results/round5/differential-prevhead-3adc547d.log` |
| D10 | A tiered-store workload's memory agrees across the creation render and the sync resolution (quota charged on the merged baseline on both sides, F8) | contract | **Regression pin** — passes on both heads by design; the coverage fix is the PR's own new equivalence rows, D10 keeps the invariant pinned here | same logs + `results/round5/reverify-head-ce4f0c4f.log` |
| D11 | The creation path emits a `RuntimeResourcesConflict` Warning event naming the conflicting pair when the merge conflicts (F10) | contract | **Fixed** — error but no event on `3adc547d`, error + event on `ce4f0c4f` | same logs |

Full re-verify at head (`results/round5/reverify-head-ce4f0c4f.log`): every contract
B1–B5, D1–D11, E2 and F7/F8/F9/F10 FIXED; F11 (E1) STILL-BROKEN as expected — the
pre-existing class-claims wedge, unchanged by this delta (the delta does not touch
`validDesiredResources`/claims), still non-blocking.

Full engine suite: 12 failures at `ce4f0c4f`, the **same byte-identical set** as at
`3adc547d` (fingerprint `c3e1929f…`, 11× `ufs_test.go` + 1× `sync_test.go`
ReportSummary, pre-existing at base) — no revision regression.

CI at the exact head `ce4f0c4f`: all 21 check-runs completed/success (DCO, unittest,
lint, staticcheck, builds, CodeQL, SonarCloud, codecov, kind-e2e-test and
backward-compat-test on v1.22.17/v1.24.17/v1.28.15/v1.30.13/v1.33.2) — the
round-start "pending" rollup had settled to fully green.

Maintainer ack (F1): still outstanding — latest reviews on the PR are
copilot-pull-request-reviewer (10-12, 10-14) and the cheyang entry 5553719814
(10-16), which is this pipeline's own round-4 summary; issue comments are bot-only;
#6165 discussion_r3804862812 still has no human reply.

`.last-reviewed` advanced to `ce4f0c4f`.
