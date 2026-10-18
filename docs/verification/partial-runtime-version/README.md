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

---

## Round 2 — incremental review of `0361cc03..382d926a` (head `382d926ae2d4`)

Re-ran the harness against the new head (`scripts/re-verify.sh`, grafted onto `382d926a`)
and reviewed the seven new commits (`7c7abfbf` digest guard, `514396bb`/`b159f1cc` tests,
`99659bf4` data-operation image, `194cba6d` incomplete-version log, `ad7eca8b`/`382d926a`
docs). Branch rebased onto `382d926a` so the branch diff is again harness-only.

| ID | Round-1 claim | Round-2 verdict | Evidence |
|----|---------------|-----------------|----------|
| F1 | digest-pinned runtime image gets a template tag appended (`repo@sha256:…:v1`) | **FIXED** | `desiredComponentVersion` now refuses to complete a tag onto an `@`-pinned image; F1a/F1b/F1c pass on the new head (re-verify: pass=3 fail=0) |
| F2 | class template image edits propagate to existing runtimes, undocumented | **FIXED (documented)** | docs (`cacheruntime_spec_update.md`, en+zh) now state the template baseline, the propagation rule, the reconcile-delay caveat and the upgrade note; behavior unchanged, so the spec was promoted canary→contract as a regression guard (manifest `changeReason`) |
| F3 | NEW: the discarded-edit signal is `e.Log.V(1)`, but the shipped chart pins `--development=false` (→ zap InfoLevel, V(1) dropped) and the binary registers no verbosity flag; docs' `-v=1` hint is not actionable | **Confirmed, open** | `results/f3-production-visibility.txt` (static chain); F3 contract spec FAILS under `UseDevMode(false)` while the dev-mode control PASSES (`results/f3-bites-check.txt`); bites check: the one-line fix `e.Log.V(1)`→`e.Log.Info` turns the contract green |

Full suite on the new head: 295 passed / 12 failed — the same 12 pre-existing
mount-handling failures recorded in round 1; the delta adds only passing specs
(`results/full-suite-head-r2.txt`, run with the harness grafted: 301 passed / 12 failed).

### F3 — detail

`logIncompleteVersion` (transform_common.go) is the only signal for the deliberately
un-completable cases (digest-pinned runtime image without a tag; template without a usable
half). It logs at V(1). The vendored controller-runtime zap defaults
(`vendor/sigs.k8s.io/controller-runtime/pkg/log/zap/zap.go` `addDefaults`) select
`InfoLevel` when `Development` is false, which drops V(1) records; the Helm chart deploys
the controller with `--development=false`; and `cmd/cache/app/cache.go` never calls
`zap.BindFlags`, so no `--zap-log-level`/`-v` flag exists to turn it back on. Issue #6178's
fallback ask was "at minimum a Warning event" for edits that are accepted and discarded;
in production these cases are still silent. Suggested fixes: log at V(0), or emit a Warning
event from `syncRuntimeSpec` (its `ctx.Recorder` is available), and drop or fix the `-v=1`
doc hint.

Polarity after this round: all findings are contract tests. F3 stays red until the signal
becomes visible in the production config; everything else is green on `382d926a`.
`.last-reviewed` advanced to `382d926ae2d433506982896ea56c4d57bd747bb7`.

---

## Round 3 — incremental review of `382d926a..902625af` (head `902625af3696`)

Six commits: `8430488e` replaces the V(1) log with `reportIncompleteVersion` (V(0) log +
`RuntimeVersionIncomplete` Warning event on the CacheRuntime), `a1bf08bb` extends the event
to the creation path (transformMaster/Worker/Client; `transformComponentPodTemplate` no
longer reports, so no per-reconcile double-fire), `3c392b29` pins event type and count in
tests, `022e319f` adds the symmetric tag-only doc warning, `003cdadc` drops the upgrade note
(verified: `05682681` "Init cacheruntime api" is contained in zero release tags — CacheRuntime
is unreleased, so the removal is factually right), `902625af` documents that the client
reports on creation only.

The round-2 F3 harness referenced the removed `logIncompleteVersion` → HARNESS-UPDATE on
first re-run (expected; not proof of a fix). The harness was updated additively to the new
symbol (`verify_pr6186_visibility_test.go`, manifest `changeReason`), then re-run:

| ID | Round-2 state | Round-3 verdict | Evidence |
|----|---------------|-----------------|----------|
| F1 | fixed | **still FIXED** | re-verify pass=3 fail=0 on `902625af` (`results/reverify/`) |
| F2 | fixed (documented) | **still FIXED** | promoted contract green on `902625af`; propagation docs intact |
| F3 | confirmed open | **FIXED** | contract "shipped production config emits the signal" now PASSES under `UseDevMode(false)` (V(0) log), plus a Warning event contract (`record.NewFakeRecorder`, exact reason+count) and a no-over-reporting control; bites check: reverting to V(1)+no-event turns both contracts red, control stays green (`results/f3-round3-bites-check.txt`) |

Full suite on `902625af` with the harness grafted: 308 passed / 12 failed — the same 12
pre-existing mount-handling failures (`results/full-suite-head-r3.txt`).

Caller-chain checks on the new code: `transform()` runs only inside `Setup`, which the
reconciler gates behind `!utils.IsSetupDone(dataset)` (`pkg/controllers/runtime_controller.go`),
so creation-path reporting is bounded by readiness and cannot fire per-reconcile forever;
the sync path reports only for enabled master/worker; disabled components return before the
report in all three transform functions; `Recorder` is always non-nil in production
(`engine.go:77` ← `ctx.Recorder` ← reconciler), the nil guard is for unit tests; the cache
controller ClusterRole already carries `events` verbs. Emitted events are aggregated by the
client-go correlator, matching existing engine patterns (`setup.go` RuntimeMountUfsFailed,
`dataload.go`).

Also fixed this round: the re-verify.sh copy now persists `results/reverify/` AFTER the
tree restore — the results dir is tracked in this branch, so the previous order let the
script's own `git checkout -f` clobber the fresh files with the committed round-1 ones.

`.last-reviewed` advanced to `902625af3696ff6687d398c1699b6f2bba191e96`.
