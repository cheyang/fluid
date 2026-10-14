# Verification — PR #6210: chore(deps) bump actions/upload-artifact 7.0.1 → 7.0.2

Reviewer: codex (stage ② of the review pipeline). Harness only — **no production code
changed**; the branch diff vs the PR head is entirely this directory.

- PR: https://github.com/fluid-cloudnative/fluid/pull/6210
- Head reviewed: `c29b9a04b0838f06549701089334f059b3695087` (base `e4a10bf49a1e8068d901e6f146ee4e435db7e785`)
- Diff under review: one `uses:` line in each of
  `.github/workflows/backward-compatibility-e2e.yml`, `.github/workflows/kind-e2e.yml`,
  `.github/workflows/sonarcloud.yaml`:
  `actions/upload-artifact@043fb46d… # v7.0.1` → `actions/upload-artifact@cf430e03… # v7.0.2`

## Premise (P0) — confirmed

Dependabot PR; no fluid runtime bug is claimed. The honest risk of such a PR is a
malformed bump: pin that is not the real upstream release, a partial bump leaving stale
pins, an action interface change that drops inputs the workflows use, or broken YAML.

- Base pins `043fb46d…` = upstream `refs/tags/v7.0.1` → the repo genuinely is one patch
  behind (see `results/tag-verification.txt`).
- Upstream `refs/tags/v7.0.2` = `cf430e030ddbb5b0abf93d22962f4752f3646cd9` exists, and
  `git diff v7.0.1..v7.0.2` upstream is exactly the advertised change
  (`@actions/artifact` 6.2.0→6.3.1 for HTTP-429 download retries; `action.yml`
  unchanged — see `results/action-diff-stat.txt`).
- Reported component (the three workflow upload steps) == patched component. Match.

## Hypotheses / contract checks

| ID | claim (contract: must hold on the PR head) | layer |
|----|--------------------------------------------|-------|
| A1 | pinned SHA == upstream `refs/tags/v7.0.2` commit; trailing `# v7.0.2` comment agrees | integration (live github.com) |
| A2 | every `actions/upload-artifact@` pin under `.github/` is the new SHA; exactly 3 call sites | unit |
| A3 | every `with:` input used (name, path, retention-days) still declared in `action.yml` at the pin | integration (live github.com) |
| A4 | the three changed workflow files still parse as YAML | unit |

## How to run

```bash
# everything (unit + integration; needs https to github.com, no cluster)
bash docs/verification/upload-artifact-7-0-2-bump-codex/scripts/run-checks.sh all

# one line re-verification against the current PR head (grafts this harness onto it):
bash docs/verification/upload-artifact-7-0-2-bump-codex/scripts/re-verify.sh

# informational: did the PR's own CI execute the bumped pin? (needs gh)
bash docs/verification/upload-artifact-7-0-2-bump-codex/scripts/check-pr-ci.sh
```

`run-checks.sh` prints go-test-JSON events (`{"Test":…,"Action":"pass|fail"}`) so the
stock `re-verify.sh` `gotest` parser consumes them unmodified; narrative detail lands in
`results/checks-run.log`.

## Observed vs expected (head c29b9a04)

| ID | expected | observed | verdict |
|----|----------|----------|---------|
| A1 | pin == `cf430e03…` (tag v7.0.2) in all 3 files | pass (results/reverify/integration.out) | holds |
| A2 | 3 pins, all == new SHA | pass (results/reverify/unit.out) | holds |
| A3 | name/path/retention-days declared in action.yml@pin | pass | holds |
| A4 | 3 workflows parse | pass | holds |
| CI | PR CI exercises the new pin | kind-e2e + backward-compat `Dump environment` steps succeeded on all 10 matrix legs (results/pr-ci-evidence.txt); `tide` pending = merge automation awaiting labels, not a failure | holds |
| — | sonarcloud.yaml pin exercised pre-merge | **not exercised** (workflow triggers on schedule/push only); first real run is post-merge | residual, low risk |

## Harness-bites proof

Mutation tests on the PR head (each reverted afterwards, worktree restored clean):

| mutation | expected | observed |
|----------|----------|----------|
| one pin reverted to the v7.0.1 SHA | A1 + A2 fail | A1 fail, A2 fail |
| bogus `with:` input added (`bogus-input: 1`) | A3 fail | A3 fail |
| YAML broken (`with: [`) | A4 fail | A4 fail |

## Live layer (L3)

Skipped per debate setup. Nearest real-environment signal is the PR's own
pull_request CI (above). Post-merge, watch the next scheduled **Sonarcloud Scan** run's
"Upload artifact" step — that workflow is the only one whose new pin was not exercised
pre-merge.

## Continuing after a fix / next round

```bash
git fetch https://github.com/cheyang/fluid.git verify/upload-artifact-7-0-2-bump-codex
git checkout verify/upload-artifact-7-0-2-bump-codex
bash docs/verification/upload-artifact-7-0-2-bump-codex/scripts/re-verify.sh   # auto-fetches current PR head
```

All four checks are **contract** polarity: green = bump still well-formed; a red check on
a new head is a real regression in the bump. No canaries to invert. Advance
`.last-reviewed` as instructed by the re-verify output.

Kickoff prompt for a fresh agent: "Continue verifying
https://github.com/fluid-cloudnative/fluid/pull/6210 from branch
verify/upload-artifact-7-0-2-bump-codex on https://github.com/cheyang/fluid.git — run
docs/verification/upload-artifact-7-0-2-bump-codex/scripts/re-verify.sh and review the
printed delta."
