# Verification: PR #6177 — merge CacheRuntime resources onto the CacheRuntimeClass baseline

Reviewer: Codex (Reviewer B). Reviewed head: `3adc547d4da45c2da9540c94ab447ff0f4269c91`
(`.last-reviewed`). Base (merge-base with master): `e0fc4c189a6e45ee17a5e812abf3fcbecf566f09`.

## Premise (P0) — verdict: CONFIRMED

Issue #6173 (open) claims: a CacheRuntime that names only some resource keys loses every
requirement it does not restate — setting just `limits.memory` clears `requests.cpu`,
`limits.cpu` and `requests.memory` from the workload on the next reconcile. Reproduced on the
base commit (see round-1/2 notes in git history): the contract tests fail with exactly the
dropped keys. Component match: the issue names `updateResources`, which only applies the
desired state it is handed; the PR fixes the two places that compute that state.

## Round 3 (e259d738 -> 497bbeaf): what changed

The delta is the round-2 consensus remedy for F6 plus its docs:

- `f02054bd` adds `validateResourceRequirements` (rejects any request above its limit) and
  wires it into both paths: creation (`transformMaster/Worker/Client` fail the transform, which
  surfaces as a `Failed to setup ddc engine` Warning event + dataset not-ready condition via
  `ReconcileRuntime`), and sync (`validDesiredResources` refuses the resource patch, emits a
  `RuntimeResourcesConflict` Warning event naming the conflicting keys, and still syncs image
  and replicas).
- `497bbeaf` documents the behavior in en+zh. The rebase also re-signed the delta (DCO green).

The round-2 F6 canary is inverted into two contract tests (both compile-safe on base,
previous head and head):

| id | claim | polarity | layer | test |
|----|-------|----------|-------|------|
| F6 | conflicting merge (request>limit) is refused at creation | contract | unit | `TestVerifyPR6177_F6_ConflictMerge_CreationRefuses` |
| F6 | conflicting merge is not synced; Warning event; converges | contract | integration | `TestVerifyPR6177_F6_ConflictMerge_SyncSkipsAndWarns` |
| F9 | non-overcommittable merge (request!=limit on hugepages/extended) must be refused too | contract | integration | `TestVerifyPR6177_F9_NonOvercommittableMerge_SyncMustRefuse` |

## Round-3 results (unit + integration layers; live layer skipped per debate setup)

| ref | P0 | G1 | G2 | G3 | F6 | F9 |
|-----|----|----|----|----|----|----|
| base e0fc4c18 | STILL-BROKEN (premise) | STILL-BROKEN (pre-clear) | pass | pass | STILL-BROKEN | STILL-BROKEN* |
| previous e259d738 | pass | pass | pass | pass | STILL-BROKEN | STILL-BROKEN |
| head 497bbeaf | pass | pass | pass | pass | **pass (fixed)** | **STILL-BROKEN (new finding)** |

*On base, the F9 input's sync wholesale-applies the CacheRuntime's own list
(`{requests: {cpu: 1}, limits: {cpu: 2, hugepages-2Mi: 2Gi}}`); the absent hugepages request
is defaulted to the limit by the API server, so the applied spec is valid. On head the merged
(hugepages request 1Gi != limit 2Gi) spec is applied instead — the apiserver-invalid applied
state is introduced by this PR's merge, and the head's new validator does not cover the
family. The contract is red on base too because the contract demands the controller refuse
to touch the workload (and emit a warning), which the base also does not do.

Raw output: `results/round3/{unit,integration}-{base-e0fc4c18,previous-e259d738,head-497bbeaf}.json`.
Full package suite at head (harness grafted): `ok pkg/ddc/cache/engine`, ginkgo 310 passed /
0 failed / 16 skipped — no revision regression (`results/round3/suite-head.txt`).

## F9 details (new this round)

`validateResourceRequirements` (transform_common.go) only rejects `request > limit`. The
apiserver's `ValidateResourceRequirements` (vendored version k8s.io/kubernetes v1.29.15,
`pkg/apis/core/validation/validation.go`) additionally requires `request == limit` for
non-overcommittable resources (`helper.IsOvercommitAllowed` = native non-hugepages only), and
requires a limit at all for them. A template with `hugepages-2Mi` request=limit=1Gi plus a
CacheRuntime raising only the limit to 2Gi merges into request 1Gi != limit 2Gi: the head
accepts and applies it with no event; the API server rejects the first Pod
("must be equal to hugepages-2Mi limit of 2Gi") and the rollout sticks silently — the exact
failure shape F6 was about, one resource family over. Reproduced at head by the F9 test above
(observed: workload mutated to request 1Gi != limit 2Gi, no event). Remedy: mirror the
apiserver rule in `validateResourceRequirements` (request must equal limit when
`!IsOvercommitAllowed(name)`), or document the limitation.

