# Verification: PR #6177 — merge CacheRuntime resources onto the CacheRuntimeClass baseline

Reviewer: Codex (Reviewer B). Reviewed head: `497bbeaf254071c522904d654a0e574651df65fd`
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

*On base, the F9 input's sync wholesale-drops requests leaving a limit-only list the API
server defaults to request=limit (valid) — the apiserver-invalid applied spec is introduced
by this PR's merge, and the head's new validator does not cover the family.

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
contract polarity: green means good. F9 stays red until the validator mirrors the apiserver's
non-overcommittable rule (or the merge otherwise guarantees apiserver-valid output).

Kickoff prompt for a fresh machine:
"Continue the review pipeline for https://github.com/fluid-cloudnative/fluid/pull/6177 —
check out verify/cacheruntime-resources-merge-codex from https://github.com/cheyang/fluid.git,
run docs/verification/cacheruntime-resources-merge/scripts/re-verify.sh, review the
.last-reviewed..head delta."
