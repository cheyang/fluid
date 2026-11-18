# cacheruntime-class-snapshot-claude — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6242
(CacheRuntime bound to a snapshot of its CacheRuntimeClass). Reviewer A (Claude).

Layers run against the **code under review** (head `5ac1064bece2d01fdf9442f9ac8327a850b02b3d` in
round 1; head `632ec0062b5847d720eab7ba4cf8699a26b7a620` after the round-2 rebase — see
"Round 2" below), with the premise additionally run against the **base**
(`ff99142f9ec5c32a0c9166de023cb8936f0e5ae5` in round 1; `5e6473599f78263a49e521854d80f918a1a10bb0`
in round 2):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (ginkgo, fake client) | full `CacheEngine.Sync` against a class edit; snapshot rebind; read-only wait | `go test -gcflags=all=-l ./pkg/ddc/cache/engine/ -run TestCacheEngine -count=1` |
| 2. Integration | not needed beyond layer 1 this round: the contracts under test are pure engine-vs-fake-client behavior; envtest would add nothing the author's own fake-client suite and the kind e2e below do not already cover | — |
| 3. Live | the curvine kind e2e `verify_runtime_class_edit_not_propagated` (test/gha-e2e/curvine/test.sh) runs in the `kind-e2e-test` workflow and passed on head across k8s v1.22–v1.33 | `bash test/gha-e2e/curvine/test.sh` on a kind cluster |

All tests are **contract** tests (they assert the intended behavior; they FAIL on code that
regresses). No bug-canaries.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is whether the problem
exists today.

