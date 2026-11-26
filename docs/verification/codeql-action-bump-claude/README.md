# Verification — PR #6289 codeql-action group bump (Reviewer A / Claude)

PR: https://github.com/fluid-cloudnative/fluid/pull/6289
Head: `bb4e3096ec1ddab2df5ff9afbb57c2a665203d8c` · Base: `b8ff60e309c341b58a9e26b085b76e6067e6bb60`
Harness branch: `verify/codeql-action-bump-claude` (production code untouched — harness only).

## Premise (P0): CONFIRMED

Dependabot claims: bump the codeql-action group (init / analyze / upload-sarif)
from 4.39.3 to 4.39.4. Verified against the **base** branch:

- base pins `33d7b85da1a04171e211befe2c739303b9a4c066` = peeled tag `v4.39.3`
- head pins `1bd86f2fdb4b86187fa4059f16f31a3202acc9ea` = peeled tag `v4.39.4`
  (both via `git ls-remote https://github.com/github/codeql-action`)

Chore/deps PR — the "problem" is routine dependency maintenance of the repo's
pinned security-scanning action; premise holds, component matches (the PR
touches exactly the three codeql-action steps in `.github/`).

## Observed vs expected

| ID | Severity | Claim | Observed (head) | Expected | Verdict |
| --- | --- | --- | --- | --- | --- |
| P0 | — | base=v4.39.3, head=v4.39.4 | both resolve as claimed | — | **Confirmed** |
| A1 | — | pin = official v4.39.4 commit, consistent across all 3 steps | 1 SHA, official tag | — | **Pass** |
| A2 | — | workflows still valid YAML | both parse | — | **Pass** |
| F1-stale-version-comments | nit (pre-existing) | `# v3.29.5` / `# v2.22.11` comments state the pinned version | comments say v3.29.5 / v2.22.11, actual v4.39.4 (all 3 lines) | comments = v4.39.4 | **Reproduced** (also on base → pre-existing) |
| F2-upload-sarif-not-exercised-on-pr | minor (pre-existing) | upload-sarif step covered by PR CI | sonarcloud.yaml triggers = push+schedule only; workflow did not run on the PR | — | **Confirmed** (design property, not a defect) |

Evidence: `results/unit-head.log`, `results/harness-bites.log`,
`results/ci-evidence.txt`, `results/tag-map.cache.json` (immutable sha→tag map).

## Integration signal (external CI)

The repo's CodeQL workflow (pull_request trigger) ran on the PR head and
passed, directly exercising `init@1bd86f2` + `analyze@1bd86f2`. `upload-sarif`
is not exercised pre-merge (F2). Captured via `gh run list --commit` and
`gh api .../check-runs`: all checks green (CodeQL, unittest, e2e, lint, …).
See `results/ci-evidence.txt`.

## Harness-bites check (Step 4)

Applied the proposed F1 fix (refresh trailing comments to `# v4.39.4`) to the
working tree → `F1-version-comment-accuracy` went green, suite exit 0. Reverted;
production diff confirmed empty (`results/harness-bites.log`).

## How to run

```bash
bash docs/verification/codeql-action-bump-claude/scripts/re-verify.sh   # auto-resolves current PR head
# or against an explicit ref:
bash docs/verification/codeql-action-bump-claude/scripts/re-verify.sh <sha>
```

Deterministic checks (no Go code in scope): python3 only. First run resolves
codeql-action tags via `git ls-remote` and caches them; later runs work offline.
Exit 0 iff all *gating* findings are fixed; F1/F2 are non-gating (nit/minor)
and reported without failing the run.

## Continuing after the fix

- If the author refreshes the trailing comments to `# v4.39.4` (or drops them),
  `F1-version-comment-accuracy` goes green on the next `re-verify.sh` run.
- If someone later adds a `pull_request` trigger to sonarcloud.yaml, the F2
  canary flips to fail — invert it then (PR coverage exists → gap closed).
- If the action is bumped again (e.g. to v4.39.5+), update
  `PR_EXPECTED` in `scripts/check_actions_pins.py` (`{"from": ..., "to": ...}`)
  — a mismatch shows up as `P0-premise-*` failures, i.e. Harness-update, not a
  silent pass.

### Kickoff prompt (fresh agent, other machine)

> You are resuming a PR review verification. Clone
> https://github.com/fluid-cloudnative/fluid, fetch branch
> `verify/codeql-action-bump-claude` from https://github.com/cheyang/fluid.git
> (the reviewer fork), check it out, then run
> `bash docs/verification/codeql-action-bump-claude/scripts/re-verify.sh`
> (no sha — it fetches the current head of PR #6289). Report the per-finding
> table it prints, review the `last-reviewed..head` delta it reports, and after
> the round advance `.last-reviewed` as the script instructs. Do not modify
> production code; the harness is additive only.
