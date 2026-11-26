# Verification — PR #6289 — chore(deps): bump the codeql-action group with 3 updates

Reviewer: Codex (Reviewer B). Round: initial.
Head: `bb4e3096ec1ddab2df5ff9afbb57c2a665203d8c` · Base: `b8ff60e309c341b58a9e26b085b76e6067e6bb60`

## Premise (P0)

Dependabot claims: `github/codeql-action/init`, `/analyze`, `/upload-sarif` are bumped
**4.39.3 → 4.39.4**. This is a CI-only dependency bump (2 workflow files, 3 `uses:` lines).

**Verdict: Confirmed.** `git ls-remote https://github.com/github/codeql-action.git`:

| tag | peeled commit | role in PR |
|---|---|---|
| `v4.39.3^{}` | `33d7b85da1a04171e211befe2c739303b9a4c066` | old pin (replaced) |
| `v4.39.4^{}` | `1bd86f2fdb4b86187fa4059f16f31a3202acc9ea` | new pin (matches diff exactly) |

Component match: the patch touches exactly the workflows that run CodeQL/SonarCloud
upload. No product code involved.

## Findings → harness map

| ID | severity | claim | test | polarity | layer | result @ head |
|---|---|---|---|---|---|---|
| F1 | nit (pre-existing) | trailing version comments say `v3.29.5`/`v2.22.11` while the pin is v4.39.4 (was equally stale at base) | `C1_StaleVersionComment` | canary | unit | PASS = still present |
| G1 | — guard | every codeql-action use is pinned to a full 40-hex SHA | `T1_PinIsFullSHA` | contract | unit | pass |
| G2 | — guard | all codeql-action pins share one SHA (no drift) | `T2_PinsConsistent` | contract | unit | pass |
| G3 | — guard | pinned SHA == peeled commit of upstream v4.39.4 | `T3_PinMatchesClaimedTag` | contract | unit | pass |
| G4 | — guard | both modified workflows parse as YAML | `T4_WorkflowsParse` | contract | unit | pass |

G1–G4 are acceptance guards, not defects: they must stay green on every future revision.

## How to run

```bash
# from a checkout of the code under test (PR head or later revision):
bash docs/verification/pr-6289-codeql-action-bump/scripts/verify-pins.sh
# full re-verify against the current PR head (fetches it itself):
bash docs/verification/pr-6289-codeql-action-bump/scripts/re-verify.sh
```

`T3` uses the network (`git ls-remote`); offline it falls back to a baked-in peel
captured 2026-11-27 and says so on stderr.

## Observed vs expected

| run | T1 | T2 | T3 | T4 | C1(canary) |
|---|---|---|---|---|---|
| PR head (`bb4e309`) | pass | pass | pass | pass | pass (stale comment present) |
| base (`b8ff60e`, expected) | pass | pass | **fail** (pins v4.39.3) | pass | pass |
| bite-1: one file reverted to old SHA | pass | **fail** | pass | pass | pass |
| bite-2: comments corrected to v4.39.4 | pass | pass | pass | pass | **fail** (canary flips = F1 fixed) |
| bite-3: both files at base state | pass | pass | **fail** | pass | pass |

Harness-bites checks 1–3 confirm each contract/canary exercises the claimed path.
Known harness weakness: with mixed pins, T3 alone sees only the lexicographically
first SHA; T2 is the mixed-pin detector (see bite-1).

Raw output: `results/unit-layer-head.txt`, `results/unit-layer-base.txt`,
`results/bite-{1,2,3}-*.txt`. CI: all 21 checks green incl. `Analyze (go)`,
`CodeQL`, `SonarCloud` (`gh pr checks 6289`, 2026-11-27).

## Continuing after a revision

1. `git fetch https://github.com/cheyang/fluid.git verify/pr-6289-codeql-action-bump-codex && git checkout <that>`
2. `bash docs/verification/pr-6289-codeql-action-bump/scripts/re-verify.sh` — resolves
   the current PR head from `manifest.pr`, grafts this harness onto it, re-runs.
   - F1 fixed ⇔ `C1` reports `FIXED(flipped)`; then invert C1 into a contract test.
   - Any G* going red = revision regression in the workflow pins.
3. Advance `.last-reviewed` to the new head and push the branch back to the fork.

Kickoff prompt for a fresh agent: "Continue verifying fluid-cloudnative/fluid PR #6289
from branch verify/pr-6289-codeql-action-bump-codex on https://github.com/cheyang/fluid.git;
run docs/verification/pr-6289-codeql-action-bump/scripts/re-verify.sh and review the
.last-reviewed..head delta."
