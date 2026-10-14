# bump-kind-action-1-15-1 (codex) — verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6208 — a Dependabot PR that bumps
the SHA-pinned `helm/kind-action` from `v1.15.0` (`06c1ae10762d3b9c1644e7fe69596ae519e015a2`)
to `v1.15.1` (`1544676c07bb3570fcb70166beaa07c4528fd4aa`) in the two workflows that
consume it: `.github/workflows/kind-e2e.yml` and
`.github/workflows/backward-compatibility-e2e.yml`.

Layers run against the **code under review** (`7e1edc06215a06119f64271ca2f57999891a62d5`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (deterministic) | pin authenticity + drift: every `helm/kind-action@<sha> # vX.Y.Z` pin in `.github/workflows/` resolves via `git ls-remote` to the upstream commit of tag `vX.Y.Z`; a single distinct sha repo-wide; the old v1.15.0 sha is gone | `bash docs/verification/bump-kind-action-1-15-1-codex/scripts/10-verify-pin.sh <repo-root>` |
| 2. Integration (static usage audit) | parses both workflow YAMLs; asserts every kind-action step passes `version` and `kubectl_version` explicitly and does not set `version_file`/`cloud_provider` — i.e. the upstream v1.15.1 default changes are inert for fluid | `python3 docs/verification/bump-kind-action-1-15-1-codex/scripts/20-verify-inputs.py <repo-root>` |
| 3. Live | skipped per review policy. The real end-to-end signal is GitHub Actions itself: `gh pr checks 6208` (captured in `results/ci-status.txt`) | — |

> Test polarity: all checks are **contract** tests. They PASS on the reviewed head;
> a bad pin, version drift, or newly unpinned input flips them red.

## Problem premise (P0)

| | |
|---|---|
| Claimed change | Dependabot: "Bumps [helm/kind-action](https://github.com/helm/kind-action) from 1.15.0 to 1.15.1" |
| Linked issue | none (no `fixes/closes` reference; mentions helm/kind-action#170, #162 upstream) |
| Reported component | `.github/workflows` CI (kind-e2e, backward-compatibility-e2e) |
| Patched component | same — 2 lines, one per consuming workflow |
| Component match | Yes |
| **Verdict** | **Confirmed** — the new pinned sha is exactly the upstream `v1.15.1` tag commit; the upstream v1.15.0→v1.15.1 diff is behaviorally inert for fluid's usage; all CI green on the head |
| Evidence | `results/10-verify-pin.out`, `results/20-verify-inputs.out`, `results/upstream-action-code.diff.txt`, `results/ci-status.txt` |

This is a non-functional dependency-pin maintenance PR, so the deep "why" analysis is
carved out; what must hold is that the pin is authentic and the upstream delta is safe
for the way fluid invokes the action. Both are proven by the harness, not asserted.

Upstream v1.15.0→v1.15.1 (verified from a clone of helm/kind-action, diff in
`results/upstream-action-code.diff.txt`):
- `feat: add version_file input` (helm/kind-action#162) — opt-in; fluid does not set it → inert.
- `chore: bump kubectl to v1.37.1` (#170) — only changes the **default**; fluid passes
  `kubectl_version: ${{ matrix.kubernetes-version }}` explicitly in both workflows → inert.
- `chore: bump cloud-provider-kind to v0.12.0` (#170) — installed only when
  `cloud_provider: true`; fluid does not set it (default `false`) → inert.
- kind cache layout (`${RUNNER_TOOL_CACHE}/kind/<version>/<arch>/{kind,kubectl}/bin`)
  is unchanged, so the existing `actions/cache` key stays valid.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| pin-authenticity | Pinned sha == upstream v1.15.1 tag commit in both workflows | 1 | Confirmed (pass) | `results/10-verify-pin.out` |
| no-version-drift | Single distinct sha repo-wide; old v1.15.0 sha gone | 1 | Confirmed (pass) | `results/10-verify-pin.out` |
| inert-upstream-delta | All behaviorally-affected inputs explicitly pinned; `version_file`/`cloud_provider` unset; YAML parses | 2 | Confirmed (pass) | `results/20-verify-inputs.out` |
| CI | kind-e2e-test + backward-compat-test green on head for all 5 k8s versions | 3 (CI) | Pass | `results/ci-status.txt` |

## Per-finding detail

**pin-authenticity + no-version-drift (contract).** `scripts/10-verify-pin.sh` greps every
workflow for `helm/kind-action@<sha> # vX.Y.Z`, resolves `refs/tags/vX.Y.Z` (peeled
`^{}` preferred) via `git ls-remote https://github.com/helm/kind-action.git`, and
requires equality. On the head: both files pin
`1544676c07bb3570fcb70166beaa07c4528fd4aa` == `refs/tags/v1.15.1`. Single distinct sha;
the v1.15.0 sha `06c1ae1...` no longer appears anywhere under `.github/workflows/`.

**inert-upstream-delta (contract).** `scripts/20-verify-inputs.py` parses all
workflow YAML and asserts each kind-action step sets `version` and `kubectl_version`
and omits `version_file`/`cloud_provider`. On the head: pass for both workflows.

**Harness-bites proof** (`results/harness-bites.out`): against a tampered copy,
case 1 (old sha with v1.15.1 comment) turns `yaml-pin-tag-match`, `yaml-pin-drift`,
`old-pin-absent` red; case 2 (dropped `kubectl_version`, added `cloud_provider: "true"`)
turns `inputs-pinned` red. The harness fails for the right reason, so green means green.

## Proposed fixes (NOT applied to production here)

None required. One pre-existing nit (not introduced by this PR): the inline comment on
`kind-e2e.yml`'s `version:` line still narrates the v1.12.0/v0.29.0 era while the pin
is v0.31.0 on action v1.15.1 — cosmetic only.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/bump-kind-action-1-15-1-codex` (reviewer fork
`https://github.com/cheyang/fluid.git`); production code untouched (the branch adds
only `docs/verification/bump-kind-action-1-15-1-codex/**` on top of the PR head).

1. One-command re-verification against the current PR head:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/bump-kind-action-1-15-1-codex
   git checkout verify/bump-kind-action-1-15-1-codex   # or FETCH_HEAD
   bash docs/verification/bump-kind-action-1-15-1-codex/scripts/re-verify.sh
   ```
   (no ref argument: the manifest's `pr` URL auto-resolves the current PR head and
   grafts the harness onto it; exit 0 iff every contract check passes)
2. Prereqs: `git`, `jq`, `python3`+PyYAML, network access to github.com.
3. Polarity: all checks are contract tests — they must stay GREEN. Any red means the
   new head broke pin authenticity, introduced drift, or unpinned an input.
4. CI layer: `gh pr checks 6208` — both e2e workflows must be green across the matrix.
5. Advance the marker after a round: `echo <new-head-sha> > docs/verification/bump-kind-action-1-15-1-codex/.last-reviewed` and commit.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/bump-kind-action-1-15-1-codex (remote
https://github.com/cheyang/fluid.git). Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6208 (helm/kind-action 1.15.0->1.15.1
pin bump) produced acceptance checks pin-authenticity, no-version-drift and inert-upstream-delta (upstream
default changes inert given fluid's explicit inputs); the harness proved both on head
7e1edc06. Read docs/verification/bump-kind-action-1-15-1-codex/README.md ("Continuing
after the fix") and follow it: run scripts/re-verify.sh (no args), mind that all checks
are contract polarity, check `gh pr checks 6208` for the e2e matrix, review the
.last-reviewed..head delta, then advance .last-reviewed and push the branch back to the
same fork. Do not touch production code.
```
