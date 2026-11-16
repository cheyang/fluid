# Verification: PR #6256 — release-scoped cluster RBAC names (Reviewer B / Codex)

- PR: https://github.com/fluid-cloudnative/fluid/pull/6256
- Base (merge-base): `f432bdc87ac35941763b74e8e87683c7a84de5d9`
- Head reviewed: `af73f5d6a8f7b918f8b1c07bd47cbdd6cebef563`
- Mode: initial. Production code untouched — harness is additive.

## Premise (P0) — CONFIRMED

Issue #6134: a second Fluid helm release on one cluster fails with
`exists and cannot be imported into the current release: invalid ownership metadata`.
Reproduced on the **base** chart against a throwaway envtest apiserver (helm 3.16.3,
k8s 1.36.2 apiserver):

- exact repro (`helm install fluid-1 ... -n test`): fails on namespaced
  `ServiceAccount "alluxioruntime-controller"` (chart default `.Values.namespace=fluid-system`).
- with per-release isolation (`--set namespace=test`): fails on exactly the reported
  `ClusterRole "alluxioruntime-controller"`.
  Evidence: `results/p0-install2.log`, `results/p0b-install2.log`.

## Findings

| ID | Verdict | Claim | Evidence |
|----|---------|-------|--------|
| F1 | **Confirmed (blocker)** | The PR does not make a second release installable: on the head chart the second install still fails — with default values on the namespaced SA collision, and with `--set namespace=test` on `CSIDriver "fuse.csi.fluid.io"` (fixed name, not gated by `csi.enabled`; name hard-coded in `pkg/common/volume.go:32` / `pkg/csi/plugins/driver.go:37`). `MutatingWebhookConfiguration fluid-pod-admission-webhook` (fixed name, hard-coded in `pkg/common/webhook.go:25`, looked up by `pkg/ctrl/watch/manager.go:234`) is the next collision. So `Fixes #6134` does not hold; merging auto-closes a live bug. | `results/f1a-install2.log`, `results/f1b-install2.log` |
| F1c | control | With only `templates/csi/driver.yaml` + `templates/webhook/webhookconfiguration.yaml` removed, two releases coexist with distinct release-scoped ClusterRoles (`fluid-*` and `fluid-1-*`) — the RBAC rename itself is correct and complete. | `results/f1c-install2.log` |
| F2 | Confirmed (minor, disclosed) | Upgrade base→head deletes old-name ClusterRole(Binding)s and creates new-name ones (brief permission-loss window); new bindings keep the same SA subjects. Upgrade itself succeeds. Note: the repo has a backward-compat e2e workflow that would exercise this path, but the PR rollup at review time showed only DCO + SonarCloud (no e2e ran); severity stays minor because the window is transient and controllers retry. | `results/f2-upgrade.log`, `results/f2-binding-subjects.txt` |
| F3 | minor | PR's tests string-match template text and re-implement sprig funcs in Go; no `helm template` render test; glob covers only `templates/role/*` + `templates/upgrade/*`. Unit layer bites-check: 38 failures on base chart, pass on head. | `results/unit-head.log`, `results/unit-base-bites.log` |

## How to run

Prereqs: `helm`, `kubectl`, `go`, envtest assets (`KUBEBUILDER_ASSETS` or
`~/.local/share/kubebuilder-envtest/k8s/*-linux-amd64` auto-detected).

```bash
# full matrix (unit + integration) against the current PR head:
bash docs/verification/rbac-release-scoped-codex/scripts/re-verify.sh
# or against an explicit ref:
bash docs/verification/rbac-release-scoped-codex/scripts/scenario.sh --head-ref <sha>
```

No live cluster / KUBECONFIG needed; the integration layer uses a local envtest
control plane.

## Interpreting results after a fix

- `F1a`/`F1b` are **contract** checks: they currently fail the install (expected).
  F1 is fixed only when a second `helm install` succeeds with default values
  (or with only a per-release `--set namespace=...`).
- `P0`/`P0b` must keep failing on the **base** chart; if they stop failing the
  harness no longer bites (harness-update).
- `F2.*` must keep passing on upgrades.

## Handoff

All state is on this branch: `verify-manifest.json`, `.last-reviewed`,
`scripts/`, `results/`. A fresh machine can resume with:
`git fetch <fork> verify/rbac-release-scoped-codex && git checkout verify/rbac-release-scoped-codex && bash docs/verification/rbac-release-scoped-codex/scripts/re-verify.sh`.
