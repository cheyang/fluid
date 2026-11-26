# Verification — PR #6291: chore(deps): bump actions/setup-go from 7.0.0 to 7.1.0

Reviewer: codex (debate pipeline, Reviewer B). Round: initial.
Head reviewed: `9c9216c48e3c8204e7c3b520520cbfe906ad2332` (base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`).

## Premise (P0) — verdict: CONFIRMED

Claim (Dependabot): the repo pins `actions/setup-go` v7.0.0 and this PR moves all
pins to upstream release v7.1.0, whose release notes list only additive
enhancements and dependency updates.

- Base state confirmed: all 6 `uses: actions/setup-go@...` sites on
  `b8ff60e` pin `b7ad1dad…` with `# v7.0.0` comments
  (`results/premise-base-pins.txt`).
- Both tag targets verified against the canonical repo
  (`results/ls-remote-tags.txt`): `v7.0.0` → `b7ad1dad…`, `v7.1.0` →
  `d8cbb8045d7f43706b9177cceb36c1f91a6644d5` — the exact SHAs the PR swaps.
- Upstream contract check (`results/action-yml-diff.txt`): between the two
  pinned commits, `action.yml` adds outputs only; no input removed, no default
  changed, runner stays `node24`. The workflows only set `go-version: 1.26.11`,
  so nothing they consume changed.

No linked issue; no `fixes #N`. Component match is trivially exact (CI
workflows only). This is a CI-only supply-chain bump; the reviewable risk is
pin authenticity, completeness, and upstream behavior change — all covered
below.

## What is verified and how

| check | claim | layer | how to run | observed vs expected |
|---|---|---|---|---|
| T1_PinIsFullSHA | every `uses: actions/setup-go` is a 40-hex SHA pin | unit (hermetic) | `bash scripts/verify-pins.sh` | head: pass (expected pass); base: pass |
| T2_PinsConsistent | exactly 6 pins, all one SHA | unit (hermetic) | same | head: pass; base: pass |
| T3_PinMatchesClaimedTag | pinned SHA == canonical `v7.1.0` tag target | unit (live `git ls-remote`, baked-in fallback) | same | head: pass (expected pass); base: **fail** (old pin) |
| T4_WorkflowsParse | 3 modified workflows still parse as YAML | unit (hermetic) | same | head: pass; base: pass |
| T5_VersionCommentAccurate | every pin line carries `# v7.1.0` | unit (hermetic) | same | head: pass; base: **fail** |
| T6_UpstreamContractAdditive | action.yml diff is additive-only (inputs/defaults/runner unchanged) | unit (hermetic, checked-in reference copies) | same | head: pass; base: pass |
| T7_OldPinFullyReplaced | no `b7ad1dad…` left anywhere under `.github/` | unit (hermetic) | same | head: pass; base: **fail** |

Harness-bites control: `results/unit-layer-base.txt` (base) vs
`results/unit-layer-head.txt` (head). The version-sensitive contracts
(T3/T5/T7) fail on base and pass on head; the invariants (T1/T2/T4/T6) pass on
both, confirming the base was consistent and the PR is a clean version advance,
not a fix for a mixed-pin state.

Integration evidence (not part of the hermetic loop):

- CI on the reviewed head: all 22 checks pass, including every job of the three
  modified workflows (`backward-compat-test` ×5, `kind-e2e-test` ×5,
  `staticcheck`/`lint`/`build`/`unittest`) — see `results/pr-checks-head.txt`.
  All three modified workflows trigger on `pull_request`, so GitHub executed
  the *modified* workflow definitions: setup-go v7.1.0 installed Go 1.26.11 in
  each of those jobs. This is the "does the bumped action actually work in our
  CI" check, exercised by the PR itself.
- Live/KUBECONFIG layer: intentionally skipped (CI-only change; nothing to run
  on a cluster).

## Findings

None. No defect, regression risk, validation gap, or missing test was found.
The verdict carried into the review ledger is COMMENT-level (no blockers, no
majors).

## Re-running / continuing after a rebase

From a checkout of this branch (`verify/pr-6291-setup-go-7-1-0-codex`):

```bash
bash docs/verification/pr-6291-setup-go-7-1-0/scripts/re-verify.sh
# resolves the current PR head from manifest.pr, grafts this harness onto it,
# runs the unit layer, prints per-check FIXED / STILL-BROKEN / HARNESS-UPDATE.
```

Polarity: every check is a **contract** test — pass = the contract holds. There
are no canaries in this harness (no bug was found to canary).

If Dependabot recreates the PR for a newer setup-go (e.g. v7.1.1), T2/T3/T5/T7
will report STILL-BROKEN against the expected v7.1.0 constants — that is the
harness doing its job: re-authenticate the new pin
(`git ls-remote https://github.com/actions/setup-go.git 'refs/tags/v7.1.1'`),
update `EXPECTED_NEW_SHA` / `EXPECTED_NEW_COMMENT` in `scripts/verify-pins.sh`
and the reference `action-v7.1.0.yml`, then re-run.

Machine prerequisites: bash, git, python3 (pyyaml preferred; structural
fallbacks included), network access for T3's live tag check (baked-in
constants used offline), `jq` for `re-verify.sh`.

Copy-paste kickoff for a fresh agent on another machine:

> Check out `verify/pr-6291-setup-go-7-1-0-codex` from
> https://github.com/cheyang/fluid.git, then run
> `bash docs/verification/pr-6291-setup-go-7-1-0/scripts/re-verify.sh`.
> All 7 contract checks must print FIXED against the current PR head.
