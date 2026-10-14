# scorecard-action-2-5-0-bump-codex — verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6207
(dependabot: bump `ossf/scorecard-action` from 2.4.4 to 2.5.0 in
`.github/workflows/sonarcloud.yaml`), head `838b73be0602b3b10ebcb82b5a54a417191324f7`,
base `e4a10bf49a1e8068d901e6f146ee4e435db7e785`.

This PR changes no product code — it re-pins a third-party GitHub Action. The review
question is therefore not "is the diff's logic right" but "is this bump well-formed and
compatible": correct pin, real upstream release, unchanged input contract, runnable image.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Deterministic | pin==tag (C1), input compatibility (C2) | `bash scripts/check-action-bump.sh <ref>` |
| 2. Integration | real GHCR image manifest (C3), real publish endpoint (C4) | same script |
| 3. Live | actual GitHub Actions run | intentionally skipped; impossible pre-merge (workflow triggers on schedule/push to master only) |

> Polarity: every check is a CONTRACT test — it must PASS on a good bump. A red check
> means the bump is broken. Harness-bites proof: against the base ref, C1 fails
> (`results/harness-bites-base-e4a10bf.txt`), so the harness is not vacuously green.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | None (dependency hygiene PR; release notes quoted in the PR body) |
| Linked issue | None (no `fixes #N`) |
| Reported component | `.github/workflows/sonarcloud.yaml` Scorecards analysis job |
| Patched component | same |
| Component match | Yes |
| **Verdict** | **Confirmed** — the v2.5.0 release exists upstream, the pin matches it, and the bump is input-compatible. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| C1 | pinned SHA == peeled commit of `refs/tags/v2.5.0` | 1 | Confirmed | both `fa7117db83c52d787ae5a89f350999701505d78e`; results/check-action-bump-head-838b73be.txt |
| C2 | `results_file`, `results_format: sarif`, `publish_results: true` still honored at new pin | 1 | Confirmed | action.yaml input set identical between v2.4.4 and v2.5.0; results/… |
| C3 | `ghcr.io/ossf/scorecard-action:v2.5.0` manifest exists | 2 | Confirmed | HTTP 200; results/… |
| C4 | publish endpoint `https://api.scorecard.dev` alive | 2 | Confirmed | HTTP 200; results/… |
| F1 | `exclude_checks`/`exclude_paths` are dead inputs | 1 | Confirmed (pre-existing) | no EXCLUDE handling in action source at either tag |

## Per-finding detail

- **C1**: `git ls-remote https://github.com/ossf/scorecard-action.git refs/tags/v2.5.0^{}`
  returns `fa7117db83c52d787ae5a89f350999701505d78e`, exactly the SHA the workflow pins.
  The v2.4.4 pin at base (`2d1146689b8cda280b9bc96326124645441f03bc`) equally matches
  `refs/tags/v2.4.4^{}`. The bump is an authentic tag-to-tag move.
- **C2**: `action.yaml` at both pins declares the identical input set
  (`results_file`, `results_format`, `repo_token`, `publish_results`, `file_mode`,
  `internal_publish_base_url`, `internal_default_token`); the only input-contract change
  is the additive `intoto` value for `results_format`. Source diff v2.4.4→v2.5.0 in
  `options/`, `main.go`, `github/`, `internal/scorecard/` shows: removal of an
  auth-token debug print (security fix), removal of an unused `SetTransport` helper,
  additive in-toto formatting. Nothing alters sarif output or publish behavior used here.
- **C3**: anonymous GHCR token + manifest GET → 200.
- **C4**: `GET https://api.scorecard.dev/` → 200 (post-migration OpenSSF infra).
- **F1 (pre-existing, not introduced by this PR)**: the workflow passes
  `exclude_checks: "Pinned-Dependencies"` and `exclude_paths: "vendor/**"`, but neither
  action version reads them (no `EXCLUDE` anywhere in the action's Go source at either
  tag). Those exclusions have never taken effect; the bump preserves the status quo.

## Proposed fixes (NOT applied)

- **F1**: drop the two dead `with:` entries, or open an upstream issue asking for real
  exclude support. Out of scope for this dependabot PR.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/scorecard-action-2-5-0-bump-codex` (production code
untouched — the only diff vs the PR head is this directory).

```bash
git fetch https://github.com/cheyang/fluid.git verify/scorecard-action-2-5-0-bump-codex
git checkout verify/scorecard-action-2-5-0-bump-codex
bash docs/verification/scorecard-action-2-5-0-bump-codex/scripts/re-verify.sh   # auto-fetches current PR head
```

Exit 0 iff every contract check passes. To advance the round marker after review:
`git rev-parse <new-head> > docs/verification/scorecard-action-2-5-0-bump-codex/.last-reviewed`
and commit.

### Kickoff prompt for a fresh agent
```text
Re-verify the scorecard-action bump harness for fluid PR #6207. Check out branch
verify/scorecard-action-2-5-0-bump-codex from https://github.com/cheyang/fluid.git, read
docs/verification/scorecard-action-2-5-0-bump-codex/README.md, then run
docs/verification/scorecard-action-2-5-0-bump-codex/scripts/re-verify.sh (it fetches the
current PR head itself). All checks are contract polarity: PASS = bump still well-formed.
Also review the printed .last-reviewed..head delta if the PR received new commits.
```
