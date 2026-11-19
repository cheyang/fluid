# PR #6262 — deterministic webhook/injector map iteration — verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6262 (round 1 head `ac9e3e1`, round 2 head `08c111a`, base `dcb7616`).

Layers run (live layer intentionally skipped; the effects are admission-time
string/slice ordering, fully decidable in-process):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `assembleMountInfos`, namespace pick, prefetcher sidecar/file-list assembly, nodeaffinity partition, mountpropagation mutate | `go test -count=1 -run 'TestVerify' ./pkg/application/inject/fuse/ ./pkg/webhook/plugins/fileprefetcher/ ./pkg/webhook/plugins/nodeaffinitywithcache/ ./pkg/webhook/plugins/mountpropagationinjector/` |
| 2. Integration (package suites incl. the PR's new strict ginkgo specs) | whole touched packages | `go test -count=1 ./pkg/application/inject/fuse/... ./pkg/webhook/plugins/...` |

> Polarity: the four `TestVerify*Deterministic` tests are CONTRACTS (fail on
> base, pass on head). The two `TestVerify*OrderIndependent` tests are
> INVARIANCE probes for findings F1/F2: they must pass on **both** base and
> head, because they document that the corresponding hunks have no observable
> effect. If a future revision makes an invariance probe fail, the hunk has
> acquired real behavior and the finding must be re-examined.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "unordered Go map iterations ... eliminate webhook mutation diffs / test flakiness across all injection sites" (PR body, §I) |
| Linked issue | none closed; follow-up to merged PRs #6217 / #6234 (same determinism theme) |
| Reported component | webhook plugins + FUSE application injectors |
| Patched component | same |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | `results/base-harness.txt`: all four contract tests FAIL on base `dcb7616` with scrambled orders; `results/head-harness.txt`: all pass on head |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | map iteration order leaks into pod mutations on base | 1 | Confirmed | base: assembleMountInfos returned `/var/data/m:/var/data/q:...` (scrambled) at iteration 0; namespace pick hit all 3 namespaces in 30 runs; prefetcher env/volumeMount/fileList orders varied (results/base-harness.txt) |
| F1 | `getRequiredAndPreferredSchedulingRuntimes` sorting hunk is a behavioral no-op (returns maps; consumers already sort via `utils.OrderedKeys`) | 1 | Confirmed (invariance) | probe passes on base AND head; full package suite green with the hunk reverted (results/head-with-nodeaffinity-hunk-reverted.txt) |
| F2 | `mountpropagationinjector` sorting hunk is a behavioral no-op, and the PR's new "deterministic order" spec is order-blind | 1 | Confirmed (invariance) | probe passes on base AND head; PR's own new spec passes against BASE plugin code — 5/5 specs green (results/base-pr-mountpropagation-test.txt) |
| R1 | fixes in mount_point_script.go / file_prefetcher.go hold | 1 | Pass at head | results/head-harness.txt |
| R2 | PR's own suites pass at head | 2 | Pass | results/head-full-suite.txt |

## Per-finding detail

### F1 — nodeaffinitywithcache: sorting is dead determinism (minor)
`getRequiredAndPreferredSchedulingRuntimes` sorts three loops that only
populate maps (`boundDataSetNames`, `preferredRuntimes`, `requiredRuntimes`).
Go maps carry no iteration order, and the only consumers
(`getTieredLocalityNodeSelectorTerms`, `getTieredLocalityPreferredSchedulingTerms`)
already iterate `utils.OrderedKeys(...)` since #6234. Verified three ways:
invariance probe green on base and head; package suite (incl.
`TestTieredLocalityDeterministicOrdering`) green with the hunk reverted;
code-path inspection shows no order-sensitive sink.

### F2 — mountpropagationinjector: no-op hunk + order-blind test (minor)
`datasetNames` order only feeds `utils.ContainsString` membership checks in
`utils.InjectMountPropagation` (and one V(1) log line), so sorting it cannot
change the mutated pod. The PR's new spec
"should mutate pod with multiple runtimeInfos in deterministic order" asserts
only that MountPropagation is set — it passes against the BASE implementation
(5/5 specs), so it does not test the property its name claims.

## Proposed fixes (NOT applied to production here)
- F1/F2: either drop the no-op hunks (keep the PR focused on sites where order
  is observable), or keep them for symmetry but rename the mountpropagation
  spec so it doesn't claim to verify ordering, e.g. "should mutate pod with
  multiple runtimeInfos".

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/pr-6262-deterministic-webhook-mutation-codex`
(production code untouched).

```bash
git fetch https://github.com/cheyang/fluid.git verify/pr-6262-deterministic-webhook-mutation-codex
git checkout verify/pr-6262-deterministic-webhook-mutation-codex
bash docs/verification/pr-6262-webhook-deterministic-mutation/scripts/re-verify.sh            # grafts onto current PR head
bash docs/verification/pr-6262-webhook-deterministic-mutation/scripts/verify-premise-on-base.sh  # re-checks P0 against base
```

## Round 2 — head `08c111a` (delta `ac9e3e1..08c111a`)

The author addressed every round-1 finding in the delta. `re-verify.sh` against
`08c111a`: all manifest findings FIXED (results/reverify/). Ledger-ID mapping:
harness F1 = ledger `F2-nodeaffinity-noop-sorted-loops`, harness F2 = ledger
`F3-mountpropagation-noop-sort` / `F4-mountpropagation-order-blind-test`.

| Ledger ID | Round-1 finding | Delta change | Round-2 status | Evidence |
|-----------|-----------------|-------|----------------|----------|
| F1-namespace-test-single-invocation | namespace spec called the path once (~1/3 blind pass on base) | spec rewritten as a 50x loop with a fresh fake client per iteration, asserting CM only in ns-a and none in ns-m/ns-z | **fixed** | grafted head test file onto BASE production code: RED (`results/round2-f1-test-bites-base.txt`); GREEN on head (`results/round2-f1-test-green-head.txt`); reproducer: `scripts/verify-f1-test-bites.sh` |
| F2-nodeaffinity-noop-sorted-loops | three no-op sorted loops | hunk fully reverted; `git diff base..head -- pkg/webhook/plugins/nodeaffinitywithcache/` is empty | **fixed** | diff inspection + package suite green on head (`results/round2-head-full-suite.txt`); invariance probe still passes (now trivially, base==head code) |
| F3-mountpropagation-noop-sort | no-op sort of datasetNames | sort retained but now commented `// Sort keys defensively so debug log output is deterministic` (the "mark it defensive" recommendation) | **fixed** | head source inspection; invariance probe still passes |
| F4-mountpropagation-order-blind-test | spec name claimed ordering it never asserts | renamed to `should mutate pod with multiple runtimeInfos`; no "deterministic order" string remains | **fixed** | head test file inspection; suite green |
| F5-loop-break-first-key | loop-break-to-first-key idiom nit | loop now performs nil-skipping (`if ri := runtimeInfos[k]; ri != nil`), so `keys[0]` indexing is no longer equivalent — the nit is obsolete | **fixed** | head source inspection (mount_point_script.go:43-48) |
| F6-nil-runtimeinfo-deref (pre-existing) | unguarded `runtimeInfos[k].GetNamespace()` | nil guard added; first *valid* runtime info picked | **fixed** | head source inspection; full suite green. Note: if every value were nil (unreachable in-tree, see round-1 analysis), injection would proceed with an empty namespace and surface a client error from `ensureScriptConfigMapExists` instead of panicking — strictly better than base. |

Regression status of the fixes themselves (R1/R2): unchanged — all contract
tests still pass on the new head; 5 sequential repeat runs of the touched
packages stable; `go vet` and `gofmt -l` clean.
