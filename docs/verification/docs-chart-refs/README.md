# docs-chart-refs — verification harness for PR #6271

Reproducible evidence for the review of
[fluid-cloudnative/fluid#6271](https://github.com/fluid-cloudnative/fluid/pull/6271)
("docs: fix chart references in advanced installation examples").

**Production code is untouched** — this PR is documentation-only, so the "code under
review" is `docs/en/userguide/install.md` and `docs/zh/userguide/install.md`. The harness
branch adds only this `docs/verification/` tree on top of the PR head.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Render ("unit") | the documented `helm install` commands, executed verbatim via `helm template` against the chart repo the guide itself registers (`https://fluid-cloudnative.github.io/charts`, chart 1.0.8 at review time) | `bash docs/verification/docs-chart-refs/scripts/run_harness.sh head` |
| 1b. Premise / bites | the OLD (merge-base) command forms, asserted to FAIL | `bash docs/verification/docs-chart-refs/scripts/run_harness.sh base` |
| 3. Live | not applicable — no cluster, no binary; rendering is the complete user-visible contract for a chart-install doc example | — |

> Polarity: T1/T2/T3/T3b and F1 are **contract** tests (assert intended behavior; red =
> broken). The P0-* premise tests assert that the pre-fix commands fail — running them
> doubles as the harness-bites check (the same render assertions go red on the old commands).

## Problem premise (P0) — Confirmed

| | |
|---|---|
| Claimed symptom | PR body: "the original examples fail to resolve the chart or open the kubelet placeholder as a file" when following the guide from an empty directory with only the `fluid` helm repo added |
| Linked issue | none ("Does this pull request fix one issue? NONE") |
| Reported component | Advanced Configuration examples 1–3 in both install guides |
| Patched component | exactly those three examples in both guides |
| Component match | Yes |
| **Verdict** | **Confirmed** (reproduced on the merge-base command forms) |
| Evidence | `helm template fluid fluid --set runtime.mountRoot=…` → `Error: non-absolute URLs should be in form of repo_name/path_to_chart, got: fluid`; `helm template fluid fluid.tgz` → same class of error; `--set csi.kubelet.rootDir=<kubelet-root-dir>` → `bash: kubelet-root-dir: No such file or directory` (the `<…>` placeholder is parsed as an input redirect). See `results/harness-base.log`. |

Docs-only PR, so the "skip premise for docs" carve-out applies, but the premise was
cheap to settle positively and the PR makes factual claims about command behavior, so it
was checked anyway.

## Summary of results (PR head 6e01dd6b)

| ID | Claim | Verdict | Evidence |
|----|-------|---------|----------|
| T1 | `helm install fluid fluid/fluid --set runtime.mountRoot=/var/lib/docker/runtime-mnt` renders; the custom mount root appears in the CSI daemonset (prune-path, env, hostPath) and controller env | Confirmed (pass) | 11 occurrences of `/var/lib/docker/runtime-mnt`; `results/harness-head.log` |
| T2 | `helm install fluid fluid/fluid --set csi.featureGates='FuseRecovery=true'` renders; `--feature-gates=FuseRecovery=true` reaches the CSI plugins container | Confirmed (pass) | `charts/fluid/fluid/templates/csi/daemonset.yaml:75-76`; render grep |
| T3 | The KUBELET_ROOTDIR shell-var form renders; `--csi-address=/data/kubelet/csi-plugins/fuse.csi.fluid.io/csi.sock`, `--kubelet-registration-path=…`, hostPath `path: /data/kubelet/plugins_registry` all follow the custom root | Confirmed (pass) | render grep |
| T3b | `csi.kubelet.certDir` still propagates when it matters (a certDir outside rootDir gets its own hostPath mount) | Confirmed (pass) | `path: "/custom/pki"` renders for certDir=/custom/pki |
| F1 | No same-class stale chart references remain in the two guides | **Not fixed (fail)** | `fluid.tgz` remains at `docs/en/userguide/install.md:50,82` and `docs/zh/userguide/install.md:50`; unresolvable `fluid fluid/` at `docs/zh/userguide/install.md:81` |

## Per-finding detail

### F1 — incomplete sweep of stale chart references (minor, pre-existing)
The PR fixes the three Advanced Configuration examples, but the same two documents still
contain the same class of broken chart reference for the repo-based flow:

- `docs/en/userguide/install.md:50` and `:82` — "For Kubernetes version lower than
  v1.17 … `helm install --set runtime.criticalFusePod=false fluid fluid.tgz`"
- `docs/zh/userguide/install.md:50` — same `fluid.tgz` note
- `docs/zh/userguide/install.md:81` — `helm upgrade --set runtime.criticalFusePod=false fluid fluid/`
  (trailing-slash ref, also unresolvable)
- The explanatory notes at `docs/en/userguide/install.md:52` ("the directory just
  unpacked") and `docs/zh/userguide/install.md:52` ("第二个`fluid.tgz`指定了helm chart
  所在路径") describe the obsolete tgz-based flow while the actual command above them
  uses `fluid/fluid`.

All fail with the same Helm error the PR was filed to fix. Not blocking: they pre-date
this PR and sit outside its stated scope, but they undermine the PR's own goal (guide
commands that work in the repo-based flow) and cost nothing to sweep in the same patch.

### Observation (nit, not a finding): certDir subsumption
In example 3, `csi.kubelet.certDir=${KUBELET_ROOTDIR}/pki` nests under `rootDir`, and
`charts/fluid/fluid/templates/csi/daemonset.yaml:124` skips the certDir-specific mounts
when `hasPrefix $kubeletRootDir .Values.csi.kubelet.certDir` — so the explicit
`--set csi.kubelet.certDir` has no visible effect on the rendered manifest in exactly the
example the docs give (T3b proves a certDir outside rootDir does render). Harmless — it
mirrors the chart's default rootDir/certDir pairing — but a one-line note ("certDir only
needs overriding when it is outside rootDir") would prevent reader confusion.

## Proposed fixes (NOT applied here)
- **F1**: sweep the remaining `fluid.tgz` / `fluid fluid/` refs in both guides to
  `fluid fluid/fluid --set runtime.criticalFusePod=false` (or drop the v1.17 notes if
  that Kubernetes floor is no longer supported), and update the two stale explanatory
  notes. Optional: add the certDir subsumption note to example 3.

## Continuing after the fix

1. The harness lives on branch `verify/docs-chart-refs-claude` (only this
   `docs/verification/` tree added; the guides are the PR's own content).
2. Prereqs: `git`, `helm >= 3`, network access to `fluid-cloudnative.github.io`
   (the `fluid` helm repo is added idempotently by the harness). No cluster, no go.
3. Re-run:
   ```bash
   bash docs/verification/docs-chart-refs/scripts/run_harness.sh head   # T*/F1 contract tests
   bash docs/verification/docs-chart-refs/scripts/run_harness.sh base   # premise / bites
   bash docs/verification/docs-chart-refs/scripts/re-verify.sh          # grafts onto current PR head
   ```
4. Polarity: all tests are contracts. T1–T3b should stay green. **F1 flips to pass once
   the stale references are swept** — that is the only currently-red test.
5. Harness-bites: `base` mode runs the merge-base command forms through the same
   assertions and shows them failing (red), confirming the tests exercise the real path.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/docs-chart-refs-claude (fork cheyang/fluid).
Background: review of fluid-cloudnative/fluid#6271 (docs-only: fix chart refs in the
advanced install examples) produced one open finding F1 (stale fluid.tgz / fluid fluid/
chart references remain in docs/{en,zh}/userguide/install.md) plus contract tests T1-T3b
proving the corrected examples render. Read docs/verification/docs-chart-refs/README.md
("Continuing after the fix"), re-run scripts/run_harness.sh in head and base modes and
scripts/re-verify.sh against the current PR head, and report the observed-vs-expected
table. F1 is fixed only when F1-no-stale-chart-refs flips to pass. No cluster needed.
```
