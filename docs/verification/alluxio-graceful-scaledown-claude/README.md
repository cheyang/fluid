# Verification — Reviewer A (Claude) — PR #6061: re-land graceful scale-down for AlluxioRuntime

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6061
- **Reviewed head:** `69ed16fd` (merge-base `a6d461c7`)
- **Harness branch:** `verify/alluxio-graceful-scaledown-claude` (production code untouched —
  one additive test file + this directory)
- **Layers:** unit only (no `KUBECONFIG`; live layer skipped by round policy). Gomonkey-free.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | #5805 added graceful worker scale-down and was reverted in #6059 "because it had no e2e coverage of the actual scale-down -> drain -> decommission -> data-integrity path"; this PR re-lands it with that e2e. |
| Linked issue | none (re-land of a reverted feature; revert #6059 verified as the parent commit 7362d7ef) |
| Reported component | AlluxioRuntime engine + e2e |
| Patched component | `pkg/ddc/alluxio` + `test/gha-e2e/alluxio-scaledown/` — match |
| **Verdict** | **Confirmed** — at merge-base `a6d461c` scale-in is ungraceful by construction (`SyncReplicas` → `Helper.SyncReplicas` immediately, no decommission path anywhere in the engine), so the problem the feature solves exists on base. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Premise: no graceful drain on base; re-land warranted | code-path | Confirmed | `results/full-pkg-tests-head.out`, git diff vs base |
| F1 | e2e never asserts the graceful path ran; deadline fallback + single-node cache placement let it pass anyway | unit (canary) | **Confirmed (gap present)** | `results/unit-harness-head.out` (G4 passes = gap) |
| F2 | PR-body claim "gate never exposed as a flag … Fixed that too" is inaccurate; no `cmd/` change in diff, gate already reachable | unit (contract) | Confirmed (reachability proven) | `results/gate-reachability-evidence.out`, G1 |
| F3 | Earlier-harness blocker (web port mismatch) fixed at head by `f2016204`? | unit (contract) | Confirmed fixed | G2 + bites check |
| F4 | Earlier-harness blocker (e2e UFS fallback) fixed at head by `69ed16fd`? | unit (contract) | Confirmed fixed | G3 |
| F5 | Is `report capacity -live` count a sound drain-complete signal? | live | Open question | see liveNote |
| F6 | Managed-network mode: HostIP:webPort decommission address reachable? | live | Hypothesis | chart analysis only |
| F7 | Scale-up mid-drain strands a decommissioned-but-alive worker | live | Hypothesis | code reading only |
| F8 | `pkg/ddc/alluxio` metadata_test failure is pre-existing | unit (differential) | Confirmed pre-existing | `results/base-preexisting-metadata-failure.out` |

Harness tests (all in `pkg/ddc/alluxio/scaledown_claude_verify_test.go`, run with
`go test ./pkg/ddc/alluxio/ -run TestVerifyClaude -count=1`):

| Test | Polarity | Covers | Result at head |
|------|----------|--------|----------------|
| `TestVerifyClaudeG1GracefulWorkerScaleDownGateRegisteredAndFlagParseable` | contract (with internal negative control) | F2 | PASS |
| `TestVerifyClaudeG2WorkerWebPortComesFromPodContainerSpec` | contract | F3 | PASS |
| `TestVerifyClaudeG3E2eTearsDownUFSAroundScaleDownReads` | contract | F4 | PASS |
| `TestVerifyClaudeG4E2eDoesNotAssertGracefulPathRan` | **canary** | F1 | PASS (gap present) |

## Per-finding detail

### F1 — the e2e cannot tell graceful from fallback (major, PR-introduced)

`test/gha-e2e/alluxio-scaledown/test.sh` waits up to 900s for the worker StatefulSet to
converge and reads the `WorkerDecommissioning` condition **only for logging** — no failure
path is tied to it. Meanwhile the engine has an explicit escape hatch: a drain that never
completes forces scale-down after `defaultWorkerDecommissionDeadline` (10 min)
(`pkg/ddc/alluxio/replicas.go`, deadline branch), and the PR's own unit test
("still forces the scale-down to proceed past the deadline instead of stalling forever")
proves that path converges the StatefulSet. On the single-node Kind cluster the fixture can
be cached on worker-0 (the surviving ordinal), so `read_after` passes even when the drained
worker's blocks were never migrated. Net: the e2e that this PR exists to deliver can pass
with the feature silently disabled. The test's own comment acknowledges it "cannot prove a
specific worker was targeted" but still claims to prove "the decommission flow runs" — it
proves convergence and cache survival, not the drain.

**Ask:** one failure-enforcing assertion, e.g. panic unless the runtime showed
`WorkerDecommissioning=True` at least once, or unless `spec.replicas` stayed at the old
value until the active-worker count dropped (or grep controller logs for a successful
decommission). The G4 canary flips to red the moment such an assertion lands; invert it
then.

### F2 — PR-body inaccuracy about the feature gate (minor)

The body says the gate was "never exposed as a flag on the alluxioruntime-controller
binary (cmd/alluxio/app/alluxio.go) … Fixed that too", but the diff touches no `cmd/` or
`charts/` file (0 changes; see `results/gate-reachability-evidence.out`). The gate was
already reachable: `cmd/alluxio` has called `DefaultMutableFeatureGate.AddFlag` since
commit `b3c0e8ec` (ancestor of merge-base), and `pkg/features`'s `init()` reaches that
binary through the import chain `cmd/alluxio → … → pkg/ddc/alluxio → pkg/features`
(`go list -deps ./cmd/alluxio`). G1 proves registration + `--feature-gates` parsing,
including a negative control on an unregistered name. No functional impact — but the
description should not claim a fix that isn't in the diff.

### F3 / F4 — earlier-round blockers, fixed at this head (re-verified)

A previous harness (branch `verify/alluxio-scaledown-e2e`, reviewed head `f55bab53`) found
two blockers: (a) decommission targeted `spec`/default port 30000 while host-network workers
bind a dynamically allocated port; (b) the e2e's post-scale-down read could be served by the
UFS. At head `69ed16fd` both are addressed: (a) `workerWebPortFromPod` reads the port from
the worker pod's own `alluxio-worker` container (commit `f2016204`); (b) `teardown_minio`
between the two reads makes the final read cache-only (commit `69ed16fd`). G2/G3 are
contract tests pinning both fixes; the bites check shows G2 fails under a regression to the
old behavior and G4-style enforcement would flip G4.

### F5 — drain-complete signal semantics (open question)

`drainScalingDownWorkers` treats
`alluxio fsadmin report capacity -live` count ≤ desired as "drained" and then lowers the
StatefulSet spec. Whether that count tracks *migration completion* (vs. decommission-request
acceptance or heartbeat expiry) is an Alluxio-runtime semantics question that only a live
cluster can answer; the e2e cannot (F1). If the count drops before blocks migrate, the pod
is deleted early and the feature silently degrades to the very data-loss it exists to
prevent.

### F6 / F7 — edge-case hypotheses (minor, unverified)

See the manifest notes: managed-network address reachability, and a worker left
decommissioned-but-alive when a drain is canceled by scaling back up.

### F8 — pre-existing test failure (not this PR)

`metadata_test.go` "should successfully sync metadata and update dataset status" fails
identically at merge-base and at head (differential runs captured). All 28 new drain specs
and 3 operations specs pass at head.

## Harness-bites check (Step 4)

`results/harness-bites.out`: (1) adding a `panic`-enforcing decommission assertion to
`test.sh` flips the G4 canary to FAIL as intended; (2) mutating `workerWebPortFromPod` to
ignore the pod spec fails G2; production files restored afterwards — the branch's production
diff is empty (verified with `git diff --quiet`). G1's negative control (unregistered gate
name must fail parsing) is built into the test.

