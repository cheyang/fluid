# pr6283-juicefs-cmd-options — bug verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6283
(issue #6282). Reviewer branch: `verify/pr6283-juicefs-cmd-options-claude`, based on the PR
head `cf975a08cacccb43ad32bd44bca721df93bfec78` (merge base `253f6bb84f26a906fba301550b5c81ebdd73af35`).
The harness is **additive only** — no production code is touched by this branch.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `isCommandChanged` option parsing, commands produced by the real `genFuseMount` generator | `go test ./pkg/ddc/juicefs/ -run 'TestVerifyP0\|TestVerifyV1\|TestVerifyV2' -v` |
| 2. Integration (fake API server) | the full `syncWorkerSpec` lifecycle: script ConfigMap rewrite + worker sts restart annotation | `go test ./pkg/ddc/juicefs/ -run 'TestVerifyV3' -v` |
| 3. Live | not run this round (see liveNote in the manifest) | — |

> Test polarity: **all tests here are CONTRACT tests** — they assert the intended correct
> behavior, FAIL on the merge base (buggy code) and PASS on the PR head (fixed). There are no
> bug-canaries in this set.

## Problem premise (P0)

Run against the **base** branch `253f6bb8` (without the patch), because the question is
whether the problem exists today.

| | |
|---|---|
| Claimed symptom (issue #6282) | "a dataset such as `demo-output` splits the command in the wrong place. Option names such as `cache-partial-only` contain `-o` too. In those cases the second piece is not the option list, the old and new options look identical and the change is never detected" → "the mount script ConfigMap is not updated and the worker StatefulSet template does not get its restart annotation" |
| Linked issue | #6282, OPEN, created 2026-11-20 (2 days before this PR), still valid — the affected code (`isCommandChanged`) is unchanged on base since the report |
| Reported component | `pkg/ddc/juicefs/sync_runtime.go` — `JuiceFSEngine.isCommandChanged`, called from `syncWorkerSpec` and `updateFuseCmdConfigmapOnChanged` |
| Patched component | exactly that function in exactly that file |
| Component match | **Yes** |
| **Verdict** | **Confirmed** |
| Evidence | `results/unit-base.out`: `TestVerifyP0_GenFuseMount_DatasetNameContainsDashO` FAILS on base with commands built by the **real** `genFuseMount` (dataset `demo-output`, only `cache-size` 1024→2048 differs, not detected). `results/unit-head.out`: PASSES on the PR head. `results/pr-tests-on-base.out` independently confirms the PR's own new test cases fail on base (author's claim checks out). |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | changed `cache-size` is not detected when the dataset name contains `-o` (commands from the real generator) | 1 | **Confirmed on base / fixed on head** | `results/unit-base.out` FAIL → `results/unit-head.out` PASS |
| V1 | changed `cache-size` is not detected when an option name contains `-o` (`cache-partial-only`) | 1 | **Confirmed on base / fixed on head** | same pair of artifacts |
| V2 | guard: same options reordered must stay "not changed" (no over-reporting) | 1 | **Passes on base AND head** | same pair — exists so future parser rewrites cannot start rolling workers every reconcile |
| V3 | user-visible lifecycle: worker script ConfigMap not rewritten, no `kubectl.kubernetes.io/restartedAt` annotation on the worker sts | 2 | **Confirmed on base / fixed on head** | base: `changed=false`, CM mount line still `cache-size=1024`, no restart annotation; head: CM rewritten + annotation present (`results/unit-{base,head}.out`) |

Environment noise check: the full `./pkg/ddc/juicefs/...` suite passes on the PR head
(`results/fullpkg-head.out`), and on base the ONLY failures in the package are the three
harness contract tests — so the base failures are attributable to the bug, not the environment.

## Per-finding detail

- **P0 / V1 mechanism** (base code): `getOption` did `strings.Split(command, "-o")` and read
  `commands[1]`. The generated command is
  `exec /bin/mount.juicefs <source> <mountPath> -o <options>` and `<mountPath>` embeds the
  dataset name, so for `demo-output` the *first* `-o` substring is inside the path
  (`demo-|o|utput`) and `commands[1]` is the fragment between the path and the real `-o`
  argument (e.g. `utput/juicefs-fuse `), identical for current and latest → the real option
  change lands in `commands[2]` and is never compared. Same for `cache-partial-|o|nly`, where
  `commands[1]` ends at ` cache-partial-`.
- **Fix under review**: `strings.Cut(command, " -o ")` — cuts at the first standalone ` -o `
  argument. Verified against the actual producer of both inputs: `genFuseMount` /
  `genWorkerMount` build the command as `strings.Join([]string{"exec", mountPath, source, "-o", options}, " ")`
  (`pkg/ddc/juicefs/transform_fuse.go:375-381`, `pkg/ddc/juicefs/transform.go:220-243`), and
  the "current" side is the last line of the `script.sh` ConfigMap, which is written verbatim
  from a previously generated command (`updateWorkerScript`/`updateFuseScript`,
  `pkg/ddc/juicefs/utils.go:428-486`; chart template `charts/juicefs/templates/worker/statefulset.yaml:189`).
  So the exact `" -o "` separator is guaranteed on both sides of the comparison; a K8s object
  name cannot contain a space, so no earlier `" -o "` can occur in the path.
- **V3 lifecycle**: `syncWorkerSpec` → `isCommandChanged` false on base → `updateWorkerScript`
  never runs and the `kubectl.kubernetes.io/restartedAt` annotation is never set. On head both
  happen. (Note for reviewers: the pre-existing `TestJuiceFSxEngine_syncWorkerSpec` never
  reaches the `isCommandChanged` call — its fixtures leave the sts update strategy unset, so
  `syncWorkerSpec` returns early after switching the strategy to OnDelete. V3 is the first test
  that actually covers this path.)

## Proposed fixes (NOT applied to production here)

- The PR itself is the fix and it is verified correct at both layers. Optional hardening
  (non-blocking, not applied): parse the command as an argument list (split on whitespace,
  find the exact `-o` token) instead of the `" -o "` substring, which would also survive any
  future generator change that varies spacing.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/pr6283-juicefs-cmd-options-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git checkout <fixed-ref>
   git checkout verify/pr6283-juicefs-cmd-options-claude -- \
     docs/verification/pr6283-juicefs-cmd-options \
     pkg/ddc/juicefs/is_command_changed_verify_test.go
   ```
   or just run `bash docs/verification/pr6283-juicefs-cmd-options/scripts/re-verify.sh`
   from a checkout of this branch — it fetches the current PR head itself.
2. Prereqs: Go toolchain only (module cache warm or network for `go mod download`).
3. Re-run: `go test ./pkg/ddc/juicefs/ -run 'TestVerify' -v`.
4. Polarity: all tests are contract tests — they should be GREEN on fixed code. There are no
   canaries to invert.
5. Harness-bites: run Layer 1 once against pre-fix code (merge base 253f6bb8) to confirm it
   goes red — already recorded in `results/unit-base.out` this round.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/pr6283-juicefs-cmd-options-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6283 (issue #6282, JuiceFS
isCommandChanged "-o" splitting) produced claims P0, V1, V2, V3; a harness reproduced
them (P0/V1/V3 failed on merge-base 253f6bb8, pass on head cf975a08). The PR may have
new commits. Read docs/verification/pr6283-juicefs-cmd-options/README.md
("Continuing after the fix") and follow it: run scripts/re-verify.sh (it resolves the
current PR head itself), report the per-claim verdict table (all tests are contract
tests; no canaries), re-review the last-reviewed..head delta, advance .last-reviewed,
commit and push. Do not run cluster-wide destructive actions.
```
