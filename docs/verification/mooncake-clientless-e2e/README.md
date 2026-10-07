# mooncake-clientless-e2e — review verification (PR #6175)

Reproducible evidence for the verification claims raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6175 (test-only PR: adds an e2e
case for a client-less CacheRuntime topology plus CI wiring).

Harness runs against the **code under review** (`8a12cfaa` = PR head).
Production code is untouched — the harness is additive (this directory + one
Go test file).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | script logic + Go labels contract | `bash scripts/re-verify.sh` (runs both unit commands) |
| 2. Integration | static cross-check of manifests/scripts vs the Go code that produces/consumes them | `python3 -I scripts/manifest_checks.py <repo>` |
| 3. Live | real kind cluster e2e | **out of scope this round** (no KUBECONFIG; see CI evidence below) |

All harness tests are **contract** polarity: green == the claim holds.

## Problem premise (P0)

PR #6175 is a tests-only PR, so the premise is a **coverage gap**, verified
against the **base** branch (merge base `e0fc4c18`):

| | |
|---|---|
| Claimed symptom (PR body) | "a `CacheRuntimeClass` whose `topology` declares only `master` and `worker`, with no `client` component … the client-less path through `cacheruntime-controller` is currently untested" |
| Linked issue | none (no `fixes #N`); guards #6157 (panic fix, merged at `d1ae1ace`, an ancestor of the merge base) |
| Reported component | test/gha-e2e cacheruntime coverage |
| Patched component | test/gha-e2e/mooncake + CI wiring |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | On base, `test/gha-e2e/` contains only alluxio / jindo / juicefs / curvine; `test/gha-e2e/curvine/cacheruntimeclass.yaml:102` declares a `client` component, so no case exercises the client-less topology. `git merge-base --is-ancestor d1ae1ace e0fc4c18` holds. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| V1 | cacheruntime-created ASTS/DaemonSet/Service carry `cacheruntime.fluid.io/name` + `component-name` (so the **new** GC selector in mooncake/curvine `wait_runtime_deleted` bites) and do **not** carry `fluid.io/managed-by` (so the **old** curvine selector was vacuous — commit 8a12cfaa is a genuine fix, not a no-op) | 1 | Confirmed | `gc_labels_verify_test.go` (54 specs, all pass); results/go-unit.out |
| V2 | `reportSummary.sh` emits exactly the json keys `CacheRuntimeReportSummary` unmarshals, with `ufsTotal == cacheCapacity` and non-trivial `cached`/`fileNum`; exits non-zero on an empty metrics response | 1 | Confirmed | results/unit-harness.out (U2, U2b) |
| V3 | the `FailedMount` grep (`fuse[[:space:]]+mount[[:space:]]*point`) matches the real CSI wording `"timeout waiting for FUSE mount point to be ready"` (`pkg/utils/mount.go:85`) and does **not** bridge across two concatenated event messages | 1 | Confirmed | results/unit-harness.out (U3) |
| V4 | all name/port/key wiring in the manifests is consistent with the code that produces/consumes it: service name `svc-<runtime>-<comp>`, pod DNS `<comp>-0.<svc>`, PVC = runtime name, PV = `<ns>-<name>`, `timeout` json tag, controller label `control-plane=cacheruntime-controller`, container `manager`, ports 50051/8080/9003/50052/9300 | 2 | Confirmed | results/manifest-checks.out (27/27 PASS) |
| V5 | `custom-entrypoint.sh` rejects the `client` role and bogus actions (client-less guard) | 1 | Confirmed | results/unit-harness.out (U4) |
| V6 | kubectl accepts `-l <selector> -c manager --previous` (the panic-regression log check is a legal invocation, not a silently-swallowed flag error) | 1 | Confirmed | `kubectl logs -l control-plane=x -c manager --previous` on kubectl v1.37.0 → no validation error (proceeds to pod lookup) |
| V7 | the PR's e2e case actually runs green in CI | 3 (CI, read-only) | Confirmed | all five `kind-e2e-test (v1.22.17–v1.33.2)` checks on head `8a12cfaa` SUCCESS |

## Per-finding detail

