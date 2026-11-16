# vllm-curvine-docs-claude — doc-claims verification

Reproducible evidence for the findings raised while reviewing
[PR #6253](https://github.com/fluid-cloudnative/fluid/pull/6253)
(docs: vLLM + Curvine sample, en/zh + TOC).

The PR is **docs-only**, so the harness is a claims checker, not a go test suite:
every code-checkable assertion in the new sample docs is checked against the Fluid
codebase at the PR head, the referenced `curvine_cache_runtime.md` sample, and
(with network) the external images / vLLM release the doc names.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (offline) | label selector, links, en/zh parity, heredoc safety, cross-doc refs, YAML-vs-CRD fields, TOC entries, DataLoad phase value | `python3 scripts/check_doc_claims.py --repo-root . --ref WORKTREE --offline` |
| 2. Integration (network) | Docker Hub pullability of every image the walk-through deploys; vLLM v0.33.0 entrypoint + `VLLM_PORT` guard | `python3 scripts/check_doc_claims.py --repo-root . --ref WORKTREE` |
| 3. Live | real GPU cluster run of the sample (author already did this; not re-run here — no GPU/KUBECONFIG) | see `verify-manifest.json:liveNote` |

Production code is **untouched** — the branch only adds this directory.
`scripts/re-verify.sh` re-runs everything against the *current* PR head (fetched via
`manifest.pr`) and prints per-finding verdicts.

> Test polarity: all checks here are **contract** tests — they assert the intended
> correct state, so they FAIL on a regression and PASS when fixed. The one currently
> red check (`C6_walkthrough_images_pullable`) encodes finding F1.

## Problem premise (P0)

Docs-only PR → premise carve-out applies, validated informally:

| | |
|---|---|
| Claimed symptom | "The existing CacheRuntime samples only read and write small files from busybox" — no realistic workload sample |
| Linked issue | none (no `fixes #N`); PR body states the problem |
| Reported component | docs samples for CacheRuntime |
| Patched component | `docs/{en,zh}/samples/cacheruntime/vllm_with_curvine.md` + TOC |
| Component match | Yes |
| **Verdict** | **Confirmed** — `git grep -il vllm <base> -- docs/` has no hits; the curvine sample's workload is a busybox job writing a 12-byte file |
| Evidence | `results/base-ref.out` / `results/unit.out` (`P0_no_prior_vllm_sample`) |

## Summary of results

### Round 2 (2026-11-17) — head `46cd54726b3877b051cfc19d2d485098af3f34fe`

Delta since round 1 (`33c1eef..46cd547`, one commit, "require kubectl 1.23 and harden the
MinIO port-forward"): the author fixed both round-1 nits — Prerequisites now states
`kubectl` 1.23+ for `--for=jsonpath` (F4), and the Step-2 port-forward now saves the PID
(`PF_PID=$!`, `kill "$PF_PID"`) and bounds the readiness wait with `timeout 60` (F3),
mirrored in zh. No manifest or delegation changes; C3/C7 re-run green.

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| D1 | every code-checkable doc claim matches the repo (8 checks) | 1 | all pass at new head | `results/reverify/unit.out` |
| F1 | delegated setup still broken on master: image tag 404 + unquoted heredoc; #6252 still OPEN | 2 | **Still-broken** | `results/reverify/integration.out` (`curvine/curvine:0.3.6-alpha -> 404`) + `results/f1-heredoc-repro.out` (CURVINE_CONF_DIR mangled to `/conf`) |
| F2 | vLLM v0.33.0 entrypoint is `vllm serve`; `VLLM_PORT` URI guard exists | 2 | pass | `results/reverify/integration.out` (`C9 … pass`) |

F1 evidence re-confirmed at the new head, both mechanisms:
1. `scripts/f1-heredoc-repro.sh 46cd547` → unquoted `cat<<EOF` in the referenced
   `curvine_cache_runtime.md` expands `${CURVINE_HOME}` away with the variable unset
   (`CURVINE_CONF_DIR="/conf"`, `CURVINE_DATA_DIR="/data"`, `CURVINE_LOG_DIR="/logs"`).
   Harness-bites: applying #6252's quoted-delimiter fix flips it green; reverting flips it
   red again; production diff left clean.
2. `C6_walkthrough_images_pullable` → `curvine/curvine:0.3.6-alpha` 404 (v-prefixed tag is 200).
3. `gh pr view 6252` → state `OPEN` (head `41f1061`), not merged; the referenced doc at
   both this PR head and current `origin/master` still carries both defects.

Round-1 nits fixed by the delta (status `fixed`): F3 (port-forward PID + bounded wait,
`vllm_with_curvine.md:53-69`), F4 (kubectl 1.23 in Prerequisites, line 15).

### Round 1 (2026-11-16) — head `33c1eef59404f55d4f582a0cec84af62fb2e1d79`

Observed at head `33c1eef59404f55d4f582a0cec84af62fb2e1d79` (base `ff99142f`):

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| D1 | every code-checkable doc claim matches the repo (8 checks) | 1 | Confirmed (all pass) | `results/unit.out` |
| F1 | sample deploys a non-pullable image until #6252 merges | 2 | Confirmed (red) | `results/integration.out`: `curvine/curvine:0.3.6-alpha -> 404` |
| F2 | vLLM v0.33.0 entrypoint is `vllm serve`; `VLLM_PORT` URI guard exists | 2 | Confirmed (pass) | `results/integration.out`: `C9_vllm_entrypoint_and_port_env pass` |

Checks in detail (unit layer, all green at head):

- `C1_label_selector_valid` — the doc's `kubectl get pod -l cacheruntime.fluid.io/name=…`
  matches `LabelCacheRuntimeName` (`pkg/common/label.go:154`), which
  `pkg/ddc/cache/component/component_manager.go:78` really sets on component pods.
  (The referenced base sample still uses a nonexistent `cacheruntime.fluid.io/runtime-name`
  label — this PR got it right.)
- `C2_doc_links_resolve` — all relative links in en+zh resolve at the ref.
- `C3_en_zh_code_parity` — code blocks identical modulo translated comments/prompts.
- `C4_heredoc_no_shell_expansion` — no `$` inside any unquoted `cat<<EOF` body
  (this is exactly the class of bug #6252 fixes in the other sample).
- `C5_cross_doc_references` — Step 1 relies on the other doc's Step 1/2/4: names
  (`curvine-secret`, `access-key`/`secret-key`, bucket `test`, class `curvine-demo`,
  `app=minio`, `minio.yaml`, `minio_create_bucket.yaml`) all present in both languages;
  the doc's CacheRuntime YAML differs from the base sample **only** by
  `quota: 1Gi → 4Gi` (byte-level diff), and the Dataset YAML is identical —
  the doc's "only one thing changes" claim is true.
- `C7_yaml_fields_in_crd` — dataset/cacheruntime/dataload YAML fields all exist in
  the chart CRD schemas; the vLLM Deployment/Service are structurally consistent
  (selector == template labels, PVC claimName == Dataset name).
- `C8_toc_entries` — TOC links + titles match the H1s in both languages.
- `C10_dataload_phase_value` — `PhaseComplete Phase = "Complete"` exists
  (`pkg/common/types.go:123`), so the doc's `kubectl wait --for=jsonpath='{.status.phase}'=Complete` waits on a real phase.

## Per-finding detail

### F1 (merge-order dependency on #6252) — currently red

- Mechanism: Step 1 of the new sample delegates MinIO/bucket/CacheRuntimeClass setup to
  `curvine_cache_runtime.md`. Its `cacheruntimeclass.yaml` heredoc deploys
  `curvine/curvine:0.3.6-alpha` (no `v`). The Docker registry tag list for
  `curvine/curvine` contains **only** `v`-prefixed tags (`v0.1.0 … v0.6.0-alpha`);
  `0.3.6-alpha` → HTTP 404.
- Consequence: on a fresh cluster, following this sample before #6252 lands means
  the Curvine master/worker/client pods sit in `ImagePullBackOff` and the whole
  walk-through stalls at Step 3 (`kubectl get dataset` never reaches `Bound`).
- The defect lives in the *referenced* doc (pre-existing, not introduced by this PR),
  and open PR **#6252** fixes exactly this (`0.3.6-alpha` → `v0.3.6-alpha`, + heredoc
  quoting). The PR body already declares "this depends on #6252".
- Proposed handling: **merge #6252 first** (or fold its two-doc fix in ahead of this
  PR). Not a content defect of this PR; recorded as a merge-order condition.
- Harness-bites: with #6252's tag fix applied locally, `C6` flips green
  (`results/fix-simulation.out`), then the fix was reverted (production diff clean).

### D1 / F2 — verified claims (regression suite)

Beyond the unit checks: `vllm/vllm-openai:v0.33.0`, `curvine/curvine:v0.3.6-alpha`
and `curlimages/curl:8.22.0` all exist on Docker Hub; vLLM v0.33.0's
`docker/Dockerfile` has `ENTRYPOINT ["vllm", "serve"]` (positional model arg valid);
`vllm/envs.py` contains the exact `VLLM_PORT '<x>' appears to be a URI … Kubernetes
service discovery` guard, so the doc's `enableServiceLinks: false` / `vllm-svc`
mitigation matches real vLLM behavior. The NVIDIA time-slicing note
(`renameByDefault: true` → `nvidia.com/gpu.shared`) matches the k8s-device-plugin README.

## Live run notes

Not run (no GPU / no KUBECONFIG in this review environment). The author reports a
full run on a kind cluster (Dataset `Bound`, DataLoad `Complete` with 953.28MiB
cached, vLLM answering `/v1/chat/completions`, both replicas after scale-out);
the doc's illustrative outputs are consistent with what the Fluid code would print
(PVC `100Pi`/`fluid`/RWX, `4.00GiB`/`0.00B` formatting from `pkg/utils/byte_size.go:57`,
client pods hidden until the fuse node label appears).

## Proposed fixes (NOT applied to production here)

- F1: none in this PR — merge #6252 first (it fixes the image tag in the referenced doc).
- Optional follow-up: the referenced sample's own stale bits (nonexistent pod label
  `cacheruntime.fluid.io/runtime-name`, old `1Gi`/`12B`/`100%` output formatting,
  condition-style DataLoad wait) could be refreshed in a later docs PR; out of scope here.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/vllm-curvine-docs-claude` (production code
untouched), so it grafts onto whatever the fixed code is.

1. Checkout this branch, then run `bash docs/verification/vllm-curvine-docs-claude/scripts/re-verify.sh`
   — no arguments: it fetches the current PR head via `manifest.pr`, grafts the
   harness, runs both layers, and prints the verdict table. Exit 0 iff all findings fixed.
2. Prereqs: git, jq, python3 (pyyaml). Integration layer needs network.
3. Read the polarity table above: all checks are contracts — green means correct;
   the only currently-red one (`C6`) is expected to go green once #6252 merges.
4. Harness-bites: `python3 scripts/check_doc_claims.py --selftest` injects label/link/heredoc
   mutations and requires the corresponding checks to fail (`results/selftest.out`).

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/vllm-curvine-docs-claude (fork cheyang/fluid).
Background: review of fluid PR #6253 (vLLM+Curvine docs sample) found one open item F1 —
the sample is unrunnable until PR #6252 (image tag fix in curvine_cache_runtime.md) merges.
Read docs/verification/vllm-curvine-docs-claude/README.md ("Continuing after the fix") and
follow it: run scripts/re-verify.sh (fetches the current PR head itself), expect D1 and F2
green and F1 green once #6252 is merged; if F1 is still red, the image tag is still broken.
All checks are contract tests. Do not modify production docs/code from the harness branch.
```
