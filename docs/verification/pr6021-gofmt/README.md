# Verification harness — PR #6021 (fluid) — gofmt violation in base_test.go

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6021
- **Head reviewed:** `7d68471e176dc5b8a55fac813888313a8f0d0a9a`
- **Base (merge-base):** `d118fbd203bbc739cb90960ae56d90fc8db275ef`
- **Reviewer:** Claude (Reviewer A)
- **Branch:** `verify/pr6021-gofmt-claude` (harness only; production diff vs PR head is empty)

## Premise (P0) — Confirmed

Issue #6023 (OPEN) asks to add comments to `TestAlluxioFileUtils_ReportSummary` in
`pkg/ddc/alluxio/operations/base_test.go`, and quotes the exact comment text. The PR adds
that text verbatim. Comments-only PR — no runtime premise to reproduce. The comment's claim
("both failure and success cases by mocking the internal exec command") matches the test
body (`ExecErr` then `ExecCommon` gomonkey patches).

## Observed vs expected

| ID | Claim | Layer | Polarity | Observed at head | Verdict |
| --- | --- | --- | --- | --- | --- |
| P0 | Issue #6023 asks for exactly these comments | unit | contract | diff adds the quoted text verbatim | **Confirmed** |
| F1 | `gofmt -l` newly flags `base_test.go` at head → CI "Code fmt check" fails | unit (deterministic tool) | contract (red = reproduced) | CI-pinned gofmt (Go 1.24.12) lists the file at head, nothing at base; real CI lint job failed at step "Code fmt check" | **Confirmed** (blocker, PR-introduced) |
| F2 (initial hypothesis) | `pkg/ddc/thin/transform_fuse.go` unformatted on base and master, making the fmt check red independent of this PR | unit | context | CI-pinned gofmt (1.24.12 / 1.25.12) lists **nothing** on base or master; only a newer gofmt (1.27) flags transform_fuse.go. Master CI is green. | **Refuted** — version artifact of a newer local gofmt, not a violation under the current CI contract |
| AC2 | comments change no behavior: `TestAlluxioFileUtils_ReportSummary` still passes | unit | contract | `ok ... 0.011s` at head | **Pass** |

## F1 mechanism

The three added lines — two comment lines and the `func TestAlluxioFileUtils_ReportSummary`
declaration — each carry a stray leading TAB, indenting a top-level declaration.
The file compiles and the test passes (Go ignores leading whitespace before top-level
decls), so `go test` and `go vet` stay green; only gofmt is not idempotent on the file.
The CI job `.github/workflows/project-check.yml` → "Code fmt check" runs exactly:

```
gofmt -l ./pkg ./cmd ./api | grep . && echo "Please run "make fmt" to verify gofmt" && exit 1 || exit 0
```

which fails whenever `gofmt -l` lists any file. Confirmed against the real CI (2026-11-06,
GitHub API): Project Check run 27689217718 at head → lint job 82040982357 → failed step
**"Code fmt check"**; the same workflow on master (run 41834068553) is green. The PR also
shows `DCO: ACTION_REQUIRED` (unsigned commit) — separate, also needs the author's action.

Fix: remove the stray tabs (i.e. `gofmt -w pkg/ddc/alluxio/operations/base_test.go`,
matching the column-0 comment style of the neighboring tests), sign the commit (DCO), and
re-run the harness — `scripts/re-verify.sh` should then print `F1: Fixed` / `ALL FIXED`.

## Version-fidelity note (why F2 was initially suspected, then refuted)

gofmt rules drift across Go minor versions. A go1.27 gofmt flags
`pkg/ddc/thin/transform_fuse.go` on master (mis-indented composite literal, upstream
commit `ea2351bd7`), which at first looked like a pre-existing reason for the fmt check to
be red on base. But CI pins GO_VERSION (1.24.12 on this PR's base; 1.25.12 on current
master), and under those gofmt versions base and master are **clean** — consistent with
master's green CI. So the only file that fails "Code fmt check" under the CI contract is
the PR's own `base_test.go`. The transform_fuse.go note remains a forward-compatibility
heads-up for whenever GO_VERSION is bumped past the formatting change (a go1.27-era gofmt
will newly flag it), recorded here for the maintainers, not as a gate on this PR.

`scripts/check-gofmt*.sh` therefore resolve the gofmt binary from the workflow's pinned
GO_VERSION (`GOTOOLCHAIN=<ver> go env GOROOT`), falling back to PATH gofmt with a warning,
so the harness reproduces the CI verdict rather than a newer gofmt's opinion.

## How to run

```bash
bash docs/verification/pr6021-gofmt/scripts/check-gofmt.sh <repo-root-at-PR-head>   # F1 contract (red at head)
bash docs/verification/pr6021-gofmt/scripts/check-gofmt-master-context.sh <root>     # F2 version-drift context
bash docs/verification/pr6021-gofmt/scripts/re-verify.sh                             # full loop, no sha needed
```

`re-verify.sh` fetches the current PR head from the manifest `pr` URL, grafts the harness
onto it, runs the unit layer (gofmt contract + the ReportSummary test) and prints
Fixed / Still-broken per finding. Exit 0 iff everything is fixed.

## Harness-bites check (done this round)

1. Applied the proposed fix (`gofmt -w base_test.go`, CI-pinned gofmt) at head → contract
   test went green, unit test still `ok`, resulting diff = exactly the 3 dedented lines
   (`results/F1-bites.txt`).
2. Reverted the fix → production diff vs PR head is empty; only `docs/verification/`
   (the harness) is added on this branch.

## Continuing after the fix

- Re-run `bash docs/verification/pr6021-gofmt/scripts/re-verify.sh` on this branch (or
  graft `docs/verification/pr6021-gofmt/` onto any checkout of the PR head). Expected:
  `F1: Fixed`, `unit: pass`, exit 0.
- F1 is a contract test — no canaries, nothing to invert.
- No live layer is needed; the claim is decided by a deterministic formatter.
- Separately remind the author about the DCO (ACTION_REQUIRED) check.

Raw evidence: `results/` (`F1-head.txt`, `F1-bites.txt`, `ci-fmt-differential.txt`,
`unit-head.txt`, `F2-context-master.txt`).
