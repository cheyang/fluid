# Verification — PR #6268 `chore(chart): bump image version to v1.1.0-dcb76167b`

Reviewer: Codex (Reviewer B). Round: initial. Reviewed head: `2fe3f726108e6a4c2be49747a0d586ed7e4ab812` (base/merge-base: `dcb76167b0143a239addc6bed83ed50b45267631`).

## Premise (P0) — REFUTED as written

The PR claims the chart bump consumes images "built from master commit dcb76167b".
The images **were** built from that commit and pushed to Docker Hub on
2026-11-17 — but under tag **`v1.1.0-dcb76167`** (8-char abbrev), not
**`v1.1.0-dcb76167b`** (9-char) as written in this PR. Docker Hub v2 API returns
`404` for the 9-char tag on all 12 managed images and `200` for the 8-char tag.
Merged as-is, a default `helm install` of this chart ImagePullBackOffs on every
fluid component. Positive evidence of absence → premise refuted as written;
the intended chore is valid, the tag string is wrong.

Root cause of the mismatch: `Makefile` / `.github/scripts/build-all-images.sh`
tag images with `git rev-parse --short HEAD`, whose abbreviation length drifts
with clone depth and repo growth (8 chars on the 2026-11-17 build machine,
9 chars on a full clone today).

## Findings and results (observed vs expected)

| ID | Claim | Polarity | Layer | Test | Expected (correct) | Observed on 2fe3f7261 | Verdict |
|----|-------|----------|-------|------|--------------------|-----------------------|---------|
| F1 | `defaultVersion`/`appVersion` tag must exist on Docker Hub for every managed image | contract | unit (Docker Hub API) | `TestDefaultVersionImagesExistOnDockerHub` | HTTP 200 ×12 | HTTP 404 ×12 | **Confirmed (Still-broken)** |
| F2 | HEAD commit must carry `Signed-off-by:` exactly matching the author (DCO) | contract | unit (git metadata) | `TestHeadCommitSignoffMatchesAuthor` | sign-off `cheyang <cheyang.cy@alibaba-inc.com>` | sign-off is `cheyang.cy <...>`; DCO check-run `action_required` | **Confirmed (Still-broken)** |
| —  | appVersion == defaultVersion minus `v` (convention guard) | contract | unit | `TestChartAppVersionConsistentWithDefaultVersion` | pass | pass | guard green |

Raw output: `results/unit-head-2fe3f7261.txt` (F1+F2 reproduction),
`results/rendered-images-head-2fe3f7261.txt` (`helm template` renders the
missing tag into all 12 image fields), plus the bites checks below.

## Harness-bites proof (step 4)

- F1: with the proposed fix applied temporarily (`v1.1.0-dcb76167`), the test
  goes green — `results/unit-bites-fixed-tag.txt`. Reverted afterwards; the
  production diff on this branch is empty.
- F2: against a known-compliant commit (`SIGNOFF_REF=6f55f846d`) the test
  passes — `results/unit-bites-dco-compliant.txt`.

## How to run

```bash
# from repo root at the code under review
go test ./docs/verification/chart-image-tag-bump/harness/ -v -count=1
bash docs/verification/chart-image-tag-bump/scripts/check-rendered-images.sh   # helm-rendered view
```

## Why CI did not catch F1 (coverage gap, F4)

- `kind-e2e.yml` builds images from the PR checkout and
  `.github/scripts/deploy-fluid-to-kind.sh` **sed-replaces** `defaultVersion`
  with the locally computed tag before `helm upgrade --install`.
- `backward-compatibility-e2e.yml` installs the **released** chart from
  `fluid-cloudnative.github.io/charts`, not the PR chart.
No job validates that the chart's declared default tag exists in a registry.

## Continuing after the fix (re-verify)

```bash
bash docs/verification/chart-image-tag-bump/scripts/re-verify.sh   # auto-fetches current PR head
```

Both findings are contract-polarity: they are Fixed only when the tests go
green. F1 turns green either by the chart adopting the pushed 8-char tag
(`v1.1.0-dcb76167`) or by a 9-char alias being pushed for all 12 images.
F2 turns green after `git rebase HEAD~1 --signoff` (or `--amend -s`) with
author identity `cheyang <cheyang.cy@alibaba-inc.com>` and a force-push.

Kickoff prompt for a fresh machine: "Check out
`verify/chart-image-tag-bump-codex` from https://github.com/cheyang/fluid.git,
read docs/verification/chart-image-tag-bump/README.md, run
docs/verification/chart-image-tag-bump/scripts/re-verify.sh, and report
Fixed/Still-broken per finding."