## Minor code notes (no harness test)

- **Event noise during drain:** every ~20s requeue while not-yet-drained emits a
  *Warning* event "Failed to sync the ddc due to workers not yet drained…" on the runtime
  (`pkg/controllers/runtime_controller.go` Sync error path) — up to ~30 per drain for a
  normal transient state. Consider skipping the event (or demoting it) for
  `errWorkersNotYetDrained`.
- **No docs / Helm value for the new Alpha gate** — operators must read the source; the e2e
  works around it with a deployment patch.
- **Title mismatch:** the inherited title says "using AdvancedStatefulSet (#4193)" while the
  feature explicitly supports only the standard highest-ordinal-first scale-down order.
- `test/gha-e2e/curvine/read_job.yaml` adds an unrelated (benign) retry loop to the curvine
  e2e — scope creep worth mentioning in the PR description but harmless.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/alluxio-graceful-scaledown-claude` of the reviewer fork
(`https://github.com/cheyang/fluid.git`), production code untouched.

```bash
git fetch https://github.com/cheyang/fluid.git verify/alluxio-graceful-scaledown-claude
git checkout <fixed-ref>
git checkout FETCH_HEAD -- docs/verification/alluxio-graceful-scaledown-claude pkg/ddc/alluxio/scaledown_claude_verify_test.go
bash docs/verification/alluxio-graceful-scaledown-claude/scripts/re-verify.sh   # no ref = current PR head
```

Polarity table for interpreting results after a fix:

| Test | Now | After F1 is fixed |
|------|-----|-------------------|
| G4 canary | PASS (gap present) | **FLIPS TO FAIL — invert it** (promote to a contract test asserting the enforcement) |
| G1/G2/G3 contract | PASS | must stay PASS (regression guard) |

Copy-paste kickoff for a fresh agent:

> You are re-verifying PR fluid-cloudnative/fluid#6061. Checkout the harness branch
> `verify/alluxio-graceful-scaledown-claude` from `https://github.com/cheyang/fluid.git`,
> read `docs/verification/alluxio-graceful-scaledown-claude/README.md` and
> `verify-manifest.json`, run `scripts/re-verify.sh` against the current PR head, and
> report per finding Fixed / Still-broken / Partial / Harness-update honoring polarity
> (G4 is a canary: fixed = flips to fail, then invert it). Do not touch production code.
