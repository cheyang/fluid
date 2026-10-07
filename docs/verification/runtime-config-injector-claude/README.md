# Verification — RuntimeConfigInjector (PR #6197) — Reviewer A (Claude)

Branch: `verify/runtime-config-injector-claude`, based on the PR head `c5ea086b`
(`origin/pr/6197`). Production code is **untouched** — the diff on this branch is
the harness (one additive test file + this docs/ tree) only.

## Premise (P0) — CONFIRMED

The PR claims (stage 2 of #6176): app pods of FUSE-less cache runtimes (Mooncake)
had no way to receive the runtime config that stage 1 (#6191) already writes as
`runtime.sh` into the ConfigMap `fluid-runtime-config-<dataset>`, so applications
hardcoded the master address; additionally the webhook lacked RBAC to read
`cacheruntimes` at all.

Verified on the **base** branch (`e0fc4c18`) — `scripts/premise-check.sh`:

- stage 1 is merged: `pkg/ddc/cache/engine/util.go` generates `runtime.sh` on base
- nothing on base delivers it to app pods: zero `runtime.sh` consumers under `pkg/webhook/`,
  no `fluid.io/datasets` annotation reader
- `base.GetRuntimeInfo` already branches on `common.CacheRuntime`, while the webhook
  chart ClusterRole lists every other runtime type but not `cacheruntimes` → the RBAC
  fix (commit `c5ea086b`) fixes a real, pre-existing gap
- all seven webhook plugins registered on base are FUSE-oriented (no clientless path)

Raw output: `results/premise-check.txt`.

## Findings — observed vs expected

| id | severity | claim | how verified | observed vs expected |
| --- | --- | --- | --- | --- |
| F1 | major | on `helm upgrade` over an existing install, the `webhook-plugins` ConfigMap keeps the pre-PR profile (no `clientless` group) via the template's `lookup` (values default `forceReplacePluginsProfile: false`), while the new `clientless.fluid.io` webhook rule is applied unconditionally → pods labeled `fluid.io/inject=true` are intercepted but admitted with **zero injection**, silently | `scripts/chart_lookup_branch_sim.go` (real template + real values.yaml, stubbed `lookup`) + Go-level consequence test (F1 canary) | **Confirmed.** Case B (upgrade, lookup hits old CM) renders a profile with no `clientless`; the Go canary shows MutatePod then no-ops (err=nil, nothing injected). Expected: profile/behavior consistent with the new webhook rule. |
| F2 | major | runtime ConfigMap missing (not created yet, or informer lag on the plugin's **cached** client — `RegisterMutatingHandlers(mgr.GetClient())`) → the plugin returns a plain error that is *not* `NeedRetryWithApiReaderError`; `Handle()` logs it and admits the pod **without** the injection; `failurePolicy: Fail` never fires because the webhook answers Allowed | F2 canary through `MutatePod` on a fake client without the ConfigMap | **Confirmed** (control-flow half). `err != nil` and `IsNeedRetryWithApiReaderError(err) == false` → per `Handle()`: logged, no retry, pod admitted unmutated. The informer-lag window itself is reasoning-only (fake client cannot lag); mechanism cited in README below. |
| T3 | info | happy path end-to-end through MutatePod | T3 contract | Passing — volume, readOnly mount, `FLUID_RUNTIME_CONFIG_PATH_MOONCAKE`, done label all correct |
| T4 | info | nonexistent dataset in the annotation → retryable error → pod creation rejected after direct-reader retry (documented) | T4 contract | Passing — matches the docs' promised hard-fail semantics |

F2 reasoning-only half: `cmd/webhook/app/webhook.go:170` calls
`plugins.RegisterMutatingHandlers(mgr.GetClient())`; `runtime_config_injector.go`
reads the ConfigMap through that cached client. A ConfigMap created moments
earlier by the cacheruntime reconcile appears in the informer store only after
propagation, so "not found" can mean lag. Because that error is not marked
retryable, the handler's direct-reader retry never happens for the plugin (the
author acknowledges this in the PR description). Reproducing the lag needs a real
API server + informer (envtest/live), which this round skips.

## Failure-semantics inconsistency (F2 context)

- dataset named in the annotation does not exist / not Bound → pod **rejected** (T4, documented)
- ConfigMap exists but has no `runtime.sh`, or is not yet visible to the plugin → pod **admitted with no injection**, reason only in webhook logs (documented in the PR docs as a known wrinkle; the author asked reviewers whether to unify)

Both behaviors are documented by the author; the harness encodes the current
state as canaries so a future unification flips them.

## How to run

From a checkout of this branch (repo root):

```bash
# unit layer incl. harness (gomonkey: keep -gcflags=all=-l)
go test -gcflags=all=-l ./pkg/webhook/handler/mutating/ -ginkgo.focus="reviewer harness" -v

# PR's own suites
go test -gcflags=all=-l ./pkg/webhook/... ./pkg/common/...
go test -gcflags=all=-l ./pkg/ddc/cache/...

# chart semantics (F1) + premise (P0)
go run docs/verification/runtime-config-injector-claude/scripts/chart_lookup_branch_sim.go
bash  docs/verification/runtime-config-injector-claude/scripts/premise-check.sh

# full chart render (needs helm)
helm template fluid charts/fluid/fluid
```

Prerequisites: Go ≥1.25 (module cache / vendor dir), helm for the render, git for
the premise check. No cluster needed for L1.

## Harness bites (Step 4 evidence)

- **F2**: temporarily wrapped the plugin's not-found error in
  `NewNeedRetryWithApiReaderError` in the production file, re-ran → the F2 canary
  went FAILED as expected (asserting non-retryable). Reverted; `git status` clean
  apart from the harness.
- **F1**: the CONTROL spec in the same file registers the with-`clientless`
  profile and observes full injection — canary and control differ *only* in the
  profile (the upgrade-state variable), proving the test detects the difference.

## Polarity table — continuing after the fix

| test | type | on current PR head | when fixed |
| --- | --- | --- | --- |
| F1 canary | canary | green (= silent no-op persists) | red → invert: expect the webhook to refuse/flag clientless pods when no clientless plugin is registered, or (chart fix) render the new profile |
| F2 canary | canary | green (= silent pass-through persists) | red → invert: expect the plugin error to be retryable or the admission to be denied |
| T3 | contract | green | stays green |
| T4 | contract | green | stays green (unless the author unifies failure semantics, then revisit) |

## Re-verify on the next round

```bash
bash docs/verification/runtime-config-injector-claude/scripts/re-verify.sh   # auto-fetches current PR head
bash docs/verification/runtime-config-injector-claude/scripts/re-verify.sh <sha-or-ref>
```

Canaries green = still broken; red = fixed (then invert them). Exit 0 iff all
findings fixed. After the round, advance `.last-reviewed` to the reviewed head and
commit+push.

## Skipped layer

Live (L3): a cluster was reachable from the review host (2 nodes, v1.36.2-aliyun,
shared), but this round runs unit + integration only per the review configuration.
The L3 signal for F1 is in `verify-manifest.json` → `layersSkipped.live.liveNote`.
