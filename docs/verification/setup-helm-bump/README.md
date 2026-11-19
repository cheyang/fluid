# Verification — PR #6270 — bump azure/setup-helm 5.0.1 → 5.1.0

Reviewer A (Claude), round 1 (initial). Harness branch:
`verify/setup-helm-bump-claude` on the reviewer fork (`cheyang/fluid`), based on
PR head `3647aeefa31ad32cc78bc659f4ef4858b9f12046`. Production code untouched —
the only diff vs the PR branch is this harness directory.

## Premise (P0)

CI-config-only Dependabot bump. Premise check per the round policy's carve-out
for non-functional changes: on **base** both `Set up Helm` steps pin
`9bc31f4e…` (v5.0.1); upstream `refs/tags/v5.1.0` resolves to exactly the SHA
this PR pins (`2f5e7bc…`), and the release carries Undici security fixes plus
checksum verification of the downloaded Helm binary. **Confirmed** — a routine,
valid dependency refresh. Component match is complete: these are the only two
`azure/setup-helm` usages in the repo, and both are bumped.

## What actually changes at runtime

The workflows pass **no `version:` input** to the action, so:

- Happy path (unchanged by this bump): the action resolves `latest` Helm via the
  GitHub API — same in v5.0.1 and v5.1.0.
- Degraded path (the only real behavior delta): when latest cannot be resolved,
  the built-in default was `v3.18.4` (Helm 3) in v5.0.1 and is `v4.3.1` (Helm 4)
  in v5.1.0, with a new `latest-fallback` input (default `true`) to opt out.
  This *converges* the degraded path with what `latest` already resolves to, so
  it is a consistency improvement, not a regression risk.

## Observed vs expected

| claim | polarity | layer | on base | on head | evidence |
| --- | --- | --- | --- | --- | --- |
| every `azure/setup-helm` usage pinned to the genuine v5.1.0 tag SHA | contract | unit | **fail** | pass | `results/base-run.txt`, `results/head-run.txt` |
| pinned SHA == `refs/tags/v5.1.0` on azure/setup-helm | contract | unit | pass | pass | idem (network check) |
| base→head delta is exactly the two workflow files, only the pin lines | contract | unit | fail | pass | idem |
| both workflows parse as YAML and keep the setup-helm step | contract | unit | pass | pass | idem |
| F1: `Set up Helm` passes no `version:` input (Helm major floats) | canary | unit | pass | pass | idem — documents pre-existing F1 |
| v5.1.0 built-in fallback is Helm v4.3.1, `latest-fallback` defaults true | contract | unit | pass | pass | idem |

The base column is the **harness-bites** demonstration: the v5.1.0-pin contract
check is red on base and green on head, so the test exercises the actual change.

## Integration evidence (captured, not re-run locally)

`results/ci-evidence.txt`: both workflows that consume the action
(`kind-e2e-test`, `backward-compat-test`) ran on the **exact PR head sha**
`3647aee…` (verified via the Actions API) and succeeded for all five matrix
Kubernetes versions (v1.33.2 … v1.22.17). Those jobs execute
`helm upgrade --install … charts/fluid/fluid`, so the chart install path under
the new action version is exercised end-to-end.

## Findings

- **F1 (minor, pre-existing, non-blocking)**: the `Set up Helm` steps don't pin
  `version:`, so the Helm major version that deploys the Fluid chart floats
  with upstream `latest` (currently Helm 4.x) — and since this bump, the
  degraded-path fallback is also Helm 4. Pre-existing on base; not introduced
  by this PR. Suggest pinning a known-good `version:` (or explicitly accepting
  latest) in a follow-up.
- **F2 (informational, confirmed)**: the only behavioral delta of the bump is
  the built-in fallback v3.18.4 → v4.3.1; documented and verified against the
  action source at the pinned SHA.

No blockers, no majors.

## Continuing after the fix / next round

From any checkout of this branch:

```bash
bash docs/verification/setup-helm-bump/scripts/re-verify.sh   # resolves current PR head itself
```

Per-claim verdicts print with polarity applied (contract → FIXED/STILL-BROKEN;
canary → UNCHANGED/FLIPPED). Network checks self-skip as non-gating when
offline. `.last-reviewed` holds the sha reviewed this round; advance it after
reviewing the delta. If the workflows ever pin a Helm `version:`, the F1 canary
flips to fail — invert it or promote it to a contract test then.
