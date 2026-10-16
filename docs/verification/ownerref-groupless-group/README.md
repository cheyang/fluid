# ownerref-groupless-group — bug verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6206
("fix(utils): recover group for group-less apiVersion in owner reference",
fixes https://github.com/fluid-cloudnative/fluid/issues/6205).

- head under review: `0daf2ba051fc25aa05dc68b2c42ea083f4f3c593` (round 2; round 1 reviewed `0fa3229416df66784dcf5b853dd636449d265a02`)
- base (merge-base): `e4a10bf49a1e8068d901e6f146ee4e435db7e785` (== merge of PR #6199)
- production code: **untouched** — the harness is additive test files + this directory.

Layers:

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `GenerateOwnerReferenceFromObject` output for group-less TypeMeta; watch Group/Kind matching logic | `go test ./pkg/utils/transformer/ -run 'TestVerifyPR6206' -v` |
| 2. Integration (envtest) | real API server (fluid CRDs installed) + real discovery RESTMapper + real `handler.EnqueueRequestForOwner` (the exact wiring of `pkg/ctrl/watch/manager.go`) | same command; auto-detects `$KUBEBUILDER_ASSETS`, `~/envtest-assets/k8s/*`, `/tmp/envtest-assets/k8s/*`, `/usr/local/kubebuilder/bin`; skips if none |
| 3. Live | skipped per round instructions | — |

Polarity: all tests are **contract** tests (assert intended behavior). They FAIL on base and
PASS on the PR head. The two `...Guard` tests assert unchanged behavior and must PASS on both.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | an owner object with group-less apiVersion (e.g. `TypeMeta{Kind: "Dataset", APIVersion: "v1alpha1"}`) yields `OwnerReference{apiVersion: "v1alpha1"}`; "the controller's owner-based watch and Kubernetes garbage collector (GC) resolve references using both Kind and Group. Without the group, the controller enqueue is skipped and GC discovery cannot resolve the owner." (issue #6205) |
| Linked issue | #6205, OPEN, filed 2026-10-11, still valid; identified during review of PR #6199 (issue #6140) |
| Reported component | `pkg/utils/transformer/owner_reference.go` `GenerateOwnerReferenceFromObject` |
| Patched component | same |
| Component match | **Yes** |
| **Verdict** | **Confirmed** on base |
| Evidence | `results/base-unit-integration.txt`: base generates `apiVersion="v1alpha1"`; the real owner-event handler enqueues **0** requests for the dependent Pod (event silently dropped). Head: `apiVersion="data.fluid.io/v1alpha1"`, exactly **1** request enqueued (`results/head-unit-integration.txt`). The API server accepts the group-less reference on write (dependent Pod create succeeds on base) — matching the issue's claim that the reference is syntactically admissible but semantically unresolvable. |

Mechanism cross-check (vendored code, controller-runtime v0.17.5
`pkg/handler/enqueue_owner.go`): `getOwnerReconcileRequest` compares
`refGV.Group == e.groupKind.Kind`'s group — `""` (from `apiVersion: v1alpha1`) never equals
`data.fluid.io`, so the request is dropped before any RESTMapping lookup. The GC half of the
impact is stock Kubernetes garbage-collector behavior over discovery (unresolvable
ownerReferences are treated as absent; a dependent whose only controller reference is
unresolvable is never collected) and was not re-proven here (live layer skipped).

Reachability note: all 16 production callers pass fluid types registered in the scheme, and
all in-repo TypeMeta constructions use the full `data.fluid.io/v1alpha1`; the group-less
shape is a helper-contract gap (reachable via objects decoded from user YAML or constructed
with a group-less apiVersion), consistent with issue #6140's scope note. This does not
diminish the fix — the contract of a shared helper is what is being repaired.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | group-less apiVersion passes through unrecovered on base; owner watch drops the event | 1+2 | **Confirmed** on base, fixed on head | results/base-unit-integration.txt vs results/head-unit-integration.txt |
| F1 | group recovered for `TypeMeta{Kind, APIVersion:"v1alpha1"}` across all owner types used by the 16 production callers (Dataset, AlluxioRuntime, ThinRuntime, DataLoad, DataMigrate, DataProcess) | 1 | Fixed on head | head-unit-integration.txt |
| F2 | group also recovered when Kind is empty too (`TypeMeta{APIVersion:"v1alpha1"}`) | 1 | Fixed on head | head-unit-integration.txt |
| F3 | generated reference matches the owner GroupKind under the exact `EnqueueRequestForOwner` comparison | 1 | Fixed on head | head-unit-integration.txt |
| F4 | end-to-end: real API server + real RESTMapper + real handler enqueue the owner (1 request) | 2 | Fixed on head | head-unit-integration.txt |
| G1 | core type with complete TypeMeta (`Pod`, `v1`) keeps apiVersion `v1` — no mangling by the widened trigger | 1 | Holds on base and head | both results files |
| G2 | fully populated fluid TypeMeta passes through untouched | 1 | Holds on base and head | both results files |

Harness-bites check: the PR's own added ginkgo entry fails on base (33/34 specs pass, only
the new entry fails — `results/base-pr-own-test.txt`) and passes on head; the reviewer
harness flips the same way. Known environment noise, **pre-existing on base and unrelated to
this PR**: `pkg/utils` `TestCheckMountPointBroken` and `pkg/utils/helm` `TestHelm` fail on
this host on base and head alike (mount/helm-binary environment dependencies).

`go build ./...` and `go test ./pkg/utils/transformer/` (full suite, 34 specs) pass on head;
`go test ./pkg/ddc/cache/component/... ./pkg/dataprocess/...` pass on head.

## Per-finding detail

### P0 / F1–F3 (unit)
Command: `go test ./pkg/utils/transformer/ -run 'TestVerifyPR6206' -v`
- base: 4 contract tests FAIL — observed `apiVersion="v1alpha1"`, parsed ref group `""` ≠ `data.fluid.io`.
- head: all PASS — observed `apiVersion="data.fluid.io/v1alpha1"`, group match succeeds.

### F4 (integration, envtest)
Same command (test auto-detects envtest binaries; skips if absent).
- base: dependent Pod with the generated group-less controller ownerReference is **created
  successfully** (API server accepts it — premise claim), but the real
  `handler.EnqueueRequestForOwner(scheme, dynamicRESTMapper, &Dataset{}, OnlyControllerOwner())`
  enqueues **0** reconcile requests → owner event silently dropped.
- head: **1** request enqueued for `default/groupless-owner`.

## Proposed fixes (NOT applied to production here)
The PR itself is the fix; nothing further required for the verified claims.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/ownerref-groupless-group-codex`
(fork `https://github.com/cheyang/fluid.git`), production code untouched.

```bash
git fetch https://github.com/cheyang/fluid.git verify/ownerref-groupless-group-codex
git checkout verify/ownerref-groupless-group-codex
bash docs/verification/ownerref-groupless-group/scripts/re-verify.sh   # auto-fetches current PR head
```

Or manually graft onto any fixed ref:
```bash
git checkout <fixed-ref>
git checkout verify/ownerref-groupless-group-codex -- \
  docs/verification/ownerref-groupless-group \
  pkg/utils/transformer/verify_pr6206_groupless_ownerref_test.go \
  pkg/utils/transformer/verify_pr6206_envtest_test.go \
  pkg/utils/transformer/verify_pr6206_logguard_test.go
go test ./pkg/utils/transformer/ -run 'TestVerifyPR6206' -v
```

All tests are contract polarity: green == fixed. If the code shape changes (symbol renamed,
signature changed), expect `Harness-update` and adjust the test, not the polarity.
Advance `.last-reviewed` after each round.

Kickoff prompt for a fresh agent: "Continue verifying
https://github.com/fluid-cloudnative/fluid/pull/6206 using the harness on branch
verify/ownerref-groupless-group-codex of https://github.com/cheyang/fluid.git; read
docs/verification/ownerref-groupless-group/README.md and run scripts/re-verify.sh."

## Round 2 — head 0daf2ba0 (delta 0fa32294..0daf2ba0)

The revision adds two commits: `75851117` gates the recovery-failure log on the kind or
version actually staying empty (fixing review finding F2) and adds the group-less +
kind-missing ginkgo entry (F1 coverage gap, part 1); `0daf2ba0` adds the group-less case
to the consumer-side `TestDatasetControllerOwnerReference` (F1 coverage gap, part 2) and
updates the header comment to "The kind, the version and the group fall back on their own"
(fixing nit F3). No production output path changes: the only behavioral delta vs 0fa32294
is whether one `log.Error` fires in the scheme-lookup-failure branch.

Re-verify on 0daf2ba0 (unit + integration): all round-1 contracts still FIXED
(`results/reverify/`). Round-2 additions:

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| F5 (= review finding F2 fix) | a complete core-type TypeMeta (`Pod`/`v1`) emits **no** "ownerReference stays incomplete" error log | 1 | Fixed on head; noise reproduced on 0fa32294 | results/round2-logguard-{base,prev,head}.txt: base PASS (noise never existed at base), 0fa32294 FAIL (log fires), 0daf2ba0 PASS (log gated) |
| G3 (over-suppression guard) | lookup failure with kind still empty must keep logging | 1 | Holds on base, 0fa32294, 0daf2ba0 | same files |

Round-2 polarity note: `TestVerifyPR6206CoreTypeCompleteTypeMetaNoErrorLog` PASSES on base
(the narrow trigger never logged for complete core types), FAILS on 0fa32294, PASSES on
0daf2ba0. "Fixed" for F5 means green on the current head; the base column is a control
showing the noise was introduced and removed within this PR's lifetime.

PR's own new tests lock the previously uncovered shapes (`results/round2-pr-own-tests-*.txt`):

| ref | transformer group-less entries (2) | consumer group-less case |
|-----|------------------------------------|--------------------------|
| base e4a10bf (head test files grafted) | 2/2 FAIL (`apiVersion="v1alpha1"`) | FAIL (`got v1alpha1`) |
| prev 0fa32294 (head test files grafted) | pass | pass |
| head 0daf2ba0 (as shipped) | 2/2 pass (35/35 suite) | pass |

Full-package sweep on head 0daf2ba0: `go build ./...` clean
(`results/round2-head-build.txt`); `go test ./pkg/utils/...` green except the two known
host-environment failures (`TestCheckMountPointBroken`, `TestHelm`) that fail identically
on base (`results/round2-head-pkg-utils.txt`); gofmt clean (`results/round2-head-gofmt.txt`).