The F9 fixture was sharpened after the first reproduction so that both inputs are
apiserver-valid pod specs on their own: the template declares cpu and hugepages-2Mi
request=limit=1Gi, and the CacheRuntime restates cpu while raising only the hugepages limit
to 2Gi (its absent hugepages request would be defaulted to the limit, so the overlay alone is
valid too). Differential, same input, observed in `results/round3/integration-*.json`: on base
e0fc4c18 the sync wholesale-applies the overlay — `{requests: {cpu: 1}, limits: {cpu: 2,
hugepages-2Mi: 2Gi}}` — which the API server accepts after defaulting the hugepages request to
the limit; on head 497bbeaf the merge applies `{requests: {cpu: 1, hugepages-2Mi: 1Gi},
limits: {cpu: 2, hugepages-2Mi: 2Gi}}`, which the API server rejects. envtest against a real
API server (results/round3/envtest-apiserver-rule.txt) confirms the merged spec is rejected
for the must-be-equal rule only (cpu/memory present), while the equal-pair control is
accepted. Harness-bites check: patching `validateResourceRequirements` in a scratch worktree
to also reject request != limit for non-overcommittable names turns the F9 test green and
keeps the rest of the suite (`pkg/ddc/cache/...`) green; the scratch was discarded, no
production change was kept.

## Round 4 (497bbeaf -> 3adc547d): F9 fixed

The delta is a single commit, `3adc547d`, the round-3 consensus remedy for F9:
`validateResourceRequirements` now mirrors the apiserver's non-overcommittable rule via a
local `isOvercommitAllowed` (verified byte-equivalent in logic against
k8s.io/kubernetes v1.29.15 `helpers.go:127` `IsOvercommitAllowed` = `IsNativeResource &&
!IsHugePageResourceName`, and `validation.go:6568-6579`; the helper package is genuinely not
vendored, `vendor/modules.txt` has no `pkg/apis/core/v1/helper`). For hugepages-*/extended
resources it rejects request != limit (subsumes request > limit) and rejects a request with
no limit; docs updated en+zh; new unit + sync specs land in the PR itself.

Additive harness change: `TestVerifyPR6177_F9_ExtendedRequestWithoutLimit_SyncMustRefuse`
pins the second sub-rule (requests-only `nvidia.com/gpu` merged onto a cpu-only template must
be refused with a Warning event naming the key). RED on base and previous head, GREEN at head.

Round-4 results (`results/round4/reverify-{base-e0fc4c18,prevhead-497bbeaf,head-3adc547d}-{unit,integration}.txt`):

| ref | P0 | G1 | G2 | G3 | F6 | F9 (2 tests) |
|-----|----|----|----|----|----|----|
| base e0fc4c18 | STILL-BROKEN | STILL-BROKEN | pass | pass | STILL-BROKEN | STILL-BROKEN |
| previous 497bbeaf | pass | pass | pass | pass | pass | STILL-BROKEN (fail=2) |
| head 3adc547d | pass | pass | pass | pass | pass | **pass (fixed)** |

F11 canary: STILL-PRESENT (pass) at head — open pre-existing finding, see below.

Full package suite at head with the harness grafted: `ok pkg/ddc/cache/engine` +
`ok pkg/ddc/cache/component` (`-count=1`), no revision regression. Master moved to e4a10bf4;
a local test merge of head into current master is clean, the merged tree keeps
`validDesiredResources` wired in sync.go, and the suite + all 10 harness tests pass on the
merge result.

