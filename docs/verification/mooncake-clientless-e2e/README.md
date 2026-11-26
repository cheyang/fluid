# Verification — PR #6286 "test(e2e): verify runtime config injection for clientless Mooncake pods"

Reviewer: codex (Reviewer B). Round: initial. Base `07efc760`, head `74d77d9b`.
Production code is untouched by this branch; everything here is additive
(`docs/verification/**`, one `zz_codex_*_test.go` in `pkg/ddc/cache/engine`).

## Premise (P0) — Confirmed

Issue #6251 (open) asks for e2e coverage of the client-less runtime config
injection added by #6197, inside the Mooncake e2e. This is a tests-only PR, so
the full premise machinery is carved out, but the contracts the new assertions
lean on were checked anyway:

- `runtime.sh` generation exists on **base** (`pkg/ddc/cache/engine/cm.go`) and
  exports exactly the variables the rewritten entrypoint/job consume —
  pinned by `TestRuntimeShContractForMooncakeE2E` (passes on head).
- The webhook contract (label `clientless.fluid.io/inject`, env
  `FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO`, ConfigMap
  `fluid-runtime-config-mooncake-demo`, read-only mount
  `/etc/fluid/config/mooncake-demo`, webhook name `clientless.fluid.io`) exists
  only in **#6197, which is still OPEN**. Hence F1.

## Findings and what proves what

| id | claim | layer | evidence |
|----|-------|-------|----------|
| F1 (blocker) | The new `check_missing_dataset_rejected` cannot pass until #6197 merges; all 5 kind-e2e CI jobs on `74d77d9b` fail there | CI logs + shell G2 | `results/ci-failure-attribution.txt` (5/5 jobs, same panic); G2 reproduces the mechanism with a stub kubectl (admission allowed → panic "expected admission to reject the missing dataset mooncake-missing") |
| F2 (minor)  | The e2e hard-codes #6197's unmerged, recently-renamed contract surface | unit + shell | Go contract test pins the merged generator side; F1/G1 pin the webhook side against #6197@b9315aca4 |
| F5 (nit)    | Worker entrypoint lost the explicit `-f` check | shell E | missing `runtime.sh` aborts via `set -e` with bash's bare "No such file or directory" |
| V-* | positive guards | shell | see table below |

## How to run

```bash
# unit layer: runtime.sh generator contract (real production code)
go test ./pkg/ddc/cache/engine/ -run TestRuntimeShContractForMooncakeE2E -count=1

# integration-ish layer: the PR's own shell logic vs stub kubectl + fixtures
go test ./docs/verification/mooncake-clientless-e2e/gowrap/ -run TestShellHarness -count=1
# or directly:
bash docs/verification/mooncake-clientless-e2e/scripts/run-shell-harness.sh
```

The shell harness sources the *actual* functions from
`test/gha-e2e/mooncake/test.sh` (everything above `function main()`) and runs
the *actual* `custom-entrypoint.sh` worker branch against the `runtime.sh`
emitted by the real Go generator (`results/runtime.sh.generated`, regenerated
by the Go test; falls back to the committed snapshot under
`scripts/fixtures/`).

## Observed vs expected (head 74d77d9b)

| case | expected | observed |
|------|----------|----------|
| Go contract: runtime.sh exports MASTER_NAME/MASTER_SERVICE_NAME/WORKER_SERVICE_NAME/WORKER_TIEREDSTORE_LEVEL_0_QUOTA_0 | present with mooncake-demo values | PASS |
| B: worker entrypoint args from runtime.sh | master/metadata/host/segment identical to old jq path | PASS |
| C: unset FLUID_RUNTIME_CONFIG_PATH | immediate abort | PASS |
| D: config path with spaces | loads | PASS |
| E: runtime.sh missing | aborts (set -e) | PASS (message is bash's, not explicit) |
| F1..F6: injection jq filter | accept fully-injected pod; reject writable mount / wrong env name / env→runtime.json / no volume / empty list | PASS x6 |
| G1: rejection check vs real denial shape | pass | PASS |
| G2: rejection check when admission allows (today's master) | panic "expected admission to reject" | PASS = reproduces the CI failure mechanism |
| G3: unrelated webhook error | panic "unexpected admission error" | PASS |

Harness-bites: mutating the Go expectation (`svc-WRONG`) fails the Go test;
renaming `MASTER_SERVICE_NAME` in the consumed runtime.sh flips shell cases
B/D to FAIL; F2..F6 and G2 are themselves negative-path proofs. Production
diff after the bites checks: empty.

## Findings ledger

- F1 [blocker] sequencing — status: open; origin: initial-pr; firstSeenSha: 74d77d9b;
  evidenceKind: differential (CI logs) + code-path; introducedByPR: true.
  Resolution: merge only after #6197; CI then re-runs green.
- F2 [minor] contract coupling with unmerged #6197 — status: open; origin:
  initial-pr; firstSeenSha: 74d77d9b; evidenceKind: code-path.
- F3 [nit] test.sh gains a runner-side jq dependency (jq removed from the image
  in the same PR) — reasoning only; GHA ubuntu runners ship jq.
- F4 [nit] hardcoded `FLUID_RUNTIME_CONFIG_PATH_MOONCAKE_DEMO` in the jq filter,
  deprecated `job-name` label selector, and an empty-pod-list panic message that
  blames injection instead of "no succeeded pod" — reasoning only.
- F5 [nit] see above — verified by shell case E.

## Continuing after the fix (next round / another machine)

```bash
git fetch https://github.com/cheyang/fluid.git verify/mooncake-clientless-e2e-codex
git checkout verify/mooncake-clientless-e2e-codex
bash docs/verification/mooncake-clientless-e2e/scripts/re-verify.sh   # auto-fetches the current PR head
```

All tests are CONTRACT polarity: green = contract holds. After #6197 merges and
this PR's CI goes green, F1 closes; if #6197 changes the contract surface first,
F1/G1 fixtures need a Harness-update (regenerate from the new injector code).
`.last-reviewed` records 74d77d9b; advance it after each round.

Kickoff prompt for a fresh agent: "Continue the review pipeline for
https://github.com/fluid-cloudnative/fluid/pull/6286 — harness branch
verify/mooncake-clientless-e2e-codex on the cheyang fork; run re-verify.sh and
review the delta from .last-reviewed."
