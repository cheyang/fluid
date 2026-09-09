# Verification — fluid#6183 (CacheRuntime in-place `replicas` docs)

Reviewer-private harness. Production code is **untouched**; the only additions
are this directory and `pkg/ddc/cache/engine/verify_6183_replicas_test.go`.

## What the PR is

PR [#6183](https://github.com/fluid-cloudnative/fluid/pull/6183) is a docs-only
change (plus removal of one stale `// TODO` comment in `sync.go`). It moves
`replicas` out of the "unsupported / requires redeploy" list and documents it as
an in-place-updatable field of CacheRuntime (new section 3.3), for both Master
and Worker.

A docs PR is only as correct as the behaviour it describes. The premise-check
(step 1.5 / P0) is normally skipped for docs PRs, but here the entire value of
the change rests on one factual claim, so it was verified rather than assumed:

> Changing `spec.{master,worker}.replicas` propagates to the backing
> AdvancedStatefulSet on the next reconcile, with no redeploy.

## Premise verdict: **CONFIRMED**

`syncRuntimeSpec` (`pkg/ddc/cache/engine/sync.go`) passes
`&runtime.Spec.{Master,Worker}.Replicas` — always non-nil, unlike `resources` —
into `ComponentSpec`. `SyncComponentSpec`
(`pkg/ddc/cache/component/advanced_statefulset_manager.go`) applies it first via
`updateReplicas` and patches the ASTS when the value differs. Proven by running
that real code path against a fake client.

## Observed vs expected

| id | claim (from the docs) | polarity | layer | verdict | evidence |
|----|-----------------------|----------|-------|---------|----------|
| C1 | `spec.worker.replicas` synced in place to Worker ASTS (3.3) | contract | unit | **Confirmed** | `results/L1-replicas-sync.txt` |
| C2 | `spec.master.replicas` synced in place to Master ASTS (table) | contract | unit | **Confirmed** | `results/L1-replicas-sync.txt` |
| C3 | scale-in (2→1) propagated, not just scale-out | contract | unit | **Confirmed** | `results/L1-replicas-sync.txt` |
| C4 | value-driven/idempotent — unchanged replicas ⇒ no patch | contract | unit | **Confirmed** | `results/L1-replicas-sync.txt` |
| S1 | scaling is silent (no RuntimeCondition, no Event) | static | grep | **Confirmed** | SyncComponentSpec/updateReplicas emit none; `pkg/ctrl/replicas.go` SyncReplicas does |
| S2 | scale-in discards cache (no drain/migration) | static | grep | **Confirmed by absence** | no drain/migrate/preStop in `pkg/ddc/cache`; inferred, matches author note |
| S3 | `replicas` not subject to cgroupv1 step-by-step limit | static | read | **Confirmed** | cgroupv1 limit (K8s #127356) is image+resources resize only |
| S4 | section 4 "any field not in section 3" is exhaustive | static | read | **Confirmed** | SyncComponentSpec touches exactly {replicas, image, resources} |
| S5 | `kubectl -n fluid-system logs deploy/cacheruntime-controller \| grep "replicas changed"` works | static | read | **Confirmed** | Deployment `cacheruntime-controller` exists in fluid ns; log at Info level (`advanced_statefulset_manager.go:263`) |

No blocker, no major finding. Every documented claim matches the code.

## Harness-bites check (step 4)

Temporarily deleting the two `Replicas:` lines from `syncRuntimeSpec` flips C1,
C2, C3 and the "without resources" case to **FAIL** (`results/L1-harness-bites.txt`);
C4 (idempotent) correctly stays green. The fix was reverted; `git diff` against
the PR head is empty for production code. So the tests genuinely exercise the
documented path rather than passing vacuously.

## Layers

- **L1 unit** — real `syncRuntimeSpec` → `SyncComponentSpec` → `updateReplicas`
  → `client.Patch`, fake API server. Decides the claim deterministically.
- **L2 integration / L3 live** — intentionally skipped. L1 already runs the real
  reconcile code path; a live CacheRuntime deploy (needs ASTS CRDs, a
  CacheRuntimeClass, underlying storage, a built controller image) is
  disproportionate for a docs change. The author separately confirmed the
  behaviour on a kind cluster (log lines quoted in the PR body).

## Run it

```bash
go test ./pkg/ddc/cache/engine/ -run 'TestVerify6183' -v
```

Note: the broader `pkg/ddc/cache/engine` Ginkgo suite has 15 pre-existing
failures on this machine (mocked command `Execute` / mount paths in
`ufs_test.go`, `dataset_test.go`, `fileutils_test.go`). They reproduce
identically on `origin/master`, so they are local-environment noise, **not**
caused by this PR. The `-run 'TestVerify6183'` filter isolates this harness.

## Continuing after a change

```bash
bash scripts/re-verify.sh            # re-runs against the current PR head from manifest.pr
```

All findings are `contract` polarity: green = still correct. If a future change
stops syncing replicas, C1–C3 go red — that is the reproduction of the docs
becoming stale.
