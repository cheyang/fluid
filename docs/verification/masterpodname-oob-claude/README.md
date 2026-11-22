# masterpodname-oob — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6284
(head 2796b3521ad37c7fd29837ec11e5ee22dfdcef1f, merge-base 253f6bb84f26a906fba301550b5c81ebdd73af35).

The PR carries its own contract tests (four malformed-output cases appended to
`TestAlluxioFileUtils_MasterPodName` in `pkg/ddc/alluxio/operations/base_test.go`), so this
harness adds **no production or test code** — only this docs/scripts/results tree. The
harness re-runs the PR's tests and re-derives the premise evidence against the base branch.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `MasterPodName` parsing guards via gomonkey-patched `exec` | `bash scripts/re-verify.sh` (auto-fetches current PR head) |
| 2. Integration | not needed — the finding is pure string-parsing logic; the unit layer decides it exactly | — |
| 3. Live | skipped in this debate setup (unit + integration only) | — |

> Test polarity: F1 is a **contract** test (asserts intended behavior). It FAILED (panicked)
> on base and PASSES on the PR head. No bug-canaries in this harness.

Unit tests MUST run with `-gcflags="all=-N -l"` (gomonkey binary patching; the repo's own
Makefile `make test` does exactly this).

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "the function panics with `index out of range` instead of returning the error it was written to return" when `alluxio fsadmin report` succeeds but its output does not have the master address on the second line |
| Linked issue | none (author states they found it by code reading; no closing reference) |
| Reported component | `AlluxioFileUtils.MasterPodName()` in `pkg/ddc/alluxio/operations/base.go`, called from `generateDataBackupValueFile` in `pkg/ddc/alluxio/backup_data.go` |
| Patched component | same function, same file |
| Component match | Yes |
| **Verdict** | **Confirmed** (real problem on base) |
| Evidence | `results/premise-base-panic.txt` — base + PR's test cases → `panic: runtime error: index out of range [1] with length 1` at `base.go:487` (`str[1]`); `results/premise-base-panic-data2.txt` — two-field second line → `index out of range [2] with length 2` (`data[2]`) |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | panic exists on base without the patch | 1 (base) | Confirmed | `results/premise-base-panic.txt` |
| F1 | malformed fsadmin output → error, not panic | 1 | Fixed by PR (contract green at head) | `results/head-unit-pass.txt` |

## Per-finding detail

**F1 — MasterPodName index-out-of-range (pre-existing, fixed by this PR).**
Mechanism on base: `strings.Split(stdout, "\n")` never returns an empty slice, so the
`len(str) < 1` guard is dead code and `str[1]` panics whenever stdout has no newline;
`strings.Fields(str[1])` guarded only by `len(data) < 2` while the code indexes `data[2]`,
so a second line with exactly two fields (e.g. `    Master Address:` with no value) also
panics. The PR tightens the guards to `len(str) < 2` and `len(data) < 3` — exactly the
lengths actually indexed. No input that previously worked becomes an error: any output
rejected by the new guards would have panicked on base. The whole
`pkg/ddc/alluxio/operations` package is green at head.

Commands:

```bash
# premise (base) + harness-bites — expect panic, exit 0 with "PREMISE CONFIRMED"
bash docs/verification/masterpodname-oob-claude/scripts/premise-check.sh

# contract tests on current PR head — expect PASS
bash docs/verification/masterpodname-oob-claude/scripts/re-verify.sh
```

## Harness-bites check

`premise-check.sh` is the bites check: it runs the PR's own contract tests against the
base code (= fix absent) and they go red with the very panic under review. The same tests
are green at head. Both directions of the polarity are demonstrated.

## Proposed fixes (NOT applied to production here)

- **F1**: none — the PR's fix is correct and complete for both panic sites. Optional polish:
  include the offending stdout in the "get wrong result" error message for diagnosability.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/masterpodname-oob-claude` (pushed to the reviewer's
fork `https://github.com/cheyang/fluid.git`), production code untouched.

1. Get it onto the code to check:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/masterpodname-oob-claude
   git checkout <ref-to-check>
   git checkout FETCH_HEAD -- docs/verification/masterpodname-oob-claude
   ```
2. Prereqs: Go toolchain only (Go ≥ 1.26 per go.mod); tests must run with
   `-gcflags="all=-N -l"`.
3. Re-run: `bash docs/verification/masterpodname-oob-claude/scripts/re-verify.sh`
   (no argument = current PR head; pass a ref to check a specific commit).
4. Read results: F1 contract tests green = still fixed. If a new round changes the parsing
   shape (renamed function, different line/field offsets), treat it as `Harness-update`
   and adjust the test cases.
5. Harness-bites: `bash scripts/premise-check.sh` against the merge-base.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/masterpodname-oob-claude (fork
https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6284 found premise P0 confirmed and F1
(index-out-of-range in AlluxioFileUtils.MasterPodName) fixed by the PR. Read
docs/verification/masterpodname-oob-claude/README.md ("Continuing after the fix") and
follow it: run scripts/re-verify.sh against the current PR head, run
scripts/premise-check.sh for the bites check, report an observed-vs-expected table with
Fixed / Still-broken / Harness-update per finding. Unit layers only in this setup.
```
