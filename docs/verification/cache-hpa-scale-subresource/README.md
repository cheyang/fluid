# cache-hpa-scale-subresource — bug verification

Reproducible evidence for the review of PR fluid-cloudnative/fluid#6223
("feat(cache): support HPA on CacheRuntime via scale subresource and status selector").

- Reviewed head: `3f39501e7b99c1e4fd925f332ee7e34faec45171`
- Base (merge-base): `fe074af38434fc4b618686de6459a9173570b48b`
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
