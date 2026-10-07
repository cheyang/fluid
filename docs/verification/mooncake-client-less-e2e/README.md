# mooncake-client-less-e2e — review verification harness

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6175
("test: add an e2e case for a client-less CacheRuntime topology").

Reviewed head: `8a12cfaa8ce2a0a051e120b3af0015c055261908` (see `.last-reviewed`).
Production code is untouched — this branch adds test files and this docs tree only.

Layers run against the **code under review** (origin/pr/6175 @ 8a12cfaa):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | premise (P0), GC-label wiring (F1), shell harnesses for reportSummary.sh / custom-entrypoint.sh, F2/F4 canaries | `go test ./pkg/ddc/cache/engine/ ./pkg/ddc/cache/component/ ./test/gha-e2e/mooncake/ -run 'P0Verify|GCSelectorLabel_Verify|Harness_Verify|Canary' -count=1 -v` |
| 2. Integration | real API server (envtest): manifest schema acceptance, F2 stale-event mechanism | `go test ./test/gha-e2e/mooncake/ -run 'TestManifestsAcceptedByAPIServer_Verify|TestStaleFailedMountEventsMatchPodNameSelector_F2Verify' -count=1 -v` (needs envtest binaries; falls back to ~/.local/share/kubebuilder-envtest/k8s/1.36.2-linux-amd64) |
| 3. Live | skipped this round | the PR's own kind-e2e run already covers it (see below) |

