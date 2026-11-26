# Verification — PR #6290: bump ossf/scorecard-action 2.5.0 → 2.6.0

PR: https://github.com/fluid-cloudnative/fluid/pull/6290 (Dependabot)
Reviewed head: `ebedb73a3187209fe3e961c56b8f137d326f1c52` · base: `b8ff60e309c341b58a9e26b085b76e6067e6bb60`
Reviewer: codex · Round: initial · Date: 2026-11-27

## Premise (P0) — CONFIRMED

The PR claims to bump `ossf/scorecard-action` from v2.5.0 to v2.6.0 in
`.github/workflows/sonarcloud.yaml`. Verified against the upstream repo (not the
PR body): tag `v2.6.0` peels to exactly the pinned SHA `23f22cd43608bae7a1f1fa2603d7314dee9538a5`,
and the old pin `fa7117db...` is exactly v2.5.0. The action.yaml diff between the
two tags is additive-only (new optional `fail_on_publish_error` defaulting false,
two new outputs, image bump); no input this workflow passes was removed or
renamed. The docker image `ghcr.io/ossf/scorecard-action:v2.6.0` exists.
See `results/upstream-investigation.txt`.

## Claims and results

| ID | claim | polarity | layer | observed | expected | verdict |
|----|-------|----------|-------|----------|----------|---------|
| P0 | pinned SHA == peeled v2.6.0 tag; no workflow input dropped; image exists | contract | script | all PASS (`results/initial-run-head-ebedb73.txt`) | PASS | Confirmed |
| F1 | `exclude_checks: "Pinned-Dependencies"` and `exclude_paths: "vendor/**"` in the workflow's `with:` are silently ignored — not declared in action.yaml, not read anywhere in the action source (v2.5.0 **and** v2.6.0) | canary (pre-existing, non-gating) | script | INFO: `exclude_checks exclude_paths` undeclared | — | Confirmed, **pre-existing on base** (not introduced by this PR) |

## Harness-bites check

`results/harness-bite.txt`: same harness against a scratch commit pinning the OLD
v2.5.0 SHA while the comment claims v2.6.0 → `pin-integrity FAIL`, exit 1, as
expected. Production diff of this branch is empty (harness is additive only).

## How to run

```bash
# from a fluid clone containing this branch:
bash docs/verification/scorecard-action-2.6.0-bump/scripts/verify-bump.sh HEAD
# re-verify against the CURRENT PR head (any machine, resolves head from the PR URL):
bash docs/verification/scorecard-action-2.6.0-bump/scripts/re-verify.sh
```

Prereqs: bash, git, python3 (+PyYAML), curl, network access to github.com /
raw.githubusercontent.com / ghcr.io. No cluster (live layer intentionally
skipped).

## Continuing after updates

- Dependabot force-pushes a new pin → `re-verify.sh` (no args) fetches the current
  PR head and re-runs all checks; advance `.last-reviewed` afterwards.
- If a future round fixes F1 (drops the dead inputs), the `dead-inputs` canary
  flips from `exclude_checks exclude_paths` to `none` — that is the fix signal.
- If upstream ever adds `exclude_checks`/`exclude_paths` as real inputs, the
  canary likewise flips; re-evaluate whether the workflow's values are honored.

## Note for merge decision

The `Sonarcloud Scan` workflow triggers on schedule + push to master/release-*
only — it is **not** exercised by PR CI, so the green PR checks say nothing about
this workflow. All pre-merge assurance here comes from the supply-chain and
compatibility checks above. First real run: next push to master or the weekly
Saturday schedule. Watch for a green `Scorecards analysis` job there.
