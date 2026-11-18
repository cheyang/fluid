# Verification harness — PR #6268 chart image tag (claude)

PR: https://github.com/fluid-cloudnative/fluid/pull/6268
Head: `2fe3f726108e6a4c2be49747a0d586ed7e4ab812`
Base: `dcb76167b0143a239addc6bed83ed50b45267631`

## Premise (P0) — Confirmed

Claim: "Bump fluid chart appVersion and default image tag to v1.1.0-dcb76167b (built from
master commit dcb76167b)." An image set built from master commit `dcb76167b014...` **does**
exist on Docker Hub — but under the tag `v1.1.0-dcb76167` (8-char sha prefix, pushed
2026-11-17T12:40Z, i.e. before the PR commit on 2026-11-18T02:12Z). The PR's intent is valid;
the tag string it uses is not the published one. See `results/P0-premise-registry.json`.

## Observed vs expected

| ID | Claim | Layer | Result on head | Result on base | Verdict |
| --- | --- | --- | --- | --- | --- |
| F1 | All `fluidcloudnative/*` images rendered by `charts/fluid/fluid` with default values exist on Docker Hub | integration (registry query) | 12/13 MISSING (404) — `v1.1.0-dcb76167b` | 13/13 OK — `v1.1.0-91a58c1d9` | **Confirmed (blocker), PR-introduced regression** |
| F2 | Every commit in base..head carries `Signed-off-by: Name <email>` matching the commit author | unit (git) | FAIL — sign-off `cheyang.cy` ≠ author `cheyang` | n/a (no commits) | **Confirmed; DCO check is red for exactly this reason (bot output: Expected "cheyang <cheyang.cy@alibaba-inc.com>", got "cheyang.cy <...>")** |

Raw evidence: `results/F1-image-tags-head.txt`, `results/F1-image-tags-base.txt`,
`results/F1-image-tags-fixed-simulation.txt`, `results/F2-dco-head.txt`,
`results/P0-premise-registry.json`.

## Mechanism (F1)

- `values.yaml:16` sets `version: &defaultVersion v1.1.0-dcb76167b`; the anchor fans out to
  **12** fluid-managed images (dataset/application/alluxio/jindo/juicefs/thin/efc/vineyard/cacheruntime
  controllers, fluid-csi, fluid-webhook, fluid-crd-upgrader) via `imageTag: *defaultVersion`.
- The images built from master `dcb76167b014...` were published under `v1.1.0-dcb76167`
  (8 chars). All 12 return 404 for the 9-char tag and 200 for the 8-char tag.
- Why CI is green anyway: `kind-e2e` and `backward-compat` workflows build images from source
  (`.github/scripts/build-all-images.sh`) with `IMAGE_TAG="$(grep VERSION Makefile)-$(git
  rev-parse --short HEAD)"` and load them into kind — they never pull from Docker Hub, so a
  wrong published tag cannot fail CI.
- Root cause of the off-by-one: `git rev-parse --short` auto-scales abbreviation length
  (published fluid tags in history are 7, 8 and 9 chars: `v1.1.0-676f47a`, `v1.1.0-dcb76167`,
  `v1.1.0-91a58c1d9`). The previous bump matched by luck (both sides 9 chars); this one does
  not (publisher got 8, chart author wrote 9).

## Polarity

Both tests are **contract** tests (assert intended-correct behavior; red = bug present).
- F1 fixed ⇔ `check_image_tags.sh` exits 0 (verified: with the tag corrected to
  `v1.1.0-dcb76167` the script goes green — see `results/F1-image-tags-fixed-simulation.txt`
  from the harness-bites check — then the production change was reverted).
- F2 fixed ⇔ `check_dco.sh` exits 0 (verified during the bites check by amending the sign-off
  to `cheyang <cheyang.cy@alibaba-inc.com>`; then reset back to the PR head).

No canaries. No live layer needed — the failing contract is a registry-existence question
decided without a cluster.

## How to run

```bash
bash docs/verification/chart-image-tag-claude/scripts/check_image_tags.sh   # F1 (needs helm + curl)
bash docs/verification/chart-image-tag-claude/scripts/check_dco.sh          # F2 (needs git)
bash docs/verification/chart-image-tag-claude/scripts/re-verify.sh          # both, vs current PR head
bash docs/verification/chart-image-tag-claude/scripts/re-verify.sh <ref>    # both, vs a fixed ref
```

`check_image_tags.sh` renders the chart with `helm template`, extracts every
`fluidcloudnative/*` image reference, and queries the Docker Hub tag API (3 retries on 429).
Third-party pinned images (registrar, init-users, vineyardd, …) are out of scope for this bump
(the csi-node-driver-registrar is checked too since it shares the prefix — it passes).

## Proposed fix (not applied — this branch ships only the harness)

Either:
1. Change both `charts/fluid/fluid/Chart.yaml:21` (appVersion) and
   `charts/fluid/fluid/values.yaml:16` to the published tag `v1.1.0-dcb76167`; or
2. Push the same images additionally under `v1.1.0-dcb76167b`.

Option 1 is the one-line fix consistent with what was actually published. Long-term: derive
the bump tag from the published tag list (or a fixed-length abbreviation) instead of
`git rev-parse --short`, and consider a CI pre-merge check like `check_image_tags.sh` —
current CI cannot catch tag typos because e2e builds its own images.

For F2: rebase/amend the sign-off to `Signed-off-by: cheyang <cheyang.cy@alibaba-inc.com>`
(matching the commit author name).

## Continuing after the fix

On any machine: clone this fork, checkout `verify/chart-image-tag-claude`, then
`bash docs/verification/chart-image-tag-claude/scripts/re-verify.sh` — it fetches the current
PR head from `manifest.pr`, grafts the harness on top, runs both contract tests, and prints
Fixed / Still-broken per finding (exit 0 iff all fixed). Prerequisites: git, jq, helm, curl.
`.last-reviewed` pins the incremental review delta; advance it after each round.
