# codeql-action-4391-claude — verification of PR #6222

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6222
(`chore(deps): bump the codeql-action group with 3 updates` — github/codeql-action
4.39.0 → 4.39.1, three steps across two workflow files).

Layers run against the **code under review** (head `9fe48f12b6be95bda15628bcde448c17c53a12d3`,
base/merge-base `fe074af38434fc4b618686de6459a9173570b48b`). The harness is additive: production
code is untouched on this branch (`git diff 9fe48f12 -- . ':!docs/verification'` is empty).

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | offline structure of `.github/workflows/*`: YAML parses, all codeql-action steps pin one identical full 40-hex SHA | `python3 docs/verification/codeql-action-4391-claude/scripts/run_checks.py --layer unit` |
| 2. Integration | resolves the pinned SHA against the real `github/codeql-action` repository (`git ls-remote`): is it a release tag? is it the current v4 tip? do the trailing `# vX.Y.Z` comments match? is the sonarcloud step PR-exercised? | `python3 docs/verification/codeql-action-4391-claude/scripts/run_checks.py --layer integration` |
| 3. Live | skipped per review instructions (no live/KUBECONFIG layer); the real-env signal is GitHub CI itself — see liveNote in `verify-manifest.json` | — |

Both layers emit `go test -json`-style events, so `scripts/re-verify.sh` drives them as `gotest`
layers and prints the per-finding verdict table.

> Test polarity: contract tests (assert intended behavior) FAIL on buggy code / PASS when fixed.
> Bug-canary tests (assert current behavior) PASS now / FLIP to red when fixed.

## Problem premise (P0)

Run against the **base** branch (`fe074af3`) without the patch.

| | |
|---|---|
| Claimed symptom | Dependabot: "Bumps the codeql-action group with 3 updates … from 4.39.0 to 4.39.1" — the pinned CodeQL action is one release behind. |
| Linked issue | none (dependabot group bump; no `fixes #N`) |
| Reported component | the `github/codeql-action/*` pins in `.github/workflows/` |
| Patched component | exactly those pins (`codeql-analysis.yml`: init + analyze; `sonarcloud.yaml`: upload-sarif) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | On base, the pinned SHA `6f2ac81f` peels to tag `v4.39.0`, while `refs/heads/releases/v4` and `refs/tags/v4` point at `9b325408` (`v4.39.1`): `TestPinnedIsLatestV4Release` FAILS on base, PASSES on head — `results/integration-base.jsonl` vs `results/integration-head.jsonl` |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0-pin-current | repo pins a codeql-action release older than the current v4 tip | 2 | **Fixed by PR** (contract: fail on base → pass on head) | base pins v4.39.0 ≠ tip v4.39.1; head pins v4.39.1 == tip; `results/integration-{base,head}.jsonl` |
| AC1-pin-integrity | the new SHA `9b325408` is the peeled `v4.39.1` release tag, not a stray commit | 2 | Pass | `git ls-remote` tag map: `9b325408` ← `refs/tags/v4.39.1^{}`; also `refs/tags/v4^{}` and `refs/heads/releases/v4`; `results/integration-head.jsonl` |
| AC2-group-consistency | all three codeql-action steps (init/analyze/upload-sarif) pin the same SHA, per the dependabot.yml group contract | 1 | Pass | 3 refs, single SHA `9b325408…`; `results/unit-head.jsonl` |
| AC3-pinning-discipline | pins are full 40-hex commit SHAs; workflows still parse as YAML | 1 | Pass | `results/unit-head.jsonl` |
| F1-stale-version-comments | trailing comments say `# v3.29.5` / `# v2.22.11` while the pin is v4.39.1 | 2 | Still-present (canary passes = stale) — pre-existing, not introduced by this PR | canary output lists all three mismatches; `results/integration-head.jsonl (output events)` |
| F2-upload-sarif-not-on-prs | sonarcloud.yaml's upload-sarif step (schedule + push-to-master only) is never exercised by PR CI, so the bumped upload-sarif lands on master untested | 2 | Still-present (canary passes = gap exists) — pre-existing | triggers = `push`, `schedule`; `results/integration-head.jsonl (output events)` |

## Per-finding detail

- **P0 / AC1 / AC2 / AC3** (contract tests): all green on the PR head. The differential run on
  base (`--repo <base worktree>`) shows `TestPinnedIsLatestV4Release` red there — both the
  premise proof and the harness-bites evidence for that test.
