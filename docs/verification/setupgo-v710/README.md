# Verification — PR #6291 (bump actions/setup-go 7.0.0 → 7.1.0) — Reviewer A (Claude)

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6291
- **Head reviewed:** `9c9216c48e3c8204e7c3b520520cbfe906ad2332` (merge-base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`)
- **Reviewer harness branch:** `verify/setupgo-v710-claude` (this branch; additive only — production diff is empty)
- **Round:** initial (2026-11-27)

## Premise verdict: **Confirmed** (claim `P0`, runs against base)

The PR (Dependabot) claims the repo pins `actions/setup-go` at v7.0.0 and should move to
v7.1.0. On the merge-base **all 6** `setup-go` usages pin
`b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0`
(`results/base-premise.out`), and `git ls-remote` confirms `b7ad1dad…` = `refs/tags/v7.0.0`
while `d8cbb804…` = `refs/tags/v7.1.0` (the latest v7 tag). v7.1.0 carries dependency
security updates (js-yaml 3.15.2, undici 6.29.1, handlebars 4.7.10, nock 15) plus additive
enhancements (go env outputs, native go-version syntax, rc/beta in go.mod). Component match
is exact: the PR touches exactly the files that hold the pins.

This is a CI-tooling dependency bump, not a functional PR — the "problem" is an outdated,
SHA-pinned action, which is real and verifiable. No linked closing issue (none needed).

## Observed vs expected

| ID | Claim | Layer | Polarity | Base | Head | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| P0 | Base pins setup-go at v7.0.0 (premise) | unit | — | **confirmed** (old pins present) | n/a | `results/base-premise.out` |
| C1 | Bump complete & behavior-preserving: every `actions/setup-go@` in `.github/` pins `d8cbb804…` (`# v7.1.0`), no stragglers, `go-version: ${{ env.GO_VERSION }}` unchanged on all 6 steps | unit | contract | FAIL (6 mismatches) | **PASS** | `results/base-premise.out`, `results/head.out` |
| C2 | Pinned SHA is the genuine `v7.1.0` tag upstream | unit (network) | contract | PASS | **PASS** | `results/head.out` (`git ls-remote`), `scripts/check_setupgo_pins.py` |

**Findings from stage ①:** none. No blocker/major/minor/nit findings — the pin is genuine,
the bump is complete (6/6 occurrences across the only 3 files referencing setup-go; no other
`setup-go` reference anywhere in the repo, no composite actions, no root `action.yml`), the
usage shape (`go-version: ${{ env.GO_VERSION }}`, no outputs consumed, no `go-version-file`,
no cache overrides) is untouched by v7.1.0's additive changes, and every workflow that uses
the action ran green on the PR head (see CI below).

## Harness-bites check

Proved three ways (production code restored after each; `git diff` empty):

1. **Differential:** the C1 contract test FAILS on merge-base `b8ff60e` (6 pin mismatches)
   and PASSES on head `9c9216c` — a red result is the reproduction of "not yet bumped".
2. **Partial-bump simulation:** reverting one occurrence (kind-e2e.yml) to the v7.0.0 pin
   makes the script exit 1 with `pin-bump FAIL` (`results/harness-bites-partial-bump.out`).
3. **Input tamper:** changing one `go-version` input to a literal makes the script exit 1
   with `go-version-input FAIL` (`results/harness-bites-goversion.out`).

## CI (the live proof; no L3 layer needed)

`gh pr view 6291 --json statusCheckRollup` on head `9c9216c`: **22/22 checks SUCCESS**,
including the three workflows this PR touches — *Project Check* (lint, staticcheck,
unittest, build on ubuntu-latest and ubuntu-24.04-arm), *E2E Backward Compatibility Check*
(5 k8s versions), *E2E Functionality Check* (kind, 5 k8s versions) — plus CodeQL,
SonarCloud, DCO, PR quota, and the `ci/circleci: build` status context. See
`results/ci-rollup.txt`. This directly demonstrates setup-go v7.1.0 installs
`GO_VERSION=1.26.11` correctly on all runner platforms used.

## How to re-run

From a checkout of this branch:

```bash
bash docs/verification/setupgo-v710/scripts/re-verify.sh            # grafts onto current PR head
bash docs/verification/setupgo-v710/scripts/re-verify.sh <sha>      # or any fixed ref
python3 docs/verification/setupgo-v710/scripts/check_setupgo_pins.py [--offline]
```

`re-verify.sh` resolves the PR head machine-independently from `verify-manifest.json`'s
`pr` URL, grafts `docs/verification/setupgo-v710/` onto it, runs the unit layer, and prints
per-finding FIXED / STILL-BROKEN verdicts (exit 0 iff all contracts pass). The
`tag-identity` check needs network; `--offline` skips it.

## Continuing after a future round

- If the PR advances (e.g. dependabot recreates at a newer version), update
  `EXPECTED_SHA`/`EXPECTED_VERSION` in `scripts/check_setupgo_pins.py`, re-run, and advance
  `.last-reviewed` (re-verify.sh prints the command).
- If a new workflow starts using `actions/setup-go`, the `pin-bump` check automatically
  covers it (it scans all of `.github/` and flags any usage not on the expected pin, plus
  strays outside `workflows/`).

## Merge checklist (acceptance criteria)

1. **Pin correctness:** every `actions/setup-go` step pins `d8cbb804…` labeled v7.1.0 —
   **passed** (`results/head.out`).
2. **Behavior preservation:** `go-version` inputs identical to base (`${{ env.GO_VERSION }}`,
   6/6) — **passed** (`results/head.out` vs `results/base-premise.out`).
3. **Tag authenticity:** pinned SHA = `refs/tags/v7.1.0` of actions/setup-go — **passed**
   (git ls-remote, `results/head.out`).
4. **Required CI:** all three touched workflows + required checks green on the PR head —
   **passed** at review time (`results/ci-rollup.txt`, 22/22 SUCCESS on `9c9216c`); re-check
   at merge time if new commits land.
