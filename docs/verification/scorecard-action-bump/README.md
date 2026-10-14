# Verification — scorecard-action bump (fluid PR #6207)

Reviewer: Claude (debate pipeline, round 1, mode `initial`).
PR: https://github.com/fluid-cloudnative/fluid/pull/6207 — one-line bump of
`ossf/scorecard-action` in `.github/workflows/sonarcloud.yaml`
(`2d114668… # v2.4.4` → `fa7117db… # v2.5.0`).
Head: `838b73be0602b3b10ebcb82b5a54a417191324f7`, base: `e4a10bf49a1e8068d901e6f146ee4e435db7e785`.

## Premise verdict (claim P0) — Confirmed

The repo pinned `ossf/scorecard-action` at v2.4.4 and v2.5.0 exists upstream:

```
$ git ls-remote --tags https://github.com/ossf/scorecard-action.git | grep -E 'v2\.(4\.4|5\.0)'
55891bbd…  refs/tags/v2.4.4
2d114668…  refs/tags/v2.4.4^{}   <- matches the base workflow pin
46dc3187…  refs/tags/v2.5.0
fa7117db…  refs/tags/v2.5.0^{}   <- matches the PR head pin
```

## Layer used

L1 only (deterministic static analysis). The changed workflow triggers on
`schedule` (weekly Sat 01:30 UTC) and `push` to `master`/`release-*` only — it
never runs in PR CI, so no PR check can exercise it. Live (L3) was skipped per
the debate setup (unit + integration only).

## How to run

From any checkout of the PR head with this tree grafted:

```bash
# main harness (network: github.com for ls-remote + raw action.yaml)
python3 docs/verification/scorecard-action-bump/scripts/check_action_pin.py \
  --workflow .github/workflows/sonarcloud.yaml \
  --prev-pin 2d1146689b8cda280b9bc96326124645441f03bc \
  --report docs/verification/scorecard-action-bump/results/l1-report.json

# harness-bites proof (mutations must make it fail)
bash docs/verification/scorecard-action-bump/scripts/bite-check.sh

# one-shot re-verify against the current PR head (auto-resolves via manifest.pr)
bash docs/verification/scorecard-action-bump/scripts/re-verify.sh
```

## Observed vs expected

| check | claim | polarity | observed @ 838b73be |
| --- | --- | --- | --- |
| C1 | workflow YAML parses | contract | PASS |
| C2 | pin SHA = upstream v2.5.0 tag commit | contract | PASS |
| C3 | all `with:` inputs declared by the action | canary (WARN) | WARN: `exclude_checks`, `exclude_paths` undeclared → **finding F1 (pre-existing, minor)** |
| C4 | all required action inputs supplied | contract | PASS |
| C5 | no used input dropped v2.4.4→v2.5.0 | contract | PASS (input sets identical) |
| C6 | action docker image tag = pin version | contract | PASS |

Bite evidence (`results/l1-bite-check.txt`): wrong pin → C2 FAIL; removed
required input → C4 FAIL; `--strict-inputs` → C3 FAIL; fixture repo that drops
inputs across a bump → C5+C6 FAIL. All four mutations flipped the intended
checks, so the harness bites.

## Findings

- **F1 (minor, pre-existing, not introduced by this PR)** — the workflow's
  `exclude_checks: "Pinned-Dependencies"` and `exclude_paths: "vendor/**"`
  inputs are silently ignored: scorecard-action declares and reads neither
  `INPUT_EXCLUDE_CHECKS` nor `INPUT_EXCLUDE_PATHS` (checked
  `options/env.go` + `action.yaml` at both the v2.4.4 and v2.5.0 pins).
  GitHub still injects them as env vars, but the container never consumes
  them, so the Pinned-Dependencies exclusion the workflow asks for never
  happens. Suggested follow-up (separate PR): drop the two inert inputs or
  move to a supported mechanism.

## Continuing after a fix / next round

- `scripts/re-verify.sh [<ref>]` grafts this tree onto the current PR head,
  resolves the previous action pin from the merge-base, and re-runs C1–C6.
- C3 is a canary: it stays WARN while the inert inputs remain and must be
  inverted/promoted when they are removed or upstream declares them.
- C2/C5 are the checks that matter for *future* scorecard-action bumps on
  this workflow: a pin that stops matching its tag comment, or an input the
  new version drops, turns them red.
- Prerequisites: python3 + PyYAML, git, network to github.com.
- Kickoff prompt for a fresh agent:
  > You are resuming a review verification for fluid PR #6207. Check out
  > branch `verify/scorecard-action-bump-claude`, read
  > `docs/verification/scorecard-action-bump/README.md` and
  > `verify-manifest.json`, then run
  > `bash docs/verification/scorecard-action-bump/scripts/re-verify.sh` and
  > report the per-check table plus any new findings on the
  > `last-reviewed..head` delta.
