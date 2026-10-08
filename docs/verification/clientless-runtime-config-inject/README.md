# clientless-runtime-config-inject — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6197 (reviewer B / Codex, first round).

Layers run against the **code under review** (`origin/pr/6197` = c5ea086bb1c9352fafd73daac27f2c2e2eafd4c5):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | mutating handler routing + RuntimeConfigInjector behavior through `FluidMutatingHandler.Handle` with a fake client (pkg/webhook/handler/mutating/verify_pr6197_codex_test.go) | `go test -count=1 -gcflags=all=-l -v ./pkg/webhook/handler/mutating/ -run TestMutating -args -ginkgo.v` (gomonkey needs inlining off) |
| 2. Integration (chart) | rendered webhook configuration, webhook ClusterRole, plugins profile | `helm template fluid charts/fluid/fluid` (see results/helm-template-checks.txt) |
| 3. Live | not run in this round (debate setup skips KUBECONFIG layer) | — |

> Test polarity: the positive control is a **contract** test (must stay green). F1/F2/F3 are
> **canaries**: they assert the currently observed behavior and FLIP to red once the behavior
> changes — invert or promote them when a fix lands.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | For cache systems with no FUSE client (Mooncake), "an application pod that mounts the PVC stays in ContainerCreating forever"; apps must talk to the cache service directly but had no way to learn the master address. Also: the webhook ClusterRole lacks `cacheruntimes`, so the cached client's informer blocks ~15s and pod creation times out for CacheRuntime datasets. |
| Linked issue | #6176 (OPEN, "[FEATURES] support app pod using no fuse cache system without dataset volume") — PR is "part of #6176" (stage 2), no auto-close. Stage 1 (#6191, runtime.sh in the ConfigMap) is merged. |
| Reported component | webhook (pod admission) + cache runtime engine |
| Patched component | pkg/webhook (handler routing + new plugin), charts (webhook rule, plugins profile, RBAC), pkg/ddc/cache/engine (constants) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | merge-base e0fc4c1 already has `generateRuntimeSh` (stage 1) but the base chart has no `clientless` webhook rule and no `cacheruntimes` in the webhook ClusterRole (results/helm-template-checks.txt); `base.GetRuntimeInfo` resolves cache datasets via `utils.GetCacheRuntime` through the webhook's cached client (pkg/ddc/base/runtime.go:633, wired in pkg/webhook/handler/register.go:52), so the missing list/watch grant explains the 15s informer stall the author observed on kind. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0-ctrl | fresh install: labeled+annotated pod gets volume+mount+env injected | 1 | Confirmed (contract passes) | results/unit-handler-pr-head.txt |
| F1 | after `helm upgrade` with default values the preserved `webhook-plugins` ConfigMap has no `clientless` section, so the (newly added) webhook rule routes pods to an empty plugin list: admitted silently unmutated | 1 + chart read | Confirmed | canary passes on PR head: results/unit-handler-pr-head.txt; template logic: charts/fluid/fluid/templates/webhook/plugins-profile.yaml keeps `$existing.data.pluginsProfile` when `forceReplacePluginsProfile: false` (the default) |
| F2 | dataset names of 43–63 chars pass annotation validation (DNS-1033 label ≤63) but the plugin rejects them (>42, volume-name limit): admitted silently unmutated | 1 | Confirmed | canary passes: results/unit-handler-pr-head.txt |
| F3 | failure modes are inconsistent: missing/unbound dataset → pod rejected (fail-closed); missing configmap / missing runtime.sh → pod admitted without injection (fail-open, plugin error swallowed by `Handle`) | 1 | Confirmed | both canaries pass: results/unit-handler-pr-head.txt |
| bites | with `Handle` temporarily changed to fail closed, the F3 and F2 canaries flip to red | 1 | Harness bites | results/harness-bites-fail-closed.txt |

## Per-finding detail

### F1 — upgrade keeps the legacy plugins profile
`plugins-profile.yaml` does `lookup ... webhook-plugins` and, when it exists (any upgraded
cluster), re-emits the **old** profile verbatim unless `forceReplacePluginsProfile: true`
(default `false`). The PR adds the `clientless` plugin group only to values.yaml, and adds the
`clientless.fluid.io` webhook rule unconditionally. So after an upgrade the rule matches
labeled pods, the handler routes them to `GetClientlessPodWithDatasetHandler()` — which is an
empty list built from the old profile — and zero plugins run. No mutation, no error, no log at
default verbosity. Verified by registering handlers from the verbatim base-release profile and
admitting a fully-prepared pod: `Allowed=true`, no volume patch (canary). The positive control
(same pod, PR's profile → injected) shows the harness distinguishes the two profiles.

### F2 — 43..63-char dataset names fall between two validators
`CollectRuntimeInfosFromAnnotations` accepts any DNS-1035 label (≤63 chars);
`RuntimeConfigInjector` rejects names longer than `63-len("fluid-runtime-config-")=42`
because the volume name derives from the ConfigMap name. In between, collection succeeds
(runtime info resolved), the plugin errors, `Handle` logs and admits the pod unmutated
(F3's swallow). Canary: 50-char Bound cache dataset + existing ConfigMap → admitted, nothing
injected, no user-facing error. Fix direction: length-check in the collector (fail closed) or
a volume name that does not embed the full dataset name.

### F3 — fail-open on plugin errors vs fail-closed on collection errors
`Handle` only acts on `NeedRetryWithApiReaderError`; any other `MutatePod` error is logged and
the (unmutated) pod is patched through. So: dataset missing/unbound → collection error →
retry → 500 → rejected (fail-closed; documented). ConfigMap missing, `runtime.sh` key missing
(old cacheruntime-controller), or name too long → plugin error → **admitted with no injection,
no event, no retry** (fail-open; only a webhook log line). The fail-open window is permanent
for that pod: nothing re-triggers admission when the ConfigMap appears. The author flagged
this inconsistency in the PR body; the harness confirms both directions.

### Harness-bites check
A one-line temporary change making `Handle` fail closed on any mutation error flips the F3
fail-open canary and the F2 canary to red (results/harness-bites-fail-closed.txt), while the
positive control stays green. Reverted afterwards; production code is untouched
(`git diff origin/pr/6197 -- pkg/` shows only the additive test file).

## Proposed fixes (NOT applied to production here)
- **F1**: document `forceReplacePluginsProfile: true` (or hand-editing `webhook-plugins`) as a required upgrade step in the new docs; better, have the chart merge the `clientless` section into a preserved profile, or warn at webhook startup when the loaded profile has no `clientless` section.
- **F2**: move the length check into `CollectRuntimeInfosFromAnnotations` so the pod is rejected with a clear message, matching the other invalid-input behavior.
- **F3**: consider failing closed for "dataset is Bound but its runtime config ConfigMap/key is missing" (e.g. wrap plugin errors that indicate a not-ready runtime in `NeedRetryWithApiReaderError`, giving one direct-reader retry then rejection), or at least emit a warning Event on the pod.

## Continuing after the fix

On any machine:
```bash
git fetch https://github.com/cheyang/fluid.git verify/clientless-runtime-config-inject-codex
git checkout verify/clientless-runtime-config-inject-codex
bash docs/verification/clientless-runtime-config-inject/scripts/re-verify.sh   # resolves the current PR head from manifest.pr
```
Polarity table for re-runs: the positive control must stay **green**; F1/F2/F3 canaries are
**fixed only when they flip to red** — then invert the assertion (or promote to a contract
test). `.last-reviewed` records c5ea086bb1c9352fafd73daac27f2c2e2eafd4c5; advance it after
each round.
