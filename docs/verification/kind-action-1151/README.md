# kind-action-1151 — verification harness

Reproducible evidence for the review of [PR fluid-cloudnative/fluid#6208](https://github.com/fluid-cloudnative/fluid/pull/6208)
(bump `helm/kind-action` v1.15.0 → v1.15.1, pinned SHA, in `kind-e2e.yml` and
`backward-compatibility-e2e.yml`).

Production code is untouched on this branch — the diff against the PR head is
this harness only.

| Layer | What it exercises | How to run (from repo root) |
|-------|-------------------|------------------------------|
| 1. Unit | pin integrity, tag identity, input compatibility, explicit overrides, YAML validity | `python3 -I docs/verification/kind-action-1151/scripts/verify_action_pin.py --report` |
| 2. Integration | actual workflow check-run conclusions on the PR head (the workflows ARE the integration surface for a CI-action bump) | `python3 -I docs/verification/kind-action-1151/scripts/check_ci.py --report` |
| Harness-bites | negative controls proving layer 1 detects what it claims | `bash docs/verification/kind-action-1151/scripts/run-bites.sh` |

All checks are **contract** tests (assert the intended-correct state; they FAIL on
a wrong/missing bump). No bug-canaries in this harness.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "Bumps helm/kind-action from 1.15.0 to 1.15.1" — routine Dependabot CI-dependency maintenance |
| Linked issue | none (mentioned #170/#162 are the *upstream action's* PRs quoted in the release notes, not fluid issues) |
| Reported component | the `helm/kind-action` pin in the two e2e workflows |
| Patched component | same — `.github/workflows/{kind-e2e,backward-compatibility-e2e}.yml`, `uses:` line only |
| Component match | Yes |
| **Verdict** | **Confirmed** (deps-maintenance PR: v1.15.1 exists, tag → `1544676c…`, base pinned v1.15.0 `06c1ae10…`) |
| Evidence | `results/unit-run.log`, `results/bites-check.log` |

## Summary of results (head `7e1edc06`)

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| F1 | Both kind-action steps pin the same 40-hex SHA equal to the upstream v1.15.1 tag commit | 1 | Confirmed (pass) | `results/unit-run.log` |
| F2 | Every `with:` input used by the workflows exists in v1.15.1's action.yml (`version`, `node_image`, `cluster_name`, `kubectl_version`) | 1 | Confirmed (pass) | `results/unit-run.log` |
| F3 | No workflow relies on inputs whose defaults changed v1.15.0→v1.15.1 (`kubectl_version` v1.37.0→v1.37.1, `version` v0.33.0 unchanged-but-explicit, cloud-provider-kind 0.6.0→0.12.0 only under `cloud_provider: true`); both steps set `version` + `kubectl_version` explicitly | 1 | Confirmed (pass) | `results/unit-run.log` |
| F4 | Both edited workflows still parse as YAML | 1 | Confirmed (pass) | `results/unit-run.log` |
| F5 | The bumped action actually creates clusters: all 10 matrix jobs (kind-e2e-test + backward-compat-test × v1.33.2/v1.30.13/v1.28.15/v1.24.17/v1.22.17) SUCCESS on the PR head | 2 | Confirmed (pass) | `results/integration-run.log` |

## Per-finding detail

- **F1 (pin integrity / supply chain).** `gh api repos/helm/kind-action/git/ref/tags/v1.15.1`
  → commit `1544676c07bb3570fcb70166beaa07c4528fd4aa` — exactly the SHA the PR pins
  in both workflows. The base branch pinned `06c1ae10762d3b9c1644e7fe69596ae519e015a2` = v1.15.0.
  The action-side diff v1.15.0→v1.15.1 (main.sh/kind.sh) is: new optional
  `version_file` input, `DEFAULT_KUBECTL_VERSION` v1.37.0→v1.37.1,
  `DEFAULT_CLOUD_PROVIDER_KIND_VERSION` 0.6.0→0.12.0. `runs: node24` unchanged.
- **F2 (input compatibility).** action.yml at the pinned SHA declares all inputs the
  workflows pass. Verified by the harness against the live action.yml.
- **F3 (default-change leakage).** Both workflows pin `version: v0.31.0` (kind) and
  `kubectl_version: ${{ matrix.kubernetes-version }}`, so neither changed default can
  alter behavior. `cloud_provider` is not enabled.
- **F4 (YAML validity).** `yaml.safe_load` on all workflow files.
- **F5 (integration).** The check-run conclusions for head `7e1edc06`: 5/5
  kind-e2e-test and 5/5 backward-compat-test matrix entries SUCCESS (plus
  build/unittest/lint/staticcheck/CodeQL green; `ci/circleci: build` and `tide`
  had no conclusion at capture time).

## Live run notes

L3 is the GitHub Actions run itself; instead of a local approximation, F5 queries
the real check-run conclusions for the PR head. No local cluster is needed or used.

## Proposed fixes

None — no defect found. One pre-existing nit (stale comment on the `version:` line
of `kind-e2e.yml`, says "bump to v0.29.0" while the pin is v0.31.0) is noted in the
findings doc as an optional improvement; it is not introduced by this PR.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/kind-action-1151-claude` (production code
untouched), so it grafts onto whatever the future PR head is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/kind-action-1151-claude
   git checkout verify/kind-action-1151-claude -- docs/verification/kind-action-1151
   ```
   or just run `bash docs/verification/kind-action-1151/scripts/re-verify.sh`
   from a checkout of the harness branch — it fetches the current PR head from
   the manifest `pr` URL, grafts the harness, and runs both layers.
2. Prereqs: Python 3 with PyYAML; network (GitHub API + raw.githubusercontent);
   `gh` optional (falls back to unauthenticated API).
3. Re-run the commands in the table at the top.
4. Polarity: everything is a contract test — all lines should read `pass`.
   A future bump to another kind-action version needs `--expected-sha`/`--tag`
   updated in the manifest cmd, or the pin-integrity check will (correctly) fail.
5. Harness-bites: `bash docs/verification/kind-action-1151/scripts/run-bites.sh`
   — the base-branch control must fail `pin-integrity`, and the broken fixture
   must fail `pin-integrity` + `input-compat` + `explicit-overrides`.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/kind-action-1151-claude
(github.com/cheyang/fluid fork of fluid-cloudnative/fluid). Background: a review
of https://github.com/fluid-cloudnative/fluid/pull/6208 (kind-action v1.15.0 ->
v1.15.1 pin bump) found no defects; a contract harness proved the pin's
integrity, input compatibility, and green CI matrix. Re-check the current PR
head: run docs/verification/kind-action-1151/scripts/re-verify.sh from a
checkout of the harness branch, then scripts/run-bites.sh for the negative
controls. All checks are contract tests and should pass; report an
observed-vs-expected table and advance .last-reviewed if the head moved.
```
