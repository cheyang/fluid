# Verification — Alluxio imagePullSecrets sync (PR #6288)

Reviewer: Claude (Reviewer A) · Round: initial · Head: `8903e33eb` · Merge-base: `07bdbcd86`
Harness branch: `verify/alluxio-sync-pullsecrets-claude` (production code untouched; only this
tree + `pkg/ddc/alluxio/sync_runtime_verify_test.go` are added).

## Premise (P0) — CONFIRMED

Issue #6277 (OPEN): "Sync imagePullSecrets changes on existing AlluxioRuntimes to the
master/worker StatefulSets and fuse DaemonSet. Alluxio has supported runtime and
per-component imagePullSecrets since #4242, but its `SyncRuntime` is a no-op, so changing
them after creation does nothing until the runtime is recreated."

- Component match: issue names AlluxioRuntime / Alluxio `SyncRuntime`; the PR patches
  `pkg/ddc/alluxio/sync_runtime.go` — exact match.
- Evidence: on base `07bdbcd86` the contract tests below are red (workloads keep the
  initial secret after a spec change — `results/base-premise-TestV0.out`); on head they are
  green (`results/head-verify-tests.out`). Problem exists on base, patch addresses it.

## Observed vs expected

| ID | Claim | Layer | Polarity | Result on head | Result on base |
| --- | --- | --- | --- | --- | --- |
| AC-1 | spec-level and per-component secrets propagate to master STS / worker STS / fuse DS; component overrides common; idempotent on steady state; other pod-spec fields preserved | unit (fake client) | contract | PASS (`TestV01_SyncRuntime_PropagatesSecretChanges`, plus the PR's own `TestAlluxioEngine_SyncRuntime`) | FAIL (no-op) |
| AC-2 | creation-parity: the values SyncRuntime writes equal what the helm charts render at creation for the same spec/env, incl. env fallback and the fuse env-over-common quirk | unit | contract | PASS (6/6 cases, `TestV02_SyncRuntime_CreationParity`) | FAIL (no-op) |
| AC-3 | removal of all spec secrets clears the pod templates (when no env fallback) | unit | contract | PASS (removal stage of `TestV01`) | FAIL |
| F2 | a missing workload (e.g. fuse DaemonSet deleted out-of-band) surfaces as a retried error, not a silent skip | unit | contract | PASS (`TestV03_SyncRuntime_MissingWorkloadSurfacesError`) | FAIL (no error on base — behavior is new) |
| AC-5 | no rolling restart when secrets change (updateStrategy OnDelete) | static | — | code-path verified only: master/worker STS hardcode `updateStrategy: OnDelete`; fuse DS defaults `OnDelete` in charts/alluxio/values.yaml:413 and the engine never overrides it | n/a |

Harness-bites: the same contract suite run against the merge-base (SyncRuntime no-op) is
red — the tests exercise the changed path, not a vacuous pass.

Full-suite attribution: `FLUID_UNIT_TEST=true go test ./pkg/ddc/alluxio/` → 149/150 specs on
head AND on base (identical single failure, `metadata_test.go:233`). The one failure is
pre-existing/environmental, not caused by this PR. `go vet ./pkg/ddc/alluxio` clean.

## Findings recap (see findings-claude.md for full text)

No blocker or major. Non-blocking: F2 (error-on-missing-workload now blocks later sync
steps in `TemplateEngine.Sync` — same posture as `CheckRuntimeHealthy`, edge case only),
F5 (env `IMAGE_PULL_SECRETS` semantics questions), F1 (missing e2e/integration coverage,
unit coverage is meaningful), F3 (pre-existing fuse env-over-common creation quirk, now
load-bearing but faithfully mirrored), F4 (nits: import alias, duplicate sts/ds helpers).

## How to re-run

From a checkout of this branch:

```bash
bash docs/verification/alluxio-sync-pullsecrets-claude/scripts/re-verify.sh
# no args: fetches the current PR head from the manifest's pr URL and re-runs
```

Layer command (unit): `FLUID_UNIT_TEST=true go test ./pkg/ddc/alluxio/ -run '^TestV0|^TestAlluxioEngine_SyncRuntime$' -v`

## Continuing after a fix

- The manifest maps AC-1..AC-3 and F2 to contract tests; all should stay green after any
  future revision. A canary flip is not expected (no canaries this round).
- If a revision changes the fallback semantics (e.g. fixing the fuse env-over-common quirk
  from F3), `TestV02_SyncRuntime_CreationParity/env-and-common` will go red — update the
  case's expectation together with the chart semantics, do not just delete the case.
- If a revision starts tolerating missing workloads, `TestV03` will go red — that is a
  deliberate contract change; rewrite it to assert the new behavior.

Copy-paste kickoff for a fresh agent:

> You are resuming the review of https://github.com/fluid-cloudnative/fluid/pull/6288.
> Check out branch `verify/alluxio-sync-pullsecrets-claude` from the reviewer fork
> (https://github.com/cheyang/fluid.git), read
> docs/verification/alluxio-sync-pullsecrets-claude/README.md, run
> scripts/re-verify.sh, then review the `last-reviewed..head` delta.
