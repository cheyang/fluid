# pr6271-install-doc-examples — verification of PR #6271 (docs install examples)

Reproducible evidence for the review of https://github.com/fluid-cloudnative/fluid/pull/6271
("docs: fix chart references in advanced installation examples").

| | |
|---|---|
| Reviewed head | `6e01dd6b1ead56e285f88c2163818ef8d4c7fcc7` |
| Base / merge-base | `dcb76167b0143a239addc6bed83ed50b45267631` |
| Harness branch | `verify/pr6271-install-doc-examples-codex` (production code untouched — the PR itself is docs-only) |
| Tooling | Helm v3.16.3 (author used 3.11.1; chart resolution semantics identical), published chart `fluid/fluid` 1.0.8 (latest; matches the author's claim) |

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| Integration (only layer) | The exact doc commands, executed from an empty directory with `helm template` (client-side render; no cluster, exactly as PR body §III prescribes), against the real published chart repo | `bash docs/verification/pr6271-install-doc-examples/scripts/run-doc-example-checks.sh` |

> Polarity: every check is a **contract** test. P0* contracts encode "the old examples fail
> the way the PR claims" (they quote the old commands verbatim, so they pass on any revision —
> they re-confirm the historical premise). F1*/F2 contracts encode "the fixed examples exist
> in the docs and render the requested values"; they FAIL on the base docs and PASS on the PR
> head. If the docs are restructured (neither old nor new example found) the check stays
> silent → `re-verify.sh` reports **Harness-update**, never a false pass.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "the original examples fail to resolve the chart or open the kubelet placeholder as a file" (PR body §III; verify condition: "From an empty directory, add the Fluid chart repository, then run the three examples with `helm template`") |
| Linked issue | none ("Does this pull request fix one issue? NONE") — claim taken from the PR body |
| Reported component | advanced installation examples in `docs/en/userguide/install.md` / `docs/zh/userguide/install.md` |
| Patched component | same two files |
| Component match | **Yes** |
| **Verdict** | **Confirmed** |
| Evidence | `results/harness-bites-base-run.log`: old ex.1/2 → `Error: non-absolute URLs should be in form of repo_name/path_to_chart, got: fluid` (rc=1); old ex.3 pasted literally → `bash: line 1: kubelet-root-dir: No such file or directory` (rc=1, the `<kubelet-root-dir>` placeholder is parsed by bash as input redirection — the "open the kubelet placeholder as a file" claim). Even with the placeholder removed, ex.3's chart ref `fluid.tgz` fails the same way as ex.1/2 (`got: fluid.tgz`). |

## Summary of results

Run on the PR head (`results/head-run.txt`); harness-bites run on the base (`results/harness-bites-base-run.txt`):

| ID | Claim | Layer | Base | Head | Evidence |
|----|-------|-------|------|------|----------|
| P0a | old example 1 (`helm install fluid --set runtime.mountRoot=... fluid`) fails: bare `fluid` is not a resolvable chart ref | integration | pass (fails as claimed) | pass | `Error: non-absolute URLs ... got: fluid` |
| P0b | old example 2 (`... csi.featureGates='FuseRecovery=true' fluid`) fails identically | integration | pass (fails as claimed) | pass | same error |
| P0c | old example 3 pasted literally dies in the shell on `<kubelet-root-dir>` before helm runs | integration | pass (fails as claimed) | pass | `kubelet-root-dir: No such file or directory` |
| F1a | new example 1 renders and injects the mount root | integration | **fail** (old text present) | **pass** | `--prune-path=/var/lib/docker/runtime-mnt` + env/mountPath/hostPath all rendered; default `--prune-path=/runtime-mnt` gone |
| F1b | new example 2 renders and enables the gate | integration | fail | pass | `--feature-gates=FuseRecovery=true` on the CSI plugin container |
| F1c | new example 3 (`KUBELET_ROOTDIR` variable snippet) renders custom kubelet paths | integration | fail | pass | `--kubelet-registration-path=/data/kubelet/csi-plugins/fuse.csi.fluid.io/csi.sock`, hostPath `path: "/data/kubelet"` |
| F2 | zh doc carries the same three fixed commands as the en doc | integration | fail | pass | all 6 canonical lines found verbatim in both docs |

## Per-finding detail

- **F1a** — mechanism: `runtime.mountRoot` is consumed in `charts/fluid/fluid/templates/csi/daemonset.yaml`
  (`--prune-path`, env, mountPath, hostPath). Observed on 1.0.8 render: `--prune-path=/var/lib/docker/runtime-mnt`.
- **F1b** — `csi.featureGates` default is `"FuseRecovery=false"` (values.yaml); the override renders
  `--feature-gates=FuseRecovery=true`. (The unrelated `--feature-gates=DataflowAffinity=false` line in the
  output is the fluidapp controller's own default, untouched.)
- **F1c** — `${KUBELET_ROOTDIR}` quoting is shell-safe; `certDir=${KUBELET_ROOTDIR}/pki` intentionally
  produces **no separate cert-dir volume** because the chart skips it when certDir is under rootDir
  ("Check subdirectory to avoid looping bind mounts", daemonset.yaml). The rootDir hostPath covers pki.

## Proposed fixes

None — no defect found in the PR. (The review raised one *pre-existing* nit about other stale
`fluid.tgz`/unpacked-directory references elsewhere in the same guide; out of this PR's scope.)

## Continuing after future revisions (possibly on another machine)

```bash
git fetch https://github.com/cheyang/fluid.git verify/pr6271-install-doc-examples-codex
git checkout verify/pr6271-install-doc-examples-codex   # or graft docs/verification/... onto any ref
bash docs/verification/pr6271-install-doc-examples/scripts/re-verify.sh   # auto-fetches current PR head
```

Prereqs: bash, git, helm 3, network to `https://fluid-cloudnative.github.io/charts`. No cluster.
Expected on a still-good head: every row `FIXED`. `HARNESS-UPDATE` = the doc text changed shape
(or the chart repo is unreachable) — adjust, don't conclude regression. Note the harness renders
the **latest published** chart; chart drift can flip F1* independently of the docs.

### Kickoff prompt for a fresh agent
```text
Re-verify the doc examples of https://github.com/fluid-cloudnative/fluid/pull/6271 using the harness on
branch verify/pr6271-install-doc-examples-codex (remote https://github.com/cheyang/fluid.git). Read
docs/verification/pr6271-install-doc-examples/README.md and run scripts/re-verify.sh. All checks are
contract tests; expect all FIXED while the docs still carry the fluid/fluid examples and the chart keys
runtime.mountRoot / csi.featureGates / csi.kubelet.rootDir|certDir exist. Report an observed-vs-expected table.
```
