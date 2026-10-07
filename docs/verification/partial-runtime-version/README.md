# partial-runtime-version — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6186
("fix(cache): complete a partial runtime version from the class template", fixes #6178).

Layers run against the **code under review** (PR head `0361cc03c9156bc486c5c418b9e3a81fb7196f91`),
and against the **base** (merge-base `d8b37f28d645ae5cf3f0d853a79fc72ad222f968`) for the premise:

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (ginkgo, fake client) | `desiredComponentVersion`, `transformComponentPodTemplate`, `syncRuntimeSpec` → `updateImage` | `go test ./pkg/ddc/cache/engine/ -run TestCacheEngine -args -ginkgo.label-filter=verify-pr6186` |
| 2. Reference validity | `github.com/distribution/reference` (the parser kubelet wraps) on the rendered strings | `cd docs/verification/partial-runtime-version/refcheck && go run .` |
| 3. Live | skipped this round (per review scope: unit + integration only). The author ran a kind cluster; the observable signal is `advanced_statefulset_manager.go` logging `image changed, will update` for a tag-only edit. | — |

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when
> fixed. Bug-canary tests (assert current behavior) PASS now / FLIP to red when the behavior
> changes. P0 is the exception: it runs against the BASE branch, where its contract
> assertions are expected to FAIL — that failure is the reproduction of the reported bug.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "Setting only `imageTag` … silently does nothing: the AdvancedStatefulSet is not patched, the pods keep the old image, and there is no error, no event and no condition." (issue #6178) |
| Linked issue | #6178, OPEN (created 2026-09-01), still valid |
| Reported component | `AdvancedStatefulSetManager.updateImage` / CacheRuntime sync path (`pkg/ddc/cache/...`) |
| Patched component | same: `pkg/ddc/cache/engine/{sync,transform_common}.go` |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | On base, the repo's own composition-matrix row `runtimeVersion: L3 sets imageTag only … [pins current behaviour, #6178]` passes on BOTH create and update paths (tag dropped); harness P0a/P0b fail on base with observed `fluid/cache:v1` vs expected `fluid/cache:v2` (`results/base-harness.txt`); same two specs pass on the PR head (`results/pr-head-harness.txt`). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | imageTag-only runtimeVersion is dropped on base | 1 | Confirmed | base: P0a/P0b FAIL, observed `fluid/cache:v1`; PR head: PASS (`results/base-harness.txt`, `results/pr-head-harness.txt`) |
| F1 | a digest-pinned `runtimeVersion.image` with no tag gets its tag completed from the template, rendering the invalid reference `repo@sha256:…:v1` on both paths | 1+2 | **Confirmed** | PR head: F1a/F1b/F1c FAIL, observed `btxu/mooncake@sha256:0676…552:v1` on the AdvancedStatefulSet and on the create path; `results/refcheck.txt`: the concatenation is `invalid reference format` while the digest alone is VALID |
| F2 | editing the CacheRuntimeClass template image now rolls every existing empty-version runtime of that class to the new image at the next sync | 1 | Confirmed (behavior change, introduced by this PR) | canary passes on PR head (worker moves `fluid/cache:v1`→`v2` after the class edit), fails on base (stays `v1`) |

## Per-finding detail

### F1 — digest image + template tag = invalid reference (blocker candidate)

`desiredComponentVersion` (transform_common.go) completes a missing half from the class
template. It refuses to complete from a digest-pinned *template* (`splitImageReference`
returns empty for `@`), but it does not check the *runtime-supplied* `image` half for a
digest. With `runtimeVersion: {image: "repo@sha256:…"}` (no tag) against a tagged template,
it fills `imageTag` from the template, and both renderers (`transformComponentPodTemplate`
and `updateImage`) concatenate `image + ":" + imageTag`, producing `repo@sha256:…:v1` —
which no container runtime can parse (`results/refcheck.txt`, proven with
`github.com/distribution/reference`, the parser kubelet wraps). The function's own doc
comment promises: "A version whose missing half cannot be recovered … stays incomplete,
because guessing there would move the workload onto something nobody asked for." A digest's
tag is exactly such a half. On base this input was dropped by the both-halves guard, so the
PR converts a silently-ignored edit into pods that fail with an invalid image.

Harness-bites: with the one-line proposed fix applied, F1a/F1b/F1c all pass
(`results/f1-bites-check.txt`); the fix was reverted afterwards — production diff is empty.

### F2 — class template image edits now propagate to existing runtimes (minor / question)

Pre-PR, the class template image was a creation-time-only input: editing it never touched
existing workloads. `syncRuntimeSpec` now completes an empty `runtimeVersion` from the
*current* class template on every reconcile (the controller requeues periodically), so a
class edit rolls every empty-version runtime of the class onto the new image — a fleet-wide
rollout from a single class edit. This is consistent with how `resources` already sync from
the template, and the PR documents the related "removing a runtimeVersion rolls back to the
template" case, but the class-edit propagation itself is not mentioned in the PR. Canary
`F2` pins the new behavior; on base it flips (no propagation).

### Pre-existing failures (claim check)

The PR claims 12 failing specs on master in this package. Confirmed: identical 12 failures
(11 in `ufs_test.go` mount handling, 1 in `sync_test.go`) on base and on the PR head; the PR
adds 17 passing specs and no new failures (`results/full-suite-base-vs-pr.txt`).

## Proposed fixes (NOT applied to production here)

- **F1**: in `desiredComponentVersion`, treat a digest-pinned runtime `image` like a
  digest-pinned template — leave the missing tag incomplete:
  ```go
  if runtimeVersion.ImageTag == "" && !strings.Contains(runtimeVersion.Image, "@") {
      runtimeVersion.ImageTag = templateTag
  }
  ```
- **F2**: no code change required; document that the class template image is now the
  continuously-reconciled baseline for runtimes that do not pin a full version (or gate the
  empty-version completion so only explicit partial versions complete).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/partial-runtime-version-codex` (production code untouched).

1. Graft and re-run in one step:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/partial-runtime-version-codex
   git checkout verify/partial-runtime-version-codex
   bash docs/verification/partial-runtime-version/scripts/re-verify.sh   # resolves the PR head itself
   ```
2. Prereqs: Go toolchain only (layer 2 uses the module cache; `go mod tidy` inside
   `refcheck/` if needed). No cluster required.
3. Polarity: F1a/F1b/F1c are contract tests — green = fixed. F2 is a canary — it flips red
   if the propagation behavior is removed or altered. P0 specs assert the fixed behavior and
   are only meaningful against base; against a fixed PR head they should be green.
4. Re-run the reference-validity check: `cd docs/verification/partial-runtime-version/refcheck && go run .`
5. Harness-bites: F1 specs must be red on the un-fixed PR head `0361cc03`.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/partial-runtime-version-codex
(remote https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6186 produced findings F1 (digest-pinned
runtime image gets a template tag appended -> invalid reference) and F2 (class template
image edits propagate to existing runtimes); a harness reproduced them. Read
docs/verification/partial-runtime-version/README.md ("Continuing after the fix") and follow
it: bash docs/verification/partial-runtime-version/scripts/re-verify.sh, mind the polarity
table (F1 contract, F2 canary), and report an observed-vs-expected table. Do not touch
production code.
```
