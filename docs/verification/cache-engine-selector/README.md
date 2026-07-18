# Verification — PR #6064: populate `Status.Selector` in CacheEngine

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6064
- **Title:** `fix: populate Status.Selector in CacheEngine for worker pod discovery`
- **Reviewed head:** `1901b8cc` (branch `fix/cache-engine-selector`, merge-base `36f0467a`)
- **Harness branch:** `verify/cache-engine-selector` (production code untouched — diff is additive tests + this dir only)
- **Layers:** unit only. No `KUBECONFIG` was provided, and the change is a pure
  selector string plus a static CRD-shape check, so unit is authoritative here.

## What the PR does

Removes two `TODO`s and populates `CacheRuntime.Status.Selector` with a worker label
selector, in both `setupMasterInternal` (`master.go`) and `CheckAndUpdateRuntimeStatus`
(`status.go`), via a new helper:

```go
func (e *CacheEngine) getWorkerSelectors() string {
    workerName := common.GetCacheComponentName(e.name, common.ComponentTypeWorker)
    return labels.SelectorFromSet(labels.Set{
        common.LabelCacheRuntimeName:          e.name,
        common.LabelCacheRuntimeComponentName: workerName,
    }).String()
}
```

## Hypotheses / findings

| ID | Finding | Polarity | Expectation on PR head |
|----|---------|----------|------------------------|
| F1 | `getWorkerSelectors()` produces a selector that **exactly matches** the labels CacheRuntime worker pods carry | contract | **PASS** (change is correct) |
| F2 | `Status.Selector` is populated but **nothing consumes it** — the CacheRuntime CRD has no `scale` subresource (`selectorpath=.status.selector`), unlike alluxio/juicefs/vineyard | canary | **PASS** (gap exists); flips when closed |
| F3 | Selector is set in **two places** (`master.go` + `status.go`) with the same deterministic value | note only | non-blocking, no harness |

## Observed vs expected

| Finding | Test(s) | Observed | Verdict |
|---------|---------|----------|---------|
| F1 | `engine.TestVerifyWorkerSelectorMatchesWorkerLabels` + `component.TestVerifyWorkerCommonLabelScheme` | both PASS | **Confirmed correct** — selector matches worker pod labels, is worker-specific, and does not match master or other-runtime pods |
| F2 | `engine.TestVerifyCacheRuntimeHasNoScaleSubresource` | PASS (canary) | **Confirmed gap** — CacheRuntime CRD subresources are `status: {}` only; no `scale:`. Peer `data.fluid.io_alluxioruntimes.yaml` has `scale:` with `labelSelectorPath: .status.selector` |
| F3 | — | code read | **Confirmed, non-blocking** — same deterministic string set in both sites; harmless redundancy |

### Harness-bites check (Step 4)

Both tests were proven to fail for the right reason, then reverted (production diff empty):

- F1: changing `getWorkerSelectors` to use the **master** component name →
  `TestVerifyWorkerSelectorMatchesWorkerLabels` **FAILS**.
- F2: injecting a `scale:` subresource into `data.fluid.io_cacheruntimes.yaml` →
  `TestVerifyCacheRuntimeHasNoScaleSubresource` **FLIPS to FAIL** (as a canary should).

## How to run

```bash
# from a checkout of this harness branch (verify/cache-engine-selector)
go test ./pkg/ddc/cache/engine/ ./pkg/ddc/cache/component/ -run TestVerify -count=1 -v
```

Raw output: `results/unit.out`.

## Notes for the author / next steps

- **F1 is a genuine, correct fix.** The selector `cacheruntime.fluid.io/component-name=<name>-worker,cacheruntime.fluid.io/name=<name>`
  matches the labels applied by `component.getCommonLabelsFromComponent`.
- **F2 is the real caveat.** As it stands, populating `Status.Selector` has **no effect on
  HPA** for `CacheRuntime`, because the CRD exposes no `scale` subresource pointing at
  `.status.selector`. The PR is a valid *prerequisite*, but the stated "worker pod discovery
  / auto-scaling" benefit is not reachable until a
  `// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=...,selectorpath=.status.selector`
  marker is added to `api/v1alpha1/cacheruntime_types.go` and the CRD regenerated. Recommend
  the author either add that marker in this PR or track it in a follow-up issue.
- **F3** is stylistic; not worth blocking.

## Continuing after a fix (resume on any machine)

All durable state is on the `verify/cache-engine-selector` branch: the harness tests, this
README, `verify-manifest.json`, `.last-reviewed`, and `scripts/re-verify.sh`.

```bash
git fetch origin && git checkout verify/cache-engine-selector
# no sha needed — re-verify resolves the current PR head from manifest.pr
bash docs/verification/cache-engine-selector/scripts/re-verify.sh
```

Polarity when interpreting results:
- **F1 (contract):** stays green while the fix is present. Red ⇒ a regression in the selector
  or the worker label scheme.
- **F2 (canary):** green means the HPA gap still exists. When it **flips to red**, the author
  has added the scale subresource — invert this test (or promote it to a contract test that
  asserts `selectorpath=.status.selector` is present) so it guards the new behavior.

After reviewing a new delta, advance the marker:

```bash
echo <new-head-sha> > docs/verification/cache-engine-selector/.last-reviewed
git commit -am "review: advance last-reviewed"
```

### Copy-paste kickoff for a fresh agent

> Resume the review pipeline for https://github.com/fluid-cloudnative/fluid/pull/6064.
> The verification branch `verify/cache-engine-selector` on remote `origin` has the harness,
> manifest, and `.last-reviewed`. Check it out, run `scripts/re-verify.sh` (no sha — it reads
> `manifest.pr`), report Fixed/Still-broken per finding honoring polarity (F2 is a canary:
> fixed only when it flips), review the `last-reviewed..head` delta, then advance the marker.
