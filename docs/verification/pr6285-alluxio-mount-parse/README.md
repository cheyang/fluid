# PR #6285 — alluxio mount-table short-line parsing — bug verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6285
(head `2a7f2322bc62447766db96a7da02e3b0ec8c7b04`, merge-base `253f6bb84f26a906fba301550b5c81ebdd73af35`).

Layers run against the code under review:

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `AlluxioFileUtils.IsMounted` / `GetMountedAlluxioPaths` parsing of `alluxio fs mount` output (exec mocked via gomonkey, the package's established pattern) | `go test -gcflags="all=-N -l" ./pkg/ddc/alluxio/operations/ -run 'TestVerifyPR6285' -v` |
| 2. Package sweep (callers) | the whole `pkg/ddc/alluxio/...` tree that calls the two functions (`mountUFS`, `ShouldMountUFS`, `CheckAndUpdateUFSMountStatus`, `FindUnmountedUFS`) | `go test -gcflags="all=-N -l" ./pkg/ddc/alluxio/...` |
| 3. Live | not run (debate phase runs unit + integration only) | — |

A true envtest integration layer is not applicable here: the changed input is the
stdout of an in-pod `exec` (`pods/exec` SPDY stream), which envtest's API server
cannot serve (no kubelet). The only seam below the functions is the kube API exec
channel, so the gomonkey mock at the `exec` boundary is the deepest layer that can
decide these claims without a live cluster.

> Test polarity: all harness tests are CONTRACT tests — they assert the intended
> behavior (no panic; short/blank lines skipped; well-formed lines parsed as before).
> They FAIL (panic) on the merge-base and PASS on the PR head.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "An empty output, or a line with fewer than three fields such as a blank line, makes them [`IsMounted` and `GetMountedAlluxioPaths`] panic with `index out of range [2]`." (PR body) |
| Linked issue | none (`NONE` in PR body; follow-up to review of #6284) |
| Reported component | `pkg/ddc/alluxio/operations/base.go` — `IsMounted`, `GetMountedAlluxioPaths` |
| Patched component | same file, same two functions |
| Component match | Yes |
| **Verdict** | **Confirmed** — real problem on the base branch |
| Evidence | `results/base-p0-premise-ismounted.txt` (`panic: runtime error: index out of range [2] with length 0` at `base.go:323`), `results/base-p0-premise-getmountedpaths.txt` (panic at `base.go:356`) |

Supporting check: the PR body also claims stdout is trimmed in
`kubeclient.ExecWithOptions`, so a normal trailing newline does not trigger this.
Verified: `ExecWithOptions` returns `strings.TrimSpace(stdout)` when
`PreserveWhitespace` is false (the default used by the alluxio `exec` wrapper).
The trigger is therefore an empty exec stdout or an interior blank/short line, both
plausible during master restarts or transient exec hiccups that still exit 0.

Impact chain: `IsMounted` is called from `AlluxioEngine.mountUFS` /
`ShouldMountUFS`, and `GetMountedAlluxioPaths` from `CheckAndUpdateUFSMountStatus` /
`FindUnmountedUFS` (`pkg/ddc/alluxio/ufs_internal.go`), inside dataset reconcile.
The repo sets no `RecoverPanic` (controller-runtime v0.17.5, recovery opt-in), so
the panic crashes the dataset-controller process — a crash-loop, not a returned
error.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Empty/short-line mount output panics `IsMounted`/`GetMountedAlluxioPaths` on base | 1 | **Confirmed** | panics at `base.go:323` / `base.go:356`; `results/base-p0-premise-*.txt` |
| F1 | PR head: `IsMounted` skips lines with <3 fields, no panic, correct true/false | 1 | **Fixed at head** | `results/head-harness.txt` |
| F2 | PR head: `GetMountedAlluxioPaths` skips lines with <3 fields, no panic, correct list | 1 | **Fixed at head** | `results/head-harness.txt` |
| F3 | Well-formed output parses identically before/after (no behavior change) | 1 | **Confirmed identical** | `results/differential.txt` (existing `TestAlluxioFileUtils_IsMounted`, `TestAlluxioFileUtils_FindUnmountedAlluxioPaths` pass on both base and head) |
| F4 | Callers' package tree still green at head | 2 | **Confirmed** | `results/head-package-sweep.txt` |
| — | Author's own new test | 1 | Passes at head | `results/head-author-test.txt` |

## Harness-bites check (step 4)

The contract tests were run on the merge-base three ways:

1. base, no fix → **panic** (`results/base-p0-premise-*.txt`) — the tests exercise the bug.
2. base + the PR's two guards applied temporarily → **all pass**
   (`results/base-harness-bites.txt`) — the tests go green exactly when the fix is
   applied, so they are red for the right reason.
3. fix reverted; base worktree left untouched (`git status` clean).

Production code is untouched on this branch; the only additions are
`pkg/ddc/alluxio/operations/verify_pr6285_mountparse_test.go` and this
`docs/verification/` tree.

## Per-finding detail

### F1/F2 — short-line guards (contract tests)

`TestVerifyPR6285_IsMountedShortLines` and
`TestVerifyPR6285_GetMountedAlluxioPathsShortLines` cover 8 output shapes:
empty, whitespace-only, one-token line, two-field line, blank line between
entries, short line before a matching entry, well-formed table, and no-match.
Independently written from the author's `TestAlluxioFileUtils_MountOutputWithShortLines`
(which adds the first direct `GetMountedAlluxioPaths` coverage in the package —
before this PR only `IsMounted`/`FindUnmountedAlluxioPaths` had direct tests).

### F3 — no behavior change for well-formed output

Same pre-existing tests pass identically on base and head
(`results/differential.txt`); the guard only adds `continue` for lines that
previously panicked.

## Proposed fixes (NOT applied to production here)

None needed — the PR's fix is correct and matches the sibling engines'
established pattern (`jindocache`/`jindofsx` `IsMounted` already guard with
`len(fields) > 2`).

Adjacent, out of this PR's scope: `MasterPodName` (same file) still has the same
class of panic on master (`str[1]` after a dead `len(str) < 1` check, `data[2]`
after `len(data) < 2`); it is being fixed separately by PR #6284 (open). The two
PRs touch disjoint hunks of `base.go`/`base_test.go` and merge in either order.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr6285-alluxio-mount-parse-codex` (production code
untouched), so it grafts onto whatever the fixed code is.

```bash
# from a checkout of this branch:
bash docs/verification/pr6285-alluxio-mount-parse/scripts/re-verify.sh
# fetches the current PR head via manifest.pr, grafts the harness, runs the unit
# layer, and prints per finding Fixed / Still-broken / Harness-update.
```

Kickoff prompt for a fresh agent: "Check out branch
`verify/pr6285-alluxio-mount-parse-codex`, read
`docs/verification/pr6285-alluxio-mount-parse/README.md`, then run its
`scripts/re-verify.sh` against the current head of
https://github.com/fluid-cloudnative/fluid/pull/6285 and report the per-finding
table. All tests are contract tests: green = fixed."

Prereqs: go >= 1.26 (module directive), network only if the toolchain module
cache is cold (repo is vendored). gomonkey needs inlining disabled — keep
`-gcflags="all=-N -l"` (repo Makefile's unit-test flags).
