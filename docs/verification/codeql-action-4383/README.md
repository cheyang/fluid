# Verification harness — codeql-action 4.38.2 → 4.38.3 pin bump (PR #6209)

Reviewer: **Claude (Reviewer A)** · Branch: `verify/codeql-action-4383-claude` (based on the PR head)
PR: https://github.com/fluid-cloudnative/fluid/pull/6209
Head reviewed: `602efe53e8f46b22c4a2df7d3e590bafd0392e90` · Base: `e4a10bf49a1e8068d901e6f146ee4e435db7e785`
Production diff of this branch vs the PR head: **empty** (harness only).

## Premise (P0) — verdict: CONFIRMED

The PR is a Dependabot group-update pin bump (a CI-maintenance change; there is no
runtime symptom to reproduce, the verifiable claim is the bump's integrity):

- Claim: "Bumps the codeql-action group with 3 updates … from 4.38.2 to 4.38.3".
- On base `e4a10bf`, all three pins (`init`, `analyze` in `codeql-analysis.yml`;
  `upload-sarif` in `sonarcloud.yaml`) point at `2892aa5e…`, which resolves (peeled
  `git ls-remote`, cross-checked against a fetched bare clone of the tags) to the
  **exact** commit of `refs/tags/v4.38.2`.
- On head `602efe53`, all three pins point at `24c54180…`, the exact commit of
  `refs/tags/v4.38.3`.
- The v4.38.3 `CHANGELOG.md` at the tag matches the release notes quoted in the PR
  body (default bundle 2.27.2, per-language-bundle decision fix, CodeQL ≤ 2.21.2
  deprecation warning — not applicable here, fluid uses the default bundle and pins
  no CodeQL version).

So the PR does exactly what it claims: a genuine, minimal, correctly-attributed
dependency pin update. No supply-chain anomaly (both SHAs are real release-tag
commits of `github/codeql-action`, verified against the upstream repo, not the PR
body).

## Observed vs expected

| # | claim | test (layer) | expected on head | observed | verdict |
| --- | --- | --- | --- | --- | --- |
| P0 | pins are genuine 4.38.2 → 4.38.3 release-tag commits | `TestPinSHAsResolveToExactReleaseTags` (unit) | pass | pass — all 3 pins = v4.38.3 peeled tag commit | Confirmed |
| F1 | `# vX` pin comments match the pinned version | `TestPinCommentMatchesResolvedTag` (unit, contract) | fail (defect present) | fail — comments say `v3.29.5`/`v2.22.11`, pins resolve to `v4.38.3` | Confirmed (pre-existing, nit) |
| — | diff touches only the 3 pin lines | `TestPRDiffTouchesOnlyCodeqlActionPins` (unit) | pass | pass | — |
| — | workflows remain valid YAML | `TestWorkflowFilesParseAsYaml` (unit) | pass | pass | — |

CI on the PR head (fetched 2026-10-15): all checks pass, including the CodeQL
workflow itself (which runs on `pull_request` and therefore exercised the new
`init`/`analyze` pins end-to-end). `sonarcloud.yaml` is schedule/push-triggered
only, so its `upload-sarif` pin is first exercised post-merge by the weekly cron —
see `liveNote` in the manifest.

## F1 — stale version comments (the only finding)

All three pins carry comments from the v3-era (`# v3.29.5`, `# v2.22.11`) that have
not tracked the actual pinned version since the repo switched to Dependabot *group*
updates (last accurate at PR #5192, v3.29.5). Every subsequent group bump —
including this one — updated the SHA but not the comment. Pre-existing, not
introduced by this PR; severity nit. Suggested fix while touching these lines:
update the comments to `# v4.38.3`.

## Harness-bites check

`results/harness-bites.txt`: on the unfixed head `TestPinCommentMatchesResolvedTag`
fails; with the proposed fix (comments corrected to `# v4.38.3`) applied in the
working tree it passes; after `git checkout --` revert it fails again and the
production diff is empty. (This check also caught and fixed a double-`v`
normalization bug in the test itself — the version the bites step exists for.)

## How to run

```bash
bash docs/verification/codeql-action-4383/scripts/run_unit_checks.sh
# emits go-test-JSON on stdout; human-readable detail on stderr
```

Requirements: git, python3 + PyYAML, network (one `git ls-remote` against
github.com/github/codeql-action per run — cached in `results/codeql-action-tags.tsv`).

Re-verify against the current PR head (or any ref):

```bash
bash docs/verification/codeql-action-4383/scripts/re-verify.sh            # auto-resolves PR head
bash docs/verification/codeql-action-4383/scripts/re-verify.sh <fixed-ref>
```

Interpretation: `F1` is a **contract** test — red means the stale-comment defect is
still present, green means fixed. The other three checks are green guards: they
fail if a future pin stops resolving to a genuine release-tag commit, if the PR
diff grows beyond pin lines, or if a workflow file stops parsing.

## Continuing after the fix

- Graft this directory onto the fixed code (re-verify.sh does this automatically via
  `harnessPaths`) and re-run; all four tests should be green.
- If the author instead decides the stale comments are acceptable (Dependabot will
  keep overwriting them on each group bump anyway), record a maintainer `decision`
  and close F1 as `deferred` — do not delete the test; it stays as the pin-hygiene
  guard.

## Kickoff prompt (fresh agent, from this branch alone)

> You are re-verifying PR https://github.com/fluid-cloudnative/fluid/pull/6209
> (codeql-action pin bump). Check out branch `verify/codeql-action-4383-claude`,
> run `bash docs/verification/codeql-action-4383/scripts/re-verify.sh` (no
> arguments — it fetches the current PR head), and report per-finding
> Fixed / Still-broken per the README's interpretation rules. F1 is a contract
> test; the other three checks are regression guards. Do not publish anything to
> GitHub.
