# Verification — PR #6266 "feat(jindo): use runtime imagePullSecrets for DataLoad"

Reviewer: B (Codex). Head: `4fac0265a36800fa4a87cc22991eb0be8556742c`. Base: `dcb76167b0143a239addc6bed83ed50b45267631`.
Production code is untouched by this harness; all files here and the four `*_verify_test.go` files are additive.

## Premise (P0) — runs against BASE

Claim: JindoFSX/JindoCache DataLoad jobs only honor the controller `IMAGE_PULL_SECRETS` env var;
a JindoRuntime's `spec.imagePullSecrets` is ignored.

**Verdict: CONFIRMED.** On base, `TestVerifyJindoFSxRuntimePullSecretsWin` fails with
`got [{env-secret}], want [{runtime-secret}]` and the helm render of base-generated values contains
no `imagePullSecrets` at all when the env var is unset — see `results/unit-base.json`,
`results/helm-base.log`. Note: linked issue #6247 names JuiceFS/Alluxio; this PR is a declared
partial ("Part of #6247") implementing the same mechanism for the Jindo engines, so there is no
auto-close hazard. The legacy `pkg/ddc/jindo` engine's DataLoad remains env-only (out of PR scope).

## Findings → tests

| ID | claim | layer | tests / checks | polarity |
|----|-------|-------|----------------|----------|
| F1 | runtime `spec.imagePullSecrets` must win; env is fallback when empty | unit | `TestVerifyJindo{FSx,Cache}RuntimePullSecrets{Win,EnvUnset}`, `TestVerifyJindo{FSx,Cache}EnvFallbackWhenRuntimeEmpty`, `TestVerifyJindoFSxNoSecretsAnywhere` | contract |
| F1-HELM | the secrets in `DataLoadInfo` must reach the rendered pod spec for Job and CronJob policies, both charts | integration | `scripts/helm_render_check.sh` checks 1,2,4,5 (+3 omission guard) | contract |
| F2 | the POSIX-path commit must not change Linux behavior of `NormalizeSubPath` / `UFSPathBuilder` / `IsValidMountRoot` | unit | `TestVerifyNormalizeSubPathCharacterization`, `TestVerifyUFSPathBuilderCharacterization`, `TestVerifyIsValidMountRootCharacterization` | contract (characterization: passes on base AND head) |

## Observed vs expected

| run | expected | observed |
|-----|----------|----------|
| unit on head | all pass | 16/16 pass (`results/unit-head.json`) |
| unit on base | F1 contract tests fail (reproduction); F2 characterization passes | exactly that (`results/unit-base.json`, exit 1: 4 runtime-secret tests fail across both engines, fallback + characterization pass) |
| helm render on head | runtime-secret in Job+CronJob pod specs for both charts; key omitted when no secrets | all 6 checks pass (`results/helm-head.log`) |
| helm render on base | runtime-secret checks fail | 4 checks fail, omission check passes (`results/helm-base.log`) |

Harness-bites: the "fix" is the PR itself; base runs above are the red state, head runs are green.
No production diff was applied or left behind.

## How to re-run

From a checkout of this branch:

```bash
# unit + integration against the current PR head (auto-fetched), per-finding verdicts:
bash docs/verification/jindo-dataload-pullsecrets/scripts/re-verify.sh
# or against an explicit ref:
bash docs/verification/jindo-dataload-pullsecrets/scripts/re-verify.sh <ref>

# integration layer alone (from repo root of the ref under test, harness files grafted):
bash docs/verification/jindo-dataload-pullsecrets/scripts/helm_render_check.sh
```

Prereqs: go (>= module version), helm v3, network only for `git fetch` in re-verify.
Tests need `-gcflags="all=-N -l"` because sibling package tests use gomonkey (repo Makefile default).

## Continuing after the fix / next round

All tests are contract polarity: green = good on the new head. The F2 characterization tests must
stay green on every revision (they pin Linux behavior). If a future revision renames
`genDataLoadValue` or changes chart value keys, expect Harness-update: adjust the additive files,
do not edit production code. `.last-reviewed` marks the reviewed head for incremental diffing.

Note on base runs: `re-verify.sh <base-sha>` reports F1 as HARNESS-UPDATE with miss=1 because the
PR's own `TestDataLoadImagePullSecrets` does not exist on the base ref — expected; the 4 failing
`TestVerify*` contract tests there are the actual reproduction. Normal rounds run against fixed
heads only, where the miss disappears.
