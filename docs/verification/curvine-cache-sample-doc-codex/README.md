# curvine-cache-sample-doc (PR #6252) — bug verification

Reproducible evidence for the review of
https://github.com/fluid-cloudnative/fluid/pull/6252
("docs(cacheruntime): fix heredocs and image tag in the Curvine sample").

Layers run against the **code under review** (head `41f10618939fae9c1373a79fee7245e7bfdc7521`,
base `ff99142f9ec5c32a0c9166de023cb8936f0e5ae5`):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | every `cat<<...` heredoc in the en+zh sample docs, executed as a reader would (`env -i bash`), plus doc-wide image-tag grep | `bash scripts/run_doc_contracts.sh <ref>` |
| 2. Integration | Docker Hub v2 API for the images the sample references | `bash scripts/check_external_images.sh` |
| 3. Live | intentionally skipped (docs-only PR; debate-pipeline deviation) | — |

> Test polarity: all tests are **contract** tests (assert intended behavior): they FAIL on the
> base doc and PASS on the PR head. There are no bug-canaries in this harness.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | "The CacheRuntimeClass (step 4) and the two read jobs (steps 9 and 10) are written with an unquoted `cat<<EOF`, so the local shell expands `${CURVINE_HOME}`, `$FLUID_DATALOAD_DATA_PATH` and `$(cat /data/minio/bar)` before the file is written. `${CURVINE_HOME}/conf` ends up as `/conf`" and "`curvine/curvine:0.3.6-alpha` does not exist on Docker Hub. The tag is `v0.3.6-alpha`" (PR body) |
| Linked issue | none (`NONE` in PR body); premise taken from the PR body itself |
| Reported component | `docs/{en,zh}/samples/cacheruntime/curvine_cache_runtime.md` |
| Patched component | same two files |
| Component match | Yes |
| **Verdict** | **Confirmed** on both claims |
| Evidence | base run generates `CURVINE_CONF_DIR="/conf"`, `IFS=: read -ra paths <<< ""`, `target_path=""`, `- set -ex; test -n ""` and executes the reader's local `cat /data/minio/bar` (results/base-contracts.jsonl, results/base-artifacts/); Docker Hub: `0.3.6-alpha` → HTTP 404, `v0.3.6-alpha` → HTTP 200 (results/integration-images.jsonl) |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0a | unquoted heredocs mangle the sample on base | 1 | **Confirmed** | base: `DocContract{EN,ZH}/{CacheRuntimeClassVars,ReadJobCmdSubst,ReadRefJobCmdSubst}` fail; generated `CURVINE_CONF_DIR="/conf"` (results/base-artifacts/EN/cacheruntimeclass.yaml), local `cat` runs at doc-follow time (results/base-artifacts/EN/07_read_job.yaml.sh.stderr) |
| P0b | `curvine/curvine:0.3.6-alpha` does not exist; `v0.3.6-alpha` does | 2 | **Confirmed** | 404 vs 200 on Docker Hub v2 API (results/integration-images.jsonl); repo e2e `test/gha-e2e/curvine/cacheruntimeclass.yaml` uses `v0.3.6-alpha` |
| C1 | head fixes the heredocs (steps 4/9/10 quoted) | 1 | **Pass** | all 6 CacheRuntimeClass/ReadJob contracts green on head (results/head-contracts.jsonl) |
| C2/C3 | head references only the v-prefixed tag; all sample images resolve | 1+2 | **Pass** | `ImageTagVPrefix` green on head; `curlimages/curl:8.22.0` and `busybox:latest` also resolve |
| G1 | the deliberately-unquoted bucket-job heredoc (step 2) is left intact and still yields `$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD` | 1 | **Pass** | `BucketJobEscapeContract` + `GeneratedYAMLValid` green on base and head |

## Per-finding detail

