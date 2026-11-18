# Verification harness — sklearn 1.4.0 → 1.5.0 bump in /samples/vineyard (PR #6260)

Reviewer: Claude (reviewer A). Branch: `verify/sklearn-150-bump-claude`, based on
PR head `e782cc7d3ea2c69c123851cc0b24e78b5414cfac`. **Production code untouched** —
this branch only adds this harness.

## Premise (P0) — verdict: Confirmed

The PR is a routine dependabot minor-version refresh of the sample's hash-locked
scikit-learn pin. The premise (pin outdated, bump applicable) holds: base pins
`scikit-learn==1.4.0`; 1.5.0 is published, its metadata is compatible with every
other pin (numpy 1.26.4 ≥ 1.19.5, scipy 1.15.3 ≥ 1.6.0, joblib 1.3.2 ≥ 1.2.0,
threadpoolctl 3.8.0 ≥ 3.1.0, python:3.10 within `>=3.9`), and the sample's
sklearn call surface (`LinearRegression`, `train_test_split`,
`mean_squared_error`) is unchanged.

## Observed vs expected

| id | claim | layer | observed | expected | verdict |
| --- | --- | --- | --- | --- | --- |
| P0 | premise: pin outdated, bump applicable | unit (PyPI metadata) | 1.4.0 pinned on base; 1.5.0 published & compatible | same | **Confirmed** |
| H1 | 21 hashes in the lockfile are real, complete, platform-covering | unit | all 21 match PyPI digests; set equality with published files; cp310 manylinux x86_64 + aarch64 present | same | **Confirmed (no defect)** |
| R1 | pinned set co-installs; sample sklearn path behaves identically under 1.5.0 | integration | venv built from the exact pins; MSE `0.00010391514254914635` and split fingerprint `8834482113319c51085a61d6858c3182` identical under 1.4.0 and 1.5.0 | same | **Confirmed (no regression)** |
| F1 | header regeneration recipe still pins `scikit-learn==1.4.0` | unit | `HEADER_RECIPE_CONTRACT: FAIL` on head, PASS on base | recipe should say 1.5.0 | **Confirmed (minor, introduced by PR)** |

Raw evidence in `results/`.

## Harness-bites check

`results/hash-check-mutated-bites.txt`: removing the cp310 manylinux x86_64 hash
from a mutated copy of the lockfile makes `check_lockfile_hashes.py` fail
(COMPLETENESS + COVERAGE), exit 1 — the hash check can actually detect a bad
bump, it is not a vacuous pass. The F1 check likewise flips PASS→FAIL between
base and head, which is the differential that proves it bites.

## How to run

```bash
# unit layer (network: pypi.org JSON API only)
bash scripts/run-unit-layer.sh samples/vineyard/requirements.txt

# integration layer (downloads wheels, builds venv, ~2-4 min)
bash scripts/run-integration-layer.sh samples/vineyard/requirements.txt

# everything, against the current PR head, with per-finding verdicts
bash scripts/re-verify.sh
```

`scripts/re-verify.sh` resolves the PR head from `verify-manifest.json.pr`
(machine-independent `git fetch <repo-url> pull/6260/head`), grafts this harness
onto it, runs both layers, and prints FIXED / STILL-BROKEN / PARTIAL /
HARNESS-UPDATE per finding. It exits non-zero while F1 is open.

## Proposed fix for F1

In `samples/vineyard/requirements.txt`, update the header recipe line

```
printf "pandas\nrequests\nnumpy\nvineyard\nscikit-learn==1.4.0\njoblib==1.3.2\n" \
```

to `scikit-learn==1.5.0` (one word). Optional hardening: drop the explicit
version from the recipe and let it float (`scikit-learn`), or add the header
sync check to a CI job so future dependabot bumps can't miss it.

## Continuing after the fix

1. `git fetch https://github.com/cheyang/fluid.git verify/sklearn-150-bump-claude && git checkout verify/sklearn-150-bump-claude`
2. `bash docs/verification/sklearn-150-bump-claude/scripts/re-verify.sh`
3. Expected after the F1 fix: all three contract checks green, exit 0.
   If a future bump regresses hash coverage or the sample path, H1/R1 go red
   and re-verify reports STILL-BROKEN.
4. Polarity table: all checks here are **contract** tests (green = correct).
   There are no bug-canaries in this harness.

Kickoff prompt for a fresh agent: "Checkout branch verify/sklearn-150-bump-claude
of cheyang/fluid, read docs/verification/sklearn-150-bump-claude/README.md, run
scripts/re-verify.sh, and report the per-finding verdicts."

## Environment notes

- Run on 2026-11-18, linux x86_64, python 3.11.6 (wheels selected for cp311;
  the sample itself builds cp310 wheels in Docker — both tags' hashes are
  verified present by the unit layer).
- Network: pypi.org JSON API + files.pythonhosted.org. `pip download` was broken
  on this host (vendored-urllib3 bug + mirror timeouts), so wheels are fetched
  with curl and sha256-verified by `build_pinned_env.sh` before install.
