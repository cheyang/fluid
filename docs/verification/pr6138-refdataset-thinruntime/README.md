# pr6138-refdataset-thinruntime — bug verification

Reproducible evidence for the findings raised while reviewing
**[PR #6138 — fix(dataset): recreate the ThinRuntime of a reference dataset stuck in NotBound](https://github.com/fluid-cloudnative/fluid/pull/6138)**.

Layers run against the **code under review** (`47077ff0`, PR head).
True merge base is `44cfae9f` (master advanced past the branch point, so `origin/master` is
*not* the base — diff against `44cfae9f`).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `utils.CreateRuntimeForReferenceDatasetIfNotExist` adopt/create branches and `DatasetReconciler.reconcileDataset` step ordering, against a controller-runtime fake client | `go test ./pkg/utils/ ./pkg/controllers/v1alpha1/dataset/ -run 'TestVerifyB1\|TestVerifyB2\|TestVerifyB3' -count=1 -p 1 -v` |
| 2. Integration | **not built — legitimately skipped.** `pkg/controllers/v1alpha1/dataset/suite_test.go` is a plain Ginkgo `RunSpecs` with no `envtest.Environment` (only `alluxio` and `dataprocess` carry envtest in this repo). All three findings are pure fake-client contract questions the unit layer settles exactly, so an envtest harness built from scratch would add cost and no signal. | n/a |
| 3. Live | **not run — deliberately.** The kubeconfig available on the verification box points at a *shared real* 3-node ACK cluster; creating/mutating state there was out of bounds. See `verify-manifest.json` → `liveNote` for the observable signal if a live check is ever wanted. | n/a |

> Test polarity: **contract** tests (assert intended behaviour) FAIL on the code under review /
> PASS when fixed. **Bug-canary** tests (assert current behaviour) PASS now / FLIP to red when
> fixed and must then be inverted. B3 is a canary — "all green" does **not** mean B3 is fixed.

Production code on this branch is **untouched**: the diff against `47077ff0` is exactly this
docs tree plus two additive `_test.go` files.

## Summary of results

| ID | Claim | Severity | Layer | Polarity | Verdict | Evidence |
|----|-------|----------|-------|----------|---------|----------|
| B1 | The adopt path of `CreateRuntimeForReferenceDatasetIfNotExist` (`pkg/utils/dataset_runtime.go:85-86`) replaces the whole `ownerReferences` slice, silently dropping any non-Dataset owner. | Medium | 1 | contract | **Confirmed real** | `TestVerifyB1AdoptPreservesForeignOwnerReferences` FAILS. The seeded `example.com/v1 SomeOtherKind` owner is **gone**; the stored runtime ends up with exactly one ref, `{data.fluid.io/v1alpha1 Dataset b1-foreign-owner 2222…}`. `results/baseline.txt` |
| B2 | The adopt path never backfills `common.LabelAnnotationDatasetId`; the label is only set in the create branch (`dataset_runtime.go:103-105`). | Low-Med | 1 | contract | **Confirmed real** | `TestVerifyB2AdoptBackfillsDatasetIdLabel` FAILS. Observed labels: `map[]` (label wholly absent). Expected `fluid.io/dataset-id = "default-b2-no-label"`. `results/baseline.txt` |
| B3 | The terminating-runtime guard makes step 3 return `RequeueIfError` before step 4, so a `NoneDatasetPhase` reference dataset never advances to `NotBound` while a leftover runtime is Terminating (`dataset_controller.go:153-174`). | Low | 1 | **canary** | **Confirmed real** (behaviour reproduced; whether it is a *defect* is a design call) | `TestVerifyB3TerminatingRuntimeBlocksNoneToNotBoundPhaseUpdate` PASSES, i.e. the canary fires. Observed `status.phase = ""` (`NoneDatasetPhase`), never `"NotBound"`, with `err = "the ThinRuntime default/b3-none-phase is terminating, …"`. `results/baseline.txt` |

Companion reference tests (green on the code under review, present so a future regression is
distinguishable from the gaps above):

| Test | Purpose |
|------|---------|
| `TestVerifyB1StaleDatasetOwnerReplacedByFreshUID` | A stale `Dataset` owner with an old UID (delete + recreate of the same name) must be superseded, not duplicated. Guards against a B1 fix that merges too eagerly and leaves **two** controller owners. |
| `TestVerifyB2CreateSetsDatasetIdLabel` | The *create* branch does set the label — keeps a create-path regression separable from the B2 adopt-path gap. |
| `TestVerifyB3NoneToNotBoundWithoutTerminatingRuntime` | Without the terminating runtime the same dataset **does** go `"" → "NotBound"`, proving the B3 canary isolates the short-circuit rather than a generally broken status update. |

## Per-finding detail

### B1 — adopt path wipes non-Dataset ownerReferences (Medium, Confirmed)

`pkg/utils/dataset_runtime.go:85-86`:

```go
runtimeToUpdate.SetOwnerReferences([]metav1.OwnerReference{
    datasetControllerOwnerReference(dataset)})
```

`SetOwnerReferences` **replaces** the slice. Anything already owning the ThinRuntime — a
foreign controller, an extra non-controller reference, or a stale `Dataset` owner carrying the
UID from before a delete/recreate of the same name — is dropped without a log line. The PR's own
`ThinRuntimeExists` fixture seeds a single Dataset-ish owner, so no existing test covers a
foreign or extra owner.

Test: seed a ThinRuntime owned by `example.com/v1 SomeOtherKind`, adopt it, assert both the
foreign owner **and** the fresh Dataset owner are present.

Observed on `47077ff0` — the foreign owner did **not** survive:

```
B1 reproduced: the pre-existing non-Dataset ownerReference example.com/v1/SomeOtherKind
(uid=11111111-1111-1111-1111-111111111111) was wiped by the adopt path; ownerReferences are
now [{data.fluid.io/v1alpha1 Dataset b1-foreign-owner 22222222-2222-2222-2222-222222222222 …}]
--- FAIL: TestVerifyB1AdoptPreservesForeignOwnerReferences
```

Note this is pre-existing behaviour, not introduced by #6138 — but #6138 is what puts the adopt
path under scrutiny, and the PR's new `Owns()` watch makes owner-reference correctness load
bearing (`FilterOwnerByKind` at `pkg/utils/transformer/owner_reference.go:45` matches on
`owner.Kind == "Dataset"`).

### B2 — adopt path never backfills the `datasetId` label (Low-Med, Confirmed)

The label is set only in the create branch (`dataset_runtime.go:103-105`), never on the
existing-runtime/adopt branch (`84-91`). A runtime that predates the label, or one reached via
the adopt branch, never acquires it. The label is read widely — `pkg/utils/label.go:190`
(`PatchLabelToObjects`), `pkg/ddc/thin/referencedataset/volume.go`,
`pkg/utils/kubeclient/configmap.go` — so the gap is not cosmetic.

Test: seed a label-less ThinRuntime, adopt it, assert `fluid.io/dataset-id` equals
`GetDatasetId(ns, name, uid)`.

Observed on `47077ff0` — the label was **absent entirely**:

```
B2 reproduced: the adopt path did not backfill the fluid.io/dataset-id label;
labels are map[] (want "default-b2-no-label")
--- FAIL: TestVerifyB2AdoptBackfillsDatasetIdLabel
```

Incidental observation: the expected value is `"default-b2-no-label"` — `GetDatasetId` →
`GetNamespacedNameValueWithPrefix` (`pkg/utils/label.go:164-181`) returns plain
`<namespace>-<name>` and only substitutes the owner UID when the composed string reaches
`DNS1035LabelMaxLength` (63). So for short names the label carries **no UID** and is *not*
recreate-unique. That is existing behaviour outside this PR's scope; flagged as a rough edge,
not a headline finding.

### B3 — Terminating-runtime guard blocks the `NoneDatasetPhase → NotBound` update (Low, Confirmed as canary)

In `reconcileDataset`, step 3 (reference-runtime creation, `dataset_controller.go:153-159`)
returns `utils.RequeueIfError(err)` before step 4 (phase update, `161-174`) ever runs. With the
PR's new terminating-runtime guard, a leftover ThinRuntime stuck in Terminating therefore keeps
a freshly created reference dataset at `NoneDatasetPhase` indefinitely — ironic given the PR
title mentions `NotBound`.

Polarity is **canary** on purpose: fail-loudly-and-back-off is a defensible design, and the
maintainers may prefer it to surfacing `NotBound` on a dataset that has no runtime. The test
records the observed phase so the decision is explicit and any reordering is caught.

Observed on `47077ff0`:

```
B3 observed: reconcileDataset err="the ThinRuntime default/b3-none-phase is terminating,
wait for it to be deleted before creating a new one for the reference dataset",
dataset phase="" (NotBound would be "NotBound")
--- PASS: TestVerifyB3TerminatingRuntimeBlocksNoneToNotBoundPhaseUpdate
```

The companion `TestVerifyB3NoneToNotBoundWithoutTerminatingRuntime` shows the very same dataset
reaching `NotBound` when no terminating runtime is present
(`Update the status of the dataset successfully {"phase": "NotBound"}`), so the canary is
isolating the short-circuit and not a broken fake-client status subresource.

## Harness-bites check (`results/harness-bites.txt`)

The proposed fixes were applied to production code **temporarily**, then reverted.

- B1/B2 fix in `pkg/utils/dataset_runtime.go`: merge owner references instead of replacing
  (dropping only the ref whose `Kind`+`APIVersion` match the desired Dataset owner, so a stale
  UID is superseded), and backfill `common.LabelAnnotationDatasetId` on the adopt branch.
- B3 fix in `pkg/controllers/v1alpha1/dataset/dataset_controller.go`: swap steps 3 and 4 so the
  phase update happens before reference-runtime creation.

Result — every test discriminated exactly as its polarity predicts:

| Test | polarity | on `47077ff0` | under proposed fix |
|------|----------|---------------|--------------------|
| `TestVerifyB1AdoptPreservesForeignOwnerReferences` | contract | FAIL | **PASS** |
| `TestVerifyB1StaleDatasetOwnerReplacedByFreshUID` | reference | PASS | PASS |
| `TestVerifyB2AdoptBackfillsDatasetIdLabel` | contract | FAIL | **PASS** |
| `TestVerifyB2CreateSetsDatasetIdLabel` | reference | PASS | PASS |
| `TestVerifyB3TerminatingRuntimeBlocksNoneToNotBoundPhaseUpdate` | **canary** | PASS | **FAIL (flipped, observed `"NotBound"`)** |
| `TestVerifyB3NoneToNotBoundWithoutTerminatingRuntime` | reference | PASS | PASS |

The fixes were then reverted and `git diff --stat` confirmed empty — the only tracked additions
on this branch are the harness files. No test was red for an unrelated reason.

## Regression check

The PR's own tests remain green with the harness present (`results/baseline.txt`):

```
go test ./pkg/utils/ -run TestCreateRuntimeForReferenceDatasetIfNotExist -count=1 -p 1   → ok
go test ./pkg/controllers/v1alpha1/dataset/... -count=1 -p 1                             → ok
```

**Known pre-existing failure, NOT a regression:** the *full* `./pkg/utils/` package fails on
`TestCheckMountPointBroken` (`pkg/utils/mount_test.go:182`, gomonkey-based). It fails identically
at the merge base `44cfae9f`, and the package's Ginkgo suite passes 115/115. Scope runs with
`-run` to keep it out of the pass/fail signal. Full-package wall time is ~112 s on a 2-vCPU box,
almost all of it in that one test.

## Cleared during review (context, not findings)

- `Owns(&ThinRuntime{})` at `dataset_controller.go:272` **does** fire.
  `cmd/dataset/app/dataset.go:239 NewCacheOptions()` label-filters only CronJobs, so ThinRuntime
  is cached unfiltered, and `datav1alpha1.AddToScheme` is registered at line 91.
- The new RBAC kubebuilder marker (`dataset_controller.go:69`) is **not** a privilege expansion —
  `charts/fluid/fluid/templates/role/dataset/rbac.yaml:98` already grants `thinruntimes` with the
  full verb set. Documentation catching up to deployed RBAC.
- The `Kind`/`APIVersion` fallback is well motivated: `FilterOwnerByKind`
  (`pkg/utils/transformer/owner_reference.go:45`) matches `owner.Kind == "Dataset"`, so an empty
  Kind silently breaks owner resolution for the 11 `GetOwnerDatasetUIDFromRuntimeMeta` callers.

## Proposed fixes (NOT applied to production on this branch)

- **B1**: merge rather than replace in the adopt branch — keep every existing reference whose
  `Kind`/`APIVersion` is not the Dataset owner, and replace only the (possibly stale) Dataset
  reference. Belt and braces: assert at most one `controller: true` reference survives.
- **B2**: backfill `common.LabelAnnotationDatasetId` on the adopt branch too, using the same
  `GetDatasetId(ns, name, uid)` value as the create branch, so both branches converge on the same
  object shape. (Both B1 and B2 sit inside the existing `reflect.DeepEqual` guard, so no extra
  write happens when nothing changed.)
- **B3**: make an explicit decision. Either (a) move the `NoneDatasetPhase → NotBound` update
  ahead of reference-runtime creation so the dataset's phase is always surfaced, or (b) keep the
  current ordering and document that a dataset blocked on a terminating runtime intentionally
  stays at the empty phase while the error is retried, ideally emitting a Warning event so the
  wedge is observable.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr6138-refdataset-thinruntime` (remote `fork` =
`https://github.com/cheyang/fluid.git`), production code untouched, so it grafts onto whatever
the fixed code is.

One-liner (auto-discovers the current PR head from `verify-manifest.json`'s `pr` URL):

```bash
bash docs/verification/pr6138-refdataset-thinruntime/scripts/re-verify.sh
```

It refuses to run on a dirty tree. Add an explicit ref to pin:
`… /re-verify.sh <fixed-ref>`. Only the `unit` layer exists, so
`--layers unit` is equivalent to the default here.

Manual route:

```bash
git fetch fork verify/pr6138-refdataset-thinruntime
git checkout <fixed-ref>
git checkout fork/verify/pr6138-refdataset-thinruntime -- \
  docs/verification/pr6138-refdataset-thinruntime \
  pkg/utils/dataset_runtime_verify_test.go \
  pkg/controllers/v1alpha1/dataset/dataset_reconciler_verify_test.go
go test ./pkg/utils/ ./pkg/controllers/v1alpha1/dataset/ \
  -run 'TestVerifyB1|TestVerifyB2|TestVerifyB3' -count=1 -p 1 -v
```

Prereqs: Layer 1 needs the Go toolchain only (the repo's `go.mod` auto-downloads Go 1.25.12,
~1 min on a first build). No envtest assets, no cluster, no credentials.

Read the results through the polarity table:
- **B1, B2** (contract): must be **GREEN**. Still red ⇒ still broken.
- **B3** (canary): **RED means fixed.** If it flips, invert the assertion to expect `"NotBound"`
  (or promote that to a contract test) so the harness stays meaningful.
- If a finding comes back `Harness-update`, the fix changed the code shape (renamed symbol, moved
  branch) — adjust the test, then re-run.
- Harness-bites: re-run Layer 1 against `47077ff0` once to confirm it still goes red on B1/B2.

The incremental-review marker lives in `.last-reviewed` next to the manifest (currently
`47077ff0`). `re-verify.sh` prints the `last-reviewed..head` delta and the one-liner to advance
it; commit the advanced marker so the next round/session/machine resumes without a typed sha.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/pr6138-refdataset-thinruntime (remote fork =
https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6138 produced findings B1-B3 and a unit harness
reproduced all three. Read docs/verification/pr6138-refdataset-thinruntime/README.md
("Continuing after the fix") and follow it: graft the harness onto the current PR head, run
  bash docs/verification/pr6138-refdataset-thinruntime/scripts/re-verify.sh
and mind the polarity table — B1/B2 are contract tests (green == fixed), B3 is a bug-canary
(red == fixed, then invert it). Layer 1 is the only layer; integration/live are documented as
deliberately skipped, do not build an envtest harness. Run the harness-bites check, advance
.last-reviewed, and report an observed-vs-expected table. Do not modify production code and do
not run anything against a real cluster.
```
