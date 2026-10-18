# clientless-runtime-inject — verification harness (Reviewer A, Claude)

Reproducible evidence for the review of [PR #6197](https://github.com/fluid-cloudnative/fluid/pull/6197)
(`feat(webhook): inject the runtime config into app pods of client-less cache runtimes`).

Rounds:

- **Round 2** (head `6d3a6d7e`): canaries confirmed findings A-F1/A-F2 (see git history of this file).
- **Round 3** (head `268012a5`, this state): the round-2 canaries **flipped** — both findings
  are fixed — and were inverted into contract tests; contracts were added for B-F5 (annotation
  name denials carry the reason), A-N1 (exact fuse-sidecar name match) and B-F6 (chart checksum
  stability across the second upgrade). Everything is green at head.

Layers run against the **code under review** (`origin/pr/6197` = `268012a5`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | plugin logic + reviewer contracts | `go test -count=1 ./pkg/webhook/... ./pkg/common/... && go test -count=1 -gcflags=all=-l ./pkg/ddc/cache/...` |
| 2. Integration | full `admission.Handler` chain (decode → MutatePod → retry with API reader → admit/deny) against fake clients | `go test -count=1 ./pkg/webhook/handler/mutating/...` |
| Chart | rendered webhook rule / plugin profile / RBAC + upgrade-merge round-trip + checksum stability | `bash scripts/re-verify.sh` (chart section), or `chart_merge_check.py` / `checksum_churn_check.py` |
| 3. Live | kind cluster | **skipped** per debate instructions (unit + integration layers only) |

> Polarity: `findings_claude_test.go` and `findings_claude_runtime_info_test.go` now hold
> **contract tests** of the fixed behavior (green = the fixes hold; red = a regression
> re-opened a finding). Everything else is a contract layer that must stay green.

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
| C-SUITE | PR's own unit/integration tests all pass on head | 1+2 | Confirmed (green) | `results/go-webhook-common.txt`, `results/go-cache.txt`, `results/go-handler.txt` |
| C-CHART | chart renders webhook rule + profile + RBAC; upgrade merge round-trips | chart | Confirmed (green) | `results/helm-render.txt`, `results/chart-merge.txt` |
| A-F1 | non-cache dataset in `fluid.io/datasets` → rejected naming the runtime type | 1 | **Fixed** at `268012a` (commit 89d26fe2) | canary flipped; contract green (`results/canaries.txt`) |
| A-F2 | dotted dataset name via PVC merge path → invalid volume name injected | 1 | **Fixed** at `268012a` (commit c76c0a72) | canary flipped; contract green (`results/canaries.txt`) |
| A-N1 | `fluid-fuse` prefix skip caught user containers named `fluid-fuse-*` | 1 | **Fixed** at `268012a` (commit 9d9bd872) | contract green, red on 6d3a6d7 (bite) |
| B-F5 | over-long/invalid annotation name denial did not state the reason | 1 | **Fixed** at `268012a` (commit c01cbcce) | contract green, red on 6d3a6d7 (bite) |
| B-F6 | one-off checksum churn on the second upgrade | chart | **Fixed** at `268012a` (commits 16c5509e + 760f5369) | `results/checksum-churn.txt` |

Harness-bites checks (Step 4 of the verifier skill):

- Round 2: both proposed fixes were applied temporarily to `runtime_config_injector.go`, the
  canaries flipped to red, then the production change was reverted.
- Round 3: the **inverted** contracts (A-F1, A-F2, A-N1, B-F5) were run against a graft of the
  previous head `6d3a6d7e` — all fail there (A-F1: not a RejectPodError; A-F2: volume injected;
  A-N1: container skipped; B-F5: reason missing) and pass on head. The B-F6 script models both
  the fixed template (byte-identical, PASS) and the pre-fix template (CHURN, reported under
  `--show-prefix`), and cross-checks its toYaml/indent model against the real `helm template`
  render. No production diff ships on this branch.

## Per-finding detail

### A-F1 — annotation path does not check the runtime type (minor; fixed)

`pkg/webhook/utils/runtime_info.go` `CollectRuntimeInfosFromAnnotations` resolves any bound
dataset, e.g. an Alluxio one, and `RuntimeConfigInjector` then looked for the
`fluid-runtime-config-<name>` ConfigMap that only cache runtimes create, denying the pod with
"may not be ready yet". Fixed by commit 89d26fe2: the plugin rejects datasets whose
`GetRuntimeType() != common.CacheRuntime` with a message naming the type and suggesting the
PVC mount. Contract: `findings_claude_test.go` → "A-F1: a dataset of a fuse-served runtime…".

### A-F2 — PVC merge path skipped the dataset-name shape validation (minor; fixed)

Names arriving through `fluid.io/datasets` are validated with `IsDNS1035Label` (no dots), but a
dataset resolved from a **mounted PVC** (`mergeClientlessRuntimeInfos`) was not: a CacheRuntime
dataset named `a.b` (legal k8s object name) yielded volume `fluid-runtime-config-a.b`, which
fails volume-name validation at the API server with an error that does not point at fluid.
Fixed by commit c76c0a72: `validation.IsDNS1123Label` on the derived volume name before any
mutation. Contract: `findings_claude_test.go` → "A-F2: a cache dataset whose name is not a
DNS-1123 label…".

### A-N1 — container-name prefix skip (nit; fixed)

`injectToContainers` and both conflict checks skipped containers whose *name has the prefix*
`fluid-fuse`; a clientless pod carries no fuse container, so the skip could only hit an app
container with an unlucky name and silently leave it without the env var. Fixed by commit
9d9bd872: `isFuseSidecar` matches exactly the names fluid generates — `fluid-fuse`
(`pkg/ddc/base/runtime_helper.go:52`) and `fluid-fuse-<index>`
(`pkg/application/inject/fuse/injector.go:207`, `fmt.Sprintf("-%d", idx)`); `init-fluid-fuse-*`
was never matched by either version (unchanged). Contract: `findings_claude_test.go` → "A-N1".

### B-F5 — over-long annotation name denial lacked the reason (minor; fixed)

The denial for an over-long/invalid dataset name given via the annotation was the generic
aggregate "failed to get the following datasets […]" while the reason only appeared in the
webhook log, contradicting the docs' "rejected, with the reason in the error". Fixed by commit
c01cbcce: per-dataset reasons are carried into the aggregate. Contracts:
`findings_claude_runtime_info_test.go` (too-long, DNS-1035, not-bound).

### B-F6 — one-off checksum churn on the second upgrade (nit; fixed)

The verbatim branch re-indented the stored profile together with its trailing newline, so the
second upgrade re-rendered the same parsed profile with different bytes and the
`checksum/plugins-profile` annotation rolled the webhook once more. Fixed by commits 16c5509e +
760f5369: `| trimSuffix "\n"` before `indent 4` — the render is byte-identical to the first
upgrade's. Script: `scripts/checksum_churn_check.py` (models sprig `indent`/`trimSuffix`, helm
`toYaml` and block-scalar clip semantics; the fresh-install model is cross-checked against the
real `helm template` output).

### Still open (not addressed by the round-3 delta; tracked in the findings ledger)

- **A-F3** (minor): a user-supplied `webhook.pluginsProfile` in values gets no clientless
  merge, and the new webhook rule has no values-level disable gate — mirrors the pre-existing
  serverless/serverful behavior, so not a regression.
- **A-F4** (hypothesis, minor): the first admission attempt reads the runtime-config ConfigMap
  through the cached client, which starts a cluster-wide ConfigMap informer for the webhook
  process lifetime (scale consideration; not measurable with the live layer skipped).

## Continuing after the fix (possibly on another machine)

1. Graft the harness onto the fixed ref:
   ```bash
   git fetch origin pull/6197/head
   git checkout <fixed-ref>
   git checkout verify/clientless-runtime-inject-claude -- \
     docs/verification/clientless-runtime-inject \
     pkg/webhook/plugins/runtimeconfiginjector/findings_claude_test.go \
     pkg/webhook/utils/findings_claude_runtime_info_test.go
   ```
2. Prereqs: Go toolchain, helm (chart layer only), python3+pyyaml (chart scripts only).
3. Re-run: `bash docs/verification/clientless-runtime-inject/scripts/re-verify.sh`
4. Read results via polarity: all layers and contracts stay green; a red findings-contract
   suite means a regression re-opened A-F1/A-F2/A-N1/B-F5, a red B-F6 line means the chart
   churn is back.
5. `.last-reviewed` records the sha this round reviewed; the script prints the delta command.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/clientless-runtime-inject-claude
(fork https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6197; rounds 2-3 confirmed and then verified
fixes for findings A-F1/A-F2/A-N1/B-F5/B-F6 (see
docs/verification/clientless-runtime-inject/README.md for the table). The PR has been updated
again. Read the README ("Continuing after the fix"), graft the harness onto the current PR
head, run scripts/re-verify.sh, and report an observed-vs-expected table (any red contract =
a regression). Do not touch production code or push anywhere but the reviewer fork's
verify/* branches.
```
