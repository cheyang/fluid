# Verification — PR #6222: chore(deps): bump the codeql-action group (4.39.0 -> 4.39.1)

Reviewed head: `9fe48f12b6be95bda15628bcde448c17c53a12d3` (base `fe074af38434fc4b618686de6459a9173570b48b`).
Harness: additive only — no production code touched. Branch `verify/pr-6222-codeql-action-bump-codex`.

## Premise (P0): Confirmed

The PR pins `github/codeql-action/{init,analyze,upload-sarif}` to
`9b3254085494b55a0f7c4c2b3d2d52cc712ce370` claiming it is v4.39.1 (from v4.39.0).
Verified against the upstream repo: annotated tag `v4.39.1` (tag object
`8498dc4c2b27cdd99baf0f87955b29ebbf30c78d`) dereferences to exactly that commit;
the old pin `6f2ac81f...` dereferences to `v4.39.0`. The 4.39.1 delta is the
default CodeQL bundle bump 2.27.2 -> 2.27.3 (github/codeql-action#4256) plus
internal fixes — no user-facing breaking change (the 4.39.0 minimum-bundle
breaking change was already in the base). The CodeQL workflow itself ran green
on the PR head (`Analyze (go)` pass, 8m24s).

## Observed vs expected

| claim | layer | head result | base result | verdict |
| --- | --- | --- | --- | --- |
| P0 pins are full 40-hex SHAs, one SHA across the group | unit (L1) | pass | pass | confirmed |
| P0 pins resolve to upstream release tags | integration (L2) | pass (v4.39.1) | pass (v4.39.0) | confirmed |
| F1 trailing `# vX.Y.Z` comment matches pinned tag | integration (L2) | **fail** (comment v3.29.5/v2.22.11 vs actual v4.39.1) | **fail** (vs actual v4.39.0) | pre-existing, non-blocking |

## How to run

- L1 (offline):  `bash docs/verification/pr-6222-codeql-action-bump/scripts/check-codeql-pin-consistency.sh`
- L2 (network):  `bash docs/verification/pr-6222-codeql-action-bump/scripts/check-codeql-tags.sh`
- Full re-verify against a future head: `bash docs/verification/pr-6222-codeql-action-bump/scripts/re-verify.sh`

## CI attribution

`kind-e2e-test` failed on all 5 k8s versions on the PR run; the failure mode is
a FUSE mount timeout (`timeout waiting for FUSE mount point to be ready`,
`jindo-demo-warmup` pod) inside `kind-e2e.yml`, a workflow this PR does not
touch — the diff is limited to `codeql-analysis.yml` and `sonarcloud.yaml`.
All checks that actually exercise the changed files (Analyze (go), CodeQL,
SonarCloud) pass. Attribution: infrastructure/flaky, not a PR regression.
See `results/e2e-failure-attribution.txt`.

## Polarity note

`VersionCommentMatchesPinnedTag` is a contract test that is red on both base and
head; it goes green when the comments are updated to the pinned tag. It is
**not** a merge blocker for this PR (Dependabot cannot fix the comment; the
mismatch predates this PR).

## Continuing after the fix

The harness is self-contained under `docs/verification/pr-6222-codeql-action-bump/`.
On another machine: fetch this branch, check it out, run the two scripts above
(or `re-verify.sh` with no args to auto-resolve the current PR head). L2 needs
network access to github.com only; no cluster, no Go toolchain required.