Scope adjudication: the round-3 remedy also floated the "HugePages require cpu or memory"
cross-key check (validation.go:6591). The author scoped it out in the commit message ("a merge
cannot break them when both inputs satisfy them"). Verified sound: the merge is key-union per
list (`mergeResourceList`), so a presence-rule violation requires an already-pod-invalid input,
and neither CR is pod-validated by the API server — a pre-existing gap, not merge-synthesized.

### F11 (new this round, pre-existing): template claims propagate without pod-level declarations

Copilot's review of this head (discussion_r4276559859) flagged that a CacheRuntimeClass edited
after workload creation to add a claim propagates the container claim to existing workloads
while `SyncComponentSpec` never syncs pod-level `spec.resourceClaims`; on DRA-enabled clusters
the API server rejects replacement Pods (`validateResourceClaimNames`, validation.go:6595).
Independently reproduced with ad-hoc probes grafted on base and head
(`results/round4/f11-claims-{base-e0fc4c18,head-3adc547d}.txt`):

- CacheRuntime declares nothing: the claim propagates on **both** base (template fallback in
  the old `desiredComponentResources`) and head — a pre-existing gap, `origin=preexisting`.
- CacheRuntime declares a partial value (cpu request only): base wholesale-applies the runtime
  list (claim dropped — the #6173 shape), head merges (claim propagates). The PR widens
  exposure to this combination while fixing the dropped-keys bug for it.

Pinned as the canary `TestVerifyPR6177_F11_TemplateClaimsPropagateWithoutPodLevelDeclaration_Canary`
(asserts current behavior: propagated, nil error, no Warning event; must flip red when a guard
lands). Filed as a minor non-blocking risk; remedy direction: validate resolved claims against
the workload's pod-level declarations and refuse with a Warning event (the F6/F9 pattern), or
sync pod-level `resourceClaims`, or document the DRA limitation.

## Round 5 (3adc547d -> ce4f0c4f): F7, F8 and F10 fixed

Six commits, all carried nits from earlier rounds, no new merge/validator behavior:

- `94121e27` guards `syncRuntimeSpec` against a class without a topology (F7, pre-existing
  defense-in-depth: `getRuntimeClass` -> `validateRuntimeClassTopology` rejects such classes
  before `syncRuntimeSpec` in production, so the guard protects direct callers only).
- `71d94397` + `4a8668f9` route all three creation transforms through the new
  `checkComponentResources`, which emits exactly one Warning event with the specific
  `RuntimeResourcesConflict` reason (naming the conflict) when the merged resources are
  rejected -- on top of the reconciler's generic `ErrorProcessRuntime` (F10). The client
  component gains its first creation-rejection spec, closing the last per-component gap.
- `44e6c90a` + `ce4f0c4f` render tiered-store workloads in the resources equivalence table
  (F8): two new rows (Memory-medium emptyDir, process memory) pin the charged quota and the
  exact memory request/limit, and the resolve check now charges the quota on the sync side
  from the rendered workload, exactly as `syncRuntimeSpec` does. Both quota helpers and the
  sync-side charge predate this PR (present on merge base e0fc4c18, #6167-era), so the remedy
  is test-only and no behavior differential exists to flip; the rows are self-pinning
  (`wantQuota` fails if the creation path stops charging).
- `4688a36c` drops the upgrade notes from the en+zh docs. Premise verified: CacheRuntime is
  unreleased -- `api/v1alpha1/cacheruntime_types.go` (05682681, 2026-01-07) is contained in no
  release tag (latest v1.0.8) and absent from the v1.0.8 tree; no release branch newer than
  release-1.0 exists. No upgrade path from a released version exists, so the note was moot.

New contracts (`zz_verify_pr6177_f7f10_contract_test.go`), all contract polarity:

| Test | base e0fc4c18 | prev 3adc547d | head ce4f0c4f |
| --- | --- | --- | --- |
| TestVerifyPR6177_F7_NilTopology_SyncIsNoop | FAIL (nil-pointer panic, recovered) | FAIL (same panic) | PASS (nil error, workloads untouched) |
| TestVerifyPR6177_F10_WorkerCreationRefusal_EmitsConflictEvent | FAIL (no validation, no error) | FAIL (error, no event) | PASS (err + exactly one Warning RuntimeResourcesConflict naming the key) |
| TestVerifyPR6177_F10_ClientCreationRefusal_EmitsConflictEvent | FAIL (no validation, no error) | FAIL (error, no event) | PASS (same) |

All round 1-4 contracts stay green at head (P0/G1/G2/G3/F6/F9 FIXED); the F11 canary still
passes, i.e. the claims half-propagation behavior is unchanged (delta does not touch claims).

No-regression sweep: `FLUID_UNIT_TEST=true go test -count=1 ./pkg/ddc/cache/...` --
previous head 312 passed / 12 failed, head 321 passed / 12 failed, the 12 failures
byte-identical (pre-existing set from round 4); +9 new specs all passing (21/21 in a focused
run of the tiered rows, the nil-topology spec and the three creation-event specs).

Raw output: `results/round5/reverify-{base-e0fc4c18,prevhead-3adc547d,head-ce4f0c4f}-{unit,integration}.out`
plus `reverify-head-ce4f0c4f-summary.txt`.

## How to run

```bash
# unit + integration layers (no cluster needed)
FLUID_UNIT_TEST=true go test -gcflags="all=-N -l" ./pkg/ddc/cache/engine/ -run 'TestVerifyPR6177' -v

# whole package (PR's own tests included)
FLUID_UNIT_TEST=true go test -gcflags="all=-N -l" ./pkg/ddc/cache/...
```

## Continuing after the fix

`bash docs/verification/cacheruntime-resources-merge/scripts/re-verify.sh` from a checkout of
this branch fetches the current PR head (from `manifest.pr`), grafts the harness onto it, runs
the unit + integration layers, and prints Fixed / Still-broken per finding. All findings are
contract polarity: green means good. All contracts, F9 included, are green at head ce4f0c4f; they
now serve as the regression suite for the whole merge/validation behavior.

Kickoff prompt for a fresh machine:
"Continue the review pipeline for https://github.com/fluid-cloudnative/fluid/pull/6177 —
check out verify/cacheruntime-resources-merge-codex from https://github.com/cheyang/fluid.git,
run docs/verification/cacheruntime-resources-merge/scripts/re-verify.sh, review the
.last-reviewed..head delta."
