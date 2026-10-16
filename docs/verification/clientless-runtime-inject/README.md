# clientless-runtime-inject — verification harness (Reviewer A, Claude)

Reproducible evidence for the review of [PR #6197](https://github.com/fluid-cloudnative/fluid/pull/6197)
(`feat(webhook): inject the runtime config into app pods of client-less cache runtimes`).

Layers run against the **code under review** (`origin/pr/6197` = `6d3a6d7e`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | plugin logic + canaries | `go test -count=1 ./pkg/webhook/... ./pkg/common/... && go test -count=1 -gcflags=all=-l ./pkg/ddc/cache/...` |
| 2. Integration | full `admission.Handler` chain (decode → MutatePod → retry with API reader → admit/deny) against fake clients | `go test -count=1 ./pkg/webhook/handler/mutating/...` |
| Chart | rendered webhook rule / plugin profile / RBAC + upgrade-merge round-trip | `bash scripts/re-verify.sh` (chart section) or `python3 -I docs/verification/clientless-runtime-inject/scripts/chart_merge_check.py` |
| 3. Live | kind cluster | **skipped** per debate instructions (unit + integration layers only) |

> Polarity: `findings_claude_test.go` holds **bug-canaries** — they assert the current
> behavior and must FLIP to red (then be inverted) when a fix lands. Everything else is a
> contract layer that must stay green.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | App pods of a fuse-less cache system (Mooncake) have no way to receive the runtime config; the master address must be hardcoded in the app. Additionally (PR §Ⅳ): pods that touch a CacheRuntime dataset hit a 15 s stall in the webhook because the cached client cannot list `cacheruntimes`. |
| Linked issue | none closing; PR is "part of #6176" (phase 2; phase 1 runtime.sh generation merged via #6191) |
| Reported component | fluid mutating webhook + its chart RBAC |
| Patched component | same: `pkg/webhook/**` + `charts/fluid/fluid/templates/**` |
| Component match | Yes |
| **Verdict** | **Confirmed** (differential/code-path evidence) |
| Evidence | `results/premise-rbac-diff.txt` — base ClusterRole lists every runtime except `cacheruntimes` while `base.GetRuntimeInfo` reads `CacheRuntime` through `mgr.GetClient()` (cached); no `fluid.io/inject` label, no clientless plugin group, no runtime-config delivery code exists on base. |

The 15 s stall symptom itself is not reproduced here (needs a live cluster, skipped); the
missing-RBAC precondition for it is positively verified on the base chart, and the mechanism
(controller-runtime cache Get waits for an informer that cannot list/watch without RBAC) is
consistent with the code.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | problem exists on base (no delivery mechanism + RBAC gap) | diff | Confirmed | `results/premise-rbac-diff.txt` |
| C-SUITE | PR's own unit/integration tests all pass on head | 1+2 | Confirmed (green) | `results/go-webhook-common.txt`, `results/go-cache.txt` |
| C-CHART | chart renders webhook rule + profile + RBAC; upgrade merge round-trips | chart | Confirmed (green) | `results/helm-render.txt`, `results/chart-merge.txt` |
| F1 | non-cache dataset in `fluid.io/datasets` → misleading "may not be ready yet" denial | 1 | Confirmed (canary green) | `results/canaries.txt` |
| F2 | dotted dataset name via PVC merge path → invalid volume name injected | 1 | Confirmed (canary green) | `results/canaries.txt` |

Harness-bites check (Step 4 of the verifier skill): both proposed fixes were applied
temporarily to `runtime_config_injector.go`, the canaries flipped to red (F1: plugin rejects
with a clear message; F2: plugin rejects the invalid volume name), and the production change
was reverted — the working tree at push time contains **no production diff**.

## Per-finding detail

### F1 — annotation path does not check the runtime type (minor)

`pkg/webhook/utils/runtime_info.go` `CollectRuntimeInfosFromAnnotations` resolves any bound
dataset, e.g. an Alluxio one, and `RuntimeConfigInjector` then looks for the
`fluid-runtime-config-<name>` ConfigMap that only cache runtimes create. The pod is denied
(after the API-reader retry) with `configmap ... not found, the cache runtime of dataset
"alluxio-demo" may not be ready yet` — which tells the user to wait for a state that will
never happen. Canary: `findings_claude_test.go` → "F1: a dataset of a fuse-served runtime...".

### F2 — PVC merge path skips the dataset-name shape validation (minor)

Names arriving through `fluid.io/datasets` are validated with `IsDNS1035Label` (no dots),
but a dataset resolved from a **mounted PVC** (`mergeClientlessRuntimeInfos`) is not. A
CacheRuntime dataset named `a.b` (legal k8s object name) yields volume
`fluid-runtime-config-a.b`, which fails volume-name validation at the API server with an
error that does not point at fluid. Canary: `findings_claude_test.go` → "F2: a cache dataset
whose name is not a DNS-1035 label...".

## Proposed fixes (NOT applied to production here)

- **F1**: in `RuntimeConfigInjector.MutateWithReader`, reject datasets whose
  `GetRuntimeType() != common.CacheRuntime` with a message naming the runtime type (or skip
  them and log, if silent-pass is preferred — then the docs should say so).
- **F2**: validate `common.GetCacheRuntimeConfigConfigMapName(ds)` with
  `validation.IsDNS1123Label` alongside the existing length check.

## Continuing after the fix (possibly on another machine)

1. Graft the harness onto the fixed ref:
   ```bash
   git fetch origin pull/6197/head
   git checkout <fixed-ref>
   git checkout verify/clientless-runtime-inject-claude -- \
     docs/verification/clientless-runtime-inject \
     pkg/webhook/plugins/runtimeconfiginjector/findings_claude_test.go
   ```
2. Prereqs: Go toolchain, helm (chart layer only), python3+pyyaml (merge check only).
3. Re-run: `bash docs/verification/clientless-runtime-inject/scripts/re-verify.sh`
4. Read results via polarity: contract layers stay green; the two canaries must FLIP to red
   once F1/F2 are fixed — invert them into contract tests at that point.
5. `.last-reviewed` records the sha this round reviewed; the script prints the delta command.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/clientless-runtime-inject-claude
(fork https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6197 produced findings F1/F2 (see
docs/verification/clientless-runtime-inject/README.md); canaries reproduce them on the PR
head. The PR has now been updated. Read the README ("Continuing after the fix"), graft the
harness onto the current PR head, run scripts/re-verify.sh, apply canary polarity (invert
flipped canaries), and report an observed-vs-expected table. Do not touch production code
or push anywhere but the reviewer fork's verify/* branches.
```
