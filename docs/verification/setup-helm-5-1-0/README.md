# Verification — PR #6270: chore(deps): bump azure/setup-helm 5.0.1 → 5.1.0

Reviewer: Codex (Reviewer B). Reviewed head: `3647aeefa31ad32cc78bc659f4ef4858b9f12046`
(base `dcb76167b0143a239addc6bed83ed50b45267631`). Production code untouched by this
harness; the branch adds only `docs/verification/**`.

## Premise (P0)

Dependabot PR: bump the SHA-pinned `azure/setup-helm` action in the two e2e
workflows from v5.0.1 to v5.1.0, picking up upstream fixes including Undici
security updates. **Verdict: Confirmed** — upstream tag `v5.1.0` resolves to
exactly the pinned SHA `2f5e7bc67f1f9972ab16657bb795f36d8f04de0e`
(`git ls-remote https://github.com/azure/setup-helm.git`), and the upstream
changelog/source at that tag matches the release notes quoted in the PR body.

## Why this bump deserves a second look (risk analysis, no defect found)

Both workflows call the action with **no `version` input**, so the installed Helm
is decided by the action, and v5.1.0 changed that logic (`src/run.ts` at the
pinned tag, diffed against v5.0.1):

1. `version` defaults to `latest` → fetched live from
   `https://get.helm.sh/helm-latest-version`. Unchanged between 5.0.1 and 5.1.0.
   Today that returns **v4.3.2** (Helm 4) — so fluid CI already ran Helm 4 under
   v5.0.1 too; the 5.1.0 bump is not the Helm 3→4 cliff.
2. On lookup failure the action installs a built-in fallback; v5.1.0 moved it
   **v3.18.4 → v4.3.1** and added 3 retries plus a `latest-fallback: false`
   opt-out. Fallback is now *closer* to latest, and less likely to trigger.
3. New `checksum-policy` input, default `auto` (= `if-available`): the `.sha256`
   sidecar is fetched and verified; a mismatch or an invalid sidecar fails the
   step. Only a 404/410 sidecar is tolerated without verification. New
   theoretical flake vector: a non-404 transient error fetching the 2-byte-ish
   sidecar now fails setup (5.0.1 never fetched it). Same host serves the
   archive itself, so probability is negligible — noted for awareness only.
4. The deprecated `token` input is no longer read anywhere in `src/run.ts` at
   v5.1.0 (grep-confirmed); nothing in fluid's usage depended on it.

## Layers

- **L1 `scripts/check-pin.sh <repo>`** — pin identity: both workflows pin the
  same SHA, `# v5.1.0` comment matches, SHA equals upstream tag
  (supply-chain check), no stragglers on other versions.
- **L2 `scripts/helm-compat.sh <repo>`** — downloads the two Helm versions the
  action can install (`latest` = v4.3.2 at run time, fallback = v4.3.1) from
  get.helm.sh, verifies each archive against its published `.sha256` (mirrors
  the new `checksum-policy: auto`), then renders:
  - the **local** `charts/fluid/fluid` with the exact `--set` flags of
    `.github/scripts/deploy-fluid-to-kind.sh` (incl. `--include-crds` pass), and
  - the **published** `fluid/fluid` chart via `helm repo add/update`, the path
    used by `gha-backward-compatibility.sh`.
- **L3 (live)** — intentionally skipped (round policy: unit + integration
  only). The PR's own `kind-e2e-test` and `backward-compat-test` checks ran the
  modified workflows on the PR head and are all green — that *is* the live
  layer for a workflow-only diff, exercised by upstream CI.

## Observed vs expected (head `3647aeef`)

| check | expected | observed |
| --- | --- | --- |
| PIN_IDENTICAL / PIN_COMMENT_MATCH | pass | pass (results/L1-check-pin.txt) |
| PIN_TAG_GENUINE | pass | pass — tag v5.1.0 == 2f5e7bc…de0e |
| PIN_NO_STRAGGLERS | pass | pass — no other setup-helm refs |
| LATEST_RESOLVES | pass | pass — latest = v4.3.2 |
| HELM_v4_3_2_CHECKSUM / TEMPLATE_LOCAL / TEMPLATE_REPO | pass | pass (results/L2-helm-compat.txt) |
| HELM_v4_3_1_CHECKSUM / TEMPLATE_LOCAL / TEMPLATE_REPO | pass | pass |

Harness-bites control: L1 against the **base** commit fails PIN_TAG_GENUINE +
PIN_NO_STRAGGLERS (base pins v5.0.1) — see
`results/L1-check-pin-BASE-negative-control.txt`. A wrong assertion in L2
(greping for CRDs that `helm template` only emits with `--include-crds`) was
itself caught by this control discipline and fixed; production code was never
modified.

## Re-running / continuing after changes

From a checkout of this branch:

```bash
bash docs/verification/setup-helm-5-1-0/scripts/re-verify.sh            # auto-fetches current PR head
bash docs/verification/setup-helm-5-1-0/scripts/re-verify.sh <sha>      # pin a ref
```

Prereqs: bash, git, curl, tar, sha256sum, python3, network to github.com +
get.helm.sh + fluid-cloudnative.github.io. No cluster, no Go toolchain.

Polarity: every check is a **contract** test — green means healthy. Note that
`latest` floats: if upstream publishes a newer Helm, the L2 check ids change
(`HELM_vX_Y_Z_*`); that is a harness re-map (Harness-update), not a regression.
If fluid ever pins `version:` in the workflows, L2 should test that pinned
version instead of `latest`.
