# pr-6209-codeql-action-bump — verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6209
("chore(deps): bump the codeql-action group with 3 updates",
`github/codeql-action` 4.38.2 -> 4.38.3 in `.github/workflows/codeql-analysis.yml`
and `.github/workflows/sonarcloud.yaml`).

This PR touches **CI configuration only** (pinned GitHub Action SHAs). The harness is
therefore not Go unit/envtest code but two shell checks that assert the invariants a
dependency-bump PR can break. Production code is untouched.

Layers run against the **code under review** (`602efe53e8f46b22c4a2df7d3e590bafd0392e90`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (offline) | all `github/codeql-action/*` usages pinned to full-length SHAs, all sharing ONE sha (group consistency required by `.github/dependabot.yml`) | `bash docs/verification/pr-6209-codeql-action-bump/scripts/check-codeql-pin-consistency.sh` |
| 2. Integration (network) | each pinned SHA resolves to a genuine upstream release tag; the trailing `# vX.Y.Z` comment matches that tag | `bash docs/verification/pr-6209-codeql-action-bump/scripts/check-codeql-tags.sh` |

> Polarity: all checks are contract tests (assert the intended-correct behavior). RED on the
> reviewed head reproduces the finding; GREEN means fixed.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | Dependabot maintenance PR; no bug claimed. The de-facto premise: "codeql-action v4.38.3 is a genuine upstream release and works in this repo's workflows; the group must be bumped together" |
| Linked issue | none |
| Reported component | CI workflows (CodeQL analysis, SonarCloud/scorecard SARIF upload) |
| Patched component | same — `.github/workflows/codeql-analysis.yml`, `.github/workflows/sonarcloud.yaml` |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | `git ls-remote`: tag `v4.38.3` dereferences to exactly the new pinned SHA `24c5418...`; old SHA `2892aa5...` is tag `v4.38.2`. CodeQL workflow run 38626852738 executed on the PR head (event `pull_request`, head_sha `602efe53...`) with the new pins: "Initialize CodeQL" and "Perform CodeQL Analysis" both succeeded. See `results/ci-and-tag-evidence.txt`. Note: for a deps-bump the premise is verified against upstream tags + the PR head CI, not the base branch. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | v4.38.3 is genuine and the bumped group is consistent | 1+2 | Confirmed | `results/layer1-unit.head.txt` (GroupUsesSingleSHA pass), `results/layer2-integration.head.txt` (PinResolvesToUpstreamTag 3/3 pass), `results/ci-and-tag-evidence.txt` |
| F1 | Trailing version comments (`# v3.29.5`, `# v2.22.11`) do not match the pinned action version (v4.38.3) | 2 | Confirmed (preexisting, carried through this bump) | `results/layer2-integration.head.txt` — VersionCommentMatchesPinnedTag fails on all 3 usage lines: comments say v3.29.5/v2.22.11, pinned SHA is tag v4.38.3 |

## Per-finding detail

### F1 — stale `# vX.Y.Z` comments next to the SHA pins (minor, preexisting)
- Mechanism: both workflow files pin `github/codeql-action/*` by full SHA (good, and what
  OSSF Scorecard wants) with a trailing human-audit comment. The comments were left at
  `v3.29.5` / `v2.22.11` by earlier bumps and Dependabot does not rewrite a comment whose
  version no longer matches the old pin, so each bump perpetuates them. `v3.29.5` resolves
  to `51f77329...`, `v2.22.11` to `03e7845b...` — neither matches the pin, before or after
  this PR (see `results/ci-and-tag-evidence.txt`).
- Impact: anyone (or any tooling) auditing the pinned version from the comment gets a wrong
  version — the exact audit path SHA-pinning is supposed to support. No runtime effect.
- Observed vs expected: expected comment `v4.38.3` (the tag of `24c5418...`); observed
  `v3.29.5` (x2) and `v2.22.11` (x1).
- Harness-bites check: with the comments corrected to `# v4.38.3` (fix applied temporarily,
  then reverted) the layer flips all-green — `results/layer2-integration.with-comment-fix.txt`.
  Production diff is empty after the check.

## Proposed fix (NOT applied to production here)
- **F1**: set all three trailing comments to `# v4.38.3` (matching the pinned SHA's tag), in
  this PR or a follow-up. Keeping the comment truthful lets future Dependabot bumps rewrite
  it automatically again.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr-6209-codeql-action-bump-codex` (production code untouched).

1. Graft onto updated code and re-run:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/pr-6209-codeql-action-bump-codex
   git checkout <fixed-ref>
   git checkout FETCH_HEAD -- docs/verification/pr-6209-codeql-action-bump
   bash docs/verification/pr-6209-codeql-action-bump/scripts/re-verify.sh   # auto-discovers the PR head
   ```
2. Prereqs: git + jq; layer 2 needs outbound network to github.com.
3. Read results via polarity: all checks are contract tests — GREEN means fixed.
4. Harness-bites: layer 2 must be RED on the unfixed head for `VersionCommentMatchesPinnedTag`.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/pr-6209-codeql-action-bump-codex
(remote https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6209 (codeql-action 4.38.2->4.38.3 bump)
found F1: stale '# vX.Y.Z' comments beside the SHA pins. Read
docs/verification/pr-6209-codeql-action-bump/README.md and run
docs/verification/pr-6209-codeql-action-bump/scripts/re-verify.sh against the current PR
head. All checks are contract polarity: green = fixed. Layer 2 needs network to github.com.
```