**V1 (labels / GC assertion).** `getCommonLabelsFromComponent`
(`pkg/ddc/cache/component/component_manager.go:65`) returns exactly two labels;
`constructAdvancedStatefulSet`, `constructService` and `constructDaemonSet` use
them as object labels and (for workloads) pod-template labels. The additive
ginkgo test `GC selector labels (PR #6175 verification)` asserts presence of
the two cacheruntime labels and **absence** of `fluid.io/managed-by` on all
constructed objects plus a reconcile round-trip. Harness-bites check: deleting
`LabelCacheRuntimeName` in `constructService` temporarily made the package
FAIL; reverted, green again, `git diff` on production code empty.

**V2/V3/V5 (script behavior).** `unit-harness.sh` stubs `curl` (the script
curls `localhost:9003/metrics/summary`) and feeds the exact metrics format the
script documents; asserts the six json keys, the values, and failure on empty
metrics. The grep tests run the *actual* `grep -qiE` pattern from
`test.sh:305` against the literal error string extracted from
`pkg/utils/mount.go`, plus a synthetic concatenation of two unrelated messages
that must NOT match.

**V4 (static integration).** `manifest_checks.py` (27 checks) parses all five
new manifests and cross-checks every hardcoded name/label/port/key against the
Go sources and the helm chart (`GetComponentServiceName`, `GetCacheComponentName`,
`GetPersistentVolumeName`, `CacheRuntimeReportSummary` json tags,
`ExecutionCommonEntry` `timeout` tag, `cacheruntime_controller.yaml`).

**Environment noise (not attributed to the PR).** `go test
./pkg/ddc/cache/engine/...` fails 12 specs (`ufs_test.go`, exec/mock related)
on the **PR head and identically on the merge base** `e0fc4c18` — pre-existing
local-environment failures. The PR touches no Go code. See
results/go-unit.out.

## Live run notes

No KUBECONFIG this round (debate setup: unit + integration only). Live-layer
substitute: CI evidence from the PR head — `kind-e2e-test` SUCCESS on all five
K8s versions (job durations 27–31 min; the `Fluid basic e2e tests` step cap is
`timeout-minutes: 30` — headroom exists but is the thinnest of the pipeline;
see finding F1 in the review findings).

## Proposed fixes (NOT applied to production here)

- none required for correctness; optional improvements are recorded in the
  review findings (F2–F5), not in code.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/mooncake-clientless-e2e-claude` (production
code untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/mooncake-clientless-e2e-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/mooncake-clientless-e2e pkg/ddc/cache/component/gc_labels_verify_test.go
   ```
   Or just run `bash docs/verification/mooncake-clientless-e2e/scripts/re-verify.sh [<ref>]`
   — with no ref it fetches the current PR head from the manifest `pr` URL,
   grafts the harness into a temp worktree, runs layers 1+2 and prints the
   per-claim verdicts. Exit 0 iff all claims hold.
2. Prereqs: Layer 1 = bash, python3 (+PyYAML), Go toolchain; Layer 2 = same;
   Layer 3 = a kind cluster with Fluid deployed and
   `fluidcloudnative/mooncake:e2e` loaded (`bash test/gha-e2e/mooncake/test.sh`).
3. Re-run and read results via the polarity table: all tests are contract
   tests, green == claim holds; anything red is either a regression or a
   harness-update (the code shape changed — adjust the test).
4. Harness-bites: temporarily drop `LabelCacheRuntimeName` from
   `constructService` and confirm `go test ./pkg/ddc/cache/component/...` goes
   red, then revert.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/mooncake-clientless-e2e-claude
(https://github.com/cheyang/fluid.git). Background: a review of PR
https://github.com/fluid-cloudnative/fluid/pull/6175 produced claims V1–V7; a
harness verified them on head 8a12cfaa. The PR may have new commits now. Read
docs/verification/mooncake-clientless-e2e/README.md ("Continuing after the
fix") and run scripts/re-verify.sh (no ref needed — it resolves the PR head).
All tests are contract polarity. Report the per-claim table, review the
last-reviewed..head delta, and advance .last-reviewed. Clean up scoped test
resources; do not run cluster-wide destructive actions.
```
