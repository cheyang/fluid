# Verification — PR #6278 — chart image tag `v1.1.0-dcb76167`

Reviewer: Claude (Reviewer A). Harness branch: `verify/chart-tag-dcb76167-claude`.
Production code untouched — this directory is the entire diff against the PR head.

## Premise (P0) — verdict: **Confirmed**

Claim under test (PR body, quoted):

> "A fresh `helm install` with the current chart therefore pulls a tag that was never
> pushed and every pod stays in `ImagePullBackOff`."

Mechanism check on the **base** branch (253f6bb84f26):

- `charts/fluid/fluid/values.yaml` `version: &defaultVersion v1.1.0-dcb76167b` and
  `Chart.yaml` `appVersion: 1.1.0-dcb76167b`.
- Rendering the chart at base (`helm template`) produces **12** fluid images tagged
  `v1.1.0-dcb76167b` (dataset/application/alluxio/jindo/juicefs/thin/cache/vineyard/efc
  controllers, fluid-csi, fluid-webhook, fluid-crd-upgrader).
- Docker Hub tag API (`/v2/repositories/fluidcloudnative/<img>/tags/<tag>`) returns
  **404 for all 12** under the 9-char tag and **200 for all 12** under the 8-char
  `v1.1.0-dcb76167` (tag pushed 2026-11-17T12:40Z, same day as commit dcb76167b).
- A reference that 404s on the registry is exactly the precondition for
  `ImagePullBackOff`; no cluster needed to decide it.

Component match: the PR touches exactly the two files that define the tag. No closing
issue; the PR body states the problem (satisfied via body).

Note on the PR body's mechanism narrative: it says the 8-char prefix is "ambiguous in
this repository". `git rev-parse --disambiguate=dcb76167` resolves exactly **one**
object, so the 9-char abbreviation comes from git's repo-size-scaled *auto* abbreviation
length, not from per-prefix ambiguity. Immaterial to the fix, but relevant to the
root cause (see risk R1): the chart-bump side and the image-publish side evidently
computed different abbreviation lengths.

## Observed vs expected

| claim | layer | polarity | base | head (46759c2) | verdict |
| --- | --- | --- | --- | --- | --- |
| P0: base chart references unpullable tags | chart render + registry | contract (red = bug present) | FAIL (12×404) | — | **Confirmed** |
| AC1: every fluid image in the rendered chart resolves on the registry | chart render + registry | contract | FAIL | PASS (12×200) | **Fixed by PR** |

Raw evidence: `results/base.json`, `results/head.json`, `results/run-log.txt`.

Harness-bites proof: the *same* contract assertion is red on base and green on head —
the check demonstrably exercises the defect (base is the negative control; no
artificial corruption needed).

Extra coverage beyond the PR body: the PR body only verified 6 images on Docker Hub;
the harness checks **all 12** the chart actually references via the `defaultVersion`
anchor, including the 4 the body omitted (jindo/juicefs/vineyard/efc/application/
thin). All 12 exist under `v1.1.0-dcb76167`.

Also verified: the `crdUpgrade` pre-upgrade hook Job (whose image is
`fluid-crd-upgrader:<defaultVersion>`) renders with name
`fluid-crds-upgrade-110-dcb76167` and `before-hook-creation` delete policy — the name
changes on every image bump by design, so the rename from `…dcb76167b` introduces no
new lifecycle behavior, and at base the hook itself referenced a 404 tag, so the fix
also repairs `helm upgrade` paths, not just fresh installs.

## How to re-run

```bash
bash docs/verification/chart-tag-dcb76167/scripts/re-verify.sh   # current PR head
bash docs/verification/chart-tag-dcb76167/scripts/re-verify.sh <sha>  # any ref
```

`re-verify.sh` resolves the PR head from `verify-manifest.json`'s `pr` URL
(`git fetch https://github.com/fluid-cloudnative/fluid pull/6278/head`), resolves the
base as merge-base against upstream master, renders the chart at the target ref via
`git archive` + `helm template`, and queries the Docker Hub tags API for every
fluidcloudnative image. Exit 0 iff every referenced tag resolves.

Prerequisites: git, helm 3.x, python3, network access to hub.docker.com. No cluster,
no KUBECONFIG.

## Non-blocking risk recorded (not fixed by this PR)

R1 (minor, preexisting, out-of-repo): whatever bumps the chart tag and whatever
publishes the images disagree on short-SHA length — the previous bump
`v1.1.0-91a58c1d9` (9 chars) *does* exist on Docker Hub (200), while this round the
publisher pushed 8 chars and the bump wrote 9. Nothing in this repo's diff can pin
that; the release automation should truncate the SHA identically on both sides
(e.g. `git rev-parse --short=8` or `${GITHUB_SHA::8}` everywhere).

## Continuing after the fix

This harness is a pure reviewer artifact: no production code, no unit test files.
To re-check on another machine: clone the fork, `git checkout
verify/chart-tag-dcb76167-claude`, run the re-verify command above. If the next
chart bump changes the tag, pass the new fragment via
`--tag-fragment <new-sha>` (or drop the flag to check every fluid image).
