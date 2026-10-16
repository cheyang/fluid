# clientless-runtime-config-inject — bug verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6197 (reviewer B / Codex).

Round 1 reviewed c5ea086b (findings F1–F3, premise P0 confirmed). Round 2 (this round)
reviews the delta c5ea086b..6d3a6d7e ("fix(webhook): fail loudly when runtime config
injection cannot happen") and re-verifies the whole PR at head 6d3a6d7e.

Layers run against the **code under review** (`origin/pr/6197` = 6d3a6d7e7a038d72fc5480f44d8285993f82a551):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (ginkgo) | mutating handler routing + RuntimeConfigInjector behavior through `FluidMutatingHandler.Handle` with a fake client (`pkg/webhook/handler/mutating/verify_pr6197_codex_test.go`) | `go test -count=1 -gcflags=all=-l ./pkg/webhook/handler/mutating/ -run TestMutating -args -ginkgo.v` |
| 2. Integration (chart, helm-equivalent render) | the real `templates/webhook/plugins-profile.yaml` text rendered with helm-identical function semantics and a stubbed `lookup`, asserting the upgrade merge (`chartmerge/chart_merge_test.go`) | `go test -count=1 ./docs/verification/clientless-runtime-config-inject/chartmerge/ -run 'TestPluginsProfileMerge|TestWebhookChecksumAnnotation|TestLegacyProfileFixtureMatchesBase|TestHelmTemplateFreshInstall'` |
| 2b. Integration (real helm + real API server) | the same upgrade path rendered by the real `helm` CLI with `lookup` resolved by an envtest API server holding a legacy `webhook-plugins` ConfigMap (`chartmerge/envtest_merge_test.go`) | `go test -count=1 ./docs/verification/clientless-runtime-config-inject/chartmerge/ -run TestHelmUpgradeMerges` (skips without helm/envtest assets) |
| 2c. Integration (watch contract) | the fluid-app controller's `ShouldInQueue` against a clientless-injected pod (`pkg/ctrl/watch/verify_pr6197_watch_test.go`) | `go test -count=1 ./pkg/ctrl/watch/ -run TestVerifyPR6197` |
| 3. Live | not run (debate setup skips KUBECONFIG layer) | — |

> Test polarity: all manifest-bound tests are **contract** tests now. In round 1, F1/F2/F3
> were **canaries** pinned to the buggy behavior; the delta flipped them (they went red),
> so this round inverted them into contracts asserting the fixed behavior. The F1
> handler-side spec stays as a `[characterization]` (documents what a legacy profile does
> to the handler) and is no longer manifest-bound.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | For cache systems with no FUSE client (Mooncake), "an application pod that mounts the PVC stays in ContainerCreating forever"; apps must talk to the cache service directly but had no way to learn the master address. Also: the webhook ClusterRole lacks `cacheruntimes`, so the cached client's informer blocks ~15s and pod creation times out for CacheRuntime datasets. |
| Linked issue | #6176 (OPEN, "[FEATURES] support app pod using no fuse cache system without dataset volume") — PR is "part of #6176" (stage 2), no auto-close. Stage 1 (#6191, runtime.sh in the ConfigMap) is merged. |
| Reported component | webhook (pod admission) + cache runtime engine |
| Patched component | pkg/webhook (handler routing + new plugin), charts (webhook rule, plugins profile, RBAC), pkg/ddc/cache/engine (constants) |
| Component match | Yes |
| **Verdict** | **Confirmed** (round 1; unchanged by the delta) |
| Evidence | merge-base e0fc4c1 already has `generateRuntimeSh` (stage 1) but the base chart has no `clientless` webhook rule and no `cacheruntimes` in the webhook ClusterRole (results/helm-template-checks.txt); `base.GetRuntimeInfo` resolves cache datasets via `utils.GetCacheRuntime` through the webhook's cached client (pkg/ddc/base/runtime.go:633, wired in pkg/webhook/handler/register.go:52). |

## Summary of results (round 2, head 6d3a6d7e)

| ID | Claim | Layer | Round-1 verdict | Round-2 verdict | Evidence |
|----|-------|-------|-----------------|-----------------|----------|
| P0-ctrl | fresh install: labeled+annotated pod gets volume+mount+env injected | 1 | Confirmed | still green (contract) | results/unit-head6d3a6d7e.txt |
| F1 | after `helm upgrade` with default values the preserved `webhook-plugins` ConfigMap had no `clientless` section → rule matches, zero plugins run, silent no-op | 2, 2b | Confirmed (canary) | **Fixed**: plugins-profile.yaml merges the missing `clientless` group into a preserved profile (user edits and pluginConfig kept; unmergeable profiles kept verbatim; second upgrade stable), and a new `checksum/plugins-profile` pod annotation rolls the webhook so it reloads the profile. Verified with helm-equivalent rendering AND with the real helm CLI resolving `lookup` against an envtest API server seeded with a marked legacy profile (marker + clientless both present; on the previous head the same test fails with "merged profile lacks clientless"). | results/integration-head6d3a6d7e.txt, results/integration-prevhead-c5ea086b.txt |
| F2 | 43–63-char dataset names passed annotation validation but the plugin rejected them → admitted silently unmutated | 1 | Confirmed (canary) | **Fixed**: `CollectRuntimeInfosFromAnnotations` rejects names > `common.MaxClientlessDatasetNameLength` (42) up front → collection error → retry → `Denied` (403). The plugin keeps a `RejectPodError` guard for the PVC-derived path. | results/unit-head6d3a6d7e.txt, results/unit-prevhead-c5ea086b.txt |
| F3 | inconsistent failure modes: missing/unbound dataset rejected (fail-closed) but missing ConfigMap / missing `runtime.sh` admitted unmutated (fail-open) | 1 | Confirmed (canary) | **Fixed**: the plugin now returns `NeedRetryWithApiReaderError` for a missing ConfigMap/key (one API-reader retry, then `Denied` for inject-labeled pods) and `RejectPodError` for permanent spec problems (name length, volume/mount conflicts, fuse-label combination). Both deny the pod with a reason; the docs match. | results/unit-head6d3a6d7e.txt, results/unit-prevhead-c5ea086b.txt |
| F4 | (author's open question) the injection completion marker reuses `done.sidecar.fluid.io/inject`, also consumed by the fluid-app controller | 2c | — (drafted in an abandoned round-0 harness; adopted this round) | **Refuted (no bug)**: `ShouldInQueue` requires a privileged fuse sidecar (label + fuse container), so a clientless-injected pod is never enqueued; the privileged-sidecar control still enqueues. Passes identically on both heads (code path untouched by the PR). | results/integration-head6d3a6d7e.txt |
| F5 | (new, minor) for an over-long name given via the annotation, the denial message names the dataset but not the reason ("too long" is only in the webhook log), although the docs say the reason is in the error | 1 | — | Confirmed as current behavior (characterization test pins it) | results/unit-head6d3a6d7e.txt |
| F6 | (new, nit) the checksum annotation render changes once more on the *second* upgrade (verbatim branch re-indents the stored profile, which carries a block-scalar trailing newline), causing one spurious webhook rollout with no profile change | probe | — | Confirmed (probe; not manifest-bound) | results/checksum-churn-probe.txt |

### Harness-bites check (round 2 form)

Contract tests must be red on the code that had the bug. Grafted onto the previous head
c5ea086b: the F2/F3 denial contracts fail (pods were admitted unmutated), the cache-PVC
injection contract fails (plugin looked up the ConfigMap by PVC name and silently failed),
the F1 chart contracts fail (no merge, no checksum annotation), and the envtest test fails
with "F1 STILL PRESENT … merged profile lacks clientless". P0-ctrl, the F4 watch contracts
and the F1 handler characterization pass on both heads, as they should (unchanged paths).
Full matrix: results/unit-prevhead-c5ea086b.txt, results/integration-prevhead-c5ea086b.txt.

### Delta observations beyond the findings (verified, no bug)

- `mergeClientlessRuntimeInfos` re-keys PVC-resolved runtime infos by dataset name and
  drops non-cache runtimes. This fixes a latent trap at c5ea086b: a pod with the inject
  label that mounts a cache-runtime PVC made the plugin look up the ConfigMap by *PVC
  name* and fail silently; at 6d3a6d7e it is injected correctly (contract test). A pod
  with the inject label and a fuse-runtime PVC is now admitted untouched (contract test).
- The new rejection of `fluid.io/inject` combined with fuse injection labels cannot break
  existing pods: the label is introduced by this PR, so no running pod can carry the
  combination.
- `IsNeedRetryWithApiReaderError`/`IsRejectPodError` now use `errors.As`, so wrapped
  instances are recognized; all plugin error paths in the clientless group are typed.
- Residual, inherent: between the MutatingWebhookConfiguration update and the webhook
  rollout finishing, a clientless-labeled pod can still be admitted by the old pod with
  the old in-memory profile (seconds, self-heals by recreating the pod). Same window
  exists for any webhook profile change.

## Proposed fixes (NOT applied to production here)

- F5: include the per-dataset reason in the aggregated annotation error (e.g. collect
  `name: reason` pairs), or reject over-long names with a dedicated message before the
  aggregate.
- F6: trim the stored profile before re-indenting in the verbatim branch
  (`indent 4 (trimSuffix "\n" $existingProfile)`), so the checksum annotation is stable
  from the first post-merge upgrade on.

## Continuing after the fix

On any machine:
```bash
git fetch https://github.com/cheyang/fluid.git verify/clientless-runtime-config-inject-codex
git checkout verify/clientless-runtime-config-inject-codex
bash docs/verification/clientless-runtime-config-inject/scripts/re-verify.sh   # resolves the current PR head from manifest.pr
```
All manifest findings are contract tests: they must stay green. `.last-reviewed` records
6d3a6d7e7a038d72fc5480f44d8285993f82a551; advance it after each round. Layer 2b needs a
`helm` binary and envtest assets (`KUBEBUILDER_ASSETS` or a known local path) and skips
cleanly without them.
