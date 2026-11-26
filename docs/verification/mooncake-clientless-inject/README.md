# Verification — mooncake clientless runtime-config injection e2e (PR #6286)

Reviewer: Claude (Reviewer A, PR-debate round 2026-11-27)
Branch: `verify/mooncake-clientless-inject-claude` (based on PR head `74d77d9b`)
Production code touched by this harness: **none** — the PR itself only changes
`test/gha-e2e/mooncake/**`, and the harness is additive on top.

## Premise (P0) — verdict: **Confirmed**

Issue #6251 (open, 2026-11-15) asks for e2e coverage of #6197's clientless runtime
config injection under `test/gha-e2e/mooncake/`, plus the jq→runtime.sh worker
simplification. The merge-base (`07efc760`) has **zero** mentions of
`clientless.fluid.io` in the mooncake e2e, and the worker branch of
`custom-entrypoint.sh` still parses `runtime.json` with jq. Coverage gap is real;
component matches the files the PR touches; the runtime.sh the worker now sources
is generated on the base branch (`pkg/ddc/cache/engine/cm.go::generateRuntimeSh`),
so the worker-side simplification does not depend on #6197.

**Important dependency note (F1):** the app-pod injection half of this e2e can only
pass on a cluster whose chart registers the `clientless.fluid.io` mutating webhook —
that code lives in **#6197, not merged**. On master (and therefore on this PR's own
CI, which deploys the chart from the PR branch), `check_missing_dataset_rejected`
always fires: the server-side dry-run is admitted because no webhook is registered.
This is the documented cause of the PR's five red CI runs and is a merge-order
constraint, not a defect in the test code. T_rejection_grep__fires_when_webhook_absent
pins the fail-fast behavior so the test cannot silently pass on a webhook-less cluster.

## What this harness proves (L1, deterministic)

Every check extracts the **real shipped artifacts** at run time (the rw job script
from `rw_job.yaml` via PyYAML, the jq program verbatim out of `test.sh`, the
entrypoint script from the image dir), so drift in the PR invalidates extraction
rather than testing stale copies.

| claim | check | observed |
| --- | --- | --- |
| P0 no clientless coverage on base | `T_premise__no_clientless_coverage_on_base` | pass — base has 0 mentions, head adds the label |
| rw job YAML parses; heredoc terminator at col 0; command still `[/bin/sh, -ec]` | `T_rw_job_script__yaml_parses` | pass |
| rw job sources the injected runtime.sh; MASTER_NAME/MASTER_SERVICE_NAME/POD_IP reach the exec'd process | `T_rw_job_script__sources_runtime_sh` | pass — env dump shows `MASTER_NAME=mooncake-demo-master`, `MASTER_SERVICE_NAME=svc-mooncake-demo-master` |
| rw job fails fast with a clear message when the webhook did not inject | `T_rw_job_script__fails_fast_without_injection` | pass — `:?` guard prints `missing injected runtime config`, rc≠0 |
| (bite) the positive check depends on the MASTER_NAME export | `T_rw_job_script__bite_missing_master_name` | pass — fixture without the export → stub records `KeyError: 'MASTER_NAME'`, script fails |
| worker entrypoint sources runtime.sh and computes master/metadata/segment/host exactly | `T_worker_entrypoint__sources_runtime_sh` | pass — `--master_server_address=svc-mooncake-demo-master.fl.svc.cluster.local:50051`, `--global_segment_size=1GB` (1Gi→GB), `--host=<pod>.svc-mooncake-demo-worker.<ns>.svc.cluster.local` |
| worker entrypoint fails fast on unset/empty config path | `T_worker_entrypoint__missing_path_fails` | pass — `missing runtime config path`, rc≠0 |
| worker entrypoint survives a config path containing spaces | `T_worker_entrypoint__path_with_spaces` | pass |
| (bite) the --host assertion is discriminating | `T_worker_entrypoint__bite_wrong_service_name` | pass — typo'd service name is caught |
| the shipped jq program accepts a correctly injected pod | `T_injection_jq__injected_pod_passes` | pass |
| …and rejects bare / volume-only / wrong-env-value / non-readonly / empty pod list | 5 negative fixtures | pass — all rejected |

