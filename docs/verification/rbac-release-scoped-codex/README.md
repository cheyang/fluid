# Verification: PR #6256 — opt-in cluster RBAC name prefix (Reviewer B / Codex)

- PR: https://github.com/fluid-cloudnative/fluid/pull/6256
- Base (merge-base): `f432bdc87ac35941763b74e8e87683c7a84de5d9`
- Head reviewed (round 2): `da020a3f11a0f6c67efdfcc489528fe977664772`
- Previous head (round 1): `af73f5d6a8f7b918f8b1c07bd47cbdd6cebef563`
- Mode: incremental. Production code untouched — harness is additive.

## What changed between rounds

Round 1 head renamed all 24 ClusterRole/ClusterRoleBinding objects via
`fluid.fullname` unconditionally and claimed `Fixes #6134`; review found the
claim false (fixed-name CSIDriver + MutatingWebhookConfiguration still blocked
a second release). Round 2 implements review option (b): the claim is dropped
(no `fixes/closes/resolves #6134` anywhere), the prefix is an **opt-in** value
`rbac.clusterScopedNamePrefix` (empty default), and the PR body explicitly
scopes itself to RBAC and lists the remaining shared cluster-wide resources.

## Premise (P0) — CONFIRMED (re-run on base in round 2)

Issue #6134: a second Fluid helm release on one cluster fails with
`exists and cannot be imported into the current release: invalid ownership metadata`.
Reproduced on the **base** chart against a throwaway envtest apiserver (helm 3.16.3,
k8s 1.36.2 apiserver): exact repro fails on namespaced SA; with
`--set namespace=test` it fails on exactly the reported
`ClusterRole "alluxioruntime-controller"`.
Evidence: `results/p0-install2.log`, `results/p0b-install2.log`.

## Round-2 results (head da020a3) — 29/29 PASS

| Check | Kind | Result | Evidence |
|-------|------|--------|----------|
| R1 | contract | head default render is **byte-identical** to base → default upgrade = zero churn | `results/scenario.log` |
| R2 | contract | distinct prefixes → 24+24 fully disjoint cluster RBAC; every roleRef resolves | `results/r2-r3-render.txt` |
| R3 | canary (holds) | CSIDriver `fuse.csi.fluid.io` + MWC `fluid-pod-admission-webhook` remain fixed-name shared singletons | `results/r2-r3-render.txt` |
| P0/P0b | premise | base second install still fails with the #6134 error | `results/p0*.log` |
| K1 | canary (holds) | head second install with defaults still fails (namespaced SA) — documented limitation | `results/k1-install2.log` |
| K2 | canary (holds) | even with a distinct prefix + own namespace, second install still fails on CSIDriver — documented limitation | `results/k2-install2.log` |
| C1 | contract | two releases with distinct prefixes install side by side once the two singleton templates are removed; bindings resolve within each release | `results/c1-*.log`, `results/c1-binding.txt` |
| U1 | contract | base→head upgrade with defaults preserves historical names + subjects | `results/u1-*.log`, `results/u1-subjects.txt` |
| U2 | contract | opting an existing release into prefix `t1` prunes old-name non-hook RBAC, creates prefixed ones, same SA subjects | `results/u2-*.log`, `results/u2-subjects.txt` |
| U3 | canary (holds) | **N1**: hook RBAC (`fluid-crds-upgrade` CR/CRB) left behind by a failed hooked upgrade is NOT pruned by the prefix opt-in | `results/u3-*.log` |

Unit layer: PR's new `TestClusterScopedRBACNamesPreserveDefaultsAndSupportOptInPrefix`
passes on head (`results/unit-head-r2.log`) and fails on base with 24 prefix
violations when grafted (`results/unit-base-bites-r2.log`) — the test bites.

Round-1 evidence is preserved under `results/round1-af73f5d/`.

## Finding status after round 2

- **F1 (blocker, round 1) → fixed**: the auto-close claim is gone; residual
  behavior is now tracked by canaries K1/K2 and is explicitly documented in the
  PR body. If a future change makes second installs work, K1/K2 flip and must
  be inverted.
- **F2 (minor) → fixed**: RBAC names no longer derive from the release name;
  `values.yaml` documents that prefixes must be unique per cluster.
- **F3 (major, round 1) → fixed**: PR test now renders the real chart with
  `helm template` and enumerates all cluster RBAC; R2 independently covers
  cross-release disjointness.
- **F5 (minor) → fixed by design**: default upgrade is a no-op (R1 + U1).
- **F6 (nit) → fixed**: `renderFullname` gone; the test uses real helm.
- **F7 (minor, pre-existing) → open**: `csi/driver.yaml` still ungated by
  `csi.enabled`; PR body now lists CSIDriver among the still-shared resources.
- **N1 (minor, new) → open**: orphaned pre-upgrade hook RBAC is never pruned
  when opting into a prefix (helm does not prune hooks;
  `before-hook-creation` matches only the new name). The PR body's "Helm will
  create the newly named RBAC objects and then prune the old ones" is accurate
  for the 22 non-hook objects but not for the 2 hook objects. Cleanup is a
  manual `kubectl delete clusterrole/clusterrolebinding fluid-crds-upgrade`.

## How to run

Prereqs: `helm`, `kubectl`, `go`, envtest assets (`KUBEBUILDER_ASSETS` or
`~/.local/share/kubebuilder-envtest/k8s/*-linux-amd64` auto-detected).

```bash
# full matrix (unit + render + integration) against the current PR head:
bash docs/verification/rbac-release-scoped-codex/scripts/re-verify.sh
# or against an explicit ref:
bash docs/verification/rbac-release-scoped-codex/scripts/scenario.sh --head-ref <sha>
```

No live cluster / KUBECONFIG needed; the integration layer uses a local envtest
control plane.

## Interpreting results after a fix

- `R1` must hold while the default stays "historical names"; if a future round
  makes the prefix mandatory, R1 flips intentionally (harness-update).
- `R3`/`K1`/`K2` are canaries documenting the out-of-scope remainder; they flip
  only when CSIDriver/MWC/namespaced SAs become release-scoped — invert then.
- `U3` is a canary for N1; it flips when orphaned hook RBAC gets cleaned up.
- `P0`/`P0b` must keep failing on the **base** chart; otherwise harness-update.

## Handoff

All state is on this branch: `verify-manifest.json`, `.last-reviewed`,
`scripts/`, `results/`. A fresh machine can resume with:
`git fetch <fork> verify/rbac-release-scoped-codex && git checkout verify/rbac-release-scoped-codex && bash docs/verification/rbac-release-scoped-codex/scripts/re-verify.sh`.