> Test polarity: contract tests FAIL on buggy code / PASS when fixed. Bug-canaries
> (F2, F4) PASS while the weakness exists and FLIP to red when fixed — "all green"
> is not "fixed" while canaries are present.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "No existing case covers that shape: the curvine case ships a client component, so the client-less path through cacheruntime-controller is currently untested" — and the case guards "the nil pointer dereference fixed in #6157" (quoted from the PR body) |
| Linked issue | none closing; references #6157 (merged fix), #6163 (merged docs sample), #6161 (closed bug), #6160 (open bug), #6165 (merged fix) |
| Reported component | cacheruntime-controller, client-less CacheRuntime topology |
| Patched component | test/gha-e2e/mooncake + .github CI wiring (tests-only PR) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | The same contract test panics with `invalid memory address or nil pointer dereference` on `d1ae1ace^` (pre-#6157) and passes on the PR head: `results/p0-premise-prefix6157.txt` vs `results/unit-layer.txt`. The curvine class does declare a client (test/gha-e2e/curvine/cacheruntimeclass.yaml:102). The docs FAQ behavior the case pins is real (docs/en/samples/cacheruntime/mooncake_cache_runtime.md, FailedMount "timeout waiting for FUSE mount point to be ready"). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | client-less topology panicked the controller pre-#6157; coverage gap is real | 1 | Confirmed | panic on d1ae1ace^, clean pass on PR head; results/p0-premise-prefix6157.txt, results/unit-layer.txt |
| F1 | (prior review blocker, claimed fixed) GC selector `cacheruntime.fluid.io/name=<dataset>` matches the objects the controller creates | 1 | Confirmed fixed | TestAdvancedStatefulSet/TestDaemonSet/TestHeadlessService CarriesGCSelectorLabel_Verify all pass against the real construct* code; old selector `fluid.io/managed-by` absent |
| F2 | check_pvc_not_mountable matches FailedMount by pod name only → stale events from a deleted pod satisfy it on repeated runs | 2 | Confirmed (mechanism) | envtest: after pod deletion the exact kubectl query still returns the stale event; uid-scoped selector excludes it. results/integration-and-shell-layer.txt |
| F3 | cacheruntimeclass.yaml comment says "Once #6165 lands" but #6165 is already merged and in the PR base | static | Confirmed | `git merge-base --is-ancestor 64a10a13 e0fc4c1` true; #6165 merged 2026-08-30 |
| F4 | wait_runtime_deleted swallows kubectl failures via 2>/dev/null → GC assertion passes vacuously on API errors | 1 | Confirmed | canary runs the extracted function with an always-failing kubectl stub; it prints "garbage collected" and exits 0. scripts/verify-wait-runtime-deleted-canary.sh |
| F5 | panic scan reads only --tail=200 of current+previous controller logs, checked every 15s during the Bound wait | reasoning | Not reproduced (robustness note) | a panic older than the tail window between checks is missed; low impact in a dedicated CI cluster |
| — | the PR's own scripts work | 1 | Confirmed | reportSummary.sh parses fabricated payloads incl. fallback paths; custom-entrypoint.sh worker turns the controller's real runtime.json into the correct mooncake_client invocation |

## Per-finding detail

### P0 (premise) — Confirmed
`TestClientLessTopologyGeneratesRuntimeConfig_P0Verify` builds the exact topology
of the PR's manifests (master+worker class, no client; CacheRuntime with default
spec.client) and calls the controller's `generateRuntimeConfigData`.
- On PR head (and on master, which contains #6157): passes; config has
  master+worker and no client section; the jq paths the worker entrypoint reads
  (`.master.service.name`, `.worker.service.name`,
  `.worker.tieredStoreLevels[0].quotas[0]`) resolve to
  `svc-mooncake-demo-master` / `svc-mooncake-demo-worker` / `1Gi`.
- On `d1ae1ace^` (pre-#6157): panics with nil pointer dereference — the premise
  reproduces on the base-of-the-fix. Harness-bites check done in both directions.

### F1 (GC label selector) — prior blocker, verified fixed
`pkg/ddc/cache/component/verify_gc_labels_test.go` asserts the literal label key
the shell scripts use (`cacheruntime.fluid.io/name`) is present with the runtime
name on the AdvancedStatefulSet, DaemonSet and Service objects the controller
constructs, and that `fluid.io/managed-by` (the previous, vacuous selector) is
absent. All pass: the new selector genuinely matches.

### F2 (stale FailedMount events) — Confirmed mechanism, minor
`check_pvc_not_mountable` selects events with
`involvedObject.name=mooncake-bad-mount,reason=FailedMount` and deletes the pod
with `--force --grace-period=0` afterwards. Events outlive the pod (default
retention 1h), so a repeated run on the same cluster matches the stale event and
passes without exercising the current behavior. envtest proves both the stale
match and that adding `involvedObject.uid=<pod uid>` excludes it. CI is
unaffected (fresh kind cluster per job), so severity is minor — it matters for
local reruns and reused clusters. Canary: `verify-failedmount-uid-scope.sh`
flips when the query gains uid scoping.

### F3 (stale comment) — nit
The comment block in `test/gha-e2e/mooncake/cacheruntimeclass.yaml` justifies
omitting `resources` with "Once #6165 lands, resources can be declared here",
but #6165 merged on 2026-08-30 and is an ancestor of the PR base. The comment
should be updated (the #6160 Dataset-Failed caveat still stands). Comment-only.

### F4 (kubectl errors swallowed in wait_runtime_deleted) — Confirmed mechanism, minor
Both the mooncake and curvine scripts do
`remaining=$(kubectl get advancedstatefulset,daemonset,svc -l ... 2>/dev/null)`.
If the call fails for anything other than "no resources" (API hiccup, missing
AdvancedStatefulSet CRD), `remaining` is empty and the loop breaks: the GC
assertion passes vacuously. The canary extracts the real function and runs it
with an always-failing kubectl stub: it prints "All runtime resources ...
garbage collected" and exits 0. Also the mooncake log line omits DaemonSet from
the resource list it actually queries. Suggested fix: check kubectl's exit code
and keep waiting (or fail) instead of treating an error as "empty".

### F5 (panic-scan tail window) — robustness note, minor
`check_controller_not_panicked` reads `--tail=200` of the current and previous
manager logs, invoked every 15s during the Bound wait and once after. A panic
that scrolls past the 200-line tail between checks, or two restarts in a row,
evades detection. The Dataset never reaching Bound would still fail the test, so
the cost is a less precise failure message, not a false pass. Minor.

### PR's own scripts — verified working
- `verify-report-summary.sh`: fabricated metrics payloads → correct JSON
  (`cached=4.19MiB`, `cacheCapacity=ufsTotal=1.00GiB`, `fileNum=3`, hit ratio
  95); zero/missing-field fallbacks work (`set -o pipefail` makes the
  `|| echo` fallbacks effective); unreachable endpoint exits non-zero.
- `verify-entrypoint-worker.sh`: feeds the real `runtime.json` generated by the
  controller code for these manifests (fixtures/runtime.json) and asserts the
  worker is exec'd with
  `--master_server_address=svc-mooncake-demo-master.default.svc.cluster.local:50051`,
  `--metadata_server=http://...:8080/metadata`,
  `--host=<pod>.svc-mooncake-demo-worker.default.svc.cluster.local`, and
  `--global_segment_size=1GB` (Fluid's `1Gi` quota converted to mooncake's
  `GB`). The client role exits non-zero as intended.

## Proposed fixes (NOT applied to production here)
- **F2**: add `involvedObject.uid=<uid of the just-created pod>` to the
  FailedMount field selector.
- **F3**: update the comment in cacheruntimeclass.yaml: #6165 has landed;
  only the #6160 caveat still justifies omitting resources.
- **F4**: capture kubectl's exit code (`if ! remaining=$(kubectl get ...); then
  ...continue/fail; fi`) in both test.sh copies; align the log text with the
  queried resource list.
- **F5**: optional — raise the tail or grep the whole log once at the end.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/mooncake-client-less-e2e-codex` (production
code untouched), so it grafts onto whatever the fixed code is.

```bash
git fetch https://github.com/cheyang/fluid.git verify/mooncake-client-less-e2e-codex
git checkout <fixed-branch>
git checkout FETCH_HEAD -- docs/verification/mooncake-client-less-e2e \
  pkg/ddc/cache/engine/verify_p0_clientless_test.go \
  pkg/ddc/cache/component/verify_gc_labels_test.go \
  test/gha-e2e/mooncake/verify_envtest_test.go \
  test/gha-e2e/mooncake/verify_shell_harness_test.go
```

Prereqs: Go toolchain for layers 1–2; envtest binaries (`KUBEBUILDER_ASSETS`,
else `~/.local/share/kubebuilder-envtest/k8s/1.36.2-linux-amd64` is auto-detected)
for layer 2; layer 2 skips cleanly without them. Layer 3 was not run — the PR's
kind-e2e workflow is the live layer and it is green on 8a12cfaa ("Fluid basic
e2e tests" 13m48s of a 30m step timeout).

Or from a checkout of the verify branch: `bash
docs/verification/mooncake-client-less-e2e/scripts/re-verify.sh` (auto-resolves
the current PR head via the manifest's `pr` URL).

Polarity table for reading results after a fix:
- contract tests (P0, F1, PR-scripts, F2-evidence) should be GREEN;
- canaries (F2 = TestFailedMountEventQueryUIDScoped_Canary, F4 =
  TestWaitRuntimeDeletedVacuousPass_Canary) are fixed only when they FLIP to
  red; then invert their assertions.

Note on the fixture: `fixtures/runtime.json` is what the controller generates
for these manifests at the reviewed head. If the runtime-config schema changes,
regenerate it with
`VERIFY_RUNTIME_JSON_OUT=$PWD/docs/verification/mooncake-client-less-e2e/fixtures/runtime.json go test ./pkg/ddc/cache/engine/ -run P0Verify`;
the P0 test's own assertions fail first if the schema drifts.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/mooncake-client-less-e2e-codex
(remote https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6175 produced findings F1..F5;
a harness reproduced/verified them. Read
docs/verification/mooncake-client-less-e2e/README.md ("Continuing after the
fix") and follow it: graft the harness onto the new PR head, re-run the unit
and integration layers, mind the polarity table (canaries must flip),
re-run the harness-bites check (P0 test against d1ae1ace^), and report an
observed-vs-expected table. Do not touch production code; do not publish to
GitHub.
```
