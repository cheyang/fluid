# Verification — PR #6186 (cache): complete a partial runtime version from the class template

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6186 (fixes #6178)
- **Round:** 1 (first round, reviewer Claude)
- **Harness branch:** `verify/cache-partial-version-claude`
- **Code under review:** `0361cc03c9156bc486c5c418b9e3a81fb7196f91` (= PR head, recorded in `.last-reviewed`)
- **Merge base:** `d8b37f28d645ae5cf3f0d853a79fc72ad222f968`
- **Production diff introduced by this harness:** none — additive test files + this docs tree only.

## Premise verdict: **CONFIRMED**

Issue #6178 claims that a `runtimeVersion` naming only `imageTag` (or only `image`) is a legal
edit that `updateImage` silently drops — no patch, no event, no condition — while the
CacheRuntime's generation increments.

Reproduced on the merge base by running this harness's P0 contract specs against `d8b37f28`
(`version_completion_sync_verify_test.go` deliberately references only base-era symbols so it
compiles there):

- `contract: an imageTag-only edit is applied to the worker workload` — **RED on base** (image
  stays `verify/worker:v1`), GREEN on PR head (becomes `verify/worker:v2`).
- `contract: an image-only edit is applied to the worker workload` — **RED on base**, GREEN on
  PR head.
- control spec (complete version) is green on both, so the harness itself is well formed.

Raw output: `results/base-with-harness.txt` (264 Passed | 16 Failed = 12 pre-existing + 4 red
harness specs) vs `results/pr-head-with-harness.txt` (291 Passed | 12 Failed — the same 12
pre-existing, all harness specs green).

Component match: the issue names `pkg/ddc/cache/component/advanced_statefulset_manager.go`
`updateImage`; the PR fixes the callers (`pkg/ddc/cache/engine/transform_common.go`,
`sync.go`) rather than `updateImage` itself, which it leaves as a safety net. Same component,
same symptom — match.

## Observed vs expected

| id | claim | polarity | layer | on base | on PR head | verdict |
|----|-------|----------|-------|---------|------------|---------|
| P0/FIX | imageTag-only / image-only edit reaches the worker ASTS | contract | integration | RED (edit dropped) | GREEN | fix confirmed |
| F1 | worker image and DataLoad image diverge for a partial version | canary | integration | n/a (no divergence on base) | GREEN (divergence present) | confirmed — PR leaves `image.go` unaligned |
| F2 | dropping a runtimeVersion (or editing the class template) now rolls the live workload to the template image | contract | integration | RED (image frozen) | GREEN | confirmed — deliberate semantics change, pinned |
| F3 | completion corner cases: trailing colon, registry port with nested path, tagless template, digest-pinned template | contract | unit | n/a (symbols new in PR) | GREEN | confirmed as documented |

## The 12 pre-existing failures

The PR body claims `pkg/ddc/cache/engine` has 12 failing specs on master, untouched by the
change. Verified: base `262 Passed | 12 Failed`, PR head `291 Passed | 12 Failed`, and the
failing spec lists are **byte-identical** (see `results/preexisting-failing-specs-base.txt`).
All 12 are mount/`Execute`-mocking specs in `ufs_test.go` / `sync_test.go`, unrelated to
version handling.

## F1 in detail (the one open finding)

`pkg/ddc/cache/engine/image.go:33` (`getDataOperationImage`, the DataLoad image resolution)
still requires both halves and otherwise falls back to the class template image. The PR's
completion does not apply there, so after an imageTag-only upgrade the worker workload runs
`verify/worker:v2` while a DataLoad that does not set its own image runs `verify/worker:v1`.

Harness-bites check: the proposed fix

```go
completed := desiredComponentVersion(runtime.Spec.Worker.RuntimeVersion,
    componentTemplateImage(runtimeClass.Topology.Worker))
if completed.Image != "" && completed.ImageTag != "" {
    image = completed.Image + ":" + completed.ImageTag
}
```

was applied temporarily to the PR head; both F1 canaries flipped to RED (they bite), then the
patch was reverted (`git checkout -- pkg/ddc/cache/engine/image.go`). Evidence:
`results/harness-bites-f1-flip.txt`.

## Layers

- **L1 unit** (`ginkgo`, deterministic): `version_completion_unit_verify_test.go` —
  `splitImageReference` / `desiredComponentVersion` corner cases, creation-path behavior over
  digest-pinned and tagless templates.
- **L2 integration** (`ginkgo` + controller-runtime fake client): `version_completion_sync_verify_test.go`
  — `syncRuntimeSpec` against a seeded worker AdvancedStatefulSet; this is the layer that
  reproduces the issue on base.
- **L3 live**: **skipped by instruction for this round** (unit + integration only). The probe
  *did* find a reachable cluster (`kubectl get no` → 2 aliyun v1.36.2 nodes via
  `~/.kube/config`); nothing on it was created, mutated or deleted. If it is ever run: create a
  CacheRuntime against a class whose worker template pins an image, patch
  `runtimeVersion.imageTag` only, and expect the ASTS generation bump, the new image, and the
  controller log line `image changed, will update`.

Run everything:

```bash
go test ./pkg/ddc/cache/engine/ -ginkgo.json-report=/tmp/gr.json
jq -r '.[].SpecReports[]? | "\(.LeafNodeText)\t\(.State)"' /tmp/gr.json | grep -E 'PR-6186|canary|contract:'
```

Re-verify after the author pushes a fix (resolves the PR head from `manifest.pr` on its own):

```bash
bash docs/verification/cache-partial-version/scripts/re-verify.sh
```

## Continuing after the fix

- `FIX`, `F2`, `F3` are contracts: they must stay green.
- `F1` is a **canary**: it is green while the divergence exists and reports `STILL-PRESENT` in
  `re-verify.sh`. When `image.go` is aligned, it flips to fail and must be inverted (assert the
  DataLoad image equals the completed worker image) or promoted to a contract test.
- If the author changes the completion rules (e.g. handles the tagless-template image-only
  case), the F3 contracts will go red — that is the signal to re-review the semantics, not
  necessarily a bug.

Fresh-agent kickoff: clone this branch (`verify/cache-partial-version-claude` from the
reviewer fork `cheyang/fluid`), then `bash docs/verification/cache-partial-version/scripts/re-verify.sh`
— no sha needed; it fetches the current PR head itself.

---

# Round 2 — head 382d926a (delta 0361cc03..382d926a)

The PR gained 7 commits since round 1. Three of them are functional:

- **99659bf4** `fix(cache): resolve the data operation image like the worker image` —
  `getDataOperationImage` now routes through `componentImage`, closing the F1 divergence.
- **7c7abfbf** `fix(cache): leave a digest-pinned runtime image without a tag` — a
  `runtimeVersion` whose `image` is pinned by digest no longer takes the template's tag.
- **194cba6d** `fix(cache): log a partial runtime version the template cannot complete` —
  `logIncompleteVersion` records the un-completable partials at V(1).

plus tests (514396bb, b159f1cc) and docs (ad7eca8b, 382d926a, en+zh).

## Re-verify outcome (canary polarity applied)

| id | polarity | on round-1 head 0361cc03 | on head 382d926a | verdict |
|----|----------|--------------------------|------------------|---------|
| FIX | contract | green | green (3/3) | fix still holds |
| F1 | canary→contract | **red** (divergence present) | **green** (2/2) | **FIXED** — canary flipped at 99659bf4, inverted into contracts |
| F2 | contract | green | green (2/2) | deliberate semantics still pinned, now documented |
| F3 | contract | green | green (5/5) | corner cases hold |
| F4 | contract (new) | **red** (runtime-side digest spec) | **green** (2/2) | regression present at round-1 head, fixed by 7c7abfbf |

F4 was discovered this round while reviewing the delta: at the round-1 head,
`desiredComponentVersion({Image: "repo@sha256:..."}, "fluid/cache:v1")` appended the
template tag, which callers render as `repo@sha256:...:v1` — an unpullable reference.
The merge base did not have this problem (it dropped the edit instead — the #6178
behavior), so the defect was introduced by the initial PR and fixed within it.

Raw evidence:

- `results/reverify-previous-head-0361cc03/` — 290 Passed | 15 Failed (12 pre-existing
  + 2 inverted F1 contracts + 1 F4 spec red).
- `results/reverify-current-head-382d926a/` — 309 Passed | 12 Failed; the 12 are the
  byte-identical pre-existing mount/Execute specs.
- The red→green flip across the two heads is also the harness-bites proof for the
  inverted F1 contracts and the new F4 contract (no production patch was applied this
  round; the author's own commits are the fix under test).

## Harness updates this round

- F1a/F1b canaries inverted into contracts (`getDataOperationImage completes a
  partially specified worker version`, `an imageTag-only upgrade moves the worker and
  the DataLoad image together`).
- F4 added (`a digest-pinned runtime image does not gain a tag`, `a digest-pinned
  template does not lend its (absent) tag to an image-only version`).
- Manifest updated accordingly (F1 → contract, F4 added, unit layer).

L3 (live) still skipped by instruction; the PR author's kind-cluster run in the PR
body remains the live evidence, and CI is fully green at this head (see the round-2
findings ledger).
