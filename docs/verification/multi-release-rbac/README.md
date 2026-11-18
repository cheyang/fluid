# multi-release-rbac — bug verification

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6256
("fix(chart): make cluster-scoped RBAC names release-scoped", fixes #6134).

Harness runs against the **PR head** `af73f5d6a8f7b918f8b1c07bd47cbdd6cebef563`
(base `f432bdc87ac35941763b74e8e87683c7a84de5d9`). Production code is untouched
on this branch — the harness is one new test file
(`charts/fluid/fluid/clusterscoped_collision_test.go`) plus this directory.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Integration (helm render) | `helm template` the chart for two releases and diff every cluster-scoped (kind, name) pair | `go test ./charts/fluid/fluid/ -run 'TestClusterScoped' -v` (skips if no `helm` binary) |
| 2. Live (L3, not run this round) | `helm install` release A, then release B, against a real cluster | see "Live run notes" |

> Test polarity: **contract** tests (assert intended behavior) FAIL on buggy
> code / PASS when fixed. All tests in this harness are contract tests; a red
> result IS the reproduction. G1 is a green-on-head control/guard.

## Problem premise (P0)

Answered before the findings, run against the **base** branch `f432bdc` without
the patch, because the question is whether the problem exists today.

| | |
|---|---|
| Claimed symptom (issue #6134) | "When installing multiple Fluid Helm releases in different namespaces, the second installation fails … Error: INSTALLATION FAILED: Unable to continue with install: ClusterRole "alluxioruntime-controller" in namespace "" exists and cannot be imported into the current release: invalid ownership metadata" |
| Linked issue | #6134, OPEN, bug, created 2026-07-22 (fluid 1.0.8, k8s 1.34) |
| Reported component | Helm chart cluster-scoped RBAC (`charts/fluid/fluid/templates/role/*/rbac.yaml`) |
| Patched component | Same RBAC templates + `upgrade/crd-upgrade.yaml` (matches) |
| Component match | Yes — for the RBAC subset. **No for the whole symptom**: the chart renders other cluster-scoped objects with fixed names that the patch does not touch (see F1) |
| **Verdict** | **Confirmed** (the problem is real on base; the fix is partial) |
| Evidence | `results/base-f432bdc.txt` — two releases collide on **26** cluster-scoped objects, including `ClusterRole/alluxioruntime-controller`, the exact object named in the issue's error message |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | Symptom of #6134 exists on base | base render | **Confirmed** — 26 collisions, incl. `ClusterRole/alluxioruntime-controller` | `results/base-f432bdc.txt` |
| G1 | The PR's RBAC renaming itself works (two release names → disjoint RBAC, roleRef resolves) | 1 | **Green on head, red on base** (differential: the claimed RBAC fix does work) | `results/head-af73f5d.txt` |
| F1 | With default values and *different* release names, release B still collides with release A on cluster-scoped objects → second `helm install` still fails → "Fixes #6134" closes a live bug | 1 | **Confirmed (red on head)** — `CSIDriver/fuse.csi.fluid.io` + `MutatingWebhookConfiguration/fluid-pod-admission-webhook` | `results/head-af73f5d.txt` |
| F2 | Same release name in two namespaces (Helm 3 permits it; `helm install fluid -n a` + `helm install fluid -n b`) still collides on all 26 objects incl. the renamed RBAC | 1 | **Confirmed (red on head)** — `fluid.fullname` ignores `.Release.Namespace` | `results/head-af73f5d.txt` |

## Per-finding detail

### F1 — the reported install failure persists (blocker)

- Mechanism: Helm adds `meta.helm.sh/release-name/-namespace` ownership labels to
  everything it installs and refuses to install over an object owned by another
  release ("exists and cannot be imported into the current release: invalid
  ownership metadata"). The PR release-scopes all 24 ClusterRole/ClusterRoleBinding
  objects, but two cluster-scoped objects keep fixed names:
  - `templates/csi/driver.yaml:4` → `CSIDriver/fuse.csi.fluid.io`, rendered **unconditionally**;
  - `templates/webhook/webhookconfiguration.yaml:5` → `MutatingWebhookConfiguration/fluid-pod-admission-webhook`, rendered with default `webhook.enabled: true`.
  Both are cluster-scoped, so with default values the second release fails exactly
  as reported — only the object in the error message changes.
- Command: `go test ./charts/fluid/fluid/ -run TestClusterScopedObjectsDisjointAcrossReleases -v`
- Observed: 2 collisions (`CSIDriver/fuse.csi.fluid.io`,
  `MutatingWebhookConfiguration/fluid-pod-admission-webhook`). Expected: 0.
- Note: both names are hardcoded in Go (`pkg/common/volume.go:32 CSIDriver`,
  `pkg/csi/plugins/driver.go:37`, `pkg/common/webhook.go:25 WebhookName` — the
  webhook controller patches the MWC's caBundle at runtime,
  `pkg/webhook/certificate_builder.go`). Renaming them is a product decision, not
  a one-line chart edit; the viable chart-level options are a `lookup` guard
  (skip creation when the object already exists) or documenting one-release-per-cluster.
  This is why the PR alone cannot deliver the multi-release install #6134 asks for.

### F2 — same release name in two namespaces still collides (major)

- Mechanism: `fluid.fullname` is derived from `.Release.Name` (and chart name)
  only. Two releases both named `fluid` (Helm 3 stores releases per-namespace,
  so this is legal and is the natural pattern for a chart named `fluid`) render
  identical names, including the newly release-scoped RBAC
  (`ClusterRole/fluid-alluxioruntime-controller` …).
- Command: `go test ./charts/fluid/fluid/ -run TestClusterScopedObjectsDisjointForSameReleaseNameInTwoNamespaces -v`
- Observed: 26 collisions. Expected: 0.

### G1 — RBAC renaming control (green)

- `TestClusterScopedRBACReleaseScopedAcrossReleases`: for releases `team-a`/`team-b`
  every ClusterRole/ClusterRoleBinding is release-scoped, every roleRef resolves
  within the same release, and no RBAC object collides. Red on base, green on head —
  the PR's own claim is reproduced and regression-guarded.

## Harness-bites check

`results/bitescheck-with-proposed-fix.txt`: in a scratch worktree the *proposed*
fix was applied (release-scope CSIDriver + MWC names, and make `fluid.fullname`
include `.Release.Namespace`); F1 and F2 tests flipped to PASS, G1 stayed green.
The scratch worktree was then discarded — production code on this branch is
untouched (`git diff af73f5d6a8f7b918f8b1c07bd47cbdd6cebef563` shows only the
harness files).

## Live run notes (L3, not run this round)

Per review constraints the live layer was skipped. To settle the residual
uncertainty (does `helm install` of release B fail *after* the RBAC succeeds, and
in what order), on a throwaway cluster run:
`helm install team-a charts/fluid/fluid -n team-a-ns --create-namespace && helm install team-b charts/fluid/fluid -n team-b-ns --create-namespace`
Expected signal: the second install fails with
`exists and cannot be imported into the current release` naming
`CSIDriver fuse.csi.fluid.io` or `MutatingWebhookConfiguration fluid-pod-admission-webhook`.
Also worth checking on upgrade (old fixed names → new release-scoped names):
whether Helm creates the new ClusterRoleBindings before deleting the old ones
(the PR body promises only a "brief window", not ordering).

## Proposed fixes (NOT applied to production here)

- **F1**: decide the ownership story for the two remaining cluster-scoped
  singletons. Chart-level: guard creation with `{{ if not (lookup "storage.k8s.io/v1" "CSIDriver" "" "fuse.csi.fluid.io") }}`
  (and equivalent for the MWC) so a second release adopts the shared singleton
  instead of failing; or make `webhook.enabled=false` + no CSI a documented
  precondition for multi-release installs; or drop `Fixes #6134` and scope the PR
  to "RBAC no longer conflicts" until those objects are handled.
- **F2**: include `.Release.Namespace` in the generated prefix (or document that
  release names must differ), otherwise the exact reported error persists for
  same-name releases.

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/multi-release-rbac-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Get it onto the fixed code:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/multi-release-rbac-claude
   git checkout <fixed-branch>
   git checkout FETCH_HEAD -- docs/verification/multi-release-rbac charts/fluid/fluid/clusterscoped_collision_test.go
   ```
2. Prereqs: `go` + `helm` on PATH (Layer 1 needs nothing else).
3. Re-run: `go test ./charts/fluid/fluid/ -run 'TestClusterScoped' -v`
   or `bash docs/verification/multi-release-rbac/scripts/re-verify.sh` (auto-resolves
   the current PR head from the manifest's `pr` URL).
4. Read results via the polarity table: F1/F2 are contract tests — they must go
   GREEN when fixed; G1 must stay green (red = RBAC regression).
5. Harness-bites: run Layer 1 once against pre-fix code (`git checkout af73f5d6a8f7b918f8b1c07bd47cbdd6cebef563 -- charts/fluid/fluid/templates && go test …`) to confirm F1/F2 still go red.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/multi-release-rbac-claude (fork
https://github.com/cheyang/fluid.git). Background: review of PR #6256 (fluid)
produced findings F1 (CSIDriver/MutatingWebhookConfiguration still collide across
releases) and F2 (same release name in two namespaces still collides); the
harness reproduced both. The PR is now fixed at <ref>. Read
docs/verification/multi-release-rbac/README.md ("Continuing after the fix") and
follow it: graft the harness onto the fixed code, re-run
`go test ./charts/fluid/fluid/ -run 'TestClusterScoped' -v` (needs helm on PATH),
mind the polarity table (F1/F2 contract tests must flip green; G1 must stay
green), optionally run the L3 live check against a throwaway cluster, and report
an observed-vs-expected table. Clean up scoped test resources; do not run
cluster-wide destructive actions.
```

---

# ROUND 2 — head `da020a3f11` ("fix(chart): make RBAC prefix opt-in")

The PR was re-scoped between rounds: the `Fixes #6134` closing keyword was
dropped (bare `#6134` mention only — no auto-close on merge), the default was
reverted to the **historical fixed RBAC names**, and a new opt-in
`rbac.clusterScopedNamePrefix` value routes all chart ClusterRole /
ClusterRoleBinding names and roleRefs through one helper
(`fluid.clusterRBACName`, `templates/_helpers.tpl`). The PR body now states
plainly: "a scoped RBAC mitigation for #6134, not complete multi-release
support; other cluster-wide resources … may still conflict or be shared."

The branch was rebased onto `da020a3f1` and the harness extended additively
(`optin_prefix_test.go`, `scripts/envtest-install-upgrade.sh`). Production
chart code is still untouched. The round-1 test file stays; its G1 guard was
superseded (the contract "release-scoped by default" is intentionally gone),
its F1/F2 contract tests stay red — #6134's full contract remains unmet, which
the re-scoped PR now declares out of scope.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Integration (helm render) | `helm template` the chart: default-vs-base byte-compat, opt-in prefix disjointness, residual shared singletons | `go test ./charts/fluid/fluid/ -run 'TestClusterScoped' -v` (skips if no `helm`) |
| 2. Integration (envtest apiserver) | real `helm install` / `helm upgrade` against a throwaway kube-apiserver with an audit log (RBAC write ordering) | `bash docs/verification/multi-release-rbac/scripts/envtest-install-upgrade.sh` (needs envtest assets; `KUBEBUILDER_ASSETS` to override) |

Polarity map for round 2: contract tests `TestClusterScopedDefaultRenderMatchesMergeBase`
and `TestClusterScopedOptInPrefixDisjointAcrossReleases` are green on head and
red on base / on af73f5d respectively (bite-check evidence:
`results/bite-check-round2/`). `TestClusterScopedSharedSingletonsRemainAfterOptInPrefix`
is a **canary**: it PASSES while the declared limitation (shared CSIDriver /
MWC) persists and must be inverted when multi-release support is completed.

## Round-2 summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0-r2 | Default-values behavior is unchanged from base (compat) | 1 | **Confirmed** — head render is byte-identical to base `f432bdc`; default upgrade performs zero RBAC writes | `optin_prefix_test.go` green; `results/envtest/D-upgrade-rbac-write-order.txt` empty |
| G1-r2 | Opt-in prefix produces prefixed, resolvable, cross-release-disjoint cluster RBAC | 1 | **Confirmed** (green on head, red on base) | `results/bite-check-round2/on-base.txt` |
| F1-r2 (canary) | Even with distinct prefixes a second release still cannot be installed (shared singletons) — the PR's declared scope boundary | 1 + 2 | **Confirmed** — render: exactly `CSIDriver/fuse.csi.fluid.io` + `MutatingWebhookConfiguration/fluid-pod-admission-webhook` remain; install: `INSTALLATION FAILED ... CSIDriver "fuse.csi.fluid.io" ... cannot be imported into the current release` | `results/envtest/B-install2-with-prefix.log` |
| Upgrade ordering | Opt-in upgrade creates new (prefixed) RBAC before deleting old — "no temporary RBAC permission gap" | 2 | **Confirmed, observed** — audit log: all 22 creates (…49.98s) strictly precede all 22 deletes (…50.06s); upgrade `deployed`; 0 old fixed names left; new binding still binds `ServiceAccount/alluxioruntime-controller` in `fluid-system` | `results/envtest/C-upgrade-rbac-write-order.txt`, `C-upgrade-with-prefix.log`, `C-clusterroles-after.txt`, `C-binding-subjects.json` |

## Continuing after the next round

1. `bash docs/verification/multi-release-rbac/scripts/re-verify.sh` — grafts
   this harness onto the current PR head and runs the render layer
   (auto-resolves the head from `verify-manifest.json`'s `pr` URL).
2. `bash docs/verification/multi-release-rbac/scripts/envtest-install-upgrade.sh`
   — reruns the install/upgrade layer (scenarios D/A/B/C) against a fresh
   throwaway apiserver.
3. Interpretation: `TestClusterScopedSharedSingletonsRemainAfterOptInPrefix`
   failing with "no shared singletons left" means multi-release support
   actually landed — invert it into a contract test. The two round-1 F1/F2
   tests going green means #6134's full contract finally holds.
