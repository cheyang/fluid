# pr6278-chart-image-tag — review verification

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6278
("fix(chart): point the image tag at the published v1.1.0-dcb76167").

Reviewed head: `46759c29228a1fc0f9e3557ad342307efea6a77b` (base `253f6bb84f26a906fba301550b5c81ebdd73af35`).
Production code untouched — this branch adds only this directory and
`test/verify/pr6278charttag/verify_chart_tag_test.go`.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | `helm template` rendering, values/Chart invariants, Makefile abbrev canary, git mechanism evidence | `go test ./test/verify/pr6278charttag/ -run 'TestChartRendersWithDefaults|TestAppVersionMatchesDefaultTag|TestMakefileUsesAutoAbbrevLength|TestGitShortShaLengthIsNine' -v` |
| 2. Integration | real registry: every default-tagged `fluidcloudnative/*` image exists on Docker Hub | `go test ./test/verify/pr6278charttag/ -run 'TestDefaultTaggedImagesExistInRegistry' -v` |
| 3. Live | skipped per review setup (would be: fresh `helm install` on a cluster → pods pull images) | — |

Prerequisites: `go`, `helm` v3 (on PATH), `git`; layer 2 needs network access to hub.docker.com.

> Polarity: contract tests FAIL on the broken (pre-PR) chart and PASS on the fixed chart.
> The canary (F1) PASSES now and must FLIP to fail once the release tooling is hardened.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "A fresh `helm install` with the current chart therefore pulls a tag that was never pushed and every pod stays in `ImagePullBackOff`." (PR body) |
| Claimed mechanism | "#6268 recorded `v1.1.0-dcb76167b` … `git rev-parse --short` prints 9 characters for that commit … The images were published under the 8-char tag `v1.1.0-dcb76167`" (PR body) |
| Reported component | helm chart `charts/fluid/fluid` (appVersion + default image tag) |
| Patched component | same: `charts/fluid/fluid/Chart.yaml` + `charts/fluid/fluid/values.yaml` |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | Base chart renders 12 default-tagged refs `*:v1.1.0-dcb76167b`; Docker Hub returns **404 for all 12** (results/integration-base.json). Head chart renders `*:v1.1.0-dcb76167`; Docker Hub returns **200 for all 12** (results/integration-head.json). Mechanism: `git rev-parse --short dcb76167` → `dcb76167b` (9 chars) on a full clone (results/abbrev.txt). |

Precision note on the mechanism: the 8-char prefix `dcb76167` is *not* ambiguous in this repo
(`git rev-parse --disambiguate=dcb76167` → exactly 1 object). The 9th char comes from git's
repo-wide auto-abbrev default, which scales with object count (~77k objects here) — see
results/abbrev.txt. The observable claim in the PR body ("`git rev-parse --short` prints 9
characters") is nonetheless exactly right.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Base chart points at a never-published tag → fresh install ImagePullBackOff | 2 | **Confirmed** | base: 12/12 refs 404 (results/integration-base.json); head: 12/12 refs 200 (results/integration-head.json) |
| F1 | Recurrence risk: Makefile builds image tags with auto-length `git rev-parse --short`, but chart bumps record whatever the bumper's clone prints; build/bump environments already disagree (9-char tag `v1.1.0-91a58c1d9` published for #6232's build, 8-char `v1.1.0-dcb76167` for this build) | 1 | Confirmed (risk present; canary armed) | `TestMakefileUsesAutoAbbrevLength` passes (=risk present); results/abbrev.txt |
| F2 | No automated check that the chart's default image tag exists in the registry; #6268's breakage reached master undetected | 2 | Confirmed (gap exists; this harness *is* the missing check) | `TestDefaultTaggedImagesExistInRegistry` fails on base / passes on head (results/reverify-base.txt) |

## Per-finding detail

### F1 — auto-length `git rev-parse --short` in the release flow (minor, pre-existing)

`Makefile:7` computes `GIT_SHA := $(shell git rev-parse --short HEAD || echo "HEAD")`, so the
published image tag length depends on the building environment's clone. Meanwhile chart bumps
(#6268 etc.) record whatever `git rev-parse --short` prints on the maintainer's full clone —
currently 9 chars, since git's auto abbrev scaled up with the repo's object count. Evidence that
build environments are inconsistent over time: `v1.1.0-91a58c1d9` (9 chars) was published for
the #6232 round while `v1.1.0-dcb76167` (8 chars) was published for this one (results/abbrev.txt).
This PR fixes today's symptom; the trap stays armed for the next bump. Hardening options:
pin `--short=8` in the Makefile, or derive the chart tag from the registry (source of truth)
instead of from a local clone.

Canary: `TestMakefileUsesAutoAbbrevLength` — passes while `Makefile` uses bare `--short`;
FLIPS to fail when a length is pinned (then invert it). Note: a registry-derived bump flow
would mitigate F1 without flipping this canary — reassess manually in that case.

### F2 — no CI check that chart default tags exist in the registry (minor)

The exact failure mode of #6268 (chart pointing at a never-pushed tag) passed all CI. The
contract test `TestDefaultTaggedImagesExistInRegistry` closes that gap: it renders the chart
with default values and asserts every default-tagged `fluidcloudnative/*` image exists on
Docker Hub. Observed vs expected: on base it fails with 12×404 (expected — that is the bug);
on the PR head it passes 12×200 (expected — the fix). Harness-bites proof: results/reverify-base.txt.

## Proposed fixes (NOT applied to production here)

- F1: pin the abbreviation length used for image tags (e.g. `git rev-parse --short=8 HEAD` in
  the Makefile and the chart-bump tooling), or have the chart-bump automation verify/derive the
  tag against the registry before committing.
- F2: run `TestDefaultTaggedImagesExistInRegistry` (or an equivalent `helm template` + registry
  HEAD check) in CI for chart-tag bump PRs.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/pr6278-chart-image-tag-codex` (fork remote), production code
untouched, so it grafts onto whatever the next head is:

```bash
git fetch https://github.com/cheyang/fluid.git verify/pr6278-chart-image-tag-codex
git checkout verify/pr6278-chart-image-tag-codex   # or cherry-pick its harness commit
bash docs/verification/pr6278-chart-image-tag/scripts/re-verify.sh   # auto-fetches current PR head
```

Expected on the current head: F2 `FIXED` (contract passes), F1 `STILL-PRESENT` (canary passes —
that is the pre-existing risk, not a regression). `.last-reviewed` holds
`46759c29228a1fc0f9e3557ad342307efea6a77b`; advance it after the next round per the script's
printed one-liner.

Kickoff prompt for a fresh agent: "Check out branch verify/pr6278-chart-image-tag-codex from
https://github.com/cheyang/fluid.git, read docs/verification/pr6278-chart-image-tag/README.md,
run scripts/re-verify.sh there, and report per-finding Fixed/Still-broken for PR
https://github.com/fluid-cloudnative/fluid/pull/6278."
