# Verification — scorecard-action v2.5.0 → v2.6.0 (PR #6290)

Reviewer: **Reviewer A (Claude)**, branch `verify/scorecard-action-v260-claude`.
PR: https://github.com/fluid-cloudnative/fluid/pull/6290
Reviewed head: `ebedb73a3187209fe3e961c56b8f137d326f1c52` · base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`

## Premise (P0): CONFIRMED

The PR is a one-line Dependabot bump of the SHA-pinned
`ossf/scorecard-action` in `.github/workflows/sonarcloud.yaml`
(`fa7117db83c…` # v2.5.0 → `23f22cd43608…` # v2.6.0). For a dependency bump the
premise is "the new release exists and is consumable":

- the `v2.6.0` tag dereferences to exactly the new pinned commit
  (`23f22cd43608bae7a1f1fa2603d7314dee9538a5`), and the old pin equals the
  `v2.5.0` tag commit — Dependabot's pin is genuine, not a guessed SHA;
- `ghcr.io/ossf/scorecard-action:v2.6.0` is published and contains a
  `linux/amd64` manifest (the workflow runs on `ubuntu-22.04`);
- every `with:` key the workflow passes is accepted by v2.6.0's `action.yaml`
  except the two that were already dead in v2.5.0 (F1 below);
- component match is exact: the PR touches the only scorecard-action usage in
  the repo.

Evidence: `results/differential-upstream.txt`, `results/unit.out`.

## Findings

| ID | severity | verdict | one-line evidence |
| --- | --- | --- | --- |
| F1-exclude-inputs-dead (pre-existing) | minor | **Still-present** (canary passes) | `exclude_checks` / `exclude_paths` are not declared inputs of the action in v2.5.0 *or* v2.6.0; GitHub drops them ("Unexpected input(s)") and the container never reads them → Pinned-Dependencies is not excluded and `vendor/**` is still scanned |
| F2-publish-activation (introduced by this bump) | minor | **Confirmed activation** (contract passes) | v2.5.0 `options.go`: `PublishResults bool` had no env tag → `publish_results: true` was silently dead; v2.6.0 adds `env:"INPUT_PUBLISH_RESULTS"` (upstream #1731) → scheduled runs will now actually publish results to api.scorecard.dev |

Neither is blocking: F1 reproduces on the merge-base (pre-existing, park it /
file a tracking issue), and F2 matches the workflow's own declared intent
(`publish_results: true` plus `id-token: write` "Used to receive a badge"), and
v2.6.0 keeps publish failures as non-fatal warnings by default
(`fail_on_publish_error: false`).

## Harness (L1, deterministic, network read-only)

`scripts/run-checks.sh` reads the workflow from the **current checkout** and
the upstream action sources at the pinned commit, and emits `go test -json`
events so the stock `scripts/re-verify.sh` parses it. Tests:

- `PinMatchesTag` (contract) — pin == tag's dereferenced commit.
- `ImageManifestAvailable` (contract) — ghcr image tag exists, linux/amd64 present.
- `PublishResultsEffective` (contract, F2) — pinned version reads
  `INPUT_PUBLISH_RESULTS` and the workflow sets `publish_results: true`.
- `ExcludeInputsIgnored` (canary, F1) — passes while the dead keys remain;
  flips to FAIL when they're removed (or upstream declares them) = fixed.

Run: `bash docs/verification/scorecard-action-v260/scripts/run-checks.sh`
(requires curl, python3, network to api.github.com / ghcr.io).

## Observed vs expected

| test | observed @ head | expected | note |
| --- | --- | --- | --- |
| PinMatchesTag | PASS | PASS | pin == v2.6.0 tag commit |
| ImageManifestAvailable | PASS | PASS | OCI index, linux/amd64 |
| PublishResultsEffective | PASS | PASS | documents F2 activation |
| ExcludeInputsIgnored | PASS (canary = still present) | documents F1 | pre-existing |

## Harness-bites check

`results/harness-bites.txt`: tampered pin → `PinMatchesTag` FAIL; removing the
exclude keys (F1 fix simulated) → canary flips to FAIL; `publish_results:
false` (opt-out simulated) → `PublishResultsEffective` FAIL. Production code
untouched — the repo diff on this branch is only `docs/verification/**`.

## Continuing after the fix

From any checkout of this branch: `bash
docs/verification/scorecard-action-v260/scripts/re-verify.sh` (fetches the
current PR head, grafts the harness, re-runs L1, prints per-finding
Fixed/Still-broken). L3 (skipped this round per policy): watch the first
post-merge run of the workflow on master — see `liveNote` in the manifest.

Polarity after a fix lands: `ExcludeInputsIgnored` (canary) is fixed only when
it **flips to FAIL** — then invert it (assert the keys are gone/declared) or
promote it to a contract test. `PublishResultsEffective` is a contract: it
stays green while publishing is wired; if the maintainers opt out of
publishing, invert it to assert opted-out.

## Merge checklist (acceptance)

1. Pin integrity — PASS (`PinMatchesTag`).
2. Image consumable on the workflow's runner — PASS (`ImageManifestAvailable`).
3. Inputs accepted by v2.6.0 — PASS for all live inputs; the two dead ones are
   pre-existing (F1), not a merge condition.
4. Publish activation (F2) acknowledged by a maintainer — decision recorded in
   the round ledger (non-blocking, aligned with the workflow's stated intent).
5. Required CI on the PR stays green (standard fluid checks; the bumped
   workflow itself only runs post-merge via push/schedule).
6. Post-merge watch: first "Sonarcloud Scan" run on master must succeed and now
   publishes to api.scorecard.dev (warnings, not failures, if the API rejects).
