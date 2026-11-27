# alluxio-sync-imagepullsecrets (codex) — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6288 ("Sync Alluxio imagePullSecrets changes",
fixes #6277). Reviewed head: `8903e33ebd8f03eecf0896ad5d251993d39ade9e`, merge-base:
`07bdbcd8616397b4585bb793f5d97c35318e5f4a`.

Layers run against the **code under review** (PR head `8903e33e`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `AlluxioEngine.SyncRuntime` nil/empty equality + `base.Sync` short-circuit coupling (fake client + gomock) | `FLUID_UNIT_TEST=true go test ./pkg/ddc/alluxio/ -run 'TestVerifyCodexPR6288(DefaultConfigStaysNoOp\|EmptyStateIdempotentAfterRemoval\|ConfiguredSecretsSync\|APIServerServesNilForOmittedSecrets)$' -v` and `FLUID_UNIT_TEST=true go test ./pkg/ddc/base/ -run 'TestVerifyCodexPR6288' -v` |
| 2. Integration | `SyncRuntime` against a real API server (envtest), workloads created exactly as the helm chart renders them (imagePullSecrets field omitted) | `KUBEBUILDER_ASSETS=<envtest-assets> FLUID_UNIT_TEST=true go test ./pkg/ddc/alluxio/ -run 'TestVerifyCodexPR6288SyncRuntimeAgainstRealAPIServer$' -v` (auto-skips when assets absent) |
| 3. Live | skipped per round policy (no KUBECONFIG layer) | — |

> Test polarity: all finding tests are **contract** tests (assert intended behavior): they FAIL
> on the PR head and PASS once the fix lands. Positive controls
> (`...ConfiguredSecretsSync`, `...APIServerServesNilForOmittedSecrets`, the base-package
> mechanism tests) PASS on the PR head and must keep passing.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "Alluxio has supported runtime and per-component imagePullSecrets since #4242, but its `SyncRuntime` is a no-op, so changing them after creation does nothing until the runtime is recreated." (issue #6277) |
| Linked issue | #6277, OPEN, created 2026-11-20 — valid |
| Reported component | AlluxioRuntime engine `SyncRuntime` (`pkg/ddc/alluxio`) |
| Patched component | `pkg/ddc/alluxio/sync_runtime.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | On merge-base `07bdbcd86`, `AlluxioEngine.SyncRuntime` is a bare `return` — no code path propagates spec imagePullSecrets to the master/worker StatefulSets or fuse DaemonSet after setup. results/p0-base-syncruntime.txt |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| F1 | Default-config AlluxioRuntime (no imagePullSecrets anywhere, `IMAGE_PULL_SECRETS` unset): `SyncRuntime` reports `changed=true` on **every** reconcile, and via `base.Sync`'s `if updated { return }` permanently skips health checks, runtime/dataset status, dataset-cache and dataset-mount sync | 1 | **Confirmed** | changed=true on all 3 calls; results/l1-unit-head.txt |
| F1 | same, against a real API server (envtest) with helm-faithful fixtures | 2 | **Confirmed** | changed=true on all 3 calls; results/l2-envtest-head.txt |
| F1 (post-removal) | after secrets are configured and then removed, the reconcile *after* the removal still reports changed=true forever | 1 | **Confirmed** | results/l1-unit-head.txt (`...EmptyStateIdempotentAfterRemoval`) |
| F1 mechanism | `base.Sync` returns early whenever `SyncRuntime` reports changed | 1 | Confirmed (gomock: later stages Times(0)) | results/l1-base-mechanism-head.txt |

## Per-finding detail

### F1 — nil-vs-empty `imagePullSecrets` equality makes the sync report "changed" forever for the default configuration

Mechanism:
1. The helm chart omits `imagePullSecrets` from the master/worker/fuse pod templates when none
   are configured (`{{- if .Values.imagePullSecrets }}` etc.), so the API server serves the field
   as **nil** (proven: `json:"imagePullSecrets,omitempty"` round-trip test, and asserted against
   the real envtest API server before running `SyncRuntime`).
2. `SyncRuntime` computes the desired value via `parseRuntimeImage`/`parseFuseImage`, whose
   fallback `docker.GetImagePullSecretsFromEnv` returns a **non-nil empty slice**
   (`[]corev1.LocalObjectReference{}`) when the env is unset.
3. `reflect.DeepEqual(nil, []…{})` is false, so every call updates the workload (writing back
   `nil` — a content no-op that still bumps resourceVersion) and sets `changed=true`.
4. `base.Sync` does `if updated { return }`, so health check, runtime status, dataset cache,
   dataset mounts and cache-node schedule info are skipped on every such reconcile. The runtime
   controller requeues every ~90s by default (`defaultRuntimeReconcileDuration`), so this never
   settles for any AlluxioRuntime that does not configure imagePullSecrets — i.e. the default
   configuration.

Observed vs expected:
- expected: `changed=false` on every call when nothing is configured (stable no-op)
- observed: `changed=true` on every call (L1 fake client, L2 real API server), incl. the
  reconcile after a real change has already been applied

Harness-bites: temporarily applying a nil/empty-aware equality (`len==0 && len==0 → equal`)
flips all contract tests green on L1 and L2 while the PR's own
`TestAlluxioEngine_SyncRuntime` keeps passing; the fix was then reverted (production diff empty).
results/harness-bites-fix-flip.txt

## Proposed fixes (NOT applied to production here)
- **F1**: in `syncStatefulSetImagePullSecrets`/`syncDaemonSetImagePullSecrets`, treat nil and
  empty as equal before comparing, e.g.
  `if len(cur)==0 && len(desired)==0 { return nil }` before the `reflect.DeepEqual` check
  (or normalize both sides with `append([]corev1.LocalObjectReference(nil), ...)` before
  comparing). Add a regression test that calls `SyncRuntime` twice on a default-config runtime
  and asserts `changed=false` on both calls.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/alluxio-sync-imagepullsecrets-codex` (production code
untouched), so it grafts onto whatever the fixed code is.

```bash
git fetch https://github.com/cheyang/fluid.git verify/alluxio-sync-imagepullsecrets-codex
git checkout <fixed-ref>
git checkout FETCH_HEAD -- docs/verification/alluxio-sync-imagepullsecrets-codex \
  pkg/ddc/alluxio/sync_runtime_codex_verify_test.go \
  pkg/ddc/alluxio/sync_runtime_codex_verify_envtest_test.go \
  pkg/ddc/base/sync_shortcircuit_codex_verify_test.go
bash docs/verification/alluxio-sync-imagepullsecrets-codex/scripts/re-verify.sh [<fixed-ref>]
```

Prerequisites: go toolchain; for L2, envtest assets (`KUBEBUILDER_ASSETS`, e.g. via
`setup-envtest`; the manifest auto-discovers `~/.local/share/kubebuilder-envtest/k8s/*`).
`re-verify.sh` with no argument fetches the current PR head from the PR URL in
`verify-manifest.json` and prints Fixed/Still-broken per finding; `.last-reviewed` records the
reviewed head for incremental rounds.

Kickoff prompt for a fresh agent: "Check out
`verify/alluxio-sync-imagepullsecrets-codex` from https://github.com/cheyang/fluid.git, read
`docs/verification/alluxio-sync-imagepullsecrets-codex/README.md`, then run
`bash docs/verification/alluxio-sync-imagepullsecrets-codex/scripts/re-verify.sh` and report
per-finding verdicts."

## Notes / environment noise (not findings)
- `TestAlluxio` (Ginkgo, `pkg/ddc/alluxio/metadata_test.go` "should successfully sync metadata
  and update dataset status") fails on this host **identically on the clean PR head without the
  harness** — pre-existing, unrelated to this PR's diff (149/150 specs pass).