- **F1 (minor, pre-existing)**: `codeql-analysis.yml:52` and `:72` carry `# v3.29.5`,
  `sonarcloud.yaml:57` carries `# v2.22.11`. The comment text has been stale since the repo
  pinned 3.x (last touched by #5192); every subsequent dependabot bump updates only the SHA.
  `git log -S "v3.29.5" -- .github/workflows/codeql-analysis.yml` shows a single commit.
  Cosmetic; the SHA is the source of truth. Proposed fix: a one-off commit updating the three
  comments to `# v4.39.1` (dependabot preserves comment text on SHA bumps, so it will stick).
- **F2 (minor, pre-existing)**: `sonarcloud.yaml` triggers on `schedule` (Saturdays) and `push`
  to master, never on `pull_request`, so `codeql-action/upload-sarif@v4.39.1` will first run
  after merge. The 4.39.0→4.39.1 delta for upload-sarif is small (`shouldSkipSarifUpload`
  refactor, docs) and the same action version already ran green on this PR via
  `Analyze (go)`, so this is a coverage note, not a defect.

## Harness-bites check

`results/bite-checks.txt` proves every test can go red on a corrupted copy of `.github/`:

1. one step left at the old SHA → `TestCodeqlPinsConsistent` FAILS;
2. comments corrected to `# v4.39.1` (the proposed F1 fix) → the F1 canary FLIPS to FAIL;
3. pin by short SHA → `TestPinsAreFullCommitShas` (and consistency) FAILS;
4. `pull_request` trigger added to sonarcloud.yaml → the F2 canary FLIPS to FAIL;
5. all steps pinned to a non-release commit (`b5e1ed9`) → `TestPinMatchesReleaseTag` FAILS;
6. control: restored head state → 3/3 unit and 4/4 integration green.

## Live run notes

Skipped per review instructions. The observable real-env signal for this change is GitHub CI:
the modified workflow itself ran on this PR head and `Analyze (go)` finished SUCCESS
(run 40644839222, 2026-10-28), which exercises `init` + `analyze` at the pinned v4.39.1.
The PR's red checks are `kind-e2e-test` matrix entries that timed out after 30 minutes in
`kind-e2e.yml` — a workflow with no codeql-action reference — and the same failures hit
unrelated PRs the same day; a separate PR (`ci: pin kind-e2e runner to ubuntu-24.04`, green)
addresses the runner. Not attributable to this PR.

## Proposed fixes (NOT applied to production here)

- **F1**: update the three trailing comments to `# v4.39.1` (one-off maintainer commit).
- **F2**: optionally let the sonarcloud workflow run on `pull_request` (or a manual
  `workflow_dispatch`) so the upload-sarif step is exercised before merge; at minimum, watch
  the first post-merge master run.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/codeql-action-4391-claude` of the reviewer fork
(`https://github.com/cheyang/fluid.git`), based on the PR head. Production code is untouched.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/codeql-action-4391-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/codeql-action-4391-claude
   bash docs/verification/codeql-action-4391-claude/scripts/re-verify.sh   # resolves the PR head itself
   ```
2. Prereqs: Python 3 with PyYAML, git, network access to github.com (integration layer only).
3. Re-run the two layer commands above, or just `scripts/re-verify.sh`.
4. Read results via the polarity table: contract tests (`TestPinnedIsLatestV4Release`,
   `TestPinMatchesReleaseTag`, `TestCodeqlPinsConsistent`, `TestPinsAreFullCommitShas`,
   `TestWorkflowYamlParses`) should stay GREEN. The two canaries
   (`TestVersionCommentsMatchPins`, `TestSonarcloudCodeqlNotOnPRs`) will FLIP to FAIL when
   their findings are fixed — invert them into contract tests then.
5. Harness-bites: `results/bite-checks.txt` documents how each test was proven to bite.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/codeql-action-4391-claude of
https://github.com/cheyang/fluid.git. Background: the review of
https://github.com/fluid-cloudnative/fluid/pull/6222 (codeql-action 4.39.0 -> 4.39.1 bump)
produced findings F1 (stale version comments) and F2 (upload-sarif not exercised on PRs);
the harness in docs/verification/codeql-action-4391-claude reproduces them. Read that
directory's README.md ("Continuing after the fix") and follow it: graft the harness onto the
fixed code, run scripts/re-verify.sh, mind the polarity table (invert the two canaries if
they flip), and report an observed-vs-expected table. No cluster is needed.
```
