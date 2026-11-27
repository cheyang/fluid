# docs-en-samples — verification of PR fluid-cloudnative/fluid#6296

Docs-only PR: adds English translations of `docs/zh/samples/other_volumes.md` and
`docs/zh/samples/hdfs_configuration.md`, and fixes two defects in the Chinese
other-volumes page (tab-indented `volumeMounts` in the `dataset.yaml` heredoc; a
`kubectl get secret fluid-secret -oy`/`aml` command split over two lines).

Layers run against the code under review (`1adbd79a1447236efa2b107d186fd4fbd857250b`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (deterministic) | YAML validity of embedded manifests, command-line sanity, en/zh code-block parity, relative-link resolution, documented-field existence | `python3 docs/verification/docs-en-samples/scripts/check_docs.py --repo . --ref <sha>` |
| 2. Integration | n/a — no controller/CRD/runtime code changed | — |
| 3. Live | n/a — docs-only | — |

> All checks are **contract** polarity: they FAIL on the base branch (defects present)
> and PASS on the PR head. One informational canary (N1) is noted below.

## Problem premise (P0)

Docs-only PR → the carve-out applies, but the PR body itself states the defects, so the
premise was still reproduced against the **base** branch `b8ff60e` (no patch).

| | |
|---|---|
| Claimed symptom | "`spec.master.volumeMounts` was indented with a tab. YAML forbids tabs for indentation, so `kubectl create -f dataset.yaml` failed on that manifest." + "`kubectl get secret fluid-secret -oyaml` was split over two lines (`-oy` / `aml`)." |
| Linked issue | none (PR states the problem itself; "NONE" for issue) |
| Reported component | `docs/zh/samples/other_volumes.md` sample manifests |
| Patched component | same file (plus the new en translations) |
| Component match | Yes |
| **Verdict** | **Confirmed** — both defects reproduce on base |
| Evidence | `results/base-premise.out`: `YAML parse error: while scanning for the next token` on the dataset.yaml heredoc (base line 57 is `  \tvolumeMounts:`), plus `-oy`/`aml` split-command failure |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| D1 | dataset.yaml heredoc parses as valid YAML in both languages after the fix | 1 | Confirmed (fixed) | `results/head.out` PASS vs `results/base-premise.out` FAIL |
| D2 | `kubectl get secret fluid-secret -oyaml` is a single line | 1 | Confirmed (fixed) | same runs |
| D3 | en fenced code blocks are byte-identical to the (fixed) zh blocks | 1 | Confirmed | parity check green at head; bites check shows it catches a seeded diff |
| D4 | relative md links in new en pages resolve (`../userguide/install.md`) | 1 | Confirmed | `results/head.out` |
| D5 | documented fields exist in code: `hadoopConfig` (alluxioruntime_types.go:226), master `volumeMounts` (:87), `volumes` (:230), `/hdfs-config` = `hadoopConfMountPath` (pkg/ddc/alluxio/const.go:52) | 1 | Confirmed | symbol checks green |
| N1 | `runtime.yaml` snippet in hdfs_configuration (en+zh) uses bare `...` lines as "fields omitted" placeholders → not paste-able YAML as-is | 1 | Confirmed (informational) | INFO line in `results/head.out`; inherited verbatim from zh, not a regression |

## Harness-bites check

`results/harness-bites.out`: both defects were temporarily reintroduced on a throwaway
commit (tab + split command). The harness went red on all three relevant checks — YAML
parse, single-line command, and en/zh parity (the parity diff pinpoints the exact tab
line) — then the throwaway commit was reverted; the production diff is empty again.

## Per-finding notes

- **D1/D2** — mechanism: PyYAML rejects tab indentation ("while scanning for the next
  token"); `kubectl create -f` would likewise reject the manifest. The PR replaces the
  tab with 4 spaces and joins the command line. Observed: base FAIL → head PASS.
- **D3** — the en pages reproduce every fenced code block byte-for-byte (only prose and
  headings differ). This protects the translation from silent drift in the manifests.
- **D5** — `spec.hadoopConfig`, `spec.volumes`, `spec.master.volumeMounts` all exist in
  `api/v1alpha1/alluxioruntime_types.go`; the `/hdfs-config` mount path matches
  `hadoopConfMountPath` in `pkg/ddc/alluxio/const.go`. The documented behavior matches
  the CRD schema.
- **N1** — pre-existing in the zh page; the `runtime.yaml` snippet cannot be pasted
  verbatim (`...` is the YAML document-end marker and is invalid inside a mapping).
  Suggestion (non-blocking): use `# ...` comment placeholders instead.

## TOC note

`other_volumes` / `hdfs_configuration` are not listed in `docs/zh/TOC.md`, and the PR
accordingly did not add the en pages to `docs/en/TOC.md`. That is self-consistent;
listed-or-not is a maintainer taste call (the en TOC does list most other samples).

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/docs-en-samples-claude` (production code untouched).

```bash
git fetch https://github.com/cheyang/fluid.git verify/docs-en-samples-claude
git checkout <fixed-branch>
git checkout FETCH_HEAD -- docs/verification/docs-en-samples
bash docs/verification/docs-en-samples/scripts/re-verify.sh          # current PR head
bash docs/verification/docs-en-samples/scripts/re-verify.sh <sha>    # explicit ref
```

Polarity: every check is a contract test — green means the docs are consistent; red at
head means either a defect regressed or the en/zh code blocks drifted. The single INFO
canary (N1) disappears if the `...` placeholder snippet is ever made valid YAML.

Copy-paste kickoff prompt for a fresh agent:

> You are re-verifying PR fluid-cloudnative/fluid#6296 (docs-only). Checkout the fixed
> branch, graft docs/verification/docs-en-samples from verify/docs-en-samples-claude,
> run scripts/re-verify.sh, and report per-finding Fixed / Still-broken / Harness-update.
> All checks are contract polarity; N1 is an INFO canary. Do not touch production docs.
