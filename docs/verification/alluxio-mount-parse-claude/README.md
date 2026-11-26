# alluxio-mount-parse — bug verification (Reviewer A / Claude)

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6285.

Harness branch: `verify/alluxio-mount-parse-claude` (based on PR head
`2a7f2322bc62447766db96a7da02e3b0ec8c7b04`; merge base
`253f6bb84f26a906fba301550b5c81ebdd73af35`). Production code is untouched —
the diff is the harness test file plus this docs tree.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `IsMounted` / `GetMountedAlluxioPaths` parsing of `alluxio fs mount` output (gomonkey-patched `exec`) | `FLUID_UNIT_TEST=true go test -gcflags=all=-l -count=1 -run TestClaudeVerify ./pkg/ddc/alluxio/operations/` |
| 2. Integration (package-level) | full existing alluxio engine + operations suites on the PR head | `FLUID_UNIT_TEST=true go test -gcflags=all=-l -count=1 ./pkg/ddc/alluxio/...` |
| 3. Live | skipped per round policy (no KUBECONFIG layer in this debate round) | n/a |

> Test polarity: all harness tests are **contract** tests (assert intended
> behavior: short lines are skipped, no panic). They FAIL on the merge base
> (panic) and PASS on the PR head.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is
whether the problem exists today.

| | |
|---|---|
| Claimed symptom | PR body: "`IsMounted` and `GetMountedAlluxioPaths` read `fields[2]` of every line of the `alluxio fs mount` output without checking how many fields the line has. An empty output, or a line with fewer than three fields such as a blank line, makes them panic with `index out of range [2]`." |
| Linked issue | none (`fixes #N` absent; PR is a follow-up to #6284 review) |
| Reported component | `pkg/ddc/alluxio/operations/base.go` — `IsMounted` (base.go:323) and `GetMountedAlluxioPaths` (base.go:356) |
| Patched component | exactly those two functions |
| Component match | Yes |
| **Verdict** | **Confirmed** — both functions panic on base with `index out of range [2] with length 0` |
| Evidence | `results/base-unit-claude.txt` (IsMounted, panic at base.go:323), `results/base-getmountedalluxiopaths-claude.txt` (GetMountedAlluxioPaths, panic at base.go:356) |

Blast radius on base: these functions run in the Fluid controller process
(`pkg/ddc/alluxio/ufs_internal.go` calls them from the engine reconcile path
`ShouldUpdateUFS` / `mountUFS` / `processUpdatingUFS`), so a panic there is a
controller crash, not a per-command failure. The trigger requires an
`alluxio fs mount` stdout that is empty or contains a short line — not the
normal shape of the command output — so the bug is real but latent.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | premise: both functions panic on short/empty mount-table output (base) | 1 | Confirmed | `results/base-*.txt` — panic `index out of range [2] with length 0` |
| F1 alluxio-mount-parse-panic | PR guard `len(fields) < 3 → continue` fixes both call sites without changing behavior for valid lines | 1 | Fixed/Confirmed on head | `results/head-unit-claude.txt` PASS; `results/head-full-alluxio-pkg.txt` full suite ok |
| F2 alluxio-gmap-silent-skip | `GetMountedAlluxioPaths` drops short lines with no logging at all (IsMounted at least logs `fields` before skipping) — harder to debug a malformed mount table | 1 | Not-a-defect, optional improvement | code reading: base.go:353-366 has no log statement |

## Per-finding detail

### P0 / F1 (alluxio-mount-parse-panic) — mechanism

`strings.Fields("")` and `strings.Fields("foo on")` yield slices of length 0
and 2; the unguarded `fields[2]` then panics. The PR adds
`if len(fields) < 3 { continue }` before both `fields[2]` accesses
(base.go:323-329 in IsMounted, base.go:359-365 in GetMountedAlluxioPaths).
This matches the guard pattern already used by the sibling engines
(`pkg/ddc/jindofsx/operations/base.go:119`,
`pkg/ddc/jindocache/operations/base.go:101`: `len(fields) > 2 && fields[2] == ...`),
so after this PR no `fields[2]`-style parser in `pkg/` is unguarded
(checked `pkg/ddc/**`, `pkg/utils/mountinfo/**` — all others guard).

- Command (head): `FLUID_UNIT_TEST=true go test -gcflags=all=-l -count=1 -run TestClaudeVerify ./pkg/ddc/alluxio/operations/`
- Observed: PASS (both harness tests).
- Command (base, harness grafted onto merge-base worktree): same command.
- Observed: `panic: runtime error: index out of range [2] with length 0` at
  `base.go:323` (IsMounted) and `base.go:356` (GetMountedAlluxioPaths).
- Expected: contract test fails on base, passes on head → the harness bites
  and the fix is proven for both functions.

### F2 (alluxio-gmap-silent-skip) — optional improvement only

Not a correctness defect. `IsMounted` logs `parse output of isMounted` with
the parsed `fields` for every line *before* the guard, so skipped lines are
visible at log level 0 there; `GetMountedAlluxioPaths` has no logging at all
(pre-existing). A `log.V(1).Info("skipping short line", "line", line)` would
make a format change in `alluxio fs mount` output diagnosable. Non-blocking.

## Live run notes

Skipped for this round (debate policy: unit + integration layers only).

## Proposed fixes (NOT applied to production here)

- None required — the PR's guard is the fix and is verified. F2 is an
  optional logging nicety the author may take or leave.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/alluxio-mount-parse-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/alluxio-mount-parse-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/alluxio-mount-parse-claude pkg/ddc/alluxio/operations/base_shortline_verify_claude_test.go
   ```
2. Prereqs: Layer 1/2 = Go toolchain only (gomonkey needs
   `-gcflags=all=-l`, which the repo Makefile test target already sets).
3. Re-run:
   ```bash
   bash docs/verification/alluxio-mount-parse-claude/scripts/re-verify.sh
   ```
   (no ref → it fetches the current PR head from the manifest `pr` URL), or
   the two `go test` commands from the layer table.
4. Polarity: all tests are contract tests — they should be GREEN. There are
   no bug-canaries, so nothing needs inverting.
5. Harness-bites: graft the harness onto the merge base and confirm it goes
   red (panic) — see `results/base-unit-claude.txt` for the recorded red run.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/alluxio-mount-parse-claude
(remote https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6285 produced finding
alluxio-mount-parse-panic (IsMounted / GetMountedAlluxioPaths panic on short
`alluxio fs mount` output lines); a harness reproduced it on the merge base
and confirmed the fix on the PR head. Read
docs/verification/alluxio-mount-parse-claude/README.md ("Continuing after the
fix") and follow it: graft the harness onto the fixed code, re-run
scripts/re-verify.sh (all tests are contract polarity, no canaries), run the
harness-bites check against the merge base, and report an
observed-vs-expected table. Do not run cluster-wide destructive actions.
```