The injected-pod fixture mirrors exactly what #6197's `buildConfigMount` produces
(volume `fluid-runtime-config-mooncake-demo`, configMap of the same name, read-only
mount at `/etc/fluid/config/mooncake-demo`, env
`FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO=/etc/fluid/config/mooncake-demo/runtime.sh`).

## Observed vs expected

| finding | verdict | evidence |
| --- | --- | --- |
| F1 (major, merge-order): `check_missing_dataset_rejected` cannot pass until #6197 merges; CI on this PR is red by construction | mechanism **confirmed by code-path** (not live-run) | webhook exists only in #6197's chart (`charts/fluid/fluid/templates/webhook/webhookconfiguration.yaml`, name `clientless.fluid.io`); master's chart has no such webhook; `T_rejection_grep__fires_when_webhook_absent` proves the check fires when the dry-run is admitted |
| Rejection substring assertions match the actual denial message #6197 produces | **confirmed by code-path** | traced: `CollectRuntimeInfosFromAnnotations` → `checkDatasetBound` → `GetDataset` NotFound (`datasets.data.fluid.io "mooncake-missing" not found`) → wrapped twice → `admission.Denied(err.Error())`; fixture `kubectl-denial.txt` matches both greps |
| rw job env contract (`FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO`) matches #6197's `envSuffix` | **confirmed** | `EnvRuntimeConfigPathPrefix + envSuffix("mooncake-demo")` == the hardcoded name; sourced vars `MASTER_NAME`, `MASTER_SERVICE_NAME` exist in runtime.sh (pinned upstream by `TestGenerateRuntimeShExportsTieredStoreLevels` etc.) |
| Worker sourcing works on base master without #6197 | **confirmed by code-path + L1** | base `cm.go` writes both `runtime.json` and `runtime.sh` keys into one ConfigMap mounted at `/etc/fluid/config`; `FLUID_RUNTIME_CONFIG_PATH=/etc/fluid/config/runtime.json` (base `transform_common.go`) so `dirname` lands on the dir holding runtime.sh |
| No defect found in the PR's own shell/jq logic | L1 all green (19/19) | `results/l1-details.txt` |

## Harness-bites check

No production-code fix was proposed (the PR changes no production code), so the
"classic" apply-fix-then-revert bite step does not apply. Instead the harness proves
it bites through negative fixtures: the jq program rejects five malformed pods, the
python stub fails without the `MASTER_NAME` export, and a typo'd worker service name
is caught by the `--host` assertion. All bite checks are green.

## How to run

```bash
bash docs/verification/mooncake-clientless-inject/scripts/run_l1.sh
# or the whole re-verify flow (fetches the current PR head, grafts this harness, runs L1):
bash docs/verification/mooncake-clientless-inject/scripts/re-verify.sh
```

Prerequisites: bash, jq, python3 (+PyYAML), git. No cluster needed for L1.

## Live layer (L3) — deferred, required before merge

Run `bash test/gha-e2e/mooncake/test.sh` on a kind cluster whose fluid deployment
includes #6197's webhook (the PR's own CI does this automatically once #6197 is the
base). Expected observable signals are in the manifest's `liveNote`. The author
reports a full pass on kind/Kubernetes v1.33.2 with #6197's `9c8d0e9f` applied, at
both `ca72c8ae` and `74d77d9b`.

## Continuing after the fix / next round

1. `git fetch origin pull/6286/head && git checkout verify/mooncake-clientless-inject-claude`
2. `bash docs/verification/mooncake-clientless-inject/scripts/re-verify.sh` — it
   resolves the current PR head from the manifest's `pr` URL, grafts the harness,
   runs L1, and prints per-finding Fixed / Still-broken / Partial / Harness-update.
3. If a check reports Harness-update, the PR rewrote the extracted artifact
   (script moved, patterns changed) — update `checks.sh` extraction, then re-run.
4. Advance `.last-reviewed` to the reviewed sha and commit.

All findings here are contract tests: they should stay green. A red L1 after a
future PR-head push means the e2e's own logic changed — treat it as Still-broken
for the corresponding claim, not as harness noise.
