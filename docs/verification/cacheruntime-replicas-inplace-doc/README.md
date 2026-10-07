# Verification: PR #6183 — CacheRuntime in-place field-update docs (`replicas`)

PR: https://github.com/fluid-cloudnative/fluid/pull/6183
Base (merge-base): `f2785f847a9e4b148920f3be556f447dc37825fe`
Reviewed head: `6bcf1887a9e0125bb83379dfd51e88b98f425b2e` (also in `.last-reviewed`)
Harness branch: `verify/cacheruntime-replicas-inplace-doc-codex` (on the reviewer's fork)

## Premise (P0) — verdict: **Confirmed**

The PR fixes issue #6182: the CacheRuntime spec-update docs said only
`runtimeVersion` and `resources` update in place and listed `replicas` as
requiring redeployment. The docs were wrong: `syncRuntimeSpec`
(`pkg/ddc/cache/engine/sync.go`) passes `&runtime.Spec.{Master,Worker}.Replicas`
(always non-nil) into `ComponentSpec`, and
`AdvancedStatefulSetManager.SyncComponentSpec` patches `asts.Spec.Replicas`
via `updateReplicas` whenever the value differs. `syncRuntimeSpec` runs on every
reconcile (`Sync` at sync.go:76, called from `RuntimeReconciler.ReconcileRuntime`,
pkg/controllers/runtime_controller.go:299).

This is a docs PR (plus removal of one stale TODO comment), so the only code-level
claim to prove is the premise itself: that the in-place replica sync already exists
on the **base** branch.

## Harness

Additive only — production code untouched (verify with
`git diff origin/pr/6183...HEAD -- pkg/ api/` → empty except the new `_test.go`).

- `pkg/ddc/cache/engine/sync_replicas_verification_test.go` — 3 Ginkgo contract specs
  driving the unexported `CacheEngine.syncRuntimeSpec` against a fake client:
  1. worker scale-out (ASTS 2 → spec 3): ASTS `spec.replicas` patched to 3
  2. master scale-in (ASTS 3 → spec 1): ASTS `spec.replicas` patched to 1
  3. no-op (spec == ASTS): no write to the ASTS (resourceVersion unchanged) —
     the sync is value-driven, not a periodic overwrite

### How to run

```bash
# unit layer (the P0 proof)
go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus 'P0 verification' -count=1 -v

# integration layer (regression sweep of the touched area)
go test ./pkg/ddc/cache/... -count=1
```

### Premise against base

The premise runs against the BASE branch. On any machine:

```bash
git worktree add /tmp/fluid-base f2785f847a9e4b148920f3be556f447dc37825fe
cp pkg/ddc/cache/engine/sync_replicas_verification_test.go /tmp/fluid-base/pkg/ddc/cache/engine/
cd /tmp/fluid-base && go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -ginkgo.focus 'P0 verification' -count=1
```

## Observed vs expected

| Claim | Layer | Expected | Observed | Verdict |
|---|---|---|---|---|
| P0: replicas synced in place on base | unit | 3 specs pass on merge-base f2785f84 | `Ran 3 of 274 Specs ... 3 Passed` (results/unit-p0-base-branch.txt) | **Confirmed** |
| P0: same on PR head 6bcf1887 | unit | 3 specs pass | `3 Passed` (results/unit-p0-pr-head.txt) | Confirmed |
| Harness bites | unit | mutating sync.go (dropping both `Replicas:` lines) makes the scale-out/scale-in specs FAIL | `1 Passed \| 2 Failed` with the mutation; green again after revert (results/harness-bites-mutation.txt) | Harness proven |
| Regression sweep | integration | no new failures vs base | identical 246/12/16 on base and head; the 12 are pre-existing ufs_test.go fake-mount fixture failures, env-related, not PR-caused (results/integration-engine-suite-base-vs-head.txt); component suite 50/50 (results/integration-component-suite-pr-head.txt) | No regression |

## Static checks backing the new doc sections (reasoning, not tests)

- "Scaling writes no RuntimeCondition and emits no Kubernetes Event": no
  Recorder/Event/condition writes anywhere in `pkg/ddc/cache/engine/sync.go` or
  `pkg/ddc/cache/component/` — confirmed by grep. Contrast `pkg/ctrl/replicas.go`
  (other engines) which does both.
- "Scaling in discards cached data": no drain/migration on the scale path
  (`grep -rni "drain|scaledown|migrat" pkg/ddc/cache/` finds only the unrelated
  DataMigrate DataOperation flow).
- `status.{master,worker}.{readyReplicas,desiredReplicas}` observability: fields
  exist in `RuntimeComponentStatus` and are populated every sync by
  `CheckAndUpdateRuntimeStatus` → `ConstructComponentStatus`.
- Client component has no `replicas` field (DaemonSet) — `CacheRuntimeClientSpec`
  in api/v1alpha1/cacheruntime_types.go.

## Continuing after the fix

This PR is itself the fix (docs). If a follow-up changes the sync path
(e.g. adds conditions/events, or stops syncing master replicas), re-run:

```bash
bash docs/verification/cacheruntime-replicas-inplace-doc/scripts/re-verify.sh
```

from a checkout of this harness branch (manifest `pr` auto-discovers the current
PR head; `.last-reviewed` marks the reviewed sha). All findings are `contract`
polarity: green = documented behavior intact. If the intended behavior changes
(e.g. master replicas become non-syncable), invert the affected spec rather than
deleting it.

Kickoff prompt for a fresh machine:
> Check out the `verify/cacheruntime-replicas-inplace-doc-codex` branch of
> https://github.com/cheyang/fluid.git, read
> docs/verification/cacheruntime-replicas-inplace-doc/README.md, then run
> `bash docs/verification/cacheruntime-replicas-inplace-doc/scripts/re-verify.sh`.
