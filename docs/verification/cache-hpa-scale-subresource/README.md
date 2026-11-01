# cache-hpa-scale-subresource — bug verification

Reproducible evidence for the review of PR fluid-cloudnative/fluid#6223
("feat(cache): support HPA on CacheRuntime via scale subresource and status selector").

- Reviewed head: `99e35af0eef60f077bfefc519e328d1e31252401` (round 4; round 3 reviewed `9a265eb3`)
- Base (merge-base): `96a3765eaae0f23140aef9b30152538396ed7ec2` (round 3; round 2 base was `fe074af38434fc4b618686de6459a9173570b48b`)
- Production code is **untouched** by this harness (additive test files + docs only).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `CacheEngine.getWorkerSelectors`, `CheckAndUpdateRuntimeStatus` selector population, selector-vs-created-workload label match | `go test ./pkg/ddc/cache/engine/ -run 'TestVerifyHPA' -v` |
| 2. Integration | the PR's CacheRuntime CRD against a real API server (envtest): GET/PUT `/scale` exactly as kubectl/HPA do | `KUBEBUILDER_ASSETS=<envtest assets> go test ./test/verification/codexhpa/ -run 'TestCacheRuntime' -v` |
| 2b. Premise (CRD half) | base CRD has no scale subresource | `CODEX_CRD_DIR=<base config/crd/bases> CODEX_CRD_DIR_EXPECT_SCALE=absent go test ./test/verification/codexhpa/ -run 'TestCacheRuntimeScalePremiseAgainstCRDDir' -v` |
| 3. Live | not run (per debate setup, live/KUBECONFIG layer skipped). The PR's own curvine e2e passed on GHA kind k8s 1.22–1.33 (GET-side scale contract), incl. `status.selector` pod matching | CI run 40716621753 |