- **P0a mechanism**: in an unquoted heredoc the local shell performs parameter expansion and
  command substitution while writing the file. Step 4's `CacheRuntimeClass` and the step 9/10
  read jobs embed *pod-side* shell (`${CURVINE_HOME}/conf`, `$FLUID_DATALOAD_DATA_PATH`,
  `$(cat /data/minio/bar)`, preStop `${CURVINE_TARGET_PATH:-...}`), so following the doc
  verbatim produced manifests pointing at `/conf` and a read job that can never pass
  (`test -n ""`). Quoting the opener (`cat<<'EOF'`) writes the body literally. Verified by
  extracting all 10 heredoc blocks and executing them under `env -i` (results/base-artifacts
  vs results/head-artifacts).
- **P0b mechanism**: Docker Hub tag list for `curvine/curvine` contains only `v`-prefixed
  releases (checked the full tag list 2026-11-16); the unprefixed `0.3.6-alpha` 404s, so the
  sample's three pods would be stuck at image pull. The repo's own e2e manifest already uses
  `v0.3.6-alpha`, corroborating the fix.
- **G1 (why step 2 must stay unquoted)**: `minio_create_bucket.yaml` needs the local shell to
  collapse `\$` to `$` so the *pod* shell can expand the env vars. Mutation check: quoting that
  opener leaves `\$MINIO_ROOT_USER` literal in the file, which is an invalid YAML double-quote
  escape — `yaml.safe_load_all` raises `found unknown escape character '$'`
  (results/harness-bites-mutation.txt). This is also the harness-bites proof: the guard
  contracts go red under that wrong "fix".

## Harness-bites check

- base → head flip: C1/C2 contracts are red on base, green on head (the exact change under review).
- mutation: `sed` step-2's opener to `<<'EOF'` ⇒ `BucketJobEscapeContract`/`GeneratedYAMLValid`
  red (results/harness-bites-mutation.txt). Harness is sensitive in both directions.
- bucket-job and YAML-validity contracts are green on *both* base and head, so the suite is not
  globally red for unrelated reasons.

## Proposed fixes (NOT applied to production here)

None — the PR itself is the fix and is verified correct. Optional, non-blocking: consider
quoting the remaining `$`-free heredocs too for uniformity (taste; step 2 must stay unquoted).

## Continuing after the fix (possibly on another machine)

The harness is on branch `verify/curvine-cache-sample-doc-codex` (production code untouched).

```bash
git fetch https://github.com/cheyang/fluid.git verify/curvine-cache-sample-doc-codex
git checkout verify/curvine-cache-sample-doc-codex   # or graft: git checkout <ref> && git checkout verify/... -- docs/verification
bash docs/verification/curvine-cache-sample-doc-codex/scripts/re-verify.sh   # no args: fetches current PR #6252 head
```

- L1 prerequisites: `bash`, `python3` + `pyyaml`, `git`. L2 additionally needs network to
  hub.docker.com. No cluster, no KUBECONFIG (live layer intentionally skipped).
- `re-verify.sh` prints per-finding Fixed / Still-broken / Harness-update and exits 0 iff all
  fixed; it resolves the current PR head from `verify-manifest.json`'s `pr` field and reports
  the incremental review delta from `.last-reviewed` (currently
  `41f10618939fae9c1373a79fee7245e7bfdc7521`).
- If a future revision adds/removes a heredoc block, the extractor's block-count check emits a
  build-fail marker → re-verify reports `HARNESS-UPDATE` (adjust `EXPECTED_BLOCKS` / contracts),
  never a silent pass.

Copy-paste kickoff prompt for a fresh agent:
> Continue verification of fluid-cloudnative/fluid PR #6252. Check out
> https://github.com/cheyang/fluid.git branch verify/curvine-cache-sample-doc-codex, read
> docs/verification/curvine-cache-sample-doc-codex/README.md, run scripts/re-verify.sh, then
> review the delta in .last-reviewed..<current PR head>.
