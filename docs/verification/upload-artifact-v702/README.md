# Verification — upload-artifact-v702 (PR fluid-cloudnative/fluid#6210)

Reviewer: **Claude (Reviewer A)** · Round: initial (2026-10-15) ·
Head: `c29b9a04b0838f06549701089334f059b3695087` · Base: `e4a10bf49a1e8068d901e6f146ee4e435db7e785`

## Premise (P0) — Confirmed

The PR is a Dependabot pin-bump of `actions/upload-artifact` v7.0.1 → v7.0.2 in
the 3 workflows that use it. Premise for a dependency bump = "the target release
exists upstream and differs from the current pin". Confirmed on the **base**
branch: tag `v7.0.2` resolves to `cf430e030ddb…` and tag `v7.0.1` to
`043fb46d1a93…` — a real, distinct release. The PR's comment text (release notes)
matches upstream PR actions/upload-artifact#822. Evidence:
`results/base-premise.out`.

## Observed vs expected

| finding | claim | verdict | evidence |
| --- | --- | --- | --- |
| C1 pin-matches-tag | every pinned SHA is exactly the commit its `# vX.Y.Z` comment names | **PASS** (3/3 files, head) | `results/head.out` — cf430e030ddb == tag v7.0.2 commit |
| C2 version-consistency | all references in the repo pin one version | **PASS** | only 3 usages exist, all v7.0.2; repo-wide grep finds none elsewhere |
| C3 yaml-parse | the 3 touched workflows parse as YAML | **PASS** | `results/head.out` |
| C4 input-compat | `with:` keys (`name`, `path`, `retention-days`) are declared inputs of the pinned version | **PASS** | action.yml fetched per tag; v7.0.1 vs v7.0.2 action.yml is byte-identical |

No findings survived verification: **0 blockers, 0 majors.** The bump is genuine,
complete, and interface-compatible.

Harness-bites check (Step 4): three corruptions (lookalike SHA, broken YAML,
mixed versions) each turned the matching check red — `results/harness-bites.out`.
Production code on this branch is untouched (`git diff c29b9a04 -- .github/` is empty).

## End-to-end evidence (no live layer needed)

GitHub CI on the PR head ran the workflows that **execute** the new pinned action
(the e2e "Dump environment" upload steps run `if: ${{ !cancelled() }}`, so they ran
on success too; SonarCloud uploads SARIF): every check in the rollup is SUCCESS.
That is the live-layer substitute for a CI-only change.

## How to run

```bash
bash docs/verification/upload-artifact-v702/scripts/re-verify.sh            # current PR head
bash docs/verification/upload-artifact-v702/scripts/re-verify.sh <some-ref> # any ref
```

Requires: git, `gh` (authenticated, for the actions/upload-artifact API reads),
PyYAML. The script is read-only (`git show <ref>:<path>`); it never mutates the tree.

## Continuing after the fix / next round

Nothing to fix. For future rounds (e.g. the next Dependabot bump of this action),
`re-verify.sh` re-runs C1–C4 against the new head automatically; C1 is the
supply-chain guard worth keeping for every future pin bump in this repo.
Polarity: all checks are **contract** tests — they must PASS; there are no canaries.

Kickoff prompt for a fresh agent:
> You are resuming a PR review of https://github.com/fluid-cloudnative/fluid/pull/6210.
> Checkout branch verify/upload-artifact-v702-claude, run
> bash docs/verification/upload-artifact-v702/scripts/re-verify.sh, read the
> README table, and re-review the last-reviewed..head delta (marker:
> docs/verification/upload-artifact-v702/.last-reviewed).