Polarity: all tests are CONTRACT tests (red on base / green on head). The two premise
probes were executed against base and failed there exactly as predicted, which is the
premise reproduction (P0 = Confirmed) and the harness-bites check in one.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom (issue #6063, OPEN, filed 2026-06-25) | "`CacheEngine.CheckAndUpdateRuntimeStatus()` ... has an unresolved TODO ... CacheRuntime never sets it, so `status.selector` is always empty." HPA / pod-discovery tooling "will see an empty string and fail to match any pods." |
| Claimed mechanism | `master.go` has the selector assignment commented out; `status.go` has a TODO where `Status.Selector` should be set |
| Reported component | `pkg/ddc/cache/engine` (CacheEngine), CacheRuntime CRD |
| Patched component | same (`pkg/ddc/cache/engine/*`, `api/v1alpha1/cacheruntime_types.go`, CRD YAMLs) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | `results/unit-base-premise.txt` — `TestVerifyHPAPremiseSelectorPopulated` FAILS on base with "PREMISE REPRODUCED: status.selector is empty after status sync"; `results/integration-base-premise.txt` — GET `/scale` returns NotFound against the base CRD. Same probes PASS on the PR head (`results/unit-head.txt`, `results/integration-head.txt`). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Base never populates `status.selector`; base CRD has no scale subresource | 1+2b | **Confirmed** | unit FAIL on base / PASS on head; GET /scale NotFound on base CRD / 200 on head CRD (`results/*`) |
| F1 | Scale subresource serves the full HPA contract (spec.replicas, status.replicas from `.status.worker.currentReplicas`, selector string from `.status.selector`) | 2 | Confirmed working | `results/integration-head.txt` — GET returns `{"spec":{"replicas":2},"status":{"replicas":2,"selector":"cacheruntime.fluid.io/..."}}` |
| F2 | `kubectl scale cacheruntime/... --replicas=4` (the HPA write path) lands on `.spec.worker.replicas` | 2 | Confirmed working | `results/integration-head.txt` — CR re-read shows `spec.worker.replicas=4`; propagation to the AdvancedStatefulSet is the pre-existing `syncRuntimeSpec`→`SyncComponentSpec` path (unit-tested at `pkg/ddc/cache/engine/sync_test.go`) |
| F3 | The scale-WRITE half of the new curvine e2e never executes in CI | CI-log inspection | **Confirmed gap** | All 5 kind-e2e jobs (k8s 1.22–1.33) log `>>> Skipping scale test: single-node cluster (1 node)`; `scale_worker_and_verify 2 scale` is gated on `node_count >= 2` and GHA kind is single-node. Only the GET-side assertions ran |
| F4 | PR body claim: "a GET on `.../scale` failing for a runtime without a worker is expected" | 2 | **Refuted (benign)** | envtest: CRD defaulting materializes `spec.worker.replicas: 1` even with no worker block; GET /scale **succeeds** (`spec.replicas=1, status.replicas=0`, no selector); `kubectl scale` on such a runtime **succeeds** and silently persists `spec.worker.replicas=3` while `syncRuntimeSpec` skips the worker (topology/disabled guard) |
| F5 | Selector written by the engine matches the pods the real creation path produces | 1 | Confirmed | `TestVerifyHPASelectorMatchesCreatedWorkerPods`: selector from `engine.getWorkerSelectors()` matches the pod-template labels of an AdvancedStatefulSet created via `component.NewComponentHelper(...).Reconciler(...)`; does not match another runtime's workers |
| F6 | `status.selector` is stamped even when the worker component is disabled | 1 | Behavior pinned (benign) | `TestVerifyHPASelectorBehaviorWhenWorkerDisabled`: selector is set and references the nonexistent `*-worker` component; matches zero pods; HPA then fails loudly with "selector is required"/0-pod metrics — acceptable, but undocumented |
| F7 | Committed CRD YAMLs are controller-gen-clean | tooling | Confirmed clean | `results/crd-regen.txt` — `controller-gen v0.19.0` output is byte-identical to the committed CRD (CI `lint` job also enforces `make update-crd`) |
| F8 | PR-head CI failure attribution | CI-log inspection | **Not this PR** | `kind-e2e-test` fails at `jindoruntime secret e2e` ("warmup pod jindo-demo-warmup is not ready", FUSE mount timeouts) on all 5 k8s versions; the curvine/mooncake/alluxio suites all PASS. An unrelated dependabot PR (codeql-action bump, run 40644839213, same day) failed at the identical jindo test with the old test order → pre-existing infra flake |

## Per-finding detail

### F3 — scale-write e2e is structurally skipped in CI
`test/gha-e2e/curvine/test.sh` runs `scale_worker_and_verify 2 scale` (the only executed
coverage of the subresource *write* path an HPA uses) only when the cluster has ≥2 nodes;
the GHA kind cluster has 1. Observed in every CI job log. Consequence: a regression that
breaks scale-write (e.g. a malformed `specReplicasPath`) turns CI red only via the GET
assertions, and a controller-side propagation break would be invisible. Fix options:
run the scale-write assertions on the single-node cluster too (scaling 1→1 is a no-op;
instead assert the *write lands on the CR*: `kubectl scale --replicas=2`, then assert
`.spec.worker.replicas==2` and let the anti-affinity-gated pod-count check stay conditional),
or add an envtest-based integration test like `test/verification/codexhpa` here.

### F4 — worker-less runtimes: scale GET/PUT succeed as silent no-ops
The PR body documents "GET on `.../scale` failing for a runtime without a worker is
expected". envtest shows the opposite: CRD defaulting creates `.spec.worker.replicas: 1`,
so GET succeeds with `status.replicas: 0` and no selector, and PUT/`kubectl scale`
persists a value the controller deliberately never syncs (worker absent from the
RuntimeClass topology, or `spec.worker.disabled=true`). Impact is benign (HPA against
such a runtime errors with "selector is required" until/unless a worker exists; kubectl
scale prints "scaled" while nothing happens), but the docs/PR description should be
corrected, and ideally `CheckAndUpdateRuntimeStatus` would skip stamping `status.selector`
when no worker component exists so scale-GET at least reports the empty-selector signal
consistently. (Note: F6 shows the selector is stamped regardless, so even
`disabled: true` workers expose a selector that matches zero pods.)

## Proposed fixes (NOT applied to production here)
- **F3**: make the scale-write assertion node-count-independent (assert the CR's
  `.spec.worker.replicas` after `kubectl scale`, keep the pod-count check conditional),
  or add an envtest integration test.
- **F4/F6**: fix the PR-body/docs claim; optionally gate `Status.Selector` population on
  the worker being enabled.

## Continuing after the fix
1. Check out this branch; `bash docs/verification/cache-hpa-scale-subresource/scripts/re-verify.sh`
   re-runs layers 1+2 against the current PR head (fetched via the `pr` URL in the manifest).
2. envtest assets: any `kube-apiserver`+`etcd`+`kubectl` bundle (tested with 1.31.0);
   `go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest` can fetch one.
3. All tests are contract tests: green = good. If F4 is "fixed" by making worker-less
   GET /scale fail, update the `worker-less runtime` subtest expectations accordingly.

## Round 3 (incremental, 2026-10-31) — head 9a265eb3, base 96a3765e

Delta `3f39501e..9a265eb3` is a single merge commit pulling master into the PR branch;
the only file it touches is `.github/workflows/kind-e2e.yml` (the `ubuntu-24.04` runner
pin from #6225, TODO referencing open issue #6224). The PR's effective diff
`base..head` is **byte-identical** to round 2's (verified via `git diff` comparison),
so no new code review surface; the runner-pin file is not part of the PR's own diff.

- `re-verify.sh` against head `9a265eb3`: all 9 harness entries green (unit + integration; envtest bundle k8s 1.36.2, the newest installed), including the newly hardened
  `TestCacheRuntimeAbsentWorkerBlockScaleFails` (round-2 log-only probe promoted to a
  contract test pinning: truly-absent `spec.worker` → block stays absent, GET /scale
  fails with the missing `.spec.worker.replicas` path error).
- Premise P0 re-verified against the NEW base `96a3765e` (base moved):
  `TestVerifyHPAPremiseSelectorPopulated` still FAILS on base (status.selector empty),
  base CRD still has no scale subresource (GET /scale NotFound). Evidence:
  `results/premise-new-base-96a3765.txt`. The base movement `fe074af..96a3765` is one
  commit touching only the workflow file, so no base-side code drift.
- CI on head `9a265eb3` is fully green (run 41179758839): all five kind-e2e jobs pass
  with the runner pin (the round-2 jindo flake is gone — confirming the infra
  attribution), and the curvine testcase still asserts `status.selector` pod matching +
  GET /scale on all five k8s versions. The scale-WRITE step is still skipped
  (`>>> Skipping scale test: single-node cluster (1 node)` in all five jobs) —
  finding F-scale-write-e2e-skipped stays OPEN.
- New Copilot review (2026-10-31, discussion_r4431023991) re-raises the already-tracked
  CurrentReplicas-under-during-rollout issue (C4420756561) with a second anchor on the
  CRD YAML; no new substance.

## Round 4 (incremental, 2026-11-02) — head 99e35af0, delta 9a265eb3..99e35af0

Delta: one commit, `fix(cache): count all pods in component status during mixed-revision rollouts`
(`advanced_statefulset_manager.go` constructStatus now reads `asts.Status.Replicas`;
new mixed-revision ginkgo spec; `test.sh` now asserts the scale WRITE unconditionally and
node-gates only the pod-count checks — the exact F3 fix).

| Item | Verdict | Evidence |
|------|---------|----------|
| C4420756561 mid-rollout undercount fix | **Fixed, differentially verified** | `TestVerifyHPAMixedRevisionReplicasCount` FAILS on previous head 9a265eb3 (`currentReplicas = 2, want 3`; `3, want 4`) and PASSES on 99e35af0 (`results/mixed-revision-prevhead-9a265eb3.txt`, `results/reverify/unit.out`). ASTS controller semantics confirmed from source: `status.Replicas` accumulates every created pod (`computeReplicaStatus`, stateful_set_control.go:876-878), so `Replicas >= CurrentReplicas` always; matches native StatefulSet scale and `pkg/ctrl/worker.go:128`. Downstream consumer `pkg/ddc/thin/referencedataset/sync.go:212-225` inherits the same convention. |
| F2-unit harness | **Harness-update** (fixture predated the new semantics) | `newAdvancedStatefulSetComponent` (PR's own engine-level helper) sets only `Status.CurrentReplicas`, which is now unread → test failed with `got 0, want 3`. Corrected locally to set the invariant-realistic `Status.Replicas`; re-run green. Note: the PR's engine fixture is left semantically impossible at head (nit, reported). |
| F3 scale-write-in-CI | **Fixed in CI** | Run 41297697216 (head 99e35af0): all five kind-e2e jobs (v1.22.17..v1.33.2) log `Scaling worker to 2 replicas (method=scale, verify_pods=false)` → `cacheruntime scaled` → `Worker scale write to 2 verified on the CR` and back to 1 (pulled v1.22.17 job 124821932907, v1.33.2 job 124821933065). |
| `scale_worker_and_verify` shell logic | Verified by stub harness | `scripts/e2e-shell-stub/run-e2e-shell-stub.sh` (5/5 pass: both write methods asserted with pods skipped; full pod-verify converges; scale-GET mismatch and spec-write mismatch both panic) — `results/e2e-shell-stub-head.txt`. |

Full re-verify on 99e35af0: all 10 manifest entries green (`results/reverify/`).