| | |
|---|---|
| Claimed symptom | "Editing a CacheRuntimeClass can currently change existing CacheRuntimes on their next reconcile" (PR body; issue #6213) |
| Linked issue | #6213, OPEN, created 2026-10-20, `features` label — still valid, symptom matches |
| Reported component | the CacheRuntime engine's live `CacheRuntimeClass` reads (`getRuntimeClass`) |
| Patched component | `pkg/ddc/cache/engine` — every class consumer now reads the bound snapshot |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | `results/p0-base/run.txt` — the identical contract spec fails on base: after the class's worker template `limits.cpu` 1→4 / `memory` 4Gi→16Gi edit, the next `Sync` patches the worker AdvancedStatefulSet. The same spec passes on head. |

The base-side run also confirms the issue's more specific claim about the base behavior:
a class edit reaches an instance whose `CacheRuntime` declares no `requests`/`limits` of its own
(the runtime in the fixture declares none, exactly the case `desiredComponentResources` on base
resolves from the live template).

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| AC1 | a CacheRuntimeClass edit must not change the workloads (resources, image) or the runtime-config ConfigMap of an existing CacheRuntime | 1 | Confirmed (passes on head, fails on base — differential) | `results/head-suite-claude-specs.txt`, `results/p0-base/run.txt` |
| AC2 | deleting the snapshot ConfigMap rebinds the runtime to the *current* class on the next Sync and converges the workloads onto it (documented remediation) | 1 | Confirmed | `results/head-suite-claude-specs.txt` |
| AC3 | while a runtime is unbound, the read-only path used by data operations returns an error instead of falling back to the live class, and recovers after the runtime controller rebinds | 1 | Confirmed | `results/head-suite-claude-specs.txt` |

## Per-finding detail

### AC1 — class-edit isolation (the PR's headline contract)
- Test: `pkg/ddc/cache/engine/runtime_class_edit_claude_test.go` — full `engine.Sync` with
  pre-existing master/worker ASTS + client DS + runtime config CM; class edited between the two
  Syncs.
- This file deliberately uses only symbols that also exist on the base branch, so the *same*
  file runs on base: red on base = the premise reproduced; green on head = fixed. That
  differential doubles as the harness-bites check for this contract.
- Observed (base): worker `limits.cpu` 4 / `memory` 16Gi reached the ASTS after the edit.
  Observed (head): resources and image unchanged.

### AC2 — rebind after snapshot deletion
- Test: `pkg/ddc/cache/engine/runtime_class_snapshot_claude_test.go` ("rebinds …").
- Observed: after deleting the snapshot and editing the class, the next `Sync` creates a new
  snapshot of the edited class (worker `limits` 2/8Gi) and syncs the workload to it.
- This is the user-facing remediation documented in
  `docs/en/samples/cacheruntime/cacheruntime_spec_update.md` §4; the author's own suite did not
  cover the rebind-while-class-edited case.

### AC3 — data operations wait while unbound
- Test: same file ("makes data operations wait …").
- Observed: `getBoundRuntimeClassReadOnly` errors with "not bound" while the snapshot is gone
  and does not read the live class; after the runtime controller's `Sync` rebinds, the read-only
  path returns the bound class.
- Harness-bites: temporarily restoring the pre-PR live-class fallback in
  `getBoundRuntimeClassReadOnly` made this spec (and the author's equivalent) fail; the edit was
  reverted and the production tree is untouched.

## Author-suite cross-check

The author's own suite (`runtime_class_snapshot_test.go`, 429 specs total in the package
including this harness) passes on head with `-gcflags=all=-l`. Note the fake client ignores UID
delete preconditions; the author already compensates with an interceptor client that enforces
them like the API server (`runtime_class_snapshot_test.go`, "should not delete the runtime's own
snapshot …"), so the 409-conflict branch is genuinely exercised.

Pre-existing local failures in `pkg/ddc/jindo` and `pkg/ddc/jindofsx` reproduce identically on
the base branch and are unrelated to this PR (not surfaced as findings).

## Live run notes

Not run this round (debate setup: unit+integration only). The curvine e2e check added by this PR
(`verify_runtime_class_edit_not_propagated`) is wired into `.github/scripts/gha-e2e.sh` and the
`kind-e2e-test` workflow reported `success` on head `5ac1064b` for k8s v1.22.17 / v1.24.17 /
v1.28.15 / v1.30.13 / v1.33.2.

## Proposed fixes (NOT applied to production here)

- none blocking; two minor suggestions recorded in the review findings (bind reads the class
  through the informer cache; no size guard on the marshaled class vs the 1 MiB ConfigMap cap)

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/cacheruntime-class-snapshot-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/cacheruntime-class-snapshot-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/cacheruntime-class-snapshot-claude \
       pkg/ddc/cache/engine/runtime_class_edit_claude_test.go \
       pkg/ddc/cache/engine/runtime_class_snapshot_claude_test.go
   ```
2. Prereqs: Layer 1 = Go toolchain only (`-gcflags=all=-l` is required; gomonkey inlines
   otherwise). Layer 3 = a kind cluster.
3. One-shot re-verify (auto-resolves the PR head from the manifest):
   ```bash
   bash docs/verification/cacheruntime-class-snapshot-claude/scripts/re-verify.sh
   ```
4. Read results via the polarity table: all contracts are expected GREEN. AC1 red on base is
   expected (that is the premise); it must stay green on any fixed head.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/cacheruntime-class-snapshot-claude
(remote https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6242 produced contracts AC1-AC3
(class-edit isolation, rebind on snapshot delete, data-ops wait while unbound);
a harness proved them. Read docs/verification/cacheruntime-class-snapshot-claude/README.md
("Continuing after the fix") and follow it: graft the harness onto the fixed code, run
scripts/re-verify.sh (unit layer only; live layer needs a kind cluster), mind the polarity
table (all contracts here), and report an observed-vs-expected table. Clean up scoped test
resources; do not run cluster-wide destructive actions.
```

## Round 2 (2026-11-18) — rebase onto new base, re-verification

The PR was rebased: head `5ac1064b` → `632ec0062` (same four commits, byte-identical PR-owned
files), base `ff99142f` → `5e6473599` (now includes #6177 as `f432bdc87`, the
sigs.k8s.io/yaml 1.4.0→1.6.0 bump #6240, #6244, #6252). Verified:

- **Base drift in blast radius is zero.** `git diff 956d78a8f 5e6473599 -- pkg/ddc/cache pkg/common`
  is empty, i.e. the #6177 code the snapshot commits sit on is identical to what the previous
  head was stacked on. The PR's own files are byte-identical between the two heads (md5-checked:
  runtime_class_snapshot.go, sync.go, cacheruntime.go, curvine test.sh).
- **Re-verify on new head**: `scripts/re-verify.sh` (auto-resolved head `632ec0062`) —
  AC1/AC2/AC3 all pass (`results/round2-2026-11-18/reverify-head-632ec006.txt`).
- **Full sweep on new head** (base-change re-verification, incl. the yaml 1.6 path in
  dataload.go): `go test -gcflags=all=-l -count=1 ./pkg/ddc/cache/... ./pkg/common/...` —
  all ok (`results/round2-2026-11-18/full-suite-head-632ec006.txt`).
- **Premise (P0) re-confirmed on the NEW base**: the AC1 contract spec grafted onto
  `5e6473599` FAILS (1 failed spec: the class edit reaches the worker ASTS — memory 4Gi→16Gi
  diff printed), and passes on head `632ec0062`
  (`results/round2-2026-11-18/p0-base-5e6473599.txt`). So even with #6177 merged into the
  base, the #6213 premise still reproduces and this PR still fixes it.
- **CI at this head**: all checks pass except `staticcheck`, which fails with ST1019
  (duplicate `sigs.k8s.io/controller-runtime/pkg/client` import) in
  `pkg/utils/dataset/lifecycle/node_test.go` — a file this PR does not touch, introduced by
  base commit `6f55f846d` (#6244) and already fixed on master by `1321a217d` (#6257, landed
  2026-11-17 14:47 +0800, one hour after the failing run). Not PR-attributed; a rebase onto
  current master (or re-run) clears it.
